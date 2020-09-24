package vsp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"decred.org/dcrwallet/wallet"
	"decred.org/dcrwallet/wallet/txauthor"
	"decred.org/dcrwallet/wallet/txsizes"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

func (v *VSP) CreateFeeTx(ctx context.Context, ticketHash chainhash.Hash, credits []wallet.Input) (*wire.MsgTx, error) {
	if len(credits) == 0 {
		return nil, fmt.Errorf("no credits passed")
	}

	v.ticketToFeeMu.Lock()
	feeInfo, exists := v.ticketToFeeMap[ticketHash]
	v.ticketToFeeMu.Unlock()
	if !exists {
		_, err := v.GetFeeAddress(ctx, ticketHash)
		if err != nil {
			return nil, err
		}
		v.ticketToFeeMu.Lock()
		feeInfo, exists = v.ticketToFeeMap[ticketHash]
		v.ticketToFeeMu.Unlock()
		if !exists {
			return nil, fmt.Errorf("failed to find fee info for ticket %v", ticketHash)
		}
	}

	// validate credits
	var totalValue int64
	for _, credit := range credits {
		totalValue += credit.PrevOut.Value
	}
	if dcrutil.Amount(totalValue) < feeInfo.FeeAmount {
		return nil, fmt.Errorf("not enough fee: %v < %v", dcrutil.Amount(totalValue), feeInfo.FeeAmount)
	}

	pkScript, err := txscript.PayToAddrScript(feeInfo.FeeAddress)
	if err != nil {
		log.Warnf("failed to generate pay to addr script for %v: %v", feeInfo.FeeAddress, err)
		return nil, err
	}

	a, err := v.w.NewChangeAddress(ctx, v.changeAccount)
	if err != nil {
		log.Warnf("failed to get new change address: %v", err)
		return nil, err
	}

	c, ok := a.(wallet.Address)
	if !ok {
		log.Warnf("failed to convert '%T' to wallet.Address", a)
		return nil, fmt.Errorf("failed to convert '%T' to wallet.Address", a)
	}

	cver, cscript := c.PaymentScript()
	cs := changeSource{
		script:  cscript,
		version: cver,
	}

	var inputSource txauthor.InputSource
	if len(credits) > 0 {
		inputSource = func(amount dcrutil.Amount) (*txauthor.InputDetail, error) {
			if amount < 0 {
				return nil, fmt.Errorf("invalid amount: %d < 0", amount)
			}

			var detail txauthor.InputDetail
			if amount == 0 {
				return &detail, nil
			}
			for _, credit := range credits {
				if detail.Amount >= amount {
					break
				}

				log.Infof("credit: %v %v", credit.OutPoint.String(), dcrutil.Amount(credit.PrevOut.Value))

				// TODO: copied from txauthor.MakeInputSource - make a shared function?
				// Unspent credits are currently expected to be either P2PKH or
				// P2PK, P2PKH/P2SH nested in a revocation/stakechange/vote output.
				var scriptSize int
				scriptClass := txscript.GetScriptClass(0, credit.PrevOut.PkScript, true) // Yes treasury
				switch scriptClass {
				case txscript.PubKeyHashTy:
					scriptSize = txsizes.RedeemP2PKHSigScriptSize
				case txscript.PubKeyTy:
					scriptSize = txsizes.RedeemP2PKSigScriptSize
				case txscript.StakeRevocationTy, txscript.StakeSubChangeTy,
					txscript.StakeGenTy:
					scriptClass, err = txscript.GetStakeOutSubclass(credit.PrevOut.PkScript, true) // Yes treasury
					if err != nil {
						return nil, fmt.Errorf(
							"failed to extract nested script in stake output: %v",
							err)
					}

					// For stake transactions we expect P2PKH and P2SH script class
					// types only but ignore P2SH script type since it can pay
					// to any script which the wallet may not recognize.
					if scriptClass != txscript.PubKeyHashTy {
						log.Errorf("unexpected nested script class for credit: %v",
							scriptClass)
						continue
					}

					scriptSize = txsizes.RedeemP2PKHSigScriptSize
				default:
					log.Errorf("unexpected script class for credit: %v",
						scriptClass)
					continue
				}

				inputs := wire.NewTxIn(&credit.OutPoint, credit.PrevOut.Value, credit.PrevOut.PkScript)

				detail.Amount += dcrutil.Amount(credit.PrevOut.Value)
				detail.Inputs = append(detail.Inputs, inputs)
				detail.Scripts = append(detail.Scripts, credit.PrevOut.PkScript)
				detail.RedeemScriptSizes = append(detail.RedeemScriptSizes, scriptSize)
			}
			return &detail, nil
		}
	}

	txOut := []*wire.TxOut{wire.NewTxOut(int64(feeInfo.FeeAmount), pkScript)}
	feeTx, err := v.w.NewUnsignedTransaction(ctx, txOut, v.w.RelayFee(), v.purchaseAccount, 6,
		wallet.OutputSelectionAlgorithmDefault, cs, inputSource)
	if err != nil {
		log.Warnf("failed to create fee transaction: %v", err)
		return nil, err
	}
	if feeTx.ChangeIndex >= 0 {
		feeTx.RandomizeChangePosition()
	}

	sigErrs, err := v.w.SignTransaction(ctx, feeTx.Tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		log.Errorf("failed to sign transaction: %v", err)
		for _, sigErr := range sigErrs {
			log.Errorf("\t%v", sigErr)
		}
		return nil, err
	}

	return feeTx.Tx, nil
}

// PayFee receives an unsigned fee tx, signs it and make a pays fee request to
// the vsp, so the ticket get registered.
func (v *VSP) PayFee(ctx context.Context, ticketHash chainhash.Hash, feeTx *wire.MsgTx) (*wire.MsgTx, error) {
	if feeTx == nil {
		return nil, fmt.Errorf("nil fee tx")
	}

	txBuf := new(bytes.Buffer)
	txBuf.Grow(feeTx.SerializeSize())
	err := feeTx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize fee transaction: %v", err)
		return nil, err
	}

	v.ticketToFeeMu.Lock()
	feeInfo, exists := v.ticketToFeeMap[ticketHash]
	v.ticketToFeeMu.Unlock()
	if !exists {
		return nil, fmt.Errorf("call GetFeeAddress first")
	}

	votingKeyWIF, err := v.w.DumpWIFPrivateKey(ctx, feeInfo.VotingAddress)
	if err != nil {
		log.Errorf("failed to retrieve privkey for %v: %v", feeInfo.VotingAddress, err)
		return nil, err
	}

	// PayFee
	voteChoices := make(map[string]string)
	voteChoices[chaincfg.VoteIDHeaderCommitments] = "yes"

	payRequest := PayFeeRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash.String(),
		FeeTx:       hex.EncodeToString(txBuf.Bytes()),
		VotingKey:   votingKeyWIF,
		VoteChoices: voteChoices,
	}

	requestBody, err := json.Marshal(payRequest)
	if err != nil {
		log.Errorf("failed to marshal pay request: %v", err)
		return nil, err
	}

	url := protocol + v.hostname + "/api/v3/payfee"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		log.Errorf("failed to create new http request: %v", err)
		return nil, err
	}
	signature, err := v.w.SignMessage(ctx, string(requestBody), feeInfo.CommitmentAddress)
	if err != nil {
		log.Errorf("failed to sign feeAddress request: %v", err)
		return nil, err
	}
	req.Header.Set("VSP-Client-Signature", base64.StdEncoding.EncodeToString(signature))

	resp, err := v.httpClient.Do(req)
	if err != nil {
		log.Errorf("payfee request failed: %v", err)
		return nil, err
	}
	serverSigStr := resp.Header.Get(serverSignature)
	if serverSigStr == "" {
		log.Warnf("pay fee response missing server signature")
		return nil, err
	}
	serverSig, err := base64.StdEncoding.DecodeString(serverSigStr)
	if err != nil {
		log.Warnf("failed to decode server signature: %v", err)
		return nil, err
	}

	// TODO - Add numBytes resp check
	responseBody, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Errorf("failed to read response body: %v", err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		log.Warnf("vsp responded with an error: %v", string(responseBody))
		return nil, fmt.Errorf("vsp responded with an error (%v): %v", resp.StatusCode, string(responseBody))
	}

	if !ed25519.Verify(v.pubKey, responseBody, serverSig) {
		log.Warnf("server failed verification")
		return nil, fmt.Errorf("server failed verification")
	}

	v.ticketToFeeMu.Lock()
	feeInfo.FeeTx = feeTx
	v.ticketToFeeMap[ticketHash] = feeInfo
	v.ticketToFeeMu.Unlock()
	v.c.Watch([]*chainhash.Hash{&ticketHash}, requiredConfs)

	log.Infof("successfully processed %v", ticketHash)

	return feeTx, nil
}

type changeSource struct {
	script  []byte
	version uint16
}

func (c changeSource) Script() ([]byte, uint16, error) {
	return c.script, c.version, nil
}

func (c changeSource) ScriptSize() int {
	return len(c.script)
}
