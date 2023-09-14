package txrules_test

import (
	"testing"

	. "decred.org/dcrwallet/v4/wallet/txrules"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
)

func TestStakePoolTicketFee(t *testing.T) {
	params := chaincfg.MainNetParams()
	tests := []struct {
		StakeDiff       dcrutil.Amount
		Fee             dcrutil.Amount
		Height          int32
		PoolFee         float64
		Expected        dcrutil.Amount
		IsDCP0010Active bool
		IsDCP0012Active bool
	}{
		0: {
			StakeDiff:       10 * 1e8,
			Fee:             0.01 * 1e8,
			Height:          25000,
			PoolFee:         1.00,
			Expected:        0.01500463 * 1e8,
			IsDCP0010Active: false,
			IsDCP0012Active: false,
		},
		1: {
			StakeDiff:       20 * 1e8,
			Fee:             0.01 * 1e8,
			Height:          25000,
			PoolFee:         1.00,
			Expected:        0.01621221 * 1e8,
			IsDCP0010Active: false,
			IsDCP0012Active: false,
		},
		2: {
			StakeDiff:       5 * 1e8,
			Fee:             0.05 * 1e8,
			Height:          50000,
			PoolFee:         2.59,
			Expected:        0.03310616 * 1e8,
			IsDCP0010Active: false,
			IsDCP0012Active: false,
		},
		3: {
			StakeDiff:       15 * 1e8,
			Fee:             0.05 * 1e8,
			Height:          50000,
			PoolFee:         2.59,
			Expected:        0.03956376 * 1e8,
			IsDCP0010Active: false,
			IsDCP0012Active: false,
		},
		4: {
			StakeDiff:       15 * 1e8,
			Fee:             0.05 * 1e8,
			Height:          50000,
			PoolFee:         2.59,
			Expected:        0.09023823 * 1e8,
			IsDCP0010Active: true,
			IsDCP0012Active: false,
		},
		5: {
			StakeDiff:       15 * 1e8,
			Fee:             0.05 * 1e8,
			Height:          50000,
			PoolFee:         2.59,
			Expected:        0.09784185 * 1e8,
			IsDCP0010Active: false,
			IsDCP0012Active: true,
		},
		6: {
			StakeDiff:       15 * 1e8,
			Fee:             0.05 * 1e8,
			Height:          50000,
			PoolFee:         2.59,
			Expected:        0.09784185 * 1e8,
			IsDCP0010Active: true,
			IsDCP0012Active: true,
		},
	}
	for i, test := range tests {
		poolFeeAmt := StakePoolTicketFee(test.StakeDiff, test.Fee, test.Height,
			test.PoolFee, params, test.IsDCP0010Active, test.IsDCP0012Active)
		if poolFeeAmt != test.Expected {
			t.Errorf("Test %d: Got %v: Want %v", i, poolFeeAmt, test.Expected)
		}
	}
}
