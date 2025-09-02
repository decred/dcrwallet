package wallet

import (
	"testing"

	"decred.org/dcrwallet/v5/wallet/txauthor"
	"decred.org/dcrwallet/v5/wallet/txrules"
	"github.com/decred/dcrd/cointype"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// TestDualCoinTxRules tests the dual-coin transaction rules and coin type detection
func TestDualCoinTxRules(t *testing.T) {
	// Test case 1: Verify that VAR outputs result in VAR coin type detection
	varOutputs := []*wire.TxOut{
		{Value: 100000000, CoinType: cointype.CoinTypeVAR}, // 1 VAR
	}

	coinType := txrules.GetPrimaryCoinTypeFromOutputs(varOutputs)
	if coinType != cointype.CoinTypeVAR {
		t.Errorf("Expected VAR coin type (0), got %d", coinType)
	}

	// Test case 2: Verify that SKA outputs result in correct SKA coin type detection
	skaOutputs := []*wire.TxOut{
		{Value: 200000000, CoinType: cointype.CoinType(1)}, // 2 SKA-1
	}

	coinType = txrules.GetPrimaryCoinTypeFromOutputs(skaOutputs)
	if coinType != cointype.CoinType(1) {
		t.Errorf("Expected SKA-1 coin type (1), got %d", coinType)
	}

	// Test case 3: Mixed outputs should return first non-VAR coin type
	mixedOutputs := []*wire.TxOut{
		{Value: 100000000, CoinType: cointype.CoinTypeVAR}, // 1 VAR
		{Value: 200000000, CoinType: cointype.CoinType(2)}, // 2 SKA-2
	}

	coinType = txrules.GetPrimaryCoinTypeFromOutputs(mixedOutputs)
	if coinType != cointype.CoinType(2) {
		t.Errorf("Expected SKA-2 coin type (2), got %d", coinType)
	}
}

// TestDualCoinFeeCalculation tests the fee calculation for different coin types
func TestDualCoinFeeCalculation(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1000) // 1000 atoms per KB
	txSize := 250                         // bytes

	// Test VAR fee calculation
	varFee := txrules.FeeForSerializeSizeDualCoin(relayFeePerKb, txSize, cointype.CoinTypeVAR)
	expectedVarFee := txrules.FeeForSerializeSize(relayFeePerKb, txSize)
	if varFee != expectedVarFee {
		t.Errorf("VAR fee calculation: expected %d, got %d", expectedVarFee, varFee)
	}

	// Test SKA fee calculation (should use same calculation as VAR)
	skaFee := txrules.FeeForSerializeSizeDualCoin(relayFeePerKb, txSize, cointype.CoinType(1))
	if skaFee != expectedVarFee {
		t.Errorf("SKA fee calculation: expected %d, got %d", expectedVarFee, skaFee)
	}

	// Verify fees are non-zero for regular transactions
	if varFee == 0 {
		t.Error("VAR fee should not be zero for regular transactions")
	}
	if skaFee == 0 {
		t.Error("SKA fee should not be zero for regular transactions")
	}
}

// TestTxAuthorFeeHandling tests that txauthor properly handles fees for all coin types
func TestTxAuthorFeeHandling(t *testing.T) {
	// Create mock input source that provides sufficient funds
	inputSource := func(target dcrutil.Amount) (*txauthor.InputDetail, error) {
		// Provide inputs with enough value to cover target + fees
		mockInput := &wire.TxIn{
			PreviousOutPoint: wire.OutPoint{Index: 0},
			ValueIn:          int64(target + 1000), // Extra for fees
		}
		return &txauthor.InputDetail{
			Amount:            target + 1000,
			Inputs:            []*wire.TxIn{mockInput},
			RedeemScriptSizes: []int{25}, // P2PKH script size
		}, nil
	}

	// Create mock change source
	changeSource := &mockChangeSource{
		script:     make([]byte, 25), // P2PKH script
		scriptSize: 25,
	}

	relayFeePerKb := dcrutil.Amount(1000) // 1000 atoms per KB

	// Test VAR transaction - should include fees
	varOutputs := []*wire.TxOut{
		{Value: 100000000, CoinType: cointype.CoinTypeVAR},
	}

	varTx, err := txauthor.NewUnsignedTransaction(varOutputs, relayFeePerKb, inputSource, changeSource, 100000)
	if err != nil {
		t.Fatalf("Failed to create VAR transaction: %v", err)
	}

	// Verify VAR transaction has fee
	varInputTotal := dcrutil.Amount(varTx.Tx.TxIn[0].ValueIn)
	varOutputTotal := dcrutil.Amount(0)
	for _, out := range varTx.Tx.TxOut {
		varOutputTotal += dcrutil.Amount(out.Value)
	}
	varFee := varInputTotal - varOutputTotal
	if varFee <= 0 {
		t.Errorf("VAR transaction should have positive fee, got %d", varFee)
	}

	// Test SKA transaction - should also include fees (fixed in our changes)
	skaOutputs := []*wire.TxOut{
		{Value: 100000000, CoinType: cointype.CoinType(1)},
	}

	skaTx, err := txauthor.NewUnsignedTransaction(skaOutputs, relayFeePerKb, inputSource, changeSource, 100000)
	if err != nil {
		t.Fatalf("Failed to create SKA transaction: %v", err)
	}

	// Verify SKA transaction has fee (this would have failed before our fix)
	skaInputTotal := dcrutil.Amount(skaTx.Tx.TxIn[0].ValueIn)
	skaOutputTotal := dcrutil.Amount(0)
	for _, out := range skaTx.Tx.TxOut {
		skaOutputTotal += dcrutil.Amount(out.Value)
	}
	skaFee := skaInputTotal - skaOutputTotal
	if skaFee <= 0 {
		t.Errorf("SKA transaction should have positive fee, got %d", skaFee)
	}

	// Verify both transactions have similar fee rates
	if skaFee != varFee {
		t.Logf("VAR fee: %d, SKA fee: %d (difference: %d)", varFee, skaFee, skaFee-varFee)
		// Allow small differences due to transaction size variations
		if abs(int64(skaFee-varFee)) > 100 {
			t.Errorf("Fee difference between VAR and SKA transactions too large: %d", abs(int64(skaFee-varFee)))
		}
	}
}

// mockChangeSource implements txauthor.ChangeSource for testing
type mockChangeSource struct {
	script     []byte
	scriptSize int
}

func (m *mockChangeSource) Script() ([]byte, uint16, error) {
	return m.script, wire.DefaultPkScriptVersion, nil
}

func (m *mockChangeSource) ScriptSize() int {
	return m.scriptSize
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
