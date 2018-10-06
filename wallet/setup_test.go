// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	_ "github.com/decred/dcrwallet/wallet/drivers/bdb"
	"github.com/decred/dcrwallet/wallet/internal/walletdb"
)

var basicWalletConfig = Config{
	PubPassphrase: []byte(InsecurePubPassphrase),
	GapLimit:      20,
	RelayFee:      dcrutil.Amount(1e5).ToCoin(),
	Params:        &chaincfg.SimNetParams,
}

func testWallet(t *testing.T, cfg *Config) (w *Wallet, teardown func()) {
	f, err := ioutil.TempFile("", "dcrwallet.testdb")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	db, err := walletdb.Create("bdb", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	rm := func() {
		db.Close()
		os.Remove(f.Name())
	}
	err = Create(opaqueDB{db}, []byte(InsecurePubPassphrase), []byte("private"), nil, cfg.Params)
	if err != nil {
		rm()
		t.Fatal(err)
	}
	cfg.DB = opaqueDB{db}
	w, err = Open(cfg)
	if err != nil {
		rm()
		t.Fatal(err)
	}
	w.Start()
	teardown = func() {
		w.Stop()
		w.WaitForShutdown()
		rm()
	}
	return
}
