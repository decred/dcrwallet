// Copyright (c) 2018-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// API originally inspired by https://commandcenter.blogspot.com/2017/12/error-handling-in-upspin.html.
// Currently, dcrwallet is in the process of converting to the new Go 1.13 error
// wrapping features, and this package wraps xerrors for compatibility with Go 1.12.

/*
Package errors provides error creation and matching for all wallet systems.  It
is imported as errors and takes over the roll of the standard library errors
package.
*/
package errors

import (
	"errors"
	"fmt"
	"golang.org/x/xerrors"
	"runtime/debug"
	"strings"
)

// Separator is inserted between nested errors when formatting as strings.  The
// default separator produces easily readable multiline errors.  Separator may
// be modified at init time to create error strings appropriate for logging
// errors on a single line.
var Separator = ":\n\t"

// Error describes an error condition raised within the wallet process.  Errors
// may optionally provide details regarding the operation and class of error for
// assistance in debugging and runtime matching of errors.
type Error struct {
	Op   Op
	Kind Kind
	Err  error

	stack  []byte
	bottom bool
}

// Op describes the operation, method, or RPC in which an error condition was
// raised.
type Op string

// Opf returns a formatted Op.
func Opf(format string, a ...interface{}) Op {
	return Op(fmt.Sprintf(format, a...))
}

// Kind describes the class of error.
type Kind int

// Error kinds.
const (
	Other               Kind = iota // Unclassified error -- does not appear in error strings
	Bug                             // Error is known to be a result of our bug
	Invalid                         // Invalid operation
	Permission                      // Permission denied
	IO                              // I/O error
	Exist                           // Item already exists
	NotExist                        // Item does not exist
	Encoding                        // Invalid encoding
	Crypto                          // Encryption or decryption error
	Locked                          // Wallet is locked
	Passphrase                      // Invalid passphrase
	Seed                            // Invalid seed
	WatchingOnly                    // Missing private keys
	InsufficientBalance             // Insufficient balance to create transaction (perhaps due to UTXO selection requirements)
	ScriptFailure                   // Transaction scripts do not execute (usually due to missing sigs)
	Policy                          // Transaction rejected by wallet policy
	Consensus                       // Consensus violation
	DoubleSpend                     // Transaction is a double spend
	Protocol                        // Protocol violation
	NoPeers                         // Decred network is unreachable due to lack of peers or dcrd RPC connections
	Deployment                      // Inactive consensus deployment
)

func (k Kind) String() string {
	switch k {
	case Other:
		return "unclassified error"
	case Bug:
		return "internal wallet error"
	case Invalid:
		return "invalid operation"
	case Permission:
		return "permission denied"
	case IO:
		return "I/O error"
	case Exist:
		return "item already exists"
	case NotExist:
		return "item does not exist"
	case Encoding:
		return "invalid encoding"
	case Crypto:
		return "encryption/decryption error"
	case Locked:
		return "wallet locked"
	case Passphrase:
		return "invalid passphrase"
	case Seed:
		return "invalid seed"
	case WatchingOnly:
		return "watching only wallet"
	case InsufficientBalance:
		return "insufficient balance"
	case ScriptFailure:
		return "transaction script fails to execute"
	case Policy:
		return "policy violation"
	case Consensus:
		return "consensus violation"
	case DoubleSpend:
		return "double spend"
	case Protocol:
		return "protocol violation"
	case NoPeers:
		return "Decred network is unreachable"
	case Deployment:
		return "inactive deployment"
	default:
		return "unknown error kind"
	}
}

func (k Kind) Error() string {
	return k.String()
}

// As implements the interface to work with the standard library's errors.As.
// If k is Other, this always returns false and target is not assigned.
// If target points to an *Error (i.e. target has type **Error), target is
// assigned an *Error using k as its Kind.
// If target points to a Kind, target is assigned the kind and As returns true.
// Else, target is not assinged and As returns false.
func (k Kind) As(target interface{}) bool {
	if k == Other {
		return false
	}
	switch target := target.(type) {
	case **Error:
		*target = &Error{Kind: k}
		return true
	case *Kind:
		*target = k
		return true
	}
	return false
}

// New creates a simple error from a string.  New is identical to "errors".New
// from the standard library.
func New(text string) error {
	return errors.New(text)
}

// Errorf creates a simple error from a format string and arguments.  If format
// has suffix ": %w" and the last argument implements error, the returned error
// implements the Unwrap method and wraps the error.
func Errorf(format string, args ...interface{}) error {
	// xerrors.Errorf does not perfectly implement the behavior of Go 1.13
	// fmt.Errorf because it will also wrap errors using the %v and %s
	// format verbs.  To avoid issues switching to the standard library's
	// behavior in a future change, this behavior is prevented by only using
	// xerrors.Errorf when the format string ends exactly in ": %w".
	if strings.HasSuffix(format, ": %w") {
		return xerrors.Errorf(format, args...)
	}
	// fmt.Errorf will also wrap for any occurrance of %w, not only with the
	// suffix ": %w".  This is also undesirable until Go 1.13 is the minimum
	// supported version as as it would cause different wrapping behavior
	// with Go 1.12.  Replaces these occurances with %v.
	format = strings.ReplaceAll(format, "%w", "%v")
	return fmt.Errorf(format, args...)
}

// E creates an *Error from one or more arguments.
//
// Each argument type is inspected when constructing the error.  If multiple
// args of similar type are passed, the final arg is recorded.  The following
// types are recognized:
//
//  errors.Op
//      The operation, method, or RPC which was invoked.
//  errors.Kind
//      The class of error.
//  string
//      Description of the error condition.  String types populate the
//      Err field and overwrite, and are overwritten by, other arguments
//      which implement the error interface.
//  error
//      The underlying error.  If the error is an *Error, the Op and Kind
//      will be promoted to the newly created error if not set to another
//      value in the args.
//
// If another *Error is passed as an argument and no other arguments differ from
// the wrapped error, instead of wrapping the error, the errors are collapsed
// and fields of the passed *Error are promoted to the returned error.
//
// Panics if no arguments are passed.
func E(args ...interface{}) error {
	if len(args) == 0 {
		panic("errors.E: no args")
	}

	var e Error
	e.bottom = true
	var prev *Error
	for _, arg := range args {
		switch arg := arg.(type) {
		case Op:
			e.Op = arg
		case Kind:
			e.Kind = arg
		case string:
			e.Err = New(arg)
			e.bottom = true
		case *Error:
			prev = arg
			e.Err = arg
			e.bottom = false
		case error:
			e.Err = arg
			e.bottom = false
		}
	}

	// Promote the Op and Kind of the nested Error to the newly created error,
	// if these fields were not part of the args.  This improves matching
	// capabilities as well as improving the order of these fields in the
	// formatted error.
	if e.Err == prev && prev != nil {
		if e.Op == "" {
			e.Op = prev.Op
		}
		if e.Kind == 0 {
			e.Kind = prev.Kind
		}

		// Remove the previous error from error chain if it does not have any
		// unique fields.
		if (prev.Op == "" || e.Op == prev.Op) && (prev.Kind == 0 || e.Kind == prev.Kind) {
			e.Err = prev.Err
			e.bottom = prev.bottom
			if e.stack == nil {
				e.stack = prev.stack
			}
		}
	}

	return &e
}

// WithStack is identical to E but includes a stacktrace with the error. Stack
// traces do not appear in formatted error strings and are not compared when
// matching errors.  Stack traces are extracted from errors using Stacks.
func WithStack(args ...interface{}) error {
	err := E(args...).(*Error)
	err.stack = debug.Stack()
	return err
}

func (e *Error) Error() string {
	var b strings.Builder

	// Record the last added fields to the string to avoid duplication.
	var last Error

	for {
		pad := false // whether to pad/separate next field
		if e.Op != "" && e.Op != last.Op {
			b.WriteString(string(e.Op))
			pad = true
			last.Op = e.Op
		}
		if e.Kind != 0 && e.Kind != last.Kind {
			if pad {
				b.WriteString(": ")
			}
			b.WriteString(e.Kind.String())
			pad = true
			last.Kind = e.Kind
		}
		if e.Err == nil {
			break
		}
		if err, ok := e.Err.(*Error); ok {
			if pad {
				b.WriteString(Separator)
			}
			e = err
			continue
		}
		if pad {
			b.WriteString(": ")
		}
		b.WriteString(e.Err.Error())
		break
	}

	s := b.String()
	if s == "" {
		return Other.String()
	}
	return s
}

// Unwrap returns the underlying wrapped error if it is not nil.
// Otherwise, if the Kind is not Other, Unwrap returns the Kind.
// Else, it returns nil.
func (e *Error) Unwrap() error {
	if e.Err != nil {
		return e.Err
	}
	if e.Kind != Other {
		return e.Kind
	}
	return nil
}

// As implements the interface to work with the standard library's errors.As.
// If target points to an *Error (i.e. target has type **Error), target is
// assigned e and As returns true.
// If target points to a Kind and e's Kind is not Other, target is assigned
// the kind and As returns true.
// Else, target is not assinged and As returns false.
func (e *Error) As(target interface{}) bool {
	switch target := target.(type) {
	case **Error:
		*target = e
		return true
	case *Kind:
		if e.Kind != Other {
			*target = e.Kind
			return true
		}
	}
	return false
}

// Is implements the interface to work with the standard library's errors.Is.
// If target is an *Error, Is returns true if every top-level and wrapped
// non-zero fields of target are equal to the same fields of e.
// If target is a Kind, Is returns true if the Kinds match and are nonzero.
// Else, Is returns false.
func (e *Error) Is(target error) bool {
	switch target := target.(type) {
	case *Error:
		return match(target, e)
	case Kind:
		return e.Kind != Other && e.Kind == target
	}
	return false
}

// Is returns whether err equals or wraps target.
func Is(err, target error) bool {
	return xerrors.Is(err, target)
}

// As attempts to assign the error pointed to by target with the first error in
// err's error chain with a compatible type.  Returns true if target is
// assigned.
func As(err error, target interface{}) bool {
	return xerrors.As(err, target)
}

func match(err1, err2 error) bool {
	e1, ok := err1.(*Error)
	if !ok {
		return false
	}
	e2, ok := err2.(*Error)
	if !ok {
		return false
	}

	if e1.Op != "" && e1.Op != e2.Op {
		return false
	}
	if e1.Kind != 0 && e1.Kind != e2.Kind {
		return false
	}
	if e1.Err == nil {
		return true
	}
	if e1.Err == e2.Err {
		return true
	}
	if _, ok := e1.Err.(*Error); ok {
		return match(e1.Err, e2.Err)
	}
	// Although errors do not cross the process boundary, comparing error
	// strings is performed to compare formatted errors which would have
	// different allocations.
	return e1.Err.Error() == e2.Err.Error()
}

// Cause returns the most deeply-nested error from an error chain.
// Cause never returns nil unless the argument is nil.
func Cause(err error) error {
	for {
		wrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		e := wrapper.Unwrap()
		if e == nil {
			return err
		}
		err = e
	}
}

// Stacks extracts all stacktraces from err, sorted from top-most to bottom-most
// error.
func Stacks(err error) [][]byte {
	var stacks [][]byte
	e, _ := err.(*Error)
	for e != nil {
		if e.stack != nil {
			stacks = append(stacks, e.stack)
		}
		e, _ = e.Err.(*Error)
	}
	return stacks
}
