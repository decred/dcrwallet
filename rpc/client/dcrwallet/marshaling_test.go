// Copyright (c) 2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrwallet

import (
	"fmt"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

func TestMarshalOutpointsToString(t *testing.T) {
	t.Parallel()

	hash1, _ := chainhash.NewHashFromStr("679c508625503e5cd00ba8d35badc40ce040df8986c11321758db831b78f94ec")
	hash2, _ := chainhash.NewHashFromStr("7bc19eb0bf3a57be73d6879b6c411404b14b0156353dd47c5e0456768704bfd1")

	tests := []struct {
		outpoints []wire.OutPoint
		expected  string
	}{
		{
			outpoints: []wire.OutPoint{},
			expected:  "[]",
		},
		{
			outpoints: []wire.OutPoint{
				{Hash: *hash1, Index: 0},
			},
			expected: `["679c508625503e5cd00ba8d35badc40ce040df8986c11321758db831b78f94ec:0"]`,
		},
		{
			outpoints: []wire.OutPoint{
				{Hash: *hash1, Index: 69},
				{Hash: *hash2, Index: 1001},
			},
			expected: `["679c508625503e5cd00ba8d35badc40ce040df8986c11321758db831b78f94ec:69","7bc19eb0bf3a57be73d6879b6c411404b14b0156353dd47c5e0456768704bfd1:1001"]`,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			t.Parallel()

			marshaler := marshalOutpointsToString(test.outpoints)
			bytes, err := marshaler.MarshalJSON()
			if err != nil {
				t.Fatalf("unexpected error from MarshalJSON: %v", err)
			}

			actual := string(bytes)
			if actual != test.expected {
				t.Fatalf("incorrect output from MarshalJSON: %q != %q", actual, test.expected)
			}
		})
	}

}
