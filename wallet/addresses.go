// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

// DefaultGapLimit is the default unused address gap limit defined by BIP0044.
const DefaultGapLimit = 20

// gapPolicy defines the policy to use when the BIP0044 address gap limit is
// exceeded.
type gapPolicy int

const (
	gapPolicyError gapPolicy = iota
	gapPolicyIgnore
	gapPolicyWrap
)

type nextAddressCallOptions struct {
	policy gapPolicy
}

// NextAddressCallOption defines a call option for the NextAddress family of
// wallet methods.
type NextAddressCallOption func(*nextAddressCallOptions)

func withGapPolicy(policy gapPolicy) NextAddressCallOption {
	return func(o *nextAddressCallOptions) {
		o.policy = policy
	}
}

// WithGapPolicyError configures the NextAddress family of methods to error
// whenever the gap limit would be exceeded.  When this default policy is used,
// callers should check errors against the apperrors.ErrExceedsGapLimit error
// code and let users specify whether to ignore the gap limit or wrap around to
// a previously returned address.
func WithGapPolicyError() NextAddressCallOption {
	return withGapPolicy(gapPolicyError)
}

// WithGapPolicyIgnore configures the NextAddress family of methods to ignore
// the gap limit entirely when generating addresses.  Exceeding the gap limit
// may result in unsynced address child indexes when seed restoring the wallet,
// unless the restoring gap limit is increased, as well as breaking automatic
// address synchronization of multiple running wallets.
//
// This is a good policy to use when addresses must never be reused, but be
// aware of the issues noted above.
func WithGapPolicyIgnore() NextAddressCallOption {
	return withGapPolicy(gapPolicyIgnore)
}

// WithGapPolicyWrap configures the NextAddress family of methods to wrap around
// to a previously returned address instead of erroring or ignoring the gap
// limit and returning a new unused address.
//
// This is a good policy to use for most individual users' wallets where funds
// are segmented by accounts and not the addresses that control each output.
func WithGapPolicyWrap() NextAddressCallOption {
	return withGapPolicy(gapPolicyWrap)
}

type addressBuffer struct {
	branchXpub *hdkeychain.ExtendedKey
	lastUsed   uint32
	// cursor is added to lastUsed to derive child index
	// warning: this is not decremented after errors, and therefore may refer
	// to children beyond the last returned child recorded in the database.
	cursor uint32
}

type bip0044AccountData struct {
	albExternal addressBuffer
	albInternal addressBuffer
}

// persistReturnedChildFunc is the function used by nextAddress to update the
// database with the child index of a returned address.  It is used to abstract
// the correct database access required depending on the caller context.
// Possible implementations can open a new database write transaction, use an
// already open database transaction, or defer the update until a later time
// when a write can be performed.
type persistReturnedChildFunc func(account, branch, child uint32) error

// persistReturnedChild returns a synchronous persistReturnedChildFunc which
// causes the DB update to occur before nextAddress returns.
//
// The returned function may be called either inside of a DB update (in which
// case maybeDBTX must be the non-nil transaction object) or not in any
// transaction at all (in which case it must be nil and the method opens a
// transaction).  It must never be called while inside a db view as this results
// in a deadlock situation.
func (w *Wallet) persistReturnedChild(maybeDBTX walletdb.ReadWriteTx) persistReturnedChildFunc {
	return func(account, branch, child uint32) (rerr error) {
		// Write the returned child index to the database, opening a write
		// transaction as necessary.
		if maybeDBTX == nil {
			var err error
			maybeDBTX, err = w.db.BeginReadWriteTx()
			if err != nil {
				return err
			}
			defer func() {
				if rerr == nil {
					rerr = maybeDBTX.Commit()
				} else {
					maybeDBTX.Rollback()
				}
			}()
		}
		return w.Manager.MarkReturnedChildIndex(maybeDBTX, account, branch, child)
	}
}

// deferPersistReturnedChild returns a persistReturnedChildFunc that is not
// immediately written to the database.  Instead, an update function is appended
// to the updates slice.  This allows all updates to be run under a single
// database update later and allows deferred child persistence even when
// generating addresess in a view (as long as the update is called after).
//
// This is preferable to running updates asynchronously using goroutines as it
// allows the updates to not be performed if a later error occurs and the child
// indexes should not be written.  It also allows the updates to be grouped
// together in a single atomic transaction.
func (w *Wallet) deferPersistReturnedChild(updates *[]func(walletdb.ReadWriteTx) error) persistReturnedChildFunc {
	// These vars are closed-over by the update function and modified by the
	// returned persist function.
	var account, branch, child uint32
	update := func(tx walletdb.ReadWriteTx) error {
		persist := w.persistReturnedChild(tx)
		return persist(account, branch, child)
	}
	*updates = append(*updates, update)
	return func(a, b, c uint32) error {
		account, branch, child = a, b, c
		return nil
	}
}

// nextAddress returns the next address of an account branch.
func (w *Wallet) nextAddress(persist persistReturnedChildFunc, account, branch uint32,
	callOpts ...NextAddressCallOption) (dcrutil.Address, error) {

	var opts nextAddressCallOptions // TODO: zero values for now, add to wallet config later.
	for _, c := range callOpts {
		c(&opts)
	}

	gapLimit := uint32(w.gapLimit)

	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()
	ad, ok := w.addressBuffers[account]
	if !ok {
		const str = "account not found"
		return nil, apperrors.E{ErrorCode: apperrors.ErrAccountNotFound, Description: str, Err: nil}
	}

	var alb *addressBuffer
	switch branch {
	case udb.ExternalBranch:
		alb = &ad.albExternal
	case udb.InternalBranch:
		alb = &ad.albInternal
	default:
		const str = "branch must be external (0) or internal (1)"
		err := apperrors.E{ErrorCode: apperrors.ErrBranch, Description: str, Err: nil}
		return nil, err
	}

	for {
		if alb.cursor >= gapLimit {
			switch opts.policy {
			case gapPolicyError:
				const str = "deriving additional addresses violates the unused address gap limit"
				err := apperrors.E{ErrorCode: apperrors.ErrExceedsGapLimit, Description: str, Err: nil}
				return nil, err

			case gapPolicyIgnore:
				// Addresses beyond the last used child + gap limit are not
				// already watched, so this must be done now if the wallet is
				// connected to a consensus RPC server.  Watch addresses in
				// batches of the gap limit at a time to avoid introducing many
				// RPCs from repeated new address calls.
				if alb.cursor%uint32(w.gapLimit) != 0 {
					break
				}
				chainClient := w.ChainClient()
				if chainClient == nil {
					break
				}
				addrs, err := deriveChildAddresses(alb.branchXpub,
					alb.lastUsed+1+alb.cursor, gapLimit, w.chainParams)
				if err != nil {
					return nil, err
				}
				err = chainClient.LoadTxFilter(false, addrs, nil)
				if err != nil {
					return nil, err
				}

			case gapPolicyWrap:
				alb.cursor = 0
			}
		}

		childIndex := alb.lastUsed + 1 + alb.cursor
		if childIndex >= hdkeychain.HardenedKeyStart {
			const str = "no more addresses can be derived for the account"
			err := apperrors.E{ErrorCode: apperrors.ErrExhaustedAccount, Description: str, Err: nil}
			return nil, err
		}
		child, err := alb.branchXpub.Child(childIndex)
		if err != nil {
			return nil, err
		}
		addr, err := child.Address(w.chainParams)
		switch err {
		case nil:
			// Write the returned child index to the database.
			err := persist(account, branch, childIndex)
			if err != nil {
				return nil, err
			}
			alb.cursor++
			return addr, nil
		case hdkeychain.ErrInvalidChild:
			alb.cursor++
			continue
		default:
			return nil, err
		}
	}
}

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

// markUsedAddress updates the database, recording that the previously looked up
// managed address has been publicly used.  After recording this usage, new
// addresses are derived and saved to the db.
func (w *Wallet) markUsedAddress(dbtx walletdb.ReadWriteTx, addr udb.ManagedAddress) error {
	ns := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	account := addr.Account()
	err := w.Manager.MarkUsed(ns, addr.Address())
	if err != nil {
		return err
	}
	if account == udb.ImportedAddrAccount {
		return nil
	}
	props, err := w.Manager.AccountProperties(ns, account)
	if err != nil {
		return err
	}
	lastUsed := props.LastUsedExternalIndex
	branch := udb.ExternalBranch
	if addr.Internal() {
		lastUsed = props.LastUsedInternalIndex
		branch = udb.InternalBranch
	}
	return w.Manager.SyncAccountToAddrIndex(ns, account,
		minUint32(hdkeychain.HardenedKeyStart-1, lastUsed+uint32(w.gapLimit)),
		branch)
}

func (w *Wallet) watchFutureAddresses(dbtx walletdb.ReadTx) error {
	// TODO: There is room here for optimization.  Improvements could be made by
	// keeping track of all accounts that have been updated and how many more
	// addresses must be generated when marking addresses as used so only those
	// need to be updated.

	gapLimit := uint32(w.gapLimit)

	client, err := w.requireChainClient()
	if err != nil {
		return err
	}

	type children struct {
		external uint32
		internal uint32
	}
	ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
	lastAccount, err := w.Manager.LastAccount(ns)
	if err != nil {
		return err
	}
	dbLastUsedChildren := make(map[uint32]children, lastAccount+1)
	var lastUsedExt, lastUsedInt uint32
	for account := uint32(0); account <= lastAccount; account++ {
		for branch := udb.ExternalBranch; branch <= udb.InternalBranch; branch++ {
			props, err := w.Manager.AccountProperties(ns, account)
			if err != nil {
				return err
			}
			switch branch {
			case udb.ExternalBranch:
				lastUsedExt = props.LastUsedExternalIndex
			case udb.InternalBranch:
				lastUsedInt = props.LastUsedInternalIndex
			}
		}
		dbLastUsedChildren[account] = children{lastUsedExt, lastUsedInt}
	}

	// Update the buffer's last used child if it was updated, and then update
	// the cursor to point to the same child index relative to the new last used
	// index.  Request transaction notifications for future addresses that will
	// be returned by the buffer.
	errs := make(chan error, lastAccount+1)
	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()
	for account, a := range w.addressBuffers {
		// startExt/Int are the indexes of the next child after the last
		// currently watched address.
		startExt := a.albExternal.lastUsed + 1 + gapLimit
		startInt := a.albInternal.lastUsed + 1 + gapLimit

		// endExt/Int are the end indexes for newly watched addresses.  Because
		// addresses ranges are described using a half open range, these indexes
		// are one beyond the last address that will be watched.
		dbLastUsed := dbLastUsedChildren[account]
		endExt := dbLastUsed.external + 1 + gapLimit
		endInt := dbLastUsed.internal + 1 + gapLimit

		xpubBranchExt := a.albExternal.branchXpub
		xpubBranchInt := a.albInternal.branchXpub

		// Create a slice of all new addresses that must be watched.
		var totalAddrs uint32
		if endExt > startExt {
			totalAddrs += endExt - startExt
		}
		if endInt > startInt {
			totalAddrs += endInt - startInt
		}
		if totalAddrs == 0 {
			errs <- nil
			continue
		}
		addrs := make([]dcrutil.Address, 0, totalAddrs)
		err := appendChildAddrsRange(&addrs, xpubBranchExt, startExt, endExt,
			w.chainParams)
		if err != nil {
			return err
		}
		err = appendChildAddrsRange(&addrs, xpubBranchInt, startInt, endInt,
			w.chainParams)
		if err != nil {
			return err
		}

		// Update the in-memory address buffers with the latest last used
		// indexes retreived from the db.
		if endExt > startExt {
			a.albExternal.lastUsed = dbLastUsed.external
			a.albExternal.cursor -= minUint32(a.albExternal.cursor, endExt-startExt)
		}
		if endInt > startInt {
			a.albInternal.lastUsed = dbLastUsed.internal
			a.albInternal.cursor -= minUint32(a.albInternal.cursor, endInt-startInt)
		}

		go func() {
			errs <- client.LoadTxFilter(false, addrs, nil)
		}()
	}

	for i := 0; i < cap(errs); i++ {
		err := <-errs
		if err != nil {
			return err
		}
	}
	return nil
}

// NewExternalAddress returns an external address.
func (w *Wallet) NewExternalAddress(account uint32, callOpts ...NextAddressCallOption) (dcrutil.Address, error) {
	return w.nextAddress(w.persistReturnedChild(nil), account, udb.ExternalBranch, callOpts...)
}

// NewInternalAddress returns an internal address.
func (w *Wallet) NewInternalAddress(account uint32, callOpts ...NextAddressCallOption) (dcrutil.Address, error) {
	return w.nextAddress(w.persistReturnedChild(nil), account, udb.InternalBranch, callOpts...)
}

func (w *Wallet) newChangeAddress(persist persistReturnedChildFunc, account uint32) (dcrutil.Address, error) {
	// Addresses can not be generated for the imported account, so as a
	// workaround, change is sent to the first account.
	//
	// Yep, our accounts are broken.
	if account == udb.ImportedAddrAccount {
		account = udb.DefaultAccountNum
	}
	return w.nextAddress(persist, account, udb.InternalBranch, WithGapPolicyWrap())
}

// NewChangeAddress returns an internal address.  This is identical to
// NewInternalAddress but handles the imported account (which can't create
// addresses) by using account 0 instead, and always uses the wrapping gap limit
// policy.
func (w *Wallet) NewChangeAddress(account uint32) (dcrutil.Address, error) {
	return w.newChangeAddress(w.persistReturnedChild(nil), account)
}

// BIP0044BranchNextIndexes returns the next external and internal branch child
// indexes of an account.
func (w *Wallet) BIP0044BranchNextIndexes(account uint32) (extChild, intChild uint32, err error) {
	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()

	acctData, ok := w.addressBuffers[account]
	if !ok {
		const str = "account not found"
		return 0, 0, apperrors.E{ErrorCode: apperrors.ErrAccountNotFound, Description: str, Err: nil}
	}
	extChild = acctData.albExternal.lastUsed + 1 + acctData.albExternal.cursor
	intChild = acctData.albInternal.lastUsed + 1 + acctData.albInternal.cursor
	return extChild, intChild, nil
}

// ExtendWatchedAddresses derives and watches additional addresses for an
// account branch they have not yet been derived.  This does not modify the next
// generated address for the branch.
func (w *Wallet) ExtendWatchedAddresses(account, branch, child uint32) error {
	var (
		branchXpub *hdkeychain.ExtendedKey
		lastUsed   uint32
	)
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		defer w.addressBuffersMu.Unlock()
		w.addressBuffersMu.Lock()

		acctData, ok := w.addressBuffers[account]
		if !ok {
			const str = "account not found"
			return apperrors.E{ErrorCode: apperrors.ErrAccountNotFound, Description: str, Err: nil}
		}
		var alb *addressBuffer
		switch branch {
		case udb.ExternalBranch:
			alb = &acctData.albInternal
		case udb.InternalBranch:
			alb = &acctData.albInternal
		default:
			const str = "branch must be external or internal"
			return apperrors.E{ErrorCode: apperrors.ErrBranch, Description: str, Err: nil}
		}

		branchXpub = alb.branchXpub
		lastUsed = alb.lastUsed

		if child < lastUsed+uint32(w.gapLimit) {
			return nil
		}
		ns := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		return w.Manager.SyncAccountToAddrIndex(ns, account, child, branch)
	})
	if err != nil {
		return err
	}

	if client := w.ChainClient(); client != nil {
		gapLimit := uint32(w.gapLimit)
		lastWatched := lastUsed + gapLimit
		if child <= lastWatched {
			// No need to derive anything more.
			return nil
		}
		additionalAddrs := child - lastWatched
		addrs, err := deriveChildAddresses(branchXpub, lastUsed+1+gapLimit,
			additionalAddrs, w.chainParams)
		if err != nil {
			return err
		}
		err = client.LoadTxFilter(false, addrs, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

// AccountBranchAddressRange returns all addresses in the range [start, end)
// belonging to the BIP0044 account and address branch.
func (w *Wallet) AccountBranchAddressRange(account, branch, start, end uint32) ([]dcrutil.Address, error) {
	if end < start {
		const str = "end index must not be less than start index"
		return nil, apperrors.E{ErrorCode: apperrors.ErrInput, Description: str, Err: nil}
	}

	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()

	acctBufs, ok := w.addressBuffers[account]
	if !ok {
		const str = "account not found"
		return nil, apperrors.E{ErrorCode: apperrors.ErrAccountNotFound, Description: str, Err: nil}
	}

	var buf *addressBuffer
	switch branch {
	case udb.ExternalBranch:
		buf = &acctBufs.albExternal
	case udb.InternalBranch:
		buf = &acctBufs.albInternal
	default:
		const str = "unknown branch"
		return nil, apperrors.E{ErrorCode: apperrors.ErrBranch, Description: str, Err: nil}
	}
	return deriveChildAddresses(buf.branchXpub, start, end-start, w.chainParams)
}

func (w *Wallet) changeSource(persist persistReturnedChildFunc, account uint32) txauthor.ChangeSource {
	return func() ([]byte, uint16, error) {
		changeAddress, err := w.newChangeAddress(persist, account)
		if err != nil {
			return nil, 0, err
		}
		script, err := txscript.PayToAddrScript(changeAddress)
		return script, txscript.DefaultScriptVersion, err
	}
}

func deriveChildAddresses(key *hdkeychain.ExtendedKey, startIndex, count uint32, params *chaincfg.Params) ([]dcrutil.Address, error) {
	addresses := make([]dcrutil.Address, 0, count)
	for i := uint32(0); i < count; i++ {
		child, err := key.Child(startIndex + i)
		if err == hdkeychain.ErrInvalidChild {
			continue
		}
		if err != nil {
			return nil, err
		}
		addr, err := child.Address(params)
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, addr)
	}
	return addresses, nil
}

func deriveChildAddress(key *hdkeychain.ExtendedKey, child uint32, params *chaincfg.Params) (dcrutil.Address, error) {
	childKey, err := key.Child(child)
	if err != nil {
		return nil, err
	}
	return childKey.Address(params)
}

func deriveBranches(acctXpub *hdkeychain.ExtendedKey) (extKey, intKey *hdkeychain.ExtendedKey, err error) {
	extKey, err = acctXpub.Child(udb.ExternalBranch)
	if err != nil {
		return
	}
	intKey, err = acctXpub.Child(udb.InternalBranch)
	return
}

// appendChildAddrsRange appends non-hardened child addresses from the range
// [a,b) to the addrs slice.  If a child is unusable, it is skipped, so the
// total number of addresses appended may not be exactly b-a.
func appendChildAddrsRange(addrs *[]dcrutil.Address, key *hdkeychain.ExtendedKey,
	a, b uint32, params *chaincfg.Params) error {

	for ; a < b && a < hdkeychain.HardenedKeyStart; a++ {
		addr, err := deriveChildAddress(key, a, params)
		if err == hdkeychain.ErrInvalidChild {
			continue
		}
		if err != nil {
			return err
		}
		*addrs = append(*addrs, addr)
	}
	return nil
}
