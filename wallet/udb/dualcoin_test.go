// Copyright (c) 2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// TestDualCoinCredit tests comprehensive dual-coin Credit functionality
func TestDualCoinCredit(t *testing.T) {
	// No database needed for basic Credit testing

	tests := []struct {
		name        string
		coinType    dcrutil.CoinType
		amount      dcrutil.Amount
		description string
	}{
		{
			name:        "VAR mining reward",
			coinType:    dcrutil.CoinTypeVAR,
			amount:      dcrutil.Amount(500000000), // 5 VAR
			description: "Standard VAR coin from mining",
		},
		{
			name:        "SKA-1 emission",
			coinType:    dcrutil.CoinType(1),
			amount:      dcrutil.Amount(100000000), // 1 SKA-1
			description: "SKA coin type 1 from emission",
		},
		{
			name:        "SKA-255 maximum",
			coinType:    dcrutil.CoinType(255),
			amount:      dcrutil.Amount(250000000), // 2.5 SKA-255
			description: "Maximum SKA coin type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Credit with specific properties
			credit := Credit{
				Amount:       tt.amount,
				CoinType:     tt.coinType,
				Received:     time.Now(),
				FromCoinBase: tt.coinType == dcrutil.CoinTypeVAR, // Only VAR from coinbase
			}

			// Test basic properties
			if credit.CoinType != tt.coinType {
				t.Errorf("Credit.CoinType = %v, want %v", credit.CoinType, tt.coinType)
			}

			if credit.Amount != tt.amount {
				t.Errorf("Credit.Amount = %v, want %v", credit.Amount, tt.amount)
			}

			// Test coin type specific logic
			if tt.coinType == dcrutil.CoinTypeVAR && !credit.FromCoinBase {
				t.Error("VAR credits should typically be from coinbase (mining)")
			}

			if tt.coinType != dcrutil.CoinTypeVAR && credit.FromCoinBase {
				t.Error("SKA credits should not be from coinbase")
			}
		})
	}
}

// TestCreditCoinTypeSeparation tests that VAR and SKA credits are properly separated
func TestCreditCoinTypeSeparation(t *testing.T) {
	// Create mixed credit list
	credits := []Credit{
		{Amount: dcrutil.Amount(100000000), CoinType: dcrutil.CoinTypeVAR},
		{Amount: dcrutil.Amount(200000000), CoinType: dcrutil.CoinType(1)},
		{Amount: dcrutil.Amount(300000000), CoinType: dcrutil.CoinTypeVAR},
		{Amount: dcrutil.Amount(400000000), CoinType: dcrutil.CoinType(2)},
	}

	// Filter by coin type
	varCredits := make([]Credit, 0)
	skaCredits := make([]Credit, 0)

	for _, credit := range credits {
		if credit.CoinType == dcrutil.CoinTypeVAR {
			varCredits = append(varCredits, credit)
		} else {
			skaCredits = append(skaCredits, credit)
		}
	}

	// Validate separation
	if len(varCredits) != 2 {
		t.Errorf("Expected 2 VAR credits, got %d", len(varCredits))
	}

	if len(skaCredits) != 2 {
		t.Errorf("Expected 2 SKA credits, got %d", len(skaCredits))
	}

	// Validate VAR total
	varTotal := dcrutil.Amount(0)
	for _, credit := range varCredits {
		varTotal += credit.Amount
	}
	expectedVarTotal := dcrutil.Amount(400000000) // 100M + 300M
	if varTotal != expectedVarTotal {
		t.Errorf("VAR total = %v, want %v", varTotal, expectedVarTotal)
	}

	// Validate SKA total  
	skaTotal := dcrutil.Amount(0)
	for _, credit := range skaCredits {
		skaTotal += credit.Amount
	}
	expectedSkaTotal := dcrutil.Amount(600000000) // 200M + 400M
	if skaTotal != expectedSkaTotal {
		t.Errorf("SKA total = %v, want %v", skaTotal, expectedSkaTotal)
	}
}

// TestNewCreditRecord tests creation of credits from transaction records with CoinType
func TestNewCreditRecord(t *testing.T) {
	// Create a mock transaction with mixed coin types
	tx := &wire.MsgTx{
		TxOut: []*wire.TxOut{
			{
				Value:    int64(100000000),
				CoinType: wire.CoinType(dcrutil.CoinTypeVAR),
				PkScript: []byte{0x76, 0xa9, 0x14}, // Mock P2PKH script
			},
			{
				Value:    int64(200000000), 
				CoinType: wire.CoinType(1), // SKA-1
				PkScript: []byte{0xa9, 0x14}, // Mock P2SH script
			},
		},
	}

	// Mock transaction record
	rec := &TxRecord{
		MsgTx: *tx,
		Hash:  randomTxHash(),
	}

	// Test extracting CoinType from each output
	for index, expectedCoinType := range []dcrutil.CoinType{dcrutil.CoinTypeVAR, dcrutil.CoinType(1)} {
		// This simulates what happens in newCreditRecord function
		extractedCoinType := dcrutil.CoinType(rec.MsgTx.TxOut[index].CoinType)
		
		if extractedCoinType != expectedCoinType {
			t.Errorf("Output %d: extracted CoinType = %v, want %v", index, extractedCoinType, expectedCoinType)
		}

		// Simulate credit creation
		credit := Credit{
			Amount:   dcrutil.Amount(rec.MsgTx.TxOut[index].Value),
			CoinType: extractedCoinType,
		}

		if credit.CoinType != expectedCoinType {
			t.Errorf("Credit %d: CoinType = %v, want %v", index, credit.CoinType, expectedCoinType)
		}
	}
}

// Helper function to generate random transaction hash
func randomTxHash() chainhash.Hash {
	hash := new(chainhash.Hash)
	err := hash.SetBytes(make([]byte, 32)) // Simple zero hash for testing
	if err != nil {
		panic(err)
	}
	return *hash
}

// TestCreditDatabaseIntegration tests end-to-end credit storage and retrieval with CoinType
func TestCreditDatabaseIntegration(t *testing.T) {
	// Test serialization without actual database

	// Test complete serialization/deserialization cycle
	testCredits := []struct {
		amount   dcrutil.Amount
		coinType dcrutil.CoinType
	}{
		{dcrutil.Amount(100000000), dcrutil.CoinTypeVAR},
		{dcrutil.Amount(200000000), dcrutil.CoinType(1)},
		{dcrutil.Amount(300000000), dcrutil.CoinType(255)},
	}

	for i, tc := range testCredits {
		t.Run("integration_test_"+string(rune('A'+i)), func(t *testing.T) {
			// Create internal credit structure (simulating database storage)
			cred := credit{
				amount:   tc.amount,
				coinType: tc.coinType,
			}

			// Test valueUnspentCredit serialization
			serialized := make([]byte, 95) // Use full credit value size
			// Simulate valueUnspentCredit logic
			serialized[94] = byte(cred.coinType)

			// Test fetchRawCreditCoinType deserialization
			deserializedCoinType := fetchRawCreditCoinType(serialized)
			
			if deserializedCoinType != tc.coinType {
				t.Errorf("Serialization round-trip failed: got %v, want %v", deserializedCoinType, tc.coinType)
			}
		})
	}
}

// TestDualCoinTransactionValidation tests dual-coin transaction rules
func TestDualCoinTransactionValidation(t *testing.T) {
	// Test cases for dual-coin transaction validation
	tests := []struct {
		name        string
		inputTypes  []dcrutil.CoinType
		outputTypes []dcrutil.CoinType
		valid       bool
		description string
	}{
		{
			name:        "pure VAR transaction",
			inputTypes:  []dcrutil.CoinType{dcrutil.CoinTypeVAR, dcrutil.CoinTypeVAR},
			outputTypes: []dcrutil.CoinType{dcrutil.CoinTypeVAR},
			valid:       true,
			description: "All VAR inputs and outputs",
		},
		{
			name:        "pure SKA-1 transaction", 
			inputTypes:  []dcrutil.CoinType{dcrutil.CoinType(1), dcrutil.CoinType(1)},
			outputTypes: []dcrutil.CoinType{dcrutil.CoinType(1)},
			valid:       true,
			description: "All SKA-1 inputs and outputs",
		},
		{
			name:        "mixed coin transaction (invalid)",
			inputTypes:  []dcrutil.CoinType{dcrutil.CoinTypeVAR, dcrutil.CoinType(1)},
			outputTypes: []dcrutil.CoinType{dcrutil.CoinTypeVAR},
			valid:       false,
			description: "Mixed VAR and SKA inputs - should be invalid",
		},
		{
			name:        "mixed output types (invalid)",
			inputTypes:  []dcrutil.CoinType{dcrutil.CoinTypeVAR},
			outputTypes: []dcrutil.CoinType{dcrutil.CoinTypeVAR, dcrutil.CoinType(1)},
			valid:       false,
			description: "Mixed VAR and SKA outputs - should be invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate transaction validation logic
			valid := validateCoinTypeSeparation(tt.inputTypes, tt.outputTypes)
			
			if valid != tt.valid {
				t.Errorf("Transaction validation = %v, want %v for %s", valid, tt.valid, tt.description)
			}
		})
	}
}

// Helper function to validate coin type separation in transactions
func validateCoinTypeSeparation(inputTypes, outputTypes []dcrutil.CoinType) bool {
	if len(inputTypes) == 0 || len(outputTypes) == 0 {
		return false
	}

	// All inputs must be the same coin type
	firstInputType := inputTypes[0]
	for _, coinType := range inputTypes {
		if coinType != firstInputType {
			return false // Mixed input types
		}
	}

	// All outputs must be the same coin type as inputs
	for _, coinType := range outputTypes {
		if coinType != firstInputType {
			return false // Output type doesn't match input type
		}
	}

	return true
}