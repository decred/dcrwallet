// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cfgutil

import "crypto/elliptic"

// CurveID specifies a recognized curve through a constant value.
type CurveID int

// Recognized curve IDs.
const (
	CurveP224 CurveID = iota
	CurveP256
	CurveP384
	CurveP521
	afterECDSA
)

// CurveFlag describes a curve and implements the flags.Marshaler and
// Unmarshaler interfaces so it can be used as a config struct field.
type CurveFlag struct {
	curveID CurveID
}

// NewCurveFlag creates a CurveFlag with a default curve.
func NewCurveFlag(defaultValue CurveID) *CurveFlag {
	return &CurveFlag{defaultValue}
}

// ECDSACurve returns the elliptic curve described by f, or (nil, false) if the
// curve is not one of the elliptic curves suitable for ECDSA.
func (f *CurveFlag) ECDSACurve() (elliptic.Curve, bool) {
	switch f.curveID {
	case CurveP224:
		return elliptic.P224(), true
	case CurveP256:
		return elliptic.P256(), true
	case CurveP384:
		return elliptic.P384(), true
	case CurveP521:
		return elliptic.P521(), true
	default:
		return nil, false
	}
}
