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

	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
)

func (v *VSP) GetFeeAddress(ctx context.Context, ticketHash *chainhash.Hash) (dcrutil.Amount, error) {
	txs, _, err := v.w.GetTransactionsByHashes(ctx, []*chainhash.Hash{ticketHash})
	if err != nil {
		log.Errorf("failed to retrieve ticket %v: %v", ticketHash, err)
		return 0, err
	}
	ticketTx := txs[0]

	const scriptVersion = 0
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(scriptVersion,
		ticketTx.TxOut[0].PkScript, v.params)
	if err != nil {
		log.Errorf("failed to extract stake submission address from %v: %v", ticketHash, err)
		return 0, err
	}
	if len(addrs) == 0 {
		log.Errorf("failed to get address from %v", ticketHash)
		return 0, fmt.Errorf("failed to get address from %v", ticketHash)
	}
	votingAddress := addrs[0]

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, v.params)
	if err != nil {
		log.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
		return 0, err
	}

	txBuf := new(bytes.Buffer)
	txBuf.Grow(ticketTx.SerializeSize())
	err = ticketTx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize ticket %v: %v", ticketHash, err)
		return 0, err
	}

	feeAddressRequest := FeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash.String(),
		TicketHex:  hex.EncodeToString(txBuf.Bytes()),
	}

	requestBody, err := json.Marshal(feeAddressRequest)
	if err != nil {
		log.Errorf("failed to marshal fee address request: %v", err)
		return 0, err
	}

	url := "https://" + v.hostname + "/api/v3/feeaddress"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		log.Errorf("failed to create new fee address request: %v", err)
		return 0, err
	}

	signature, err := v.w.SignMessage(ctx, string(requestBody), commitmentAddr)
	if err != nil {
		log.Errorf("failed to sign feeAddress request: %v", err)
		return 0, err
	}
	req.Header.Set("VSP-Client-Signature", base64.StdEncoding.EncodeToString(signature))

	resp, err := v.httpClient.Do(req)
	if err != nil {
		log.Errorf("fee address request failed: %v", err)
		return 0, err
	}
	// TODO - Add numBytes resp check

	responseBody, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Errorf("failed to read fee address response: %v", err)
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		log.Warnf("vsp responded with an error: %v", string(responseBody))
		return 0, fmt.Errorf("vsp with an error (%v): %v", resp.StatusCode, string(responseBody))
	}

	serverSigStr := resp.Header.Get(serverSignature)
	if serverSigStr == "" {
		log.Warnf("feeaddress missing server signature")
		return 0, fmt.Errorf("server signature missing from feeaddress response")
	}
	serverSig, err := base64.StdEncoding.DecodeString(serverSigStr)
	if err != nil {
		log.Warnf("failed to decode server signature: %v", err)
		return 0, err
	}

	if !ed25519.Verify(v.pubKey, responseBody, serverSig) {
		log.Warnf("server failed verification")
		return 0, fmt.Errorf("server failed verification")
	}

	var feeResponse FeeAddressResponse
	err = json.Unmarshal(responseBody, &feeResponse)
	if err != nil {
		log.Warnf("failed to unmarshal feeaddress response: %v", err)
		return 0, err
	}

	// verify initial request matches server
	serverRequestBody, err := json.Marshal(feeResponse.Request)
	if err != nil {
		log.Warnf("failed to marshal response request: %v", err)
		return 0, err
	}
	if !bytes.Equal(requestBody, serverRequestBody) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, serverRequestBody)
		return 0, fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?

	feeAddress, err := dcrutil.DecodeAddress(feeResponse.FeeAddress, v.params)
	if err != nil {
		log.Warnf("server fee address invalid: %v", err)
		return 0, fmt.Errorf("server fee address invalid: %v", err)
	}
	feeAmount := dcrutil.Amount(feeResponse.FeeAmount)

	// TODO - convert to vsp.maxfee config option
	maxFee, err := dcrutil.NewAmount(0.1)
	if err != nil {
		return 0, err
	}

	if feeAmount > maxFee {
		log.Warnf("fee amount too high: %v > %v", feeAmount, maxFee)
		return 0, fmt.Errorf("server fee amount too high: %v > %v", feeAmount, maxFee)
	}

	v.ticketToFeeMu.Lock()
	v.ticketToFeeMap[*ticketHash] = PendingFee{
		CommitmentAddress: commitmentAddr,
		VotingAddress:     votingAddress,
		FeeAddress:        feeAddress,
		FeeAmount:         feeAmount,
	}
	v.ticketToFeeMu.Unlock()

	return feeAmount, nil
}
