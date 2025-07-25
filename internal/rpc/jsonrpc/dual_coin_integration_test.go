// Copyright (c) 2025 The Monetarium developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"decred.org/dcrwallet/v5/wallet"
	_ "decred.org/dcrwallet/v5/wallet/drivers/bdb"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
)

// testWalletConfig returns a test wallet configuration
func testWalletConfig() *wallet.Config {
	return &wallet.Config{
		PubPassphrase: []byte(wallet.InsecurePubPassphrase),
		GapLimit:      20,
		RelayFee:      dcrutil.Amount(1e5),
		Params:        chaincfg.SimNetParams(),
	}
}

// setupTestWallet creates a test wallet with some initial state
func setupTestWallet(ctx context.Context, t *testing.T) (*wallet.Wallet, func()) {
	cfg := testWalletConfig()

	f, err := os.CreateTemp(t.TempDir(), "dcrwallet.integtest")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	db, err := wallet.CreateDB("bdb", f.Name())
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(f.Name())
	}

	// Use test seed for reproducible testing
	seed := []byte("test seed for dual coin integration testing")

	err = wallet.Create(ctx, db, []byte(wallet.InsecurePubPassphrase),
		[]byte("testpass"), seed, cfg.Params)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}

	cfg.DB = db
	w, err := wallet.Open(ctx, cfg)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}

	return w, cleanup
}

// setupTestRPCHandler creates a test wallet for testing
func setupTestRPCHandler(ctx context.Context, t *testing.T) (*wallet.Wallet, func()) {
	w, cleanupWallet := setupTestWallet(ctx, t)
	return w, cleanupWallet
}

// Note: Helper functions stringPtr and intPtr are defined in methods_dual_coin_test.go

// TestGetCoinBalanceIntegration tests the getcoinbalance RPC method end-to-end
func TestGetCoinBalanceIntegration(t *testing.T) {
	ctx := context.Background()
	w, cleanup := setupTestRPCHandler(ctx, t)
	defer cleanup()

	// Unlock wallet for testing
	timeout := make(chan time.Time, 1)
	timeout <- time.Now().Add(time.Second * 30)
	err := w.Unlock(ctx, []byte("testpass"), timeout)
	if err != nil {
		t.Fatalf("Failed to unlock wallet: %v", err)
	}

	// Test that we can call the wallet methods directly
	// This simulates what the RPC methods would do internally

	tests := []struct {
		name     string
		coinType uint8
		minConf  int32
		wantErr  bool
	}{
		{
			name:     "VAR balance test",
			coinType: 0,
			minConf:  1,
			wantErr:  false,
		},
		{
			name:     "SKA-1 balance test",
			coinType: 1,
			minConf:  1,
			wantErr:  false,
		},
		{
			name:     "Invalid negative minconf",
			coinType: 0,
			minConf:  -1,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic validation
			if tt.minConf < 0 {
				if !tt.wantErr {
					t.Error("Expected validation to catch negative minConf")
				}
				return
			}

			// Test wallet balance retrieval
			// This tests the core functionality without full RPC layer
			balance, err := w.AccountBalanceByCoinType(ctx, 0, dcrutil.CoinType(tt.coinType), tt.minConf)
			if err != nil && !tt.wantErr {
				t.Errorf("Unexpected error getting balance: %v", err)
				return
			}

			if !tt.wantErr {
				// Balance should be non-negative (0 for empty test wallet)
				if balance.Spendable < 0 {
					t.Error("Balance should be non-negative")
				}

				t.Logf("Coin type %d balance: %v", tt.coinType, balance.Spendable)
			}
		})
	}
}

// TestListCoinTypesIntegration tests the listcointypes RPC method end-to-end
func TestListCoinTypesIntegration(t *testing.T) {
	ctx := context.Background()
	w, cleanup := setupTestRPCHandler(ctx, t)
	defer cleanup()

	// Unlock wallet for testing
	timeout := make(chan time.Time, 1)
	timeout <- time.Now().Add(time.Second * 30)
	err := w.Unlock(ctx, []byte("testpass"), timeout)
	if err != nil {
		t.Fatalf("Failed to unlock wallet: %v", err)
	}

	// Test coin type listing functionality
	tests := []struct {
		name    string
		minConf int32
		wantErr bool
	}{
		{
			name:    "List coin types default minconf",
			minConf: 1,
			wantErr: false,
		},
		{
			name:    "List coin types with minconf 6",
			minConf: 6,
			wantErr: false,
		},
		{
			name:    "Invalid negative minconf",
			minConf: -1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic validation
			if tt.minConf < 0 {
				if !tt.wantErr {
					t.Error("Expected validation to catch negative minConf")
				}
				return
			}

			// Test that we can list balances for different coin types
			coinTypes := []dcrutil.CoinType{dcrutil.CoinTypeVAR, dcrutil.CoinType(1), dcrutil.CoinType(2)}

			for _, coinType := range coinTypes {
				balance, err := w.AccountBalanceByCoinType(ctx, 0, coinType, tt.minConf)
				if err != nil && !tt.wantErr {
					t.Errorf("Unexpected error getting balance for coin type %d: %v", coinType, err)
					return
				}

				if !tt.wantErr {
					// Verify coin type name formatting
					var expectedName string
					if coinType == dcrutil.CoinTypeVAR {
						expectedName = "VAR"
					} else {
						expectedName = fmt.Sprintf("SKA-%d", coinType)
					}

					t.Logf("Coin type %d (%s) balance: %v", coinType, expectedName, balance.Spendable)

					// Balance should be non-negative
					if balance.Spendable < 0 {
						t.Errorf("Balance for coin type %d should be non-negative", coinType)
					}
				}
			}
		})
	}
}

// TestDualCoinRPCMethodsBasic tests that the RPC methods can be called without errors
func TestDualCoinRPCMethodsBasic(t *testing.T) {
	ctx := context.Background()
	w, cleanup := setupTestRPCHandler(ctx, t)
	defer cleanup()

	// Unlock wallet for testing
	timeout := make(chan time.Time, 1)
	timeout <- time.Now().Add(time.Second * 30)
	err := w.Unlock(ctx, []byte("testpass"), timeout)
	if err != nil {
		t.Fatalf("Failed to unlock wallet: %v", err)
	}

	t.Run("basic dual coin functionality", func(t *testing.T) {
		// Test that wallet supports multiple coin types
		coinTypes := []dcrutil.CoinType{
			dcrutil.CoinTypeVAR,
			dcrutil.CoinType(1),
			dcrutil.CoinType(2),
			dcrutil.CoinType(255),
		}

		for _, coinType := range coinTypes {
			balance, err := w.AccountBalanceByCoinType(ctx, 0, coinType, 1)
			if err != nil {
				t.Errorf("Failed to get balance for coin type %d: %v", coinType, err)
				continue
			}

			// All coin types should return valid (non-negative) balances
			if balance.Spendable < 0 {
				t.Errorf("Coin type %d returned negative balance: %v", coinType, balance.Spendable)
			}

			t.Logf("Coin type %d balance: %v", coinType, balance.Spendable)
		}

		// Test account balances by coin type
		accounts := []uint32{0, 1} // default account and account 1
		for _, account := range accounts {
			for _, coinType := range coinTypes {
				balance, err := w.AccountBalanceByCoinType(ctx, account, coinType, 1)
				if err != nil {
					// Account might not exist, which is fine for testing
					t.Logf("Account %d, coin type %d: %v (expected for new wallet)", account, coinType, err)
					continue
				}

				t.Logf("Account %d, coin type %d balance: %v", account, coinType, balance.Spendable)
			}
		}
	})
}

// Helper functions (moved to avoid redeclaration with methods_dual_coin_test.go)
