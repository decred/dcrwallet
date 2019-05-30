// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cfgutil

import (
	"crypto/elliptic"

	"github.com/decred/dcrwallet/errors"
)

// CurveID specifies a recognized curve through a constant value.
type CurveID int

// Recognized curve IDs.
const (
	CurveP224 CurveID = iota
	CurveP256
	CurveP384
	CurveP521
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

// MarshalFlag satisfies the flags.Marshaler interface.
func (f *CurveFlag) MarshalFlag() (name string, err error) {
	switch f.curveID {
	case CurveP224:
		name = "P-224"
	case CurveP256:
		name = "P-256"
	case CurveP384:
		name = "P-384"
	case CurveP521:
		name = "P-521"
	default:
		err = errors.Errorf("unknown curve ID %v", int(f.curveID))
	}
	return
}

// UnmarshalFlag satisfies the flags.Unmarshaler interface.
func (f *CurveFlag) UnmarshalFlag(value string) error {
	switch value {
	case "P-224":
		f.curveID = CurveP224
	case "P-256":
		f.curveID = CurveP256
	case "P-384":
		f.curveID = CurveP384
	case "P-521":
		f.curveID = CurveP521
	default:
		return errors.Errorf("unrecognized curve %v", value)
	}
	return nil
}

// Curve returns the elliptic.Curve specified by the flag.
func (f *CurveFlag) Curve() elliptic.Curve {
	switch f.curveID {
	case CurveP224:
		return elliptic.P224()
	case CurveP256:
		return elliptic.P256()
	case CurveP384:
		return elliptic.P384()
	case CurveP521:
		return elliptic.P521()
	default:
		panic("unreachable")
	}
}
