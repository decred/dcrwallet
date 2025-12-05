// Copyright (c) 2018-2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package badgerdb registers the badgerdb driver at init time.  Importing
// badgerdb allows the wallet.OpenDB and wallet.CreateDB functions to be
// called with the following arguments:
//
//	var directory string
//	db, err := wallet.CreateDB("badgerdb", directory)
//	if err != nil { /* handle error */ }
//	db, err = wallet.OpenDB("badgerdb", directory)
//	if err != nil { /* handle error */ }
package badgerdb

import _ "decred.org/dcrwallet/v5/wallet/internal/badgerdb" // Register badgerdb driver during init
