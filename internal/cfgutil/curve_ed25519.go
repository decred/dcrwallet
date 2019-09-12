// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build go1.13

package cfgutil

import (
	"time"

	"github.com/decred/dcrd/certgen"
	"github.com/decred/dcrwallet/errors/v2"
)

const (
	Ed25519 CurveID = afterECDSA + iota
)

// PreferredCurve is the curve that should be used as the application default.
const PreferredCurve = Ed25519

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
	case Ed25519:
		name = "Ed25519"
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
	case "Ed25519":
		f.curveID = Ed25519
	default:
		return errors.Errorf("unrecognized curve %v", value)
	}
	return nil
}

func (f *CurveFlag) CertGen(org string, validUntil time.Time, extraHosts []string) (cert, key []byte, err error) {
	if ec, ok := f.ECDSACurve(); ok {
		return certgen.NewTLSCertPair(ec, org, validUntil, extraHosts)
	}
	if f.curveID == Ed25519 {
		return certgen.NewEd25519TLSCertPair(org, validUntil, extraHosts)
	}
	return nil, nil, errors.New("unknown curve ID")
}
