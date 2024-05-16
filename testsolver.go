// Copyright (c) 2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/rpc"

	"decred.org/cspp/v2/solverrpc"
	"github.com/decred/dcrd/mixing"
)

func testStartedSolverWorks() error {
	if err := solverrpc.StartSolver(); err != nil {
		return err
	}

	// Values don't matter; we just need to not observe an io.UnexpectedEOF
	// or net/rpc.ErrShutdown.
	coeffs := []*big.Int{
		big.NewInt(1),
		big.NewInt(2),
	}
	_, err := solverrpc.Roots(coeffs, mixing.F)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, rpc.ErrShutdown) {
		return fmt.Errorf("rpc failed: %w", err)
	}

	return nil
}
