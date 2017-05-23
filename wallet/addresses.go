// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

// DefaultGapLimit is the default unused address gap limit defined by BIP0044.
const DefaultGapLimit = 20

type addressBufferError int

type addressBuffer struct {
	branchXpub *hdkeychain.ExtendedKey
	lastUsed   uint32
	cursor     uint32 // added to lastUsed to derive child index
}

type bip0044AccountData struct {
	albExternal addressBuffer
	albInternal addressBuffer
}

func (w *Wallet) nextAddress(account, branch uint32) (dcrutil.Address, error) {
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

	if alb.cursor >= uint32(w.gapLimit) {
		// TODO: Ideally the user would be presented with this error and then
		// given the choice to either violate the gap limit and continue
		// deriving more addresses, or to wrap around to a previously generated
		// address.  Older wallet versions used wraparound without user consent,
		// so this behavior was kept.
		//
		//const str = "deriving additional addresses violates the unused address gap limit"
		//err := apperrors.E{ErrorCode: apperrors.ErrExceedsGapLimit, Description: str, Err: nil}
		//return nil, err
		alb.cursor = 0
	}

	for {
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
	m := make(map[uint32]children, lastAccount+1)
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
		m[account] = children{lastUsedExt, lastUsedInt}
	}

	// Update the buffer's last used child if it was updated, and then update
	// the cursor to point to the same child index relative to the new last used
	// index.  Request transaction notifications for future addresses that will
	// be returned by the buffer.
	errs := make(chan error, len(m))
	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()
	for account, a := range w.addressBuffers {
		storedLastUsed := m[account]
		deriveExt := storedLastUsed.external - a.albExternal.lastUsed
		deriveInt := storedLastUsed.internal - a.albInternal.lastUsed
		// Comparisons against 1<<31 are used to determine if there are a
		// "positive" or "negative" number of addresses to derive after
		// performing unsigned subtraction.  This works because the valid range
		// of child addresses is [0,1<<31).
		if (deriveExt == 0 || deriveExt >= 1<<31) && (deriveInt == 0 || deriveInt >= 1<<31) {
			errs <- nil
			continue
		}
		var childExt, childInt uint32
		if deriveExt < 1<<31 {
			a.albExternal.lastUsed = storedLastUsed.external
			a.albExternal.cursor -= minUint32(a.albExternal.cursor, deriveExt)
			childExt = a.albExternal.lastUsed + 1 + a.albExternal.cursor
		}
		if deriveInt < 1<<31 {
			a.albInternal.lastUsed = storedLastUsed.internal
			a.albInternal.cursor -= minUint32(a.albInternal.cursor, deriveInt)
			childInt = a.albInternal.lastUsed + 1 + a.albInternal.cursor
		}

		xpubBranchExt := a.albExternal.branchXpub
		xpubBranchInt := a.albInternal.branchXpub

		addrs := make([]dcrutil.Address, 0, deriveExt+deriveInt)
		for i := uint32(0); i < deriveExt; i, childExt = i+1, childExt+1 {
			// Only derive non-hardened keys.
			if childExt >= hdkeychain.HardenedKeyStart {
				break
			}
			childXpub, err := xpubBranchExt.Child(childExt)
			switch err {
			case nil:
			case hdkeychain.ErrInvalidChild:
				// Skip to the next child
				continue
			default:
				return err
			}
			addr, err := childXpub.Address(w.chainParams)
			if err != nil {
				return err
			}
			addrs = append(addrs, addr)
		}
		for i := uint32(0); i < deriveInt; i, childInt = i+1, childInt+1 {
			// As above but derive for the internal branch.
			if childInt >= hdkeychain.HardenedKeyStart {
				break
			}
			childXpub, err := xpubBranchInt.Child(childInt)
			switch err {
			case nil:
			case hdkeychain.ErrInvalidChild:
				continue
			default:
				return err
			}
			addr, err := childXpub.Address(w.chainParams)
			if err != nil {
				return err
			}
			addrs = append(addrs, addr)
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

// NewExternalAddress returns an unused external address.
func (w *Wallet) NewExternalAddress(account uint32) (dcrutil.Address, error) {
	return w.nextAddress(account, udb.ExternalBranch)
}

// NewInternalAddress returns an unused internal address.
func (w *Wallet) NewInternalAddress(account uint32) (dcrutil.Address, error) {
	return w.nextAddress(account, udb.InternalBranch)
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

func (w *Wallet) changeSource(account uint32) txauthor.ChangeSource {
	return func() ([]byte, uint16, error) {
		changeAddress, err := w.changeAddress(account)
		if err != nil {
			return nil, 0, err
		}
		script, err := txscript.PayToAddrScript(changeAddress)
		return script, txscript.DefaultScriptVersion, err
	}
}

func (w *Wallet) changeAddress(account uint32) (dcrutil.Address, error) {
	// Addresses can not be generated for the imported account, so as a
	// workaround, change is sent to the first account.
	//
	// Yep, our accounts are broken.
	if account == udb.ImportedAddrAccount {
		account = udb.DefaultAccountNum
	}
	return w.NewInternalAddress(account)
}

func deriveChildren(key *hdkeychain.ExtendedKey, startIndex, count uint32) ([]*hdkeychain.ExtendedKey, error) {
	children := make([]*hdkeychain.ExtendedKey, 0, count)
	for i := uint32(0); i < count; i++ {
		child, err := key.Child(i)
		if err == hdkeychain.ErrInvalidChild {
			continue
		}
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, nil
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
