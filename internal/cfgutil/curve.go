// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cfgutil

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"io"
	"time"

	"decred.org/dcrwallet/errors"
	"github.com/decred/dcrd/certgen"
)

// CurveID specifies a recognized curve through a constant value.
type CurveID int

// Recognized curve IDs.
const (
	CurveP256 CurveID = iota
	CurveP384
	CurveP521
	Ed25519

	// PreferredCurve is the curve that should be used as the application default.
	PreferredCurve = Ed25519
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

// MarshalFlag satisfies the flags.Marshaler interface.
func (f *CurveFlag) MarshalFlag() (name string, err error) {
	switch f.curveID {
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

func (f *CurveFlag) GenerateKeyPair(rand io.Reader) (pub, priv interface{}, err error) {
	if ec, ok := f.ECDSACurve(); ok {
		var key *ecdsa.PrivateKey
		key, err = ecdsa.GenerateKey(ec, rand)
		if err != nil {
			return
		}
		pub, priv = key.Public(), key
		return
	}
	if f.curveID == Ed25519 {
		seed := make([]byte, ed25519.SeedSize)
		_, err = io.ReadFull(rand, seed)
		if err != nil {
			return
		}
		key := ed25519.NewKeyFromSeed(seed)
		pub, priv = key.Public(), key
		return
	}
	return nil, nil, errors.New("unknown curve ID")
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
