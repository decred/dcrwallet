// Copyright (c) 2017-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"encoding/binary"
	"runtime/trace"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/internal/compat"
	"github.com/decred/dcrwallet/wallet/v3/internal/txsizes"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// V0Scripter is a type (usually addresses) which create or encode to version 0
// output scripts.
type V0Scripter interface {
	ScriptV0() []byte
}

// SecpPubKeyHasher is any address used to create scripts paying to the hash of
// a secp256k1 pubkey, requiring the pubkey and a signature to redeem.
type SecpPubKeyHasher interface {
	SecpPubKeyHash(version uint16) ([]byte, error)
}

// SecpPubKeyer is any type (usually addresses) where a secp256k1 public key is
// known.
type SecpPubKeyer interface {
	SecpPubKey() []byte
}

// SecpPubKeyHash160er is any address created from the HASH160 of a pubkey.
type SecpPubKeyHash160er interface {
	SecpPubKeyHash160() *[20]byte
}

// AddressP2PKHv0 is a union of interfaces implemented by version 0 P2PKH
// addresses.
type AddressP2PKHv0 interface {
	dcrutil.Address
	V0Scripter
	SecpPubKeyHasher
	SecpPubKeyHash160er
}

// BIP44AccountXpubPather is any address which is derived from a BIP0044
// extended pubkey.
type BIP44AccountXpubPather interface {
	BIP44AccountXpubPath() (xpub *hdkeychain.ExtendedKey, branch, child uint32)
}

// BIP44XpubP2PKHv0 is a union of interfaces implemented by version 0 P2PKH
// addresses derived from a known BIP0044 account xpub.
type BIP44XpubP2PKHv0 interface {
	AddressP2PKHv0
	SecpPubKeyer
	BIP44AccountXpubPather
}

type stakeAddress interface {
	voteRights() (script []byte, version uint16)
	ticketChange() (script []byte, version uint16)
	rewardCommitment(amount dcrutil.Amount, limits uint16) (script []byte, version uint16)
	payVoteCommitment() (script []byte, version uint16)
	payRevokeCommitment() (script []byte, version uint16)
}

type xpubAddress struct {
	*dcrutil.AddressPubKeyHash
	xpub   *hdkeychain.ExtendedKey
	branch uint32
	child  uint32
}

var _ BIP44XpubP2PKHv0 = (*xpubAddress)(nil)
var _ stakeAddress = (*xpubAddress)(nil)

func (x *xpubAddress) ScriptV0() []byte {
	s := []byte{
		0:  txscript.OP_DUP,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
		24: txscript.OP_CHECKSIG,
	}
	copy(s[3:23], x.Hash160()[:])
	return s
}

func (x *xpubAddress) voteRights() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSTX,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.SecpPubKeyHash160()[:])
	return s, 0
}

func (x *xpubAddress) ticketChange() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSTXCHANGE,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.SecpPubKeyHash160()[:])
	return s, 0
}

func (x *xpubAddress) rewardCommitment(amount dcrutil.Amount, limits uint16) ([]byte, uint16) {
	s := make([]byte, 32)
	s[0] = txscript.OP_RETURN
	s[1] = txscript.OP_DATA_30
	copy(s[2:22], x.SecpPubKeyHash160()[:])
	binary.LittleEndian.PutUint64(s[22:30], uint64(amount))
	binary.LittleEndian.PutUint16(s[30:32], limits)
	return s, 0
}

func (x *xpubAddress) payVoteCommitment() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSGEN,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.SecpPubKeyHash160()[:])
	return s, 0
}

func (x *xpubAddress) payRevokeCommitment() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSRTX,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.SecpPubKeyHash160()[:])
	return s, 0
}

func (x *xpubAddress) SecpPubKey() []byte {
	// All errors are unexpected, since the P2PKH address must have already
	// been created from the same path.
	branchKey, err := x.xpub.Child(x.branch)
	if err != nil {
		panic(err)
	}
	childKey, err := branchKey.Child(x.child)
	if err != nil {
		panic(err)
	}
	pubkey, err := childKey.ECPubKey()
	if err != nil {
		panic(err)
	}
	return pubkey.SerializeCompressed()
}

func (x *xpubAddress) SecpPubKeyHash160() *[20]byte {
	return x.Hash160()
}

func (x *xpubAddress) SecpPubKeyHash(version uint16) ([]byte, error) {
	switch version {
	case 0:
		return x.Hash160()[:], nil
	default:
		return nil, errors.New("unrecognized script version")
	}
}

func (x *xpubAddress) BIP44AccountXpubPath() (xpub *hdkeychain.ExtendedKey, branch, child uint32) {
	return x.xpub, x.branch, x.child
}

// addressScript returns an output script paying to address.  This func is
// always preferred over direct usage of txscript.PayToAddrScript due to the
// latter failing on unexpected concrete types.
func addressScript(addr dcrutil.Address) (pkScript []byte, version uint16, err error) {
	switch addr := addr.(type) {
	case V0Scripter:
		return addr.ScriptV0(), 0, nil
	default:
		pkScript, err = txscript.PayToAddrScript(addr)
		return pkScript, 0, err
	}
}

func voteRightsScript(addr dcrutil.Address) (script []byte, version uint16, err error) {
	switch addr := addr.(type) {
	case stakeAddress:
		script, version = addr.voteRights()
	default:
		script, err = txscript.PayToSStx(addr)
	}
	return
}

func ticketChangeScript(addr dcrutil.Address) (script []byte, version uint16, err error) {
	switch addr := addr.(type) {
	case stakeAddress:
		script, version = addr.ticketChange()
	default:
		script, err = txscript.PayToSStxChange(addr)
	}
	return
}

func rewardCommitment(addr dcrutil.Address, amount dcrutil.Amount, limits uint16) (script []byte, version uint16, err error) {
	switch addr := addr.(type) {
	case stakeAddress:
		script, version = addr.rewardCommitment(amount, limits)
	default:
		script, err = txscript.GenerateSStxAddrPush(addr, amount, limits)
	}
	return
}

func payVoteCommitment(addr dcrutil.Address) (script []byte, version uint16, err error) {
	switch addr := addr.(type) {
	case stakeAddress:
		script, version = addr.payVoteCommitment()
	default:
		script, err = txscript.PayToSSGen(addr)
	}
	return
}

func payRevokeCommitment(addr dcrutil.Address) (script []byte, version uint16, err error) {
	switch addr := addr.(type) {
	case stakeAddress:
		script, version = addr.payRevokeCommitment()
	default:
		script, err = txscript.PayToSSRtx(addr)
	}
	return
}

// DefaultGapLimit is the default unused address gap limit defined by BIP0044.
const DefaultGapLimit = 20

// DefaultAccountGapLimit is the default number of accounts that can be
// created in a row without using any of them
const DefaultAccountGapLimit = 10

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
// callers should check errors against the GapLimit error code and let users
// specify whether to ignore the gap limit or wrap around to a previously
// returned address.
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
	xpub        *hdkeychain.ExtendedKey
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
func (w *Wallet) persistReturnedChild(ctx context.Context, maybeDBTX walletdb.ReadWriteTx) persistReturnedChildFunc {
	return func(account, branch, child uint32) (rerr error) {
		// Write the returned child index to the database, opening a write
		// transaction as necessary.
		if maybeDBTX == nil {
			var err error
			defer trace.StartRegion(ctx, "db.Update").End()
			maybeDBTX, err = w.db.BeginReadWriteTx()
			if err != nil {
				return err
			}
			region := trace.StartRegion(ctx, "db.ReadWriteTx")
			defer func() {
				if rerr == nil {
					rerr = maybeDBTX.Commit()
				} else {
					maybeDBTX.Rollback()
				}
				region.End()
			}()
		}
		ns := maybeDBTX.ReadWriteBucket(waddrmgrNamespaceKey)
		err := w.Manager.SyncAccountToAddrIndex(ns, account, child, branch)
		if err != nil {
			return err
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
func (w *Wallet) deferPersistReturnedChild(ctx context.Context, updates *[]func(walletdb.ReadWriteTx) error) persistReturnedChildFunc {
	// These vars are closed-over by the update function and modified by the
	// returned persist function.
	var account, branch, child uint32
	update := func(tx walletdb.ReadWriteTx) error {
		persist := w.persistReturnedChild(ctx, tx)
		return persist(account, branch, child)
	}
	*updates = append(*updates, update)
	return func(a, b, c uint32) error {
		account, branch, child = a, b, c
		return nil
	}
}

// nextAddress returns the next address of an account branch.
func (w *Wallet) nextAddress(ctx context.Context, op errors.Op, persist persistReturnedChildFunc, account, branch uint32,
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
		return nil, errors.E(op, errors.NotExist, errors.Errorf("account %d", account))
	}

	var alb *addressBuffer
	switch branch {
	case udb.ExternalBranch:
		alb = &ad.albExternal
	case udb.InternalBranch:
		alb = &ad.albInternal
	default:
		return nil, errors.E(op, errors.Invalid, "branch must be external (0) or internal (1)")
	}

	for {
		if alb.cursor >= gapLimit {
			switch opts.policy {
			case gapPolicyError:
				return nil, errors.E(op, errors.Policy,
					"generating next address violates the unused address gap limit policy")

			case gapPolicyIgnore:
				// Addresses beyond the last used child + gap limit are not
				// already watched, so this must be done now if the wallet is
				// connected to a consensus RPC server.  Watch addresses in
				// batches of the gap limit at a time to avoid introducing many
				// RPCs from repeated new address calls.
				if alb.cursor%uint32(w.gapLimit) != 0 {
					break
				}
				n, err := w.NetworkBackend()
				if err != nil {
					break
				}
				addrs, err := deriveChildAddresses(alb.branchXpub,
					alb.lastUsed+1+alb.cursor, gapLimit, w.chainParams)
				if err != nil {
					return nil, errors.E(op, err)
				}
				err = n.LoadTxFilter(ctx, false, addrs, nil)
				if err != nil {
					return nil, err
				}

			case gapPolicyWrap:
				alb.cursor = 0
			}
		}

		childIndex := alb.lastUsed + 1 + alb.cursor
		if childIndex >= hdkeychain.HardenedKeyStart {
			return nil, errors.E(op, errors.Errorf("account %d branch %d exhausted",
				account, branch))
		}
		child, err := alb.branchXpub.Child(childIndex)
		if errors.Is(err, hdkeychain.ErrInvalidChild) {
			alb.cursor++
			continue
		}
		if err != nil {
			return nil, errors.E(op, err)
		}
		apkh, err := compat.HD2Address(child, w.chainParams)
		if err != nil {
			return nil, errors.E(op, err)
		}
		// Write the returned child index to the database.
		err = persist(account, branch, childIndex)
		if err != nil {
			return nil, err
		}
		alb.cursor++
		addr := &xpubAddress{
			AddressPubKeyHash: apkh,
			xpub:              ad.xpub,
			branch:            branch,
			child:             childIndex,
		}
		log.Infof("Returning address (account=%v branch=%v child=%v)", account, branch, childIndex)
		return addr, nil
	}
}

func (w *Wallet) nextImportedXpubAddress(ctx context.Context, op errors.Op, maybeDBTX walletdb.ReadWriteTx,
	account uint32, branch uint32, callOpts ...NextAddressCallOption) (addr dcrutil.Address, err error) {

	dbtx := maybeDBTX
	if dbtx == nil {
		dbtx, err = w.db.BeginReadWriteTx()
		if err != nil {
			return nil, err
		}
		defer func() {
			if err == nil {
				err = dbtx.Commit()
			} else {
				dbtx.Rollback()
			}
		}()
	}

	ns := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	xpub, err := w.Manager.AccountExtendedPubKey(dbtx, account)
	if err != nil {
		return nil, errors.E(op, err)
	}
	props, err := w.Manager.AccountProperties(ns, account)
	branchKey, err := xpub.Child(branch)
	if err != nil {
		return nil, errors.E(op, err)
	}
	var childKey *hdkeychain.ExtendedKey
	var child uint32
	switch branch {
	case 0:
		child = props.LastReturnedExternalIndex + 1
	case 1:
		child = props.LastReturnedInternalIndex + 1
	default:
		return nil, errors.E(op, "branch is required to be 0 or 1")
	}
	for {
		childKey, err = branchKey.Child(child)
		if err == hdkeychain.ErrInvalidChild {
			child++
			continue
		}
		if err != nil {
			return nil, errors.E(op, err)
		}
		break
	}
	pk, err := childKey.ECPubKey()
	if err != nil {
		return nil, errors.E(op, err)
	}
	pkh := dcrutil.Hash160(pk.Serialize())
	apkh, err := dcrutil.NewAddressPubKeyHash(pkh, w.chainParams,
		dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, errors.E(op, err)
	}
	addr = &xpubAddress{
		AddressPubKeyHash: apkh,
		xpub:              xpub,
		branch:            branch,
		child:             child,
	}
	err = w.Manager.MarkReturnedChildIndex(dbtx, account, branch, child)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return addr, nil
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
func (w *Wallet) markUsedAddress(op errors.Op, dbtx walletdb.ReadWriteTx, addr udb.ManagedAddress) error {
	ns := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	account := addr.Account()
	err := w.Manager.MarkUsed(ns, addr.Address())
	if err != nil {
		return errors.E(op, err)
	}
	if account == udb.ImportedAddrAccount {
		return nil
	}
	props, err := w.Manager.AccountProperties(ns, account)
	if err != nil {
		return errors.E(op, err)
	}
	lastUsed := props.LastUsedExternalIndex
	branch := udb.ExternalBranch
	if addr.Internal() {
		lastUsed = props.LastUsedInternalIndex
		branch = udb.InternalBranch
	}
	err = w.Manager.SyncAccountToAddrIndex(ns, account,
		minUint32(hdkeychain.HardenedKeyStart-1, lastUsed+uint32(w.gapLimit)),
		branch)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// watchFutureAddresses loads the transaction filter with future addresses, one
// gap limit beyond the last used address.
//
// This method requires that the wallet's addressBuffersMu held be held, prior
// to opening the database transaction.
func (w *Wallet) watchFutureAddresses(ctx context.Context, dbtx walletdb.ReadTx) error {
	const op errors.Op = "wallet.watchFutureAddresses"

	// TODO: There is room here for optimization.  Improvements could be made by
	// keeping track of all accounts that have been updated and how many more
	// addresses must be generated when marking addresses as used so only those
	// need to be updated.

	gapLimit := uint32(w.gapLimit)

	n, err := w.NetworkBackend()
	if err != nil {
		return errors.E(op, err)
	}

	type children struct {
		external uint32
		internal uint32
	}
	ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
	lastAccount, err := w.Manager.LastAccount(ns)
	if err != nil {
		return errors.E(op, err)
	}
	lastImportedAccount, err := w.Manager.LastImportedAccount(dbtx)
	if err != nil {
		return errors.E(op, err)
	}
	numAccounts := lastAccount + 1 + (lastImportedAccount - udb.ImportedAddrAccount)
	dbLastUsedChildren := make(map[uint32]children, numAccounts)
	dbLastRetChildren := make(map[uint32]children, numAccounts)
	var lastUsedExt, lastUsedInt uint32
	var lastRetExt, lastRetInt uint32
	readIndexes := func(account uint32) error {
		for branch := udb.ExternalBranch; branch <= udb.InternalBranch; branch++ {
			props, err := w.Manager.AccountProperties(ns, account)
			if err != nil {
				return err
			}
			switch branch {
			case udb.ExternalBranch:
				lastUsedExt = props.LastUsedExternalIndex
				lastRetExt = props.LastReturnedExternalIndex
			case udb.InternalBranch:
				lastUsedInt = props.LastUsedInternalIndex
				lastRetInt = props.LastReturnedInternalIndex
			}
		}
		dbLastUsedChildren[account] = children{lastUsedExt, lastUsedInt}
		dbLastRetChildren[account] = children{lastRetExt, lastRetInt}
		return nil
	}
	for account := uint32(0); account <= lastAccount; account++ {
		err := readIndexes(account)
		if err != nil {
			return errors.E(op, err)
		}
	}
	for account := uint32(udb.ImportedAddrAccount + 1); account <= lastImportedAccount; account++ {
		err := readIndexes(account)
		if err != nil {
			return errors.E(op, err)
		}
	}

	// Update the buffer's last used child if it was updated, and then update
	// the cursor to point to the same child index relative to the new last used
	// index.  Request transaction notifications for future addresses that will
	// be returned by the buffer.
	errs := make(chan error, len(w.addressBuffers))
	for account, a := range w.addressBuffers {
		// startExt/Int are the indexes of the next child after the last
		// currently watched address.
		startExt := a.albExternal.lastUsed + 1 + gapLimit
		startInt := a.albInternal.lastUsed + 1 + gapLimit

		dbLastUsed := dbLastUsedChildren[account]
		dbLastRet := dbLastRetChildren[account]

		// endExt/Int are the end indexes for newly watched addresses.  Because
		// addresses ranges are described using a half open range, these indexes
		// are one beyond the last address that will be watched.
		endExt := dbLastRet.external + 1 + gapLimit
		endInt := dbLastRet.internal + 1 + gapLimit

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
			return errors.E(op, err)
		}
		err = appendChildAddrsRange(&addrs, xpubBranchInt, startInt, endInt,
			w.chainParams)
		if err != nil {
			return errors.E(op, err)
		}

		// Update the in-memory address buffers with the latest last
		// used and last returned indexes retreived from the db.
		if endExt > startExt {
			a.albExternal.lastUsed = dbLastUsed.external
			a.albExternal.cursor = dbLastRet.external - dbLastUsed.external
		}
		if endInt > startInt {
			a.albInternal.lastUsed = dbLastUsed.internal
			a.albInternal.cursor = dbLastRet.internal - dbLastUsed.internal
		}

		go func() {
			errs <- n.LoadTxFilter(ctx, false, addrs, nil)
		}()
	}

	for i := 0; i < cap(errs); i++ {
		err := <-errs
		if err != nil {
			return errors.E(op, err)
		}
	}
	return nil
}

// NewExternalAddress returns an external address.
func (w *Wallet) NewExternalAddress(ctx context.Context, account uint32, callOpts ...NextAddressCallOption) (dcrutil.Address, error) {
	const op errors.Op = "wallet.NewExternalAddress"
	return w.nextAddress(ctx, op, w.persistReturnedChild(ctx, nil), account, udb.ExternalBranch, callOpts...)
}

// NewInternalAddress returns an internal address.
func (w *Wallet) NewInternalAddress(ctx context.Context, account uint32, callOpts ...NextAddressCallOption) (dcrutil.Address, error) {
	const op errors.Op = "wallet.NewExternalAddress"
	return w.nextAddress(ctx, op, w.persistReturnedChild(ctx, nil), account, udb.InternalBranch, callOpts...)
}

func (w *Wallet) newChangeAddress(ctx context.Context, op errors.Op, persist persistReturnedChildFunc, account uint32, gap gapPolicy) (dcrutil.Address, error) {
	// Addresses can not be generated for the imported account, so as a
	// workaround, change is sent to the first account.
	//
	// Yep, our accounts are broken.
	if account == udb.ImportedAddrAccount {
		account = udb.DefaultAccountNum
	}
	return w.nextAddress(ctx, op, persist, account, udb.InternalBranch, withGapPolicy(gap))
}

// NewChangeAddress returns an internal address.  This is identical to
// NewInternalAddress but handles the imported account (which can't create
// addresses) by using account 0 instead, and always uses the wrapping gap limit
// policy.
func (w *Wallet) NewChangeAddress(ctx context.Context, account uint32) (dcrutil.Address, error) {
	const op errors.Op = "wallet.NewChangeAddress"
	return w.newChangeAddress(ctx, op, w.persistReturnedChild(ctx, nil), account, gapPolicyWrap)
}

// BIP0044BranchNextIndexes returns the next external and internal branch child
// indexes of an account.
func (w *Wallet) BIP0044BranchNextIndexes(ctx context.Context, account uint32) (extChild, intChild uint32, err error) {
	const op errors.Op = "wallet.BIP0044BranchNextIndexes"

	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()

	acctData, ok := w.addressBuffers[account]
	if !ok {
		return 0, 0, errors.E(op, errors.NotExist, errors.Errorf("account %v", account))
	}
	extChild = acctData.albExternal.lastUsed + 1 + acctData.albExternal.cursor
	intChild = acctData.albInternal.lastUsed + 1 + acctData.albInternal.cursor
	return extChild, intChild, nil
}

// SyncLastReturnedAddress advances the last returned child address for a
// BIP00044 account branch.  The next returned address for the branch will be
// child+1.
func (w *Wallet) SyncLastReturnedAddress(ctx context.Context, account, branch, child uint32) error {
	const op errors.Op = "wallet.ExtendWatchedAddresses"

	var (
		branchXpub *hdkeychain.ExtendedKey
		lastUsed   uint32
	)
	err := func() error {
		defer w.addressBuffersMu.Unlock()
		w.addressBuffersMu.Lock()

		acctData, ok := w.addressBuffers[account]
		if !ok {
			return errors.E(op, errors.NotExist, errors.Errorf("account %v", account))
		}
		var alb *addressBuffer
		switch branch {
		case udb.ExternalBranch:
			alb = &acctData.albInternal
		case udb.InternalBranch:
			alb = &acctData.albInternal
		default:
			return errors.E(op, errors.Invalid, "branch must be external (0) or internal (1)")
		}

		branchXpub = alb.branchXpub
		lastUsed = alb.lastUsed
		if lastUsed != ^uint32(0) && child > lastUsed {
			alb.cursor = child - lastUsed
		}
		return nil
	}()
	if err != nil {
		return err
	}

	err = walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		err = w.Manager.SyncAccountToAddrIndex(ns, account, child, branch)
		if err != nil {
			return err
		}
		return w.Manager.MarkReturnedChildIndex(tx, account, branch, child)
	})
	if err != nil {
		return err
	}

	if n, err := w.NetworkBackend(); err == nil {
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
			return errors.E(op, err)
		}
		err = n.LoadTxFilter(ctx, false, addrs, nil)
		if err != nil {
			return errors.E(op, err)
		}
	}

	return nil
}

// AccountBranchAddressRange returns all addresses in the range [start, end)
// belonging to the BIP0044 account and address branch.
//
// Deprecated: Dump the xpub and perform this yourself.  The addresses returned by
// this function do not implement all of the wallet's address interfaces.
func (w *Wallet) AccountBranchAddressRange(account, branch, start, end uint32) ([]dcrutil.Address, error) {
	const op errors.Op = "wallet.AccountBranchAddressRange"

	if end < start {
		return nil, errors.E(op, errors.Invalid, "end < start")
	}

	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()

	acctBufs, ok := w.addressBuffers[account]
	if !ok {
		return nil, errors.E(op, errors.NotExist, errors.Errorf("account %d", account))
	}

	var buf *addressBuffer
	switch branch {
	case udb.ExternalBranch:
		buf = &acctBufs.albExternal
	case udb.InternalBranch:
		buf = &acctBufs.albInternal
	default:
		return nil, errors.E(op, errors.Invalid, "branch must be external (0) or internal (1)")
	}
	addrs, err := deriveChildAddresses(buf.branchXpub, start, end-start, w.chainParams)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return addrs, nil
}

type p2PKHChangeSource struct {
	persist   persistReturnedChildFunc
	account   uint32
	wallet    *Wallet
	ctx       context.Context
	gapPolicy gapPolicy
}

func (src *p2PKHChangeSource) Script() ([]byte, uint16, error) {
	changeAddress, err := src.wallet.newChangeAddress(src.ctx, "", src.persist, src.account, src.gapPolicy)
	if err != nil {
		return nil, 0, err
	}
	return addressScript(changeAddress)
}

func (src *p2PKHChangeSource) ScriptSize() int {
	return txsizes.P2PKHPkScriptSize
}

func deriveChildAddresses(key *hdkeychain.ExtendedKey, startIndex, count uint32, params *chaincfg.Params) ([]dcrutil.Address, error) {
	addresses := make([]dcrutil.Address, 0, count)
	for i := uint32(0); i < count; i++ {
		child, err := key.Child(startIndex + i)
		if errors.Is(err, hdkeychain.ErrInvalidChild) {
			continue
		}
		if err != nil {
			return nil, err
		}
		addr, err := compat.HD2Address(child, params)
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
	return compat.HD2Address(childKey, params)
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
		if errors.Is(err, hdkeychain.ErrInvalidChild) {
			continue
		}
		if err != nil {
			return err
		}
		*addrs = append(*addrs, addr)
	}
	return nil
}
