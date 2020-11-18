package vsp

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
)

func (v *VSP) GetFeeAddress(ctx context.Context, ticketHash chainhash.Hash) (dcrutil.Amount, error) {
	// Fetch ticket
	txs, _, err := v.cfg.Wallet.GetTransactionsByHashes(ctx, []*chainhash.Hash{&ticketHash})
	if err != nil {
		log.Errorf("failed to retrieve ticket %v: %v", ticketHash, err)
		return 0, err
	}
	ticketTx := txs[0]

	// Fetch parent transaction
	parentHash := ticketTx.TxIn[0].PreviousOutPoint.Hash
	parentTxs, _, err := v.cfg.Wallet.GetTransactionsByHashes(ctx, []*chainhash.Hash{&parentHash})
	if err != nil {
		log.Errorf("failed to retrieve parent %v of ticket %v: %v", parentHash, ticketHash, err)
		return 0, err
	}
	parentTx := parentTxs[0]

	const scriptVersion = 0
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(scriptVersion,
		ticketTx.TxOut[0].PkScript, v.cfg.Params, true) // Yes treasury
	if err != nil {
		log.Errorf("failed to extract stake submission address from %v: %v", ticketHash, err)
		return 0, err
	}
	if len(addrs) == 0 {
		log.Errorf("failed to get address from %v", ticketHash)
		return 0, fmt.Errorf("failed to get address from %v", ticketHash)
	}
	votingAddress := addrs[0]

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, v.cfg.Params)
	if err != nil {
		log.Errorf("failed to extract script addr from %v: %v", ticketHash, err)
		return 0, err
	}

	// Serialize ticket
	txBuf := new(bytes.Buffer)
	txBuf.Grow(ticketTx.SerializeSize())
	err = ticketTx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize ticket %v: %v", ticketHash, err)
		return 0, err
	}
	ticketHex := hex.EncodeToString(txBuf.Bytes())

	// Serialize parent
	txBuf.Reset()
	txBuf.Grow(parentTx.SerializeSize())
	err = parentTx.Serialize(txBuf)
	if err != nil {
		log.Errorf("failed to serialize parent %v of ticket %v: %v", parentHash, ticketHash, err)
		return 0, err
	}
	parentHex := hex.EncodeToString(txBuf.Bytes())

	var feeResponse FeeAddressResponse
	requestBody, err := json.Marshal(&FeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: ticketHash.String(),
		TicketHex:  ticketHex,
		ParentHex:  parentHex,
	})
	if err != nil {
		return 0, err
	}
	err = v.client.post(ctx, "/api/v3/feeaddress", commitmentAddr, &feeResponse,
		json.RawMessage(requestBody))
	if err != nil {
		return 0, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, feeResponse.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, feeResponse.Request)
		return 0, fmt.Errorf("server response contains differing request")
	}
	// TODO - validate server timestamp?

	feeAddress, err := dcrutil.DecodeAddress(feeResponse.FeeAddress, v.cfg.Params)
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
	v.ticketToFeeMap[ticketHash] = PendingFee{
		CommitmentAddress: commitmentAddr,
		VotingAddress:     votingAddress,
		FeeAddress:        feeAddress,
		FeeAmount:         feeAmount,
	}
	v.ticketToFeeMu.Unlock()

	return feeAmount, nil
}
