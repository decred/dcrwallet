// Copyright (c) 2018-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package accountmixer

import (
	"context"
	"net"
	"runtime/trace"
	"sync"

	"github.com/decred/dcrwallet/wallet/v3"
)

const minconf = 1

// AccountMixerConfig modifies the behavior of TB.
type Config struct {
	// CSPP-related options
	CSPPServer         string
	DialCSPPServer     func(ctx context.Context, network, addr string) (net.Conn, error)
	MixedAccount       uint32
	MixedAccountBranch uint32
	ChangeAccount      uint32
}

// AccountMixer is an automated account mixing service.  On every new
// main chain notification, a new MixAccount request is attempted.
type AccountMixer struct {
	wallet *wallet.Wallet

	cfg Config
	mu  sync.Mutex
}

// New returns a new AccountMixer to mix
func New(w *wallet.Wallet) *AccountMixer {
	return &AccountMixer{wallet: w}
}

// Run executes the account mixer.  If the private passphrase is incorrect, or
// ever becomes incorrect due to a wallet passphrase change, Run exits with an
// errors.Passphrase error.
func (m *AccountMixer) Run(ctx context.Context, passphrase []byte) error {
	err := m.wallet.Unlock(ctx, passphrase, nil)
	if err != nil {
		return err
	}

	c := m.wallet.NtfnServer.MainTipChangedNotifications()
	defer c.Done()

	ctx, _ = context.WithCancel(ctx)
	var fatal error
	var fatalMu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			fatalMu.Lock()
			err := fatal
			fatalMu.Unlock()
			if err != nil {
				return err
			}
			return ctx.Err()
		case <-c.C:
			go func() {
				err := m.mixChange(ctx)
				if err != nil {
					log.Error(err)
				}
			}()
		}
	}
}

func (m *AccountMixer) mixChange(ctx context.Context) error {
	// Read config
	m.mu.Lock()
	dial := m.cfg.DialCSPPServer
	csppServer := m.cfg.CSPPServer
	mixedAccount := m.cfg.MixedAccount
	mixedBranch := m.cfg.MixedAccountBranch
	changeAccount := m.cfg.ChangeAccount
	m.mu.Unlock()

	ctx, task := trace.NewTask(ctx, "accountMixer.mixChange")
	defer task.End()

	return m.wallet.MixAccount(ctx, dial, csppServer, changeAccount, mixedAccount, mixedBranch)
}
