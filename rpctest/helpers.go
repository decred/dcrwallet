// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"math"
	"testing"
	"time"
	"strconv"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
)

// Create a test chain with the desired number of mature coinbase outputs
func generateTestChain(numToGenerate uint32, node *rpcclient.Client) error {
	fmt.Printf("Generating %v blocks...\n", numToGenerate)
	_, err := node.Generate(numToGenerate)
	if err != nil {
		return err
	}
	fmt.Println("Block generation complete.")
	return nil
}

// Waits for wallet to sync to the target height
func syncWalletTo(rpcClient *rpcclient.Client, desiredHeight int64) (int64, error) {
	var count int64 = 0
	var err error = nil
	for count != desiredHeight {
		Sleep(1000)
		count, err = rpcClient.GetBlockCount()
		if err != nil {
			return -1, err
		}
		fmt.Println("   sync to: " + strconv.FormatInt(count, 10))
	}
	return count, nil
}

func getMiningAddr(walletClient *rpcclient.Client) dcrutil.Address {
	var miningAddr dcrutil.Address
	var err error = nil
	for i := 0; i < 100; i++ {
		miningAddr, err = walletClient.GetNewAddress("default")
		if err != nil {
			fmt.Println("err: " + err.Error())
			time.Sleep(time.Duration(math.Log(float64(i+3))) * 50 * time.Millisecond)
			continue
		}
		break
	}
	if miningAddr == nil {
		ReportTestSetupMalfunction(errors.Errorf(
			"RPC not up for mining addr"))
	}
	return miningAddr
}

func mustGetStakeDiffNext(r *Harness, t *testing.T) float64 {
	stakeDiffResult, err := r.WalletRPCClient().GetStakeDifficulty()
	if err != nil {
		t.Fatal("GetStakeDifficulty failed:", err)
	}

	return stakeDiffResult.NextStakeDifficulty
}

func newBlockAt(currentHeight uint32, r *Harness,
	t *testing.T) (uint32, *dcrutil.Block, []*chainhash.Hash) {
	height, block, blockHashes := newBlockAtQuick(currentHeight, r, t)

	time.Sleep(700 * time.Millisecond)

	return height, block, blockHashes
}

func newBlockAtQuick(currentHeight uint32, r *Harness,
	t *testing.T) (uint32, *dcrutil.Block, []*chainhash.Hash) {

	blockHashes, err := r.GenerateBlock(currentHeight)
	if err != nil {
		t.Fatalf("Unable to generate single block: %v", err)
	}

	block, err := r.DcrdRPCClient().GetBlock(blockHashes[0])
	if err != nil {
		t.Fatalf("Unable to get block: %v", err)
	}

	return block.Header.Height, dcrutil.NewBlock(block), blockHashes
}

func getBestBlockHeight(r *Harness, t *testing.T) uint32 {
	_, height, err := r.DcrdRPCClient().GetBestBlock()
	if err != nil {
		t.Fatalf("Failed to GetBestBlock: %v", err)
	}

	return uint32(height)
}

func newBestBlock(r *Harness,
	t *testing.T) (uint32, *dcrutil.Block, []*chainhash.Hash) {
	height := getBestBlockHeight(r, t)
	height, block, blockHash := newBlockAt(height, r, t)
	return height, block, blockHash
}

// includesTx checks if a block contains a transaction hash
func includesTx(txHash *chainhash.Hash, block *dcrutil.Block) bool {
	if len(block.Transactions()) <= 1 {
		return false
	}

	blockTxs := block.Transactions()

	for _, minedTx := range blockTxs {
		minedTxHash := minedTx.Hash()
		if *txHash == *minedTxHash {
			return true
		}
	}

	return false
}

// getWireMsgTxFee computes the effective absolute fee from a Tx as the amount
// spent minus sent.
func getWireMsgTxFee(tx *dcrutil.Tx) dcrutil.Amount {
	var totalSpent int64
	for _, txIn := range tx.MsgTx().TxIn {
		totalSpent += txIn.ValueIn
	}

	var totalSent int64
	for _, txOut := range tx.MsgTx().TxOut {
		totalSent += txOut.Value
	}

	return dcrutil.Amount(totalSpent - totalSent)
}

// getOutPointString uses OutPoint.String() to combine the tx hash with vout
// index from a ListUnspentResult.
func getOutPointString(utxo *dcrjson.ListUnspentResult) (string, error) {
	txhash, err := chainhash.NewHashFromStr(utxo.TxID)
	if err != nil {
		return "", err
	}
	return wire.NewOutPoint(txhash, utxo.Vout, utxo.Tree).String(), nil
}

// GenerateBlock is a helper function to ensure that the chain has actually
// incremented due to FORK blocks after stake voting height that may occur.
func (h *Harness) GenerateBlock(startHeight uint32) ([]*chainhash.Hash, error) {
	blockHashes, err := h.DcrdRPCClient().Generate(1)
	if err != nil {
		return nil, errors.Errorf("unable to generate single block: %v", err)
	}
	blockHeader, err := h.DcrdRPCClient().GetBlockHeader(blockHashes[0])
	if err != nil {
		return nil, errors.Errorf("unable to get block header: %v", err)
	}
	newHeight := blockHeader.Height
	for newHeight == startHeight {
		blockHashes, err = h.DcrdRPCClient().Generate(1)
		if err != nil {
			return nil, errors.Errorf("unable to generate single block: %v", err)
		}
		blockHeader, err = h.DcrdRPCClient().GetBlockHeader(blockHashes[0])
		if err != nil {
			return nil, errors.Errorf("unable to get block header: %v", err)
		}
		newHeight = blockHeader.Height
	}
	return blockHashes, nil
}
