// Copyright (c) 2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// We use a build tag to change the default GODEBUG value to the latest Go
// release, rather than setting this default in the go.mod for two reasons:
//
// 1. Our go.mod specifies at most the previous supported Go version as a
//    minimum, and the latest Go toolchain's default GODEBUG cannot be specified
//    without breaking builds with the older toolchains.
// 2. If a go.work is used, the godebug directives in go.mod will be ignored.

//go:build go1.24

//go:debug default=go1.24

package main
