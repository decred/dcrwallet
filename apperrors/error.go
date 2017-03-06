// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package apperrors

// Code identifies an error by a specific integer code.
type Code int

//go:generate stringer -type=Code

// These constants are used to identify a specific ManagerError.
const (
	// ErrDatabase indicates an error with the underlying database.  When
	// this error code is set, the Err field of the ManagerError will be
	// set to the underlying error returned from the database.
	ErrDatabase Code = iota

	// ErrUpgrade indicates the manager needs to be upgraded.  This should
	// not happen in practice unless the version number has been increased
	// and there is not yet any code written to upgrade.
	ErrUpgrade

	// ErrKeyChain indicates an error with the key chain typically either
	// due to the inability to create an extended key or deriving a child
	// extended key.  When this error code is set, the Err field of the
	// ManagerError will be set to the underlying error.
	ErrKeyChain

	// ErrCrypto indicates an error with the cryptography related operations
	// such as decrypting or encrypting data, parsing an EC public key,
	// or deriving a secret key from a password.  When this error code is
	// set, the Err field of the ManagerError will be set to the underlying
	// error.
	ErrCrypto

	// ErrInvalidKeyType indicates an error where an invalid crypto
	// key type has been selected.
	ErrInvalidKeyType

	// ErrNoExist indicates that the specified database does not exist.
	ErrNoExist

	// ErrAlreadyExists indicates that the specified database already exists.
	ErrAlreadyExists

	// ErrCoinTypeTooHigh indicates that the coin type specified in the provided
	// network parameters is higher than the max allowed value as defined
	// by the maxCoinType constant.
	ErrCoinTypeTooHigh

	// ErrAccountNumTooHigh indicates that the specified account number is higher
	// than the max allowed value as defined by the MaxAccountNum constant.
	ErrAccountNumTooHigh

	// ErrLocked indicates that an operation, which requires the account
	// manager to be unlocked, was requested on a locked account manager.
	ErrLocked

	// ErrWatchingOnly indicates that an operation, which requires the
	// account manager to have access to private data, was requested on
	// a watching-only account manager.
	ErrWatchingOnly

	// ErrInvalidAccount indicates that the requested account is not valid.
	ErrInvalidAccount

	// ErrAddressNotFound indicates that the requested address is not known to
	// the account manager.
	ErrAddressNotFound

	// ErrAccountNotFound indicates that the requested account is not known to
	// the account manager.
	ErrAccountNotFound

	// ErrDuplicateAddress indicates an address already exists.
	ErrDuplicateAddress

	// ErrDuplicateAccount indicates an account already exists.
	ErrDuplicateAccount

	// ErrTooManyAddresses indicates that more than the maximum allowed number of
	// addresses per account have been requested.
	ErrTooManyAddresses

	// ErrWrongPassphrase indicates that the specified passphrase is incorrect.
	// This could be for either public or private master keys.
	ErrWrongPassphrase

	// ErrWrongNet indicates that the private key to be imported is not for the
	// the same network the account manager is configured for.
	ErrWrongNet

	// ErrCallBackBreak is used to break from a callback function passed
	// down to the manager.
	ErrCallBackBreak

	// ErrEmptyPassphrase indicates that the private passphrase was refused
	// due to being empty.
	ErrEmptyPassphrase

	// ErrCreateAddress is used to indicate that an address could not be
	// created from a public key.
	ErrCreateAddress

	// ErrMetaPoolIdxNoExist indicates that the address index for some
	// account's address pool was unset or short.
	ErrMetaPoolIdxNoExist

	// ErrBranch indicates that the branch passed was not internal
	// or external for some account.
	ErrBranch

	// ErrSyncToIndex indicates that the passed address index to sync
	// an account branch to was erroneous.
	ErrSyncToIndex

	// ErrData describes an error where data stored in the transaction
	// database is incorrect.  This may be due to missing values, values of
	// wrong sizes, or data from different buckets that is inconsistent with
	// itself.  Recovering from an ErrData requires rebuilding all
	// transaction history or manual database surgery.  If the failure was
	// not due to data corruption, this error category indicates a
	// programming error in this package.
	ErrData

	// ErrInput describes an error where the variables passed into this
	// function by the caller are obviously incorrect.  Examples include
	// passing transactions which do not serialize, or attempting to insert
	// a credit at an index for which no transaction output exists.
	ErrInput

	// ErrValueNoExists describes an error indicating that the value for
	// a given key does not exist in the database queried.
	ErrValueNoExists

	// ErrDoubleSpend indicates that an output was attempted to be spent
	// twice.
	ErrDoubleSpend

	// ErrNeedsUpgrade describes an error during store opening where the
	// database contains an older version of the store.
	ErrNeedsUpgrade

	// ErrUnknownVersion describes an error where the store already exists
	// but the database version is newer than latest version known to this
	// software.  This likely indicates an outdated binary.
	ErrUnknownVersion

	// ErrIsClosed indicates that the transaction manager is closed.
	ErrIsClosed

	// ErrDuplicate describes an error inserting an item into the store due to
	// the data already existing.
	//
	// This error code is a late addition to the API and at the moment only a
	// select number of APIs use it.  Methods that might return this error
	// documents the behavior in a doc comment.
	ErrDuplicate

	// ErrSStxNotFound indicates that the requested tx hash is not known to
	// the SStx store.
	ErrSStxNotFound

	// ErrSSGensNotFound indicates that the requested tx hash is not known to
	// the SSGens store.
	ErrSSGensNotFound

	// ErrSSRtxsNotFound indicates that the requested tx hash is not known to
	// the SSRtxs store.
	ErrSSRtxsNotFound

	// ErrPoolUserTicketsNotFound indicates that the requested script hash
	// is not known to the meta bucket.
	ErrPoolUserTicketsNotFound

	// ErrPoolUserInvalTcktsNotFound indicates that the requested script hash
	// is not known to the meta bucket.
	ErrPoolUserInvalTcktsNotFound

	// ErrBadPoolUserAddr indicates that the passed pool user address was
	// faulty.
	ErrBadPoolUserAddr

	// ErrStoreClosed indicates that a function was called after the stake
	// store was closed.
	ErrStoreClosed
)

// E describes an application-level error.  An error code is provided to
// programically act on the error as well as a human-readable description of the
// error that can be presented to users or printed to logs.  If there was an
// underlying error, it is included in the Err field.
type E struct {
	ErrorCode   Code   // Describes the kind of error
	Description string // Human readable description of the issue
	Err         error  // Underlying error
}

// Error satisfies the error interface and prints human-readable errors.
func (e E) Error() string {
	if e.Err != nil {
		return e.Description + ": " + e.Err.Error()
	}
	return e.Description
}

// IsError returns whether the error is a ManagerError with a matching error
// code.
func IsError(err error, code Code) bool {
	e, ok := err.(E)
	return ok && e.ErrorCode == code
}
