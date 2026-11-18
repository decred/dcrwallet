// Copyright (c) 2017-2022 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package main

import (
	_ "embed"
)

//go:embed sample-dcrwallet.conf
var sampleDcrwalletConf string

// Dcrd returns a string containing the commented example config for dcrd.
func Dcrwallet() string {
	return sampleDcrwalletConf
}

// FileContents returns a string containing the commented example config for
// dcrwallet.
//
// Deprecated: Use the [Dcrwallet] function instead.
func FileContents() string {
	return Dcrwallet()
}
