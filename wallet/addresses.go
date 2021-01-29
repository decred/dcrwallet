// Copyright (c) 2017-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"encoding/binary"
	"runtime/trace"

	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/internal/compat"
	"decred.org/dcrwallet/v2/wallet/txsizes"
	"decred.org/dcrwallet/v2/wallet/udb"
	"decred.org/dcrwallet/v2/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4"
)

// AccountKind describes the purpose and type of a wallet account.
type AccountKind int

const (
	// AccountKindBIP0044 describes a BIP0044 account derived from the
	// wallet seed.  New addresses created from the account encode secp256k1
	// P2PKH output scripts.
	AccountKindBIP0044 AccountKind = iota

	// AccountKindImported describes an account with only singular, possibly
	// unrelated imported keys.  Keys must be manually reimported after seed
	// restore.  New addresses can not be derived from the account.
	AccountKindImported

	// AccountKindImportedXpub describes a BIP0044 account created from an
	// imported extended key.  It operates like a seed-derived BIP0044
	// account.
	AccountKindImportedXpub
)

// Address is a human-readable encoding of an output script.
//
// Address encodings may include a network identifier, to prevent misuse on an
// alternate Decred network.
type Address interface {
	String() string

	// PaymentScript returns the output script and script version to pay the
	// address.  The version is always returned with the script, as it is
	// not useful to use the script without the version.
	PaymentScript() (version uint16, script []byte)

	// ScriptLen returns the known length of the address output script.
	ScriptLen() int
}

// KnownAddress represents an address recorded by the wallet.  It is potentially
// watched for involving transactions during wallet syncs.
type KnownAddress interface {
	Address

	// AccountName returns the account name associated with the known
	// address.
	AccountName() string

	// AccountKind describes the kind or purpose of the address' account.
	AccountKind() AccountKind
}

// PubKeyHashAddress is a KnownAddress for a secp256k1 pay-to-pubkey-hash
// (P2PKH) output script.
type PubKeyHashAddress interface {
	KnownAddress

	// PubKey returns the serialized compressed public key.  This key must
	// be included in scripts redeeming P2PKH outputs paying the address.
	PubKey() []byte

	// PubKeyHash returns the hashed compressed public key.  This hash must
	// appear in output scripts paying to the address.
	PubKeyHash() []byte
}

// BIP0044Address is a KnownAddress for a secp256k1 pay-to-pubkey-hash output,
// with keys created from a derived or imported BIP0044 account extended pubkey.
type BIP0044Address interface {
	PubKeyHashAddress

	// Path returns the BIP0032 indexes to derive the BIP0044 address from
	// the coin type key.  The account index is always the non-hardened
	// identifier, with values between 0 through 1<<31 - 1 (inclusive).  The
	// account index will always be zero if this address belongs to an
	// imported xpub or imported xpriv account.
	Path() (account, branch, child uint32)
}

// P2SHAddress is a KnownAddress which pays to the hash of an arbitrary script.
type P2SHAddress interface {
	KnownAddress

	// RedeemScript returns the preimage of the script hash.  The returned
	// version is the script version of the address, or the script version
	// of the redeemed previous output, and must be used for any operations
	// involving the script.
	RedeemScript() (version uint16, script []byte)
}

// managedAddress implements KnownAddress for a wrapped udb.ManagedAddress.
type managedAddress struct {
	acct      string
	acctKind  AccountKind
	addr      udb.ManagedAddress
	script    func() (uint16, []byte)
	scriptLen int
}

func (m *managedAddress) String() string                  { return m.addr.Address().String() }
func (m *managedAddress) PaymentScript() (uint16, []byte) { return m.script() }
func (m *managedAddress) ScriptLen() int                  { return m.scriptLen }
func (m *managedAddress) AccountName() string             { return m.acct }
func (m *managedAddress) AccountKind() AccountKind        { return m.acctKind }

// Possible implementations of m.script
func (m *managedAddress) p2pkhScript() (uint16, []byte) {
	pkh := m.addr.(udb.ManagedPubKeyAddress).AddrHash()
	s := []byte{
		0:  txscript.OP_DUP,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
		24: txscript.OP_CHECKSIG,
	}
	copy(s[3:23], pkh)
	return 0, s
}
func (m *managedAddress) p2shScript() (uint16, []byte) {
	sh := m.addr.(udb.ManagedScriptAddress).AddrHash()
	s := []byte{
		0:  txscript.OP_HASH160,
		1:  txscript.OP_DATA_20,
		22: txscript.OP_EQUALVERIFY,
	}
	copy(s[2:22], sh)
	return 0, s
}

// managedP2PKHAddress implements PubKeyHashAddress for a wrapped udb.ManagedAddress.
type managedP2PKHAddress struct {
	managedAddress
}

func (m *managedP2PKHAddress) PubKey() []byte {
	return m.addr.(udb.ManagedPubKeyAddress).PubKey()
}
func (m *managedP2PKHAddress) PubKeyHash() []byte {
	return m.addr.(udb.ManagedPubKeyAddress).AddrHash()
}

// managedBIP0044Address implements BIP0044Address for a wrapped udb.ManagedAddress.
type managedBIP0044Address struct {
	managedP2PKHAddress
	account, branch, child uint32
}

func (m *managedBIP0044Address) Path() (account, branch, child uint32) {
	return m.account, m.branch, m.child
}

// managedP2SHAddress implements P2SHAddress for a wrapped udb.ManagedAddress.
type managedP2SHAddress struct {
	managedAddress
}

func (m *managedP2SHAddress) RedeemScript() (uint16, []byte) {
	return m.addr.(udb.ManagedScriptAddress).RedeemScript()
}

func wrapManagedAddress(addr udb.ManagedAddress, account string, kind AccountKind) (KnownAddress, error) {
	ma := managedAddress{
		acct:     account,
		acctKind: kind,
		addr:     addr,
	}
	switch a := addr.(type) {
	case udb.ManagedPubKeyAddress:
		ma.script = ma.p2pkhScript
		ma.scriptLen = 25

		if kind == AccountKindImported {
			return &managedP2PKHAddress{ma}, nil
		}

		var acctNum, branch uint32
		if kind == AccountKindBIP0044 {
			acctNum = a.Account()
		}
		if a.Internal() {
			branch = 1
		}
		return &managedBIP0044Address{
			managedP2PKHAddress: managedP2PKHAddress{ma},
			account:             acctNum,
			branch:              branch,
			child:               a.Index(),
		}, nil
	case udb.ManagedScriptAddress:
		ma.script = ma.p2shScript
		ma.scriptLen = 23
		return &managedP2SHAddress{ma}, nil
	default:
		err := errors.Errorf("don't know how to wrap %T", a)
		return nil, errors.E(errors.Bug, err)
	}
}

// KnownAddress returns the KnownAddress implementation for an address.  The
// returned address may implement other interfaces (such as, but not limited to,
// PubKeyHashAddress, BIP0044Address, or P2SHAddress) depending on the script
// type and account for the address.
func (w *Wallet) KnownAddress(ctx context.Context, a dcrutil.Address) (KnownAddress, error) {
	const op errors.Op = "wallet.KnownAddress"

	var ma udb.ManagedAddress
	var acct uint32
	var acctName string
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		ma, err = w.manager.Address(addrmgrNs, a)
		if err != nil {
			return err
		}
		acct = ma.Account()
		acctName, err = w.manager.AccountName(addrmgrNs, acct)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	var acctKind AccountKind
	switch {
	case acct < udb.ImportedAddrAccount:
		acctKind = AccountKindBIP0044
	case acct == udb.ImportedAddrAccount:
		acctKind = AccountKindImported
	default:
		acctKind = AccountKindImportedXpub
	}
	return wrapManagedAddress(ma, acctName, acctKind)
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
	xpub        *hdkeychain.ExtendedKey
	accountName string
	account     uint32
	branch      uint32
	child       uint32
}

var _ BIP0044Address = (*xpubAddress)(nil)
var _ stakeAddress = (*xpubAddress)(nil)

func (x *xpubAddress) PaymentScript() (uint16, []byte) {
	s := []byte{
		0:  txscript.OP_DUP,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
		24: txscript.OP_CHECKSIG,
	}
	copy(s[3:23], x.Hash160()[:])
	return 0, s
}

func (x *xpubAddress) ScriptLen() int      { return txsizes.P2PKHPkScriptSize }
func (x *xpubAddress) AccountName() string { return x.accountName }

func (x *xpubAddress) AccountKind() AccountKind {
	if x.account > udb.ImportedAddrAccount {
		return AccountKindImportedXpub
	}
	return AccountKindBIP0044
}
func (x *xpubAddress) Path() (account, branch, child uint32) {
	account, branch, child = x.account, x.branch, x.child
	if x.account > udb.ImportedAddrAccount {
		account = 0
	}
	return
}

func (x *xpubAddress) PubKey() []byte {
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
	return childKey.SerializedPubKey()
}

func (x *xpubAddress) PubKeyHash() []byte { return x.Hash160()[:] }

func (x *xpubAddress) voteRights() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSTX,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.PubKeyHash())
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
	copy(s[4:24], x.PubKeyHash())
	return s, 0
}

func (x *xpubAddress) rewardCommitment(amount dcrutil.Amount, limits uint16) ([]byte, uint16) {
	s := make([]byte, 32)
	s[0] = txscript.OP_RETURN
	s[1] = txscript.OP_DATA_30
	copy(s[2:22], x.PubKeyHash())
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
	copy(s[4:24], x.PubKeyHash())
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
	copy(s[4:24], x.PubKeyHash())
	return s, 0
}

// addressScript returns an output script paying to address.  This func is
// always preferred over direct usage of txscript.PayToAddrScript due to the
// latter failing on unexpected concrete types.
func addressScript(addr dcrutil.Address) (script []byte, vers uint16, err error) {
	type scripter interface {
		PaymentScript() (uint16, []byte)
	}
	switch addr := addr.(type) {
	case scripter:
		vers, script = addr.PaymentScript()
	default:
		script, err = txscript.PayToAddrScript(addr)
	}
	return script, vers, err
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
const DefaultGapLimit = uint32(20)

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
	cursor      uint32
	lastWatched uint32
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
		err := w.manager.SyncAccountToAddrIndex(ns, account, child, branch)
		if err != nil {
			return err
		}
		return w.manager.MarkReturnedChildIndex(maybeDBTX, account, branch, child)
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
func (w *Wallet) nextAddress(ctx context.Context, op errors.Op, persist persistReturnedChildFunc,
	accountName string, account, branch uint32,
	callOpts ...NextAddressCallOption) (dcrutil.Address, error) {

	var opts nextAddressCallOptions // TODO: zero values for now, add to wallet config later.
	for _, c := range callOpts {
		c(&opts)
	}

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
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if alb.cursor >= w.gapLimit {
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
				if alb.cursor%w.gapLimit != 0 {
					break
				}
				n, err := w.NetworkBackend()
				if err != nil {
					break
				}
				addrs, err := deriveChildAddresses(alb.branchXpub,
					alb.lastUsed+1+alb.cursor, w.gapLimit, w.chainParams)
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
			return nil, errors.E(op, err)
		}
		alb.cursor++
		addr := &xpubAddress{
			AddressPubKeyHash: apkh,
			xpub:              ad.xpub,
			account:           account,
			accountName:       accountName,
			branch:            branch,
			child:             childIndex,
		}
		log.Infof("Returning address (account=%v branch=%v child=%v)", account, branch, childIndex)
		return addr, nil
	}
}

func (w *Wallet) nextImportedXpubAddress(ctx context.Context, op errors.Op,
	maybeDBTX walletdb.ReadWriteTx, accountName string, account uint32, branch uint32,
	callOpts ...NextAddressCallOption) (addr dcrutil.Address, err error) {

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
	xpub, err := w.manager.AccountExtendedPubKey(dbtx, account)
	if err != nil {
		return nil, errors.E(op, err)
	}
	props, err := w.manager.AccountProperties(ns, account)
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
	pkh := dcrutil.Hash160(childKey.SerializedPubKey())
	apkh, err := dcrutil.NewAddressPubKeyHash(pkh, w.chainParams,
		dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, errors.E(op, err)
	}
	addr = &xpubAddress{
		AddressPubKeyHash: apkh,
		xpub:              xpub,
		account:           account,
		branch:            branch,
		child:             child,
	}
	err = w.manager.MarkReturnedChildIndex(dbtx, account, branch, child)
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
	err := w.manager.MarkUsed(dbtx, addr.Address())
	if err != nil {
		return errors.E(op, err)
	}
	if account == udb.ImportedAddrAccount {
		return nil
	}
	props, err := w.manager.AccountProperties(ns, account)
	if err != nil {
		return errors.E(op, err)
	}
	lastUsed := props.LastUsedExternalIndex
	branch := udb.ExternalBranch
	if addr.Internal() {
		lastUsed = props.LastUsedInternalIndex
		branch = udb.InternalBranch
	}
	err = w.manager.SyncAccountToAddrIndex(ns, account,
		minUint32(hdkeychain.HardenedKeyStart-1, lastUsed+w.gapLimit),
		branch)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// NewExternalAddress returns an external address.
func (w *Wallet) NewExternalAddress(ctx context.Context, account uint32, callOpts ...NextAddressCallOption) (dcrutil.Address, error) {
	const op errors.Op = "wallet.NewExternalAddress"

	accountName, _ := w.AccountName(ctx, account)
	return w.nextAddress(ctx, op, w.persistReturnedChild(ctx, nil),
		accountName, account, udb.ExternalBranch, callOpts...)
}

// NewInternalAddress returns an internal address.
func (w *Wallet) NewInternalAddress(ctx context.Context, account uint32, callOpts ...NextAddressCallOption) (dcrutil.Address, error) {
	const op errors.Op = "wallet.NewExternalAddress"

	accountName, _ := w.AccountName(ctx, account)
	return w.nextAddress(ctx, op, w.persistReturnedChild(ctx, nil),
		accountName, account, udb.InternalBranch, callOpts...)
}

func (w *Wallet) newChangeAddress(ctx context.Context, op errors.Op, persist persistReturnedChildFunc,
	accountName string, account uint32, gap gapPolicy) (dcrutil.Address, error) {
	// Addresses can not be generated for the imported account, so as a
	// workaround, change is sent to the first account.
	//
	// Yep, our accounts are broken.
	if account == udb.ImportedAddrAccount {
		account = udb.DefaultAccountNum
	}
	return w.nextAddress(ctx, op, persist, accountName, account, udb.InternalBranch, withGapPolicy(gap))
}

// NewChangeAddress returns an internal address.  This is identical to
// NewInternalAddress but handles the imported account (which can't create
// addresses) by using account 0 instead, and always uses the wrapping gap limit
// policy.
func (w *Wallet) NewChangeAddress(ctx context.Context, account uint32) (dcrutil.Address, error) {
	const op errors.Op = "wallet.NewChangeAddress"
	accountName, _ := w.AccountName(ctx, account)
	return w.newChangeAddress(ctx, op, w.persistReturnedChild(ctx, nil), accountName, account, gapPolicyWrap)
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
		err = w.manager.SyncAccountToAddrIndex(ns, account, child, branch)
		if err != nil {
			return err
		}
		return w.manager.MarkReturnedChildIndex(tx, account, branch, child)
	})
	if err != nil {
		return err
	}

	if n, err := w.NetworkBackend(); err == nil {
		lastWatched := lastUsed + w.gapLimit
		if child <= lastWatched {
			// No need to derive anything more.
			return nil
		}
		additionalAddrs := child - lastWatched
		addrs, err := deriveChildAddresses(branchXpub, lastUsed+1+w.gapLimit,
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

// ImportedAddresses returns each of the addresses imported into an account.
func (w *Wallet) ImportedAddresses(ctx context.Context, account string) (_ []KnownAddress, err error) {
	const opf = "wallet.ImportedAddresses(%q)"
	defer func() {
		if err != nil {
			op := errors.Opf(opf, account)
			err = errors.E(op, err)
		}
	}()

	if account != "imported" {
		return nil, errors.E("account does not record imported keys")
	}

	var addrs []KnownAddress
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
		f := func(a udb.ManagedAddress) error {
			ma, err := wrapManagedAddress(a, account, AccountKindImported)
			if err != nil {
				return err
			}
			addrs = append(addrs, ma)
			return nil
		}
		return w.manager.ForEachAccountAddress(ns, udb.ImportedAddrAccount, f)
	})
	return addrs, err
}

type p2PKHChangeSource struct {
	persist   persistReturnedChildFunc
	account   uint32
	wallet    *Wallet
	ctx       context.Context
	gapPolicy gapPolicy
}

func (src *p2PKHChangeSource) Script() ([]byte, uint16, error) {
	const accountName = "" // not returned, so can be faked.
	changeAddress, err := src.wallet.newChangeAddress(src.ctx, "", src.persist,
		accountName, src.account, src.gapPolicy)
	if err != nil {
		return nil, 0, err
	}
	return addressScript(changeAddress)
}

func (src *p2PKHChangeSource) ScriptSize() int {
	return txsizes.P2PKHPkScriptSize
}

// p2PKHTreasuryChangeSource is the change source that shall be used when there
// is change on an OP_TADD treasury send.
type p2PKHTreasuryChangeSource struct {
	persist   persistReturnedChildFunc
	account   uint32
	wallet    *Wallet
	ctx       context.Context
	gapPolicy gapPolicy
}

// Script returns the treasury change script and is required for change source
// interface.
func (src *p2PKHTreasuryChangeSource) Script() ([]byte, uint16, error) {
	const accountName = "" // not returned, so can be faked.
	changeAddress, err := src.wallet.newChangeAddress(src.ctx, "", src.persist,
		accountName, src.account, src.gapPolicy)
	if err != nil {
		return nil, 0, err
	}
	script, vers, err := addressScript(changeAddress)
	if err != nil {
		return nil, 0, err
	}

	// Prefix script with OP_SSTXCHANGE.
	s := make([]byte, len(script)+1)
	s[0] = txscript.OP_SSTXCHANGE
	copy(s[1:], script)

	return s, vers, err
}

// ScriptSize returns the treasury change script size. This function is
// required for the change source interface.
func (src *p2PKHTreasuryChangeSource) ScriptSize() int {
	return txsizes.P2PKHPkTreasruryScriptSize
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
