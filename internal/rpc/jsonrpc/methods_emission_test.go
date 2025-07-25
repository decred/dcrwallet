// Copyright (c) 2025 The Monetarium developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"decred.org/dcrwallet/v5/rpc/jsonrpc/types"
)

// TestGenerateEmissionKeyCmd tests the GenerateEmissionKeyCmd structure.
func TestGenerateEmissionKeyCmd(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *types.GenerateEmissionKeyCmd
		wantValid   bool
		description string
	}{
		{
			name: "Valid command with all fields",
			cmd: &types.GenerateEmissionKeyCmd{
				KeyName:    "test_emission_key",
				Passphrase: "test_passphrase",
				CoinType:   func() *uint8 { v := uint8(1); return &v }(),
			},
			wantValid:   true,
			description: "Should be valid with proper key name, passphrase, and coin type",
		},
		{
			name: "Valid command without coin type (should default)",
			cmd: &types.GenerateEmissionKeyCmd{
				KeyName:    "default_key",
				Passphrase: "test_passphrase",
			},
			wantValid:   true,
			description: "Should be valid without coin type specified",
		},
		{
			name: "Invalid command with empty key name",
			cmd: &types.GenerateEmissionKeyCmd{
				KeyName:    "",
				Passphrase: "test_passphrase",
			},
			wantValid:   false,
			description: "Should be invalid with empty key name",
		},
		{
			name: "Invalid command with empty passphrase",
			cmd: &types.GenerateEmissionKeyCmd{
				KeyName:    "test_key",
				Passphrase: "",
			},
			wantValid:   false,
			description: "Should be invalid with empty passphrase",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Test basic validation logic
			isValid := validateGenerateEmissionKeyCmd(test.cmd)
			
			if isValid != test.wantValid {
				t.Errorf("Expected validation result %t, got %t: %s", 
					test.wantValid, isValid, test.description)
			}
		})
	}
}

// TestImportEmissionKeyCmd tests the ImportEmissionKeyCmd structure.
func TestImportEmissionKeyCmd(t *testing.T) {
	// Generate a test private key
	testPrivKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}
	testPrivKeyHex := hex.EncodeToString(testPrivKey.Serialize())

	tests := []struct {
		name        string
		cmd         *types.ImportEmissionKeyCmd
		wantValid   bool
		description string
	}{
		{
			name: "Valid import with raw private key",
			cmd: &types.ImportEmissionKeyCmd{
				KeyName:    "imported_raw_key",
				PrivateKey: testPrivKeyHex,
				Passphrase: "import_passphrase",
				CoinType:   func() *uint8 { v := uint8(1); return &v }(),
			},
			wantValid:   true,
			description: "Should be valid with proper private key hex",
		},
		{
			name: "Valid import with encrypted private key format",
			cmd: &types.ImportEmissionKeyCmd{
				KeyName:    "imported_encrypted_key",
				PrivateKey: "aes256gcm:mock_iv:mock_encrypted_data",
				Passphrase: "test_passphrase",
				CoinType:   func() *uint8 { v := uint8(2); return &v }(),
			},
			wantValid:   true,
			description: "Should be valid with encrypted private key format",
		},
		{
			name: "Invalid private key format",
			cmd: &types.ImportEmissionKeyCmd{
				KeyName:    "invalid_key",
				PrivateKey: "invalid_hex",
				Passphrase: "test_passphrase",
			},
			wantValid:   false,
			description: "Should be invalid with malformed private key",
		},
		{
			name: "Empty key name",
			cmd: &types.ImportEmissionKeyCmd{
				KeyName:    "",
				PrivateKey: testPrivKeyHex,
				Passphrase: "test_passphrase",
			},
			wantValid:   false,
			description: "Should be invalid with empty key name",
		},
		{
			name: "Empty passphrase",
			cmd: &types.ImportEmissionKeyCmd{
				KeyName:    "test_key",
				PrivateKey: testPrivKeyHex,
				Passphrase: "",
			},
			wantValid:   false,
			description: "Should be invalid with empty passphrase",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Test basic validation logic
			isValid := validateImportEmissionKeyCmd(test.cmd)
			
			if isValid != test.wantValid {
				t.Errorf("Expected validation result %t, got %t: %s", 
					test.wantValid, isValid, test.description)
			}
		})
	}
}

// TestCreateAuthorizedEmissionCmd tests the CreateAuthorizedEmissionCmd structure.
func TestCreateAuthorizedEmissionCmd(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *types.CreateAuthorizedEmissionCmd
		wantValid   bool
		description string
	}{
		{
			name: "Valid emission creation",
			cmd: &types.CreateAuthorizedEmissionCmd{
				CoinType:        1,
				EmissionKeyName: "emission_test_key",
				Passphrase:      "test_passphrase",
			},
			wantValid:   true,
			description: "Should be valid with proper coin type, key name, and passphrase",
		},
		{
			name: "Invalid coin type 0 (VAR)",
			cmd: &types.CreateAuthorizedEmissionCmd{
				CoinType:        0, // VAR not allowed
				EmissionKeyName: "emission_test_key",
				Passphrase:      "test_passphrase",
			},
			wantValid:   false,
			description: "Should be invalid with coin type 0 (VAR)",
		},
		{
			name: "Empty emission key name",
			cmd: &types.CreateAuthorizedEmissionCmd{
				CoinType:        1,
				EmissionKeyName: "",
				Passphrase:      "test_passphrase",
			},
			wantValid:   false,
			description: "Should be invalid with empty emission key name",
		},
		{
			name: "Empty passphrase",
			cmd: &types.CreateAuthorizedEmissionCmd{
				CoinType:        1,
				EmissionKeyName: "emission_test_key",
				Passphrase:      "",
			},
			wantValid:   false,
			description: "Should be invalid with empty passphrase",
		},
		{
			name: "Valid high coin type",
			cmd: &types.CreateAuthorizedEmissionCmd{
				CoinType:        255, // Max valid value
				EmissionKeyName: "emission_test_key",
				Passphrase:      "test_passphrase",
			},
			wantValid:   true,
			description: "Should be valid with coin type 255",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Test basic validation logic
			isValid := validateCreateAuthorizedEmissionCmd(test.cmd)
			
			if isValid != test.wantValid {
				t.Errorf("Expected validation result %t, got %t: %s", 
					test.wantValid, isValid, test.description)
			}
		})
	}
}

// TestEmissionKeyEncryptionFormat tests the encryption format validation.
func TestEmissionKeyEncryptionFormat(t *testing.T) {
	tests := []struct {
		name           string
		encryptedKey   string
		expectedValid  bool
		description    string
	}{
		{
			name:           "Valid AES-256-GCM format",
			encryptedKey:   "aes256gcm:1234567890abcdef:fedcba0987654321",
			expectedValid:  true,
			description:    "Should recognize valid AES-256-GCM format",
		},
		{
			name:           "Invalid format - missing prefix",
			encryptedKey:   "1234567890abcdef:fedcba0987654321",
			expectedValid:  false,
			description:    "Should reject format without aes256gcm prefix",
		},
		{
			name:           "Invalid format - wrong prefix",
			encryptedKey:   "aes128gcm:1234567890abcdef:fedcba0987654321",
			expectedValid:  false,
			description:    "Should reject format with wrong encryption prefix",
		},
		{
			name:           "Invalid format - missing parts",
			encryptedKey:   "aes256gcm:1234567890abcdef",
			expectedValid:  false,
			description:    "Should reject format with missing encrypted data part",
		},
		{
			name:           "Invalid format - empty string",
			encryptedKey:   "",
			expectedValid:  false,
			description:    "Should reject empty encrypted key",
		},
		{
			name:           "Invalid format - too many parts",
			encryptedKey:   "aes256gcm:1234:5678:9abc:def0",
			expectedValid:  false,
			description:    "Should reject format with too many colon-separated parts",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isValid := isValidEncryptedKeyFormat(test.encryptedKey)
			
			if isValid != test.expectedValid {
				t.Errorf("Expected %t for '%s': %s", 
					test.expectedValid, test.encryptedKey, test.description)
			}
		})
	}
}

// TestPrivateKeyHexValidation tests private key hex validation.
func TestPrivateKeyHexValidation(t *testing.T) {
	// Generate a valid private key for testing
	validPrivKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("Failed to generate valid private key: %v", err)
	}
	validHex := hex.EncodeToString(validPrivKey.Serialize())

	tests := []struct {
		name        string
		privateKey  string
		expectedValid bool
		description string
	}{
		{
			name:          "Valid private key hex",
			privateKey:    validHex,
			expectedValid: true,
			description:   "Should accept valid 32-byte private key hex",
		},
		{
			name:          "Invalid hex characters",
			privateKey:    "invalid_hex_characters_here",
			expectedValid: false,
			description:   "Should reject non-hex characters",
		},
		{
			name:          "Wrong length - too short",
			privateKey:    "1234567890abcdef",
			expectedValid: false,
			description:   "Should reject hex string that's too short",
		},
		{
			name:          "Wrong length - too long",
			privateKey:    validHex + "extra",
			expectedValid: false,
			description:   "Should reject hex string that's too long",
		},
		{
			name:          "Empty private key",
			privateKey:    "",
			expectedValid: false,
			description:   "Should reject empty private key",
		},
		{
			name:          "All zeros (invalid private key)",
			privateKey:    "0000000000000000000000000000000000000000000000000000000000000000",
			expectedValid: false,
			description:   "Should reject all-zero private key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			isValid := isValidPrivateKeyHex(test.privateKey)
			
			if isValid != test.expectedValid {
				t.Errorf("Expected %t for private key validation: %s", 
					test.expectedValid, test.description)
			}
		})
	}
}

// Helper functions for validation (these would be implemented in the actual RPC methods)

func validateGenerateEmissionKeyCmd(cmd *types.GenerateEmissionKeyCmd) bool {
	if cmd.KeyName == "" {
		return false
	}
	if cmd.Passphrase == "" {
		return false
	}
	if cmd.CoinType != nil && *cmd.CoinType == 0 {
		return false // VAR coin type not allowed for emission
	}
	return true
}

func validateImportEmissionKeyCmd(cmd *types.ImportEmissionKeyCmd) bool {
	if cmd.KeyName == "" {
		return false
	}
	if cmd.Passphrase == "" {
		return false
	}
	if cmd.PrivateKey == "" {
		return false
	}
	
	// Check if it's encrypted format or raw hex
	if strings.HasPrefix(cmd.PrivateKey, "aes256gcm:") {
		return isValidEncryptedKeyFormat(cmd.PrivateKey)
	}
	
	return isValidPrivateKeyHex(cmd.PrivateKey)
}

func validateCreateAuthorizedEmissionCmd(cmd *types.CreateAuthorizedEmissionCmd) bool {
	if cmd.CoinType == 0 {
		return false // VAR not allowed
	}
	if cmd.CoinType > 255 {
		return false // Above valid range
	}
	if cmd.EmissionKeyName == "" {
		return false
	}
	if cmd.Passphrase == "" {
		return false
	}
	return true
}

func isValidEncryptedKeyFormat(encryptedKey string) bool {
	if !strings.HasPrefix(encryptedKey, "aes256gcm:") {
		return false
	}
	
	parts := strings.Split(encryptedKey, ":")
	return len(parts) == 3 && parts[1] != "" && parts[2] != ""
}

func isValidPrivateKeyHex(privateKey string) bool {
	if privateKey == "" {
		return false
	}
	
	// Check if it's valid hex
	decoded, err := hex.DecodeString(privateKey)
	if err != nil {
		return false
	}
	
	// Check correct length (32 bytes for secp256k1)
	if len(decoded) != 32 {
		return false
	}
	
	// Check that it's not all zeros (invalid private key)
	allZeros := true
	allOnes := true
	for _, b := range decoded {
		if b != 0 {
			allZeros = false
		}
		if b != 0xff {
			allOnes = false
		}
	}
	
	// Reject all-zeros and all-ones private keys
	return !allZeros && !allOnes
}