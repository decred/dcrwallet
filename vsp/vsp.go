// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package vsp

import (
	"context"
	"net"

	"decred.org/dcrwallet/v3/internal/vsp"
	"github.com/decred/dcrd/dcrutil/v4"
)

// NewClient creats a new vsp client that can be used to purchase tickets,
// monitor the vsp fee, and set vote preferences.
func NewClient(wf vsp.WalletFetcher, vspHost, vspPubKey string, feeAcct, changeAcct uint32,
	maxFee dcrutil.Amount, dialer func(ctx context.Context, network, addr string) (net.Conn, error)) (*vsp.Client, error) {

	return vsp.New(vsp.Config{
		URL:    vspHost,
		PubKey: vspPubKey,
		Dialer: dialer,
		Wallet: wf,
		Policy: vsp.Policy{
			MaxFee:     maxFee,
			FeeAcct:    feeAcct,
			ChangeAcct: changeAcct,
		},
	})
}
