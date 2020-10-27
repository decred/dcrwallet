package vsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

func (v *VSP) TicketStatus(ctx context.Context, hash *chainhash.Hash) (*TicketStatusResponse, error) {
	txs, _, err := v.cfg.Wallet.GetTransactionsByHashes(ctx, []*chainhash.Hash{hash})
	if err != nil {
		log.Errorf("failed to retrieve ticket %v: %v", hash, err)
		return nil, err
	}
	ticketTx := txs[0]

	if !stake.IsSStx(ticketTx) {
		log.Errorf("%v is not a ticket", hash)
		return nil, fmt.Errorf("%v is not a ticket", hash)
	}
	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, v.cfg.Params)
	if err != nil {
		log.Errorf("failed to extract script addr from %v: %v", hash, err)
		return nil, err
	}

	var resp TicketStatusResponse
	requestBody, err := json.Marshal(&TicketStatusRequest{
		TicketHash: hash.String(),
	})
	if err != nil {
		return nil, err
	}
	err = v.client.post(ctx, "/api/v3/ticketstatus", commitmentAddr, &resp, json.RawMessage(requestBody))
	if err != nil {
		return nil, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, resp.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, resp.Request)
		return nil, fmt.Errorf("server response contains differing request")
	}

	// TODO - validate server timestamp?

	return &resp, nil
}
