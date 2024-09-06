// Copyright (c) 2022 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

func mustParseHash(s string) chainhash.Hash {
	h, err := chainhash.NewHashFromStr(s)
	if err != nil {
		panic(err)
	}
	return *h
}

var testnet3block962928Hash = mustParseHash("0000004fd1b267fd39111d456ff557137824538e6f6776168600e56002e23b93")

// CheckpointHash returns the block hash of a checkpoint block for the network
// and height, or nil if there is no checkpoint.
func CheckpointHash(network wire.CurrencyNet, height int32) *chainhash.Hash {
	if network == wire.TestNet3 {
		switch height {
		case 962928:
			return &testnet3block962928Hash
		}
	}
	return nil
}

// BadCheckpoint returns whether a block hash violates a checkpoint rule for the
// network and block height.
func BadCheckpoint(network wire.CurrencyNet, hash *chainhash.Hash, height int32) bool {
	ckpt := CheckpointHash(network, height)
	if ckpt != nil && *ckpt != *hash {
		return true
	}
	return false
}

func (w *Wallet) rollbackInvalidCheckpoints(dbtx walletdb.ReadWriteTx) error {
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)
	var checkpoints []int32
	if w.chainParams.Net == wire.TestNet3 {
		checkpoints = []int32{962928}
	}
	for _, height := range checkpoints {
		h, err := w.txStore.GetMainChainBlockHashForHeight(txmgrNs, height)
		if errors.Is(err, errors.NotExist) {
			continue
		}
		if err != nil {
			return err
		}
		ckpt := CheckpointHash(w.chainParams.Net, height)
		if h != *ckpt {
			err := w.txStore.Rollback(dbtx, height)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
