package vsp

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrwallet/wallet"
	"decred.org/dcrwallet/wallet/txauthor"
	"decred.org/dcrwallet/wallet/txsizes"
	"github.com/decred/dcrd/chaincfg/chainhash"
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

	a, err := v.cfg.Wallet.NewChangeAddress(ctx, v.cfg.ChangeAccount)
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
	feeTx, err := v.cfg.Wallet.NewUnsignedTransaction(ctx, txOut, v.cfg.Wallet.RelayFee(), v.cfg.PurchaseAccount, 6,
		wallet.OutputSelectionAlgorithmDefault, cs, inputSource)
	if err != nil {
		log.Warnf("failed to create fee transaction: %v", err)
		return nil, err
	}
	if feeTx.ChangeIndex >= 0 {
		feeTx.RandomizeChangePosition()
	}

	sigErrs, err := v.cfg.Wallet.SignTransaction(ctx, feeTx.Tx, txscript.SigHashAll, nil, nil, nil)
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

	votingKeyWIF, err := v.cfg.Wallet.DumpWIFPrivateKey(ctx, feeInfo.VotingAddress)
	if err != nil {
		log.Errorf("failed to retrieve privkey for %v: %v", feeInfo.VotingAddress, err)
		return nil, err
	}

	// Retrieve voting preferences
	agendaChoices, _, err := v.cfg.Wallet.AgendaChoices(ctx, &ticketHash)
	if err != nil {
		log.Errorf("failed to retrieve agenda choices for %v: %v", ticketHash, err)
		return nil, err
	}

	voteChoices := make(map[string]string)
	for _, agendaChoice := range agendaChoices {
		voteChoices[agendaChoice.AgendaID] = agendaChoice.ChoiceID
	}

	var payfeeResponse PayFeeResponse
	requestBody, err := json.Marshal(&PayFeeRequest{
		Timestamp:   time.Now().Unix(),
		TicketHash:  ticketHash.String(),
		FeeTx:       hex.EncodeToString(txBuf.Bytes()),
		VotingKey:   votingKeyWIF,
		VoteChoices: voteChoices,
	})
	if err != nil {
		return nil, err
	}
	err = v.client.post(ctx, "/api/v3/payfee", feeInfo.CommitmentAddress,
		&payfeeResponse, json.RawMessage(requestBody))
	if err != nil {
		return nil, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, payfeeResponse.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, payfeeResponse.Request)
		return nil, fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?

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
