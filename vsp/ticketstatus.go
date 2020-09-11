package vsp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

func (v *VSP) TicketStatus(ctx context.Context, hash *chainhash.Hash) (*TicketStatusResponse, error) {
	url := protocol + v.hostname + "/api/v3/ticketstatus"

	txs, _, err := v.w.GetTransactionsByHashes(ctx, []*chainhash.Hash{hash})
	if err != nil {
		log.Errorf("failed to retrieve ticket %v: %v", hash, err)
		return nil, err
	}
	ticketTx := txs[0]

	if !stake.IsSStx(ticketTx) {
		log.Errorf("%v is not a ticket", hash)
		return nil, fmt.Errorf("%v is not a ticket", hash)
	}
	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, v.params)
	if err != nil {
		log.Errorf("failed to extract script addr from %v: %v", hash, err)
		return nil, err
	}

	ticketStatusRequest := TicketStatusRequest{
		TicketHash: hash.String(),
	}

	requestBody, err := json.Marshal(&ticketStatusRequest)
	if err != nil {
		log.Errorf("failed to marshal ticket status request: %v", err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		log.Errorf("failed to create new fee address request: %v", err)
		return nil, err
	}

	signature, err := v.w.SignMessage(ctx, string(requestBody), commitmentAddr)
	if err != nil {
		log.Errorf("failed to sign feeAddress request: %v", err)
		return nil, err
	}
	req.Header.Set("VSP-Client-Signature", base64.StdEncoding.EncodeToString(signature))

	resp, err := v.httpClient.Do(req)
	if err != nil {
		log.Errorf("ticket status request failed: %v", err)
		return nil, err
	}
	serverSigStr := resp.Header.Get(serverSignature)
	if serverSigStr == "" {
		log.Warnf("ticket status response missing server signature")
		return nil, fmt.Errorf("ticket status response missing server signature")
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

	var ticketStatusResponse TicketStatusResponse
	err = json.Unmarshal(responseBody, &ticketStatusResponse)
	if err != nil {
		log.Warnf("failed to unmarshal ticketstatus response: %v", err)
		return nil, err
	}

	// verify initial request matches server
	serverRequestBody, err := json.Marshal(ticketStatusResponse.Request)
	if err != nil {
		log.Warnf("failed to marshal response request: %v", err)
		return nil, err
	}
	if !bytes.Equal(requestBody, serverRequestBody) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, serverRequestBody)
		return nil, fmt.Errorf("server response contains differing request")
	}

	// TODO - validate server timestamp?

	return &ticketStatusResponse, nil
}
