// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package errors

import "testing"

func depth(err error) int {
	if err == nil {
		return 0
	}
	e, ok := err.(*Error)
	if !ok {
		return 1
	}
	return 1 + depth(e.Err)
}

func eq(e0, e1 *Error) bool {
	if e0.Op != e1.Op {
		return false
	}
	if e0.Kind != e1.Kind {
		return false
	}
	if e0.Err != e1.Err {
		return false
	}
	return true
}

func TestCollapse(t *testing.T) {
	e0 := E(Op("abc"))
	e0 = E(e0, Passphrase)
	if depth(e0) != 1 {
		t.Fatal("e0 was not collapsed")
	}

	e1 := E(Op("abc"), Passphrase)
	if !eq(e0.(*Error), e1.(*Error)) {
		t.Fatal("e0 was not collapsed to e1")
	}
}

func TestMatch(t *testing.T) {
	e := E(Errorf("%s", "some error"), Op("operation"), Permission)
	if !Match(E("some error"), e) {
		t.Fatal("no match on error strings")
	}
	if Match(E("different error"), e) {
		t.Fatal("match on different error strings")
	}
	if !Match(E(Op("operation")), e) {
		t.Fatal("no match on operation")
	}
	if Match(E(Op("different operation")), e) {
		t.Fatal("match on different operation")
	}
	if !Match(E(Permission), e) {
		t.Fatal("no match on kind")
	}
	if Match(E(Invalid), e) {
		t.Fatal("match on different kind")
	}
}

func TestCause(t *testing.T) {
	inner := New("inner")
	outer := E(inner)
	if Cause(outer) != inner {
		t.Fatal("Cause is not equal to inner error")
	}
	if Cause(nil) != nil {
		t.Fatal("Cause(nil) must be nil")
	}
	// Cause(E("string")) must return the bottom *Error type, not stdlib errors.errorString
	for _, e := range []error{
		E("bottom"),
		E(Passphrase, E(Invalid, "bottom")),
	} {
		c := Cause(e)
		if _, ok := c.(*Error); !ok {
			t.Fatalf("wrong bottom error type %T", c)
		}
	}
}
