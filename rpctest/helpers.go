// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrwallet/errors"
)

func mineBlock(t *testing.T, r *Harness) {
	_, heightBefore, err := r.DcrdRPCClient().GetBestBlock()
	if err != nil {
		t.Fatal("Failed to get chain height:", err)
	}

	err = generateTestChain(1, r.DcrdRPCClient())
	if err != nil {
		t.Fatal("Failed to mine block:", err)
	}

	_, heightAfter, err := r.DcrdRPCClient().GetBestBlock()

	if heightAfter != heightBefore+1 {
		t.Fatal("Failed to mine block:", heightAfter, heightBefore)
	}

	if err != nil {
		t.Fatal("Failed to GetBestBlock:", err)
	}

	count, err := syncWalletTo(r.WalletRPCClient(), heightAfter)
	if err != nil {
		t.Fatal("Failed to sync wallet to target:", err)
	}

	if heightAfter != count {
		t.Fatal("Failed to sync wallet to target:", count)
	}
}

func reverse(results []dcrjson.ListTransactionsResult) []dcrjson.ListTransactionsResult {
	i := 0
	j := len(results) - 1
	for i < j {
		results[i], results[j] = results[j], results[i]
		i++
		j--
	}
	return results
}

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
