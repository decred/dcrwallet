// Copyright (c) 2018-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "decred.org/dcrwallet/v5/wallet/drivers/badgerdb"
	_ "decred.org/dcrwallet/v5/wallet/drivers/bdb"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
)

var testPrivPass = []byte("private")

var basicWalletConfig = Config{
	PubPassphrase: []byte(InsecurePubPassphrase),
	GapLimit:      20,
	RelayFee:      dcrutil.Amount(1e5),
	Params:        chaincfg.SimNetParams(),
	MixingEnabled: true,
}

func testWallet(ctx context.Context, t *testing.T, cfg *Config, seed []byte) *Wallet {
	dbDir, err := os.MkdirTemp(t.TempDir(), "dcrwallet.testdb")
	if err != nil {
		t.Fatal(err)
	}
	db, err := walletdb.Create(*driverFlag, filepath.Join(dbDir, "wallet.db"))
	if err != nil {
		t.Fatal(err)
	}
	err = Create(ctx, opaqueDB{db}, []byte(InsecurePubPassphrase), testPrivPass, seed, cfg.Params)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DB = opaqueDB{db}
	w, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
	})
	return w
}
