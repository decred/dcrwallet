// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/chaincfg/v2/chainec"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/hdkeychain/v2"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/deployments/v2"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	"github.com/decred/dcrwallet/rpc/jsonrpc/types"
	"github.com/decred/dcrwallet/wallet/v3/internal/compat"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"golang.org/x/sync/errgroup"
)

const (
	// InsecurePubPassphrase is the default outer encryption passphrase used
	// for public data (everything but private keys).  Using a non-default
	// public passphrase can prevent an attacker without the public
	// passphrase from discovering all past and future wallet addresses if
	// they gain access to the wallet database.
	//
	// NOTE: at time of writing, public encryption only applies to public
	// data in the waddrmgr namespace.  Transactions are not yet encrypted.
	InsecurePubPassphrase = "public"
)

var (
	// SimulationPassphrase is the password for a wallet created for simnet
	// with --createtemp.
	SimulationPassphrase = []byte("password")
)

// Namespace bucket keys.
var (
	waddrmgrNamespaceKey  = []byte("waddrmgr")
	wtxmgrNamespaceKey    = []byte("wtxmgr")
	wstakemgrNamespaceKey = []byte("wstakemgr")
)

// StakeDifficultyInfo is a container for stake difficulty information updates.
type StakeDifficultyInfo struct {
	BlockHash       *chainhash.Hash
	BlockHeight     int64
	StakeDifficulty int64
}

// Wallet is a structure containing all the components for a
// complete wallet.  It contains the Armory-style key store
// addresses and keys),
type Wallet struct {
	// Data stores
	db       walletdb.DB
	Manager  *udb.Manager
	TxStore  *udb.Store
	StakeMgr *udb.StakeStore

	// Handlers for stake system.
	stakeSettingsLock  sync.Mutex
	voteBits           stake.VoteBits
	votingEnabled      bool
	balanceToMaintain  dcrutil.Amount
	poolAddress        dcrutil.Address
	poolFees           float64
	stakePoolEnabled   bool
	stakePoolColdAddrs map[string]struct{}
	subsidyCache       *blockchain.SubsidyCache

	// Start up flags/settings
	gapLimit        int
	accountGapLimit int

	networkBackend   NetworkBackend
	networkBackendMu sync.Mutex

	lockedOutpoints  map[wire.OutPoint]struct{}
	lockedOutpointMu sync.Mutex

	relayFee                dcrutil.Amount
	relayFeeMu              sync.Mutex
	ticketFeeIncrementLock  sync.Mutex
	ticketFeeIncrement      dcrutil.Amount
	DisallowFree            bool
	AllowHighFees           bool
	disableCoinTypeUpgrades bool

	// Internal address handling.
	addressReuse     bool
	ticketAddress    dcrutil.Address
	addressBuffers   map[uint32]*bip0044AccountData
	addressBuffersMu sync.Mutex

	// Passphrase unlock
	passphraseUsedMu        sync.RWMutex
	passphraseTimeoutMu     sync.Mutex
	passphraseTimeoutCancel chan struct{}

	NtfnServer *NotificationServer

	chainParams *chaincfg.Params
}

// Config represents the configuration options needed to initialize a wallet.
type Config struct {
	DB DB

	PubPassphrase []byte

	VotingEnabled bool
	AddressReuse  bool
	VotingAddress dcrutil.Address
	PoolAddress   dcrutil.Address
	PoolFees      float64
	TicketFee     float64

	GapLimit                int
	AccountGapLimit         int
	DisableCoinTypeUpgrades bool

	StakePoolColdExtKey string
	AllowHighFees       bool
	RelayFee            float64
	Params              *chaincfg.Params
}

// FetchOutput fetches the associated transaction output given an outpoint.
// It cannot be used to fetch multi-signature outputs.
func (w *Wallet) FetchOutput(outPoint *wire.OutPoint) (*wire.TxOut, error) {
	const op errors.Op = "wallet.FetchOutput"

	var out *wire.TxOut
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		outTx, err := w.TxStore.Tx(txmgrNs, &outPoint.Hash)
		if err != nil {
			return err
		}

		out = outTx.TxOut[outPoint.Index]
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	return out, nil
}

// BalanceToMaintain is used to get the current balancetomaintain for the wallet.
func (w *Wallet) BalanceToMaintain() dcrutil.Amount {
	w.stakeSettingsLock.Lock()
	balance := w.balanceToMaintain
	w.stakeSettingsLock.Unlock()

	return balance
}

// SetBalanceToMaintain is used to set the current w.balancetomaintain for the wallet.
func (w *Wallet) SetBalanceToMaintain(balance dcrutil.Amount) {
	w.stakeSettingsLock.Lock()
	w.balanceToMaintain = balance
	w.stakeSettingsLock.Unlock()
}

// VotingEnabled returns whether the wallet is configured to vote tickets.
func (w *Wallet) VotingEnabled() bool {
	w.stakeSettingsLock.Lock()
	enabled := w.votingEnabled
	w.stakeSettingsLock.Unlock()
	return enabled
}

func voteVersion(params *chaincfg.Params) uint32 {
	switch params.Net {
	case wire.MainNet:
		return 6
	case 0x48e7a065: // TestNet2
		return 6
	case wire.TestNet3:
		return 7
	case wire.SimNet:
		return 7
	default:
		return 1
	}
}

// CurrentAgendas returns the current stake version for the active network and
// this version of the software, and all agendas defined by it.
func CurrentAgendas(params *chaincfg.Params) (version uint32, agendas []chaincfg.ConsensusDeployment) {
	version = voteVersion(params)
	if params.Deployments == nil {
		return version, nil
	}
	return version, params.Deployments[version]
}

func (w *Wallet) readDBVoteBits(dbtx walletdb.ReadTx) stake.VoteBits {
	version, deployments := CurrentAgendas(w.chainParams)
	vb := stake.VoteBits{
		Bits:         0x0001,
		ExtendedBits: make([]byte, 4),
	}
	binary.LittleEndian.PutUint32(vb.ExtendedBits, version)

	if len(deployments) == 0 {
		return vb
	}

	for i := range deployments {
		d := &deployments[i]
		choiceID := udb.AgendaPreference(dbtx, version, d.Vote.Id)
		if choiceID == "" {
			continue
		}
		for j := range d.Vote.Choices {
			choice := &d.Vote.Choices[j]
			if choiceID == choice.Id {
				vb.Bits |= choice.Bits
				break
			}
		}
	}

	return vb
}

// VoteBits returns the vote bits that are described by the currently set agenda
// preferences.  The previous block valid bit is always set, and must be unset
// elsewhere if the previous block's regular transactions should be voted
// against.
func (w *Wallet) VoteBits() stake.VoteBits {
	w.stakeSettingsLock.Lock()
	vb := w.voteBits
	w.stakeSettingsLock.Unlock()
	return vb
}

// AgendaChoice describes a user's choice for a consensus deployment agenda.
type AgendaChoice struct {
	AgendaID string
	ChoiceID string
}

// AgendaChoices returns the choice IDs for every agenda of the supported stake
// version.  Abstains are included.
func (w *Wallet) AgendaChoices() (choices []AgendaChoice, voteBits uint16, err error) {
	const op errors.Op = "wallet.AgendaChoices"
	version, deployments := CurrentAgendas(w.chainParams)
	if len(deployments) == 0 {
		return nil, 0, nil
	}
	choices = make([]AgendaChoice, len(deployments))
	for i := range choices {
		choices[i].AgendaID = deployments[i].Vote.Id
		choices[i].ChoiceID = "abstain"
	}

	voteBits = 1
	err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		for i := range deployments {
			agenda := &deployments[i].Vote
			choice := udb.AgendaPreference(tx, version, agenda.Id)
			if choice == "" {
				continue
			}
			choices[i].ChoiceID = choice
			for j := range agenda.Choices {
				if agenda.Choices[j].Id == choice {
					voteBits |= agenda.Choices[j].Bits
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, errors.E(op, err)
	}
	return choices, voteBits, nil
}

// SetAgendaChoices sets the choices for agendas defined by the supported stake
// version.  If a choice is set multiple times, the last takes preference.  The
// new votebits after each change is made are returned.
func (w *Wallet) SetAgendaChoices(choices ...AgendaChoice) (voteBits uint16, err error) {
	const op errors.Op = "wallet.SetAgendaChoices"
	version, deployments := CurrentAgendas(w.chainParams)
	if len(deployments) == 0 {
		return 0, errors.E("no agendas to set for this network")
	}

	type maskChoice struct {
		mask uint16
		bits uint16
	}
	var appliedChoices []maskChoice

	err = walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		for _, c := range choices {
			var matchingAgenda *chaincfg.Vote
			for i := range deployments {
				if deployments[i].Vote.Id == c.AgendaID {
					matchingAgenda = &deployments[i].Vote
					break
				}
			}
			if matchingAgenda == nil {
				return errors.E(errors.Invalid, errors.Errorf("no agenda with ID %q", c.AgendaID))
			}

			var matchingChoice *chaincfg.Choice
			for i := range matchingAgenda.Choices {
				if matchingAgenda.Choices[i].Id == c.ChoiceID {
					matchingChoice = &matchingAgenda.Choices[i]
					break
				}
			}
			if matchingChoice == nil {
				return errors.E(errors.Invalid, errors.Errorf("agenda %q has no choice ID %q", c.AgendaID, c.ChoiceID))
			}

			err := udb.SetAgendaPreference(tx, version, c.AgendaID, c.ChoiceID)
			if err != nil {
				return err
			}
			appliedChoices = append(appliedChoices, maskChoice{
				mask: matchingAgenda.Mask,
				bits: matchingChoice.Bits,
			})
		}
		return nil
	})
	if err != nil {
		return 0, errors.E(op, err)
	}

	// With the DB update successful, modify the actual votebits cached by the
	// wallet structure.
	w.stakeSettingsLock.Lock()
	for _, c := range appliedChoices {
		w.voteBits.Bits &^= c.mask // Clear all bits from this agenda
		w.voteBits.Bits |= c.bits  // Set bits for this choice
	}
	voteBits = w.voteBits.Bits
	w.stakeSettingsLock.Unlock()

	return voteBits, nil
}

// TicketAddress gets the ticket address for the wallet to give the ticket
// voting rights to.
func (w *Wallet) TicketAddress() dcrutil.Address {
	return w.ticketAddress
}

// PoolAddress gets the pool address for the wallet to give ticket fees to.
func (w *Wallet) PoolAddress() dcrutil.Address {
	return w.poolAddress
}

// PoolFees gets the per-ticket pool fee for the wallet.
func (w *Wallet) PoolFees() float64 {
	return w.poolFees
}

// RelayFee returns the current minimum relay fee (per kB of serialized
// transaction) used when constructing transactions.
func (w *Wallet) RelayFee() dcrutil.Amount {
	w.relayFeeMu.Lock()
	relayFee := w.relayFee
	w.relayFeeMu.Unlock()
	return relayFee
}

// SetRelayFee sets a new minimum relay fee (per kB of serialized
// transaction) used when constructing transactions.
func (w *Wallet) SetRelayFee(relayFee dcrutil.Amount) {
	w.relayFeeMu.Lock()
	w.relayFee = relayFee
	w.relayFeeMu.Unlock()
}

// TicketFeeIncrement is used to get the current feeIncrement for the wallet.
func (w *Wallet) TicketFeeIncrement() dcrutil.Amount {
	w.ticketFeeIncrementLock.Lock()
	fee := w.ticketFeeIncrement
	w.ticketFeeIncrementLock.Unlock()

	return fee
}

// SetTicketFeeIncrement is used to set the current w.ticketFeeIncrement for the wallet.
func (w *Wallet) SetTicketFeeIncrement(fee dcrutil.Amount) {
	w.ticketFeeIncrementLock.Lock()
	w.ticketFeeIncrement = fee
	w.ticketFeeIncrementLock.Unlock()
}

// MainChainTip returns the hash and height of the tip-most block in the main
// chain that the wallet is synchronized to.
func (w *Wallet) MainChainTip() (hash chainhash.Hash, height int32) {
	// TODO: after the main chain tip is successfully updated in the db, it
	// should be saved in memory.  This will speed up access to it, and means
	// there won't need to be an ignored error here for ergonomic access to the
	// hash and height.
	walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		hash, height = w.TxStore.MainChainTip(txmgrNs)
		return nil
	})
	return
}

// BlockInMainChain returns whether hash is a block hash of any block in the
// wallet's main chain.  If the block is in the main chain, invalidated reports
// whether a child block in the main chain stake invalidates the queried block.
func (w *Wallet) BlockInMainChain(hash *chainhash.Hash) (haveBlock, invalidated bool, err error) {
	const op errors.Op = "wallet.BlockInMainChain"
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		haveBlock, invalidated = w.TxStore.BlockInMainChain(dbtx, hash)
		return nil
	})
	if err != nil {
		return false, false, errors.E(op, err)
	}
	return haveBlock, invalidated, nil
}

// BlockHeader returns the block header for a block by it's identifying hash, if
// it is recorded by the wallet.
func (w *Wallet) BlockHeader(blockHash *chainhash.Hash) (*wire.BlockHeader, error) {
	var header *wire.BlockHeader
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		header, err = w.TxStore.GetBlockHeader(dbtx, blockHash)
		return err
	})
	return header, err
}

// CFilter returns the regular compact filter for a block.
func (w *Wallet) CFilter(blockHash *chainhash.Hash) (*gcs.Filter, error) {
	var f *gcs.Filter
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		f, err = w.TxStore.CFilter(dbtx, blockHash)
		return err
	})
	return f, err
}

// loadActiveAddrs loads the consensus RPC server with active addresses for
// transaction notifications.  For logging purposes, it returns the total number
// of addresses loaded.
func (w *Wallet) loadActiveAddrs(ctx context.Context, dbtx walletdb.ReadTx, nb NetworkBackend) (uint64, error) {
	// loadBranchAddrs loads addresses for the branch with the child range [0,n].
	loadBranchAddrs := func(branchKey *hdkeychain.ExtendedKey, n uint32, errs chan<- error) {
		const step = 256
		var g errgroup.Group
		for child := uint32(0); child <= n; child += step {
			child := child
			g.Go(func() error {
				addrs := make([]dcrutil.Address, 0, step)
				stop := minUint32(n+1, child+step)
				for ; child < stop; child++ {
					addr, err := deriveChildAddress(branchKey, child, w.chainParams)
					if err == hdkeychain.ErrInvalidChild {
						continue
					}
					if err != nil {
						return err
					}
					addrs = append(addrs, addr)
				}
				return nb.LoadTxFilter(ctx, false, addrs, nil)
			})
		}
		errs <- g.Wait()
	}

	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	lastAcct, err := w.Manager.LastAccount(addrmgrNs)
	if err != nil {
		return 0, err
	}
	errs := make(chan error, int(lastAcct+1)*2+1)
	var bip0044AddrCount, importedAddrCount uint64
	for acct := uint32(0); acct <= lastAcct; acct++ {
		props, err := w.Manager.AccountProperties(addrmgrNs, acct)
		if err != nil {
			return 0, err
		}
		acctXpub, err := w.Manager.AccountExtendedPubKey(dbtx, acct)
		if err != nil {
			return 0, err
		}
		extKey, intKey, err := deriveBranches(acctXpub)
		if err != nil {
			return 0, err
		}
		gapLimit := uint32(w.gapLimit)
		extn := minUint32(props.LastReturnedExternalIndex+gapLimit, hdkeychain.HardenedKeyStart-1)
		intn := minUint32(props.LastReturnedInternalIndex+gapLimit, hdkeychain.HardenedKeyStart-1)
		// pre-cache the pubkey results so concurrent access does not race.
		extKey.ECPubKey()
		intKey.ECPubKey()
		go loadBranchAddrs(extKey, extn, errs)
		go loadBranchAddrs(intKey, intn, errs)
		// loadBranchAddrs loads addresses through extn/intn, and the actual
		// number of watched addresses is one more for each branch due to zero
		// indexing.
		bip0044AddrCount += uint64(extn) + uint64(intn) + 2
	}
	go func() {
		// Imported addresses are still sent as a single slice for now.  Could
		// use the optimization above to avoid appends and reallocations.
		var addrs []dcrutil.Address
		err := w.Manager.ForEachAccountAddress(addrmgrNs, udb.ImportedAddrAccount,
			func(a udb.ManagedAddress) error {
				addrs = append(addrs, a.Address())
				return nil
			})
		if err != nil {
			errs <- err
			return
		}
		importedAddrCount = uint64(len(addrs))
		errs <- nb.LoadTxFilter(ctx, false, addrs, nil)
	}()
	for i := 0; i < cap(errs); i++ {
		err := <-errs
		if err != nil {
			return 0, err
		}
	}

	return bip0044AddrCount + importedAddrCount, nil
}

// CoinType returns the active BIP0044 coin type. For watching-only wallets,
// which do not save the coin type keys, this method will return an error with
// code errors.WatchingOnly.
func (w *Wallet) CoinType() (uint32, error) {
	const op errors.Op = "wallet.CoinType"
	var coinType uint32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		var err error
		coinType, err = w.Manager.CoinType(tx)
		return err
	})
	if err != nil {
		return coinType, errors.E(op, err)
	}
	return coinType, nil
}

// CoinTypePrivKey returns the BIP0044 coin type private key.
func (w *Wallet) CoinTypePrivKey() (*hdkeychain.ExtendedKey, error) {
	const op errors.Op = "wallet.CoinTypePrivKey"
	var coinTypePrivKey *hdkeychain.ExtendedKey
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		var err error
		coinTypePrivKey, err = w.Manager.CoinTypePrivKey(tx)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return coinTypePrivKey, nil
}

// LoadActiveDataFilters loads filters for all active addresses and unspent
// outpoints for this wallet.
func (w *Wallet) LoadActiveDataFilters(ctx context.Context, n NetworkBackend, reload bool) error {
	const op errors.Op = "wallet.LoadActiveDataFilters"
	log.Infof("Loading active addresses and unspent outputs...")

	if reload {
		err := n.LoadTxFilter(ctx, true, nil, nil)
		if err != nil {
			return err
		}
	}

	var addrCount, utxoCount uint64
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		addrCount, err = w.loadActiveAddrs(ctx, dbtx, n)
		if err != nil {
			return err
		}

		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		unspent, err := w.TxStore.UnspentOutpoints(txmgrNs)
		if err != nil {
			return err
		}
		utxoCount = uint64(len(unspent))
		return n.LoadTxFilter(ctx, false, nil, unspent)
	})
	if err != nil {
		return errors.E(op, err)
	}

	log.Infof("Registered for transaction notifications for %v address(es) "+
		"and %v output(s)", addrCount, utxoCount)
	return nil
}

// CommittedTickets takes a list of tickets and returns a filtered list of
// tickets that are controlled by this wallet.
func (w *Wallet) CommittedTickets(tickets []*chainhash.Hash) ([]*chainhash.Hash, []dcrutil.Address, error) {
	const op errors.Op = "wallet.CommittedTickets"
	hashes := make([]*chainhash.Hash, 0, len(tickets))
	addresses := make([]dcrutil.Address, 0, len(tickets))
	// Verify we own this ticket
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		for _, v := range tickets {
			// Make sure ticket exists
			tx, err := w.TxStore.Tx(txmgrNs, v)
			if err != nil {
				log.Debugf("%v", err)
				continue
			}
			if !stake.IsSStx(tx) {
				continue
			}

			// Commitment outputs are at alternating output
			// indexes, starting at 1.
			var bestAddr dcrutil.Address
			var bestAmount dcrutil.Amount

			for i := 1; i < len(tx.TxOut); i += 2 {
				scr := tx.TxOut[i].PkScript
				addr, err := stake.AddrFromSStxPkScrCommitment(scr,
					w.chainParams)
				if err != nil {
					log.Debugf("%v", err)
					break
				}
				if _, ok := addr.(*dcrutil.AddressPubKeyHash); !ok {
					log.Tracef("Skipping commitment at "+
						"index %v: address is not "+
						"P2PKH", i)
					continue
				}
				amt, err := stake.AmountFromSStxPkScrCommitment(scr)
				if err != nil {
					log.Debugf("%v", err)
					break
				}
				if amt > bestAmount {
					bestAddr = addr
					bestAmount = amt
				}
			}

			if bestAddr == nil {
				log.Debugf("no best address")
				continue
			}

			if !w.Manager.ExistsHash160(addrmgrNs,
				bestAddr.Hash160()[:]) {
				log.Debugf("not our address: %x",
					bestAddr.Hash160())
				continue
			}
			ticketHash := tx.TxHash()
			log.Tracef("Ticket purchase %v: best commitment"+
				" address %v amount %v", &ticketHash, bestAddr,
				bestAmount)

			hashes = append(hashes, v)
			addresses = append(addresses, bestAddr)
		}
		return nil
	})
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	return hashes, addresses, nil
}

// fetchMissingCFilters checks to see if there are any missing committed filters
// then, if so requests them from the given peer.  The progress channel, if
// non-nil, is sent the first height and last height of the range of filters
// that were retrieved in that peer request.
func (w *Wallet) fetchMissingCFilters(ctx context.Context, p Peer, progress chan<- MissingCFilterProgress) error {
	var missing bool
	var height int32

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		missing = w.TxStore.IsMissingMainChainCFilters(dbtx)
		if missing {
			height, err = w.TxStore.MissingCFiltersHeight(dbtx)
		}
		return err
	})
	if err != nil {
		return err
	}
	if !missing {
		return nil
	}

	const span = 2000
	storage := make([]chainhash.Hash, span)
	storagePtrs := make([]*chainhash.Hash, span)
	for i := range storage {
		storagePtrs[i] = &storage[i]
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var hashes []chainhash.Hash
		var get []*chainhash.Hash
		var cont bool
		err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
			ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
			var err error
			missing = w.TxStore.IsMissingMainChainCFilters(dbtx)
			if !missing {
				return nil
			}
			hash, err := w.TxStore.GetMainChainBlockHashForHeight(ns, height)
			if err != nil {
				return err
			}
			_, err = w.TxStore.CFilter(dbtx, &hash)
			if err == nil {
				height += span
				cont = true
				return nil
			}
			storage = storage[:cap(storage)]
			hashes, err = w.TxStore.GetMainChainBlockHashes(ns, &hash, true, storage)
			if err != nil {
				return err
			}
			if len(hashes) == 0 {
				const op errors.Op = "udb.GetMainChainBlockHashes"
				return errors.E(op, errors.Bug, "expected over 0 results")
			}
			get = storagePtrs[:len(hashes)]
			if get[0] != &hashes[0] {
				const op errors.Op = "udb.GetMainChainBlockHashes"
				return errors.E(op, errors.Bug, "unexpected slice reallocation")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !missing {
			return nil
		}
		if cont {
			continue
		}

		filters, err := p.CFilters(ctx, get)
		if err != nil {
			return err
		}

		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			_, err := w.TxStore.CFilter(dbtx, get[len(get)-1])
			if err == nil {
				cont = true
				return nil
			}
			return w.TxStore.InsertMissingCFilters(dbtx, get, filters)
		})
		if err != nil {
			return err
		}
		if cont {
			continue
		}

		if progress != nil {
			progress <- MissingCFilterProgress{BlockHeightStart: height, BlockHeightEnd: height + span - 1}
		}
		log.Infof("Fetched cfilters for blocks %v-%v", height, height+span-1)
	}
}

// FetchMissingCFilters records any missing compact filters for main chain
// blocks.  A database upgrade requires all compact filters to be recorded for
// the main chain before any more blocks may be attached, but this information
// must be fetched at runtime after the upgrade as it is not already known at
// the time of upgrade.
func (w *Wallet) FetchMissingCFilters(ctx context.Context, p Peer) error {
	const opf = "wallet.FetchMissingCFilters(%v)"

	err := w.fetchMissingCFilters(ctx, p, nil)
	if err != nil {
		op := errors.Opf(opf, p)
		return errors.E(op, err)
	}
	return nil
}

// MissingCFilterProgress records the first and last height of the progress
// that was received and any errors that were received during the fetching.
type MissingCFilterProgress struct {
	Err              error
	BlockHeightStart int32
	BlockHeightEnd   int32
}

// FetchMissingCFiltersWithProgress records any missing compact filters for main chain
// blocks.  A database upgrade requires all compact filters to be recorded for
// the main chain before any more blocks may be attached, but this information
// must be fetched at runtime after the upgrade as it is not already known at
// the time of upgrade.  This function reports to a channel with any progress
// that may have seen.
func (w *Wallet) FetchMissingCFiltersWithProgress(ctx context.Context, p Peer, progress chan<- MissingCFilterProgress) {
	const opf = "wallet.FetchMissingCFilters(%v)"

	defer close(progress)

	err := w.fetchMissingCFilters(ctx, p, progress)
	if err != nil {
		op := errors.Opf(opf, p)
		progress <- MissingCFilterProgress{Err: errors.E(op, err)}
	}
}

// createHeaderData creates the header data to process from hex-encoded
// serialized block headers.
func createHeaderData(headers []*wire.BlockHeader) ([]udb.BlockHeaderData, error) {
	data := make([]udb.BlockHeaderData, len(headers))
	var buf bytes.Buffer
	for i, header := range headers {
		var headerData udb.BlockHeaderData
		headerData.BlockHash = header.BlockHash()
		buf.Reset()
		err := header.Serialize(&buf)
		if err != nil {
			return nil, err
		}
		copy(headerData.SerializedHeader[:], buf.Bytes())
		data[i] = headerData
	}
	return data, nil
}

// log2 calculates an integer approximation of log2(x).  This is used to
// approximate the cap to use when allocating memory for the block locators.
func log2(x int) int {
	res := 0
	for x != 0 {
		x /= 2
		res++
	}
	return res
}

// BlockLocators returns block locators, suitable for use in a getheaders wire
// message or dcrd JSON-RPC request, for the blocks in sidechain and saved in
// the wallet's main chain.  For memory and lookup efficiency, many older hashes
// are skipped, with increasing gaps between included hashes.
//
// When sidechain has zero length, locators for only main chain blocks starting
// from the tip are returned.  Otherwise, locators are created starting with the
// best (last) block of sidechain and sidechain[0] must be a child of a main
// chain block (sidechain may not contain orphan blocks).
func (w *Wallet) BlockLocators(sidechain []*BlockNode) ([]*chainhash.Hash, error) {
	const op errors.Op = "wallet.BlockLocators"
	var locators []*chainhash.Hash
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		locators, err = w.blockLocators(dbtx, sidechain)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return locators, nil
}

func (w *Wallet) blockLocators(dbtx walletdb.ReadTx, sidechain []*BlockNode) ([]*chainhash.Hash, error) {
	ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
	var hash chainhash.Hash
	var height int32
	if len(sidechain) == 0 {
		hash, height = w.TxStore.MainChainTip(ns)
	} else {
		n := sidechain[len(sidechain)-1]
		hash = *n.Hash
		height = int32(n.Header.Height)
	}

	locators := make([]*chainhash.Hash, 1, 10+log2(int(height)))
	locators[0] = &hash

	var step int32 = 1
	for height >= 0 {
		if len(sidechain) > 0 && height-int32(sidechain[0].Header.Height) >= 0 {
			n := sidechain[height-int32(sidechain[0].Header.Height)]
			hash := n.Hash
			locators = append(locators, hash)
		} else {
			hash, err := w.TxStore.GetMainChainBlockHashForHeight(ns, height)
			if err != nil {
				return nil, err
			}
			locators = append(locators, &hash)
		}

		height -= step

		if len(locators) > 10 {
			step *= 2
		}
	}

	return locators, nil
}

func (w *Wallet) fetchHeaders(ctx context.Context, op errors.Op, p Peer) (firstNew chainhash.Hash, err error) {
	var blockLocators []*chainhash.Hash
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		blockLocators, err = w.blockLocators(dbtx, nil)
		return err
	})
	if err != nil {
		return firstNew, err
	}

	// Fetch and process headers until no more are returned.
	hashStop := chainhash.Hash{}
	for {
		var chainBuilder SidechainForest

		headers, err := p.Headers(ctx, blockLocators, &hashStop)
		if err != nil {
			return firstNew, err
		}
		headerHashes := make([]*chainhash.Hash, 0, len(headers))
		for _, h := range headers {
			hash := h.BlockHash()
			headerHashes = append(headerHashes, &hash)
		}
		filters, err := p.CFilters(ctx, headerHashes)
		if err != nil {
			return firstNew, err
		}

		if len(headers) == 0 {
			return firstNew, err
		}

		for i := range headers {
			chainBuilder.AddBlockNode(NewBlockNode(headers[i], headerHashes[i], filters[i]))
		}

		headerData, err := createHeaderData(headers)
		if err != nil {
			return firstNew, err
		}
		log.Debugf("First header: block %v", &headerData[0].BlockHash)

		var brk bool
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			ns := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)
			addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
			chain, err := w.EvaluateBestChain(&chainBuilder)
			if err != nil {
				return err
			}
			if len(chain) == 0 {
				brk = true
				return nil
			}
			_, err = w.validateHeaderChainDifficulties(dbtx, chain, 0)
			if err != nil {
				return err
			}

			if firstNew == (chainhash.Hash{}) {
				firstNew = *chain[0].Hash
				tip, _ := w.TxStore.MainChainTip(ns)
				if chain[0].Header.PrevBlock != tip {
					err := w.TxStore.Rollback(ns, addrmgrNs, int32(chain[0].Header.Height))
					if err != nil {
						return err
					}
				}
			}
			for _, n := range chain {
				_, err = w.extendMainChain("", dbtx, n.Header, n.Filter, nil)
				if err != nil {
					return err
				}
			}
			blockLocators, err = w.blockLocators(dbtx, nil)
			return err
		})
		if brk || err != nil {
			return firstNew, err
		}
		log.Infof("Fetched %v header(s) from %s", len(headers), p)
	}
}

// FetchHeaders fetches headers from a Peer and updates the main chain tip with
// the latest block.  The number of new headers fetched is returned, along with
// the hash of the first previously-unseen block hash now in the main chain.
// This is the block a rescan should begin at (inclusive), and is only relevant
// when the number of fetched headers is not zero.
func (w *Wallet) FetchHeaders(ctx context.Context, p Peer) (count int, rescanFrom chainhash.Hash, rescanFromHeight int32, mainChainTipBlockHash chainhash.Hash, mainChainTipBlockHeight int32, err error) {
	const op errors.Op = "wallet.FetchHeaders"

	rescanFrom, err = w.fetchHeaders(ctx, op, p)
	if err != nil {
		err = errors.E(op, err)
		return
	}

	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)

		mainChainTipBlockHash, mainChainTipBlockHeight = w.TxStore.MainChainTip(ns)
		if rescanFrom != (chainhash.Hash{}) {
			firstHeader, err := w.TxStore.GetBlockHeader(dbtx, &rescanFrom)
			if err != nil {
				return err
			}
			rescanFromHeight = int32(firstHeader.Height)
			count = int(mainChainTipBlockHeight - rescanFromHeight + 1)
		}
		return nil
	})
	if err != nil {
		err = errors.E(op, err)
	}
	return
}

// Consolidate consolidates as many UTXOs as are passed in the inputs argument.
// If that many UTXOs can not be found, it will use the maximum it finds. This
// will only compress UTXOs in the default account
func (w *Wallet) Consolidate(inputs int, account uint32, address dcrutil.Address) (*chainhash.Hash, error) {
	heldUnlock, err := w.holdUnlock()
	if err != nil {
		return nil, err
	}
	defer heldUnlock.release()
	return w.compressWallet("wallet.Consolidate", inputs, account, address)
}

// CreateMultisigTx creates and signs a multisig transaction.
func (w *Wallet) CreateMultisigTx(account uint32, amount dcrutil.Amount, pubkeys []*dcrutil.AddressSecpPubKey, nrequired int8, minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {
	heldUnlock, err := w.holdUnlock()
	if err != nil {
		return nil, nil, nil, err
	}
	defer heldUnlock.release()
	return w.txToMultisig("wallet.CreateMultisigTx", account, amount, pubkeys, nrequired, minconf)
}

// PurchaseTickets purchases tickets, returning the hashes of all ticket
// purchase transactions.
//
// Deprecated: Use PurchaseTicketsContext for solo buying.
func (w *Wallet) PurchaseTickets(minBalance, spendLimit dcrutil.Amount, minConf int32, votingAddr dcrutil.Address, account uint32, count int, poolAddress dcrutil.Address,
	poolFees float64, expiry int32, txFee dcrutil.Amount, ticketFee dcrutil.Amount) ([]*chainhash.Hash, error) {

	const op errors.Op = "wallet.PurchaseTickets"

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, errors.E(op, err)
	}

	req := &PurchaseTicketsRequest{
		Count:         count,
		SourceAccount: account,
		VotingAddress: votingAddr,
		MinConf:       minConf,
		Expiry:        expiry,

		minBalance:  minBalance,
		spendLimit:  spendLimit,
		poolAddress: poolAddress,
		poolFees:    poolFees,
		txFee:       txFee,
		ticketFee:   ticketFee,
	}

	heldUnlock, err := w.holdUnlock()
	if err != nil {
		return nil, errors.E(op, err)
	}
	defer heldUnlock.release()

	return w.purchaseTickets(context.Background(), op, n, req)
}

// PurchaseTicketsRequest describes the parameters for purchasing tickets.
type PurchaseTicketsRequest struct {
	Count         int
	SourceAccount uint32
	VotingAddress dcrutil.Address
	MinConf       int32
	Expiry        int32

	// may be set by deprecated methods, subject to change
	minBalance  dcrutil.Amount
	spendLimit  dcrutil.Amount
	poolAddress dcrutil.Address
	poolFees    float64
	txFee       dcrutil.Amount
	ticketFee   dcrutil.Amount
}

// PurchaseTicketsContext purchases tickets, returning the hashes of all ticket
// purchase transactions.
func (w *Wallet) PurchaseTicketsContext(ctx context.Context, n NetworkBackend, req *PurchaseTicketsRequest) ([]*chainhash.Hash, error) {
	const op errors.Op = "wallet.PurchaseTicketsContext"

	heldUnlock, err := w.holdUnlock()
	if err != nil {
		return nil, errors.E(op, err)
	}
	defer heldUnlock.release()

	return w.purchaseTickets(ctx, op, n, req)
}

// heldUnlock is a tool to prevent the wallet from automatically locking after
// some timeout before an operation which needed the unlocked wallet has
// finished.  Any acquired heldUnlock *must* be released (preferably with a
// defer) or the wallet will forever remain unlocked.
type heldUnlock chan struct{}

// Unlock unlocks the wallet, allowing access to private keys and secret scripts.
// An unlocked wallet will be locked before returning with a Passphrase error if
// the passphrase is incorrect.
// If the wallet is currently unlocked without any timeout, timeout is ignored
// and read in a background goroutine to avoid blocking sends.
// If the wallet is locked and a non-nil timeout is provided, the wallet will be
// locked in the background after reading from the channel.
// If the wallet is already unlocked with a previous timeout, the new timeout
// replaces the prior.
func (w *Wallet) Unlock(passphrase []byte, timeout <-chan time.Time) error {
	const op errors.Op = "wallet.Unlock"

	w.passphraseUsedMu.RLock()
	wasLocked := w.Manager.IsLocked()
	err := w.Manager.UnlockedWithPassphrase(passphrase)
	w.passphraseUsedMu.RUnlock()
	switch {
	case errors.Is(errors.WatchingOnly, err):
		return errors.E(op, err)
	case errors.Is(errors.Passphrase, err):
		w.Lock()
		if !wasLocked {
			log.Info("The wallet has been locked due to an incorrect passphrase.")
		}
		return errors.E(op, err)
	default:
		return errors.E(op, err)
	case errors.Is(errors.Locked, err):
		defer w.passphraseUsedMu.Unlock()
		w.passphraseUsedMu.Lock()
		err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
			addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
			return w.Manager.Unlock(addrmgrNs, passphrase)
		})
		if err != nil {
			return errors.E(op, errors.Passphrase, err)
		}
	case err == nil:
	}
	w.replacePassphraseTimeout(wasLocked, timeout)
	return nil
}

func (w *Wallet) replacePassphraseTimeout(wasLocked bool, newTimeout <-chan time.Time) {
	defer w.passphraseTimeoutMu.Unlock()
	w.passphraseTimeoutMu.Lock()
	hadTimeout := w.passphraseTimeoutCancel != nil
	if !wasLocked && !hadTimeout && newTimeout != nil {
		go func() { <-newTimeout }()
	} else {
		oldCancel := w.passphraseTimeoutCancel
		var newCancel chan struct{}
		if newTimeout != nil {
			newCancel = make(chan struct{}, 1)
		}
		w.passphraseTimeoutCancel = newCancel

		if oldCancel != nil {
			oldCancel <- struct{}{}
		}
		if newTimeout != nil {
			go func() {
				select {
				case <-newTimeout:
					w.Lock()
					log.Info("The wallet has been locked due to timeout.")
				case <-newCancel:
					<-newTimeout
				}
			}()
		}
	}
	switch {
	case (wasLocked || hadTimeout) && newTimeout == nil:
		log.Info("The wallet has been unlocked without a time limit")
	case (wasLocked || !hadTimeout) && newTimeout != nil:
		log.Info("The wallet has been temporarily unlocked")
	}
}

// Lock locks the wallet's address manager.
func (w *Wallet) Lock() {
	w.passphraseUsedMu.Lock()
	w.passphraseTimeoutMu.Lock()
	_ = w.Manager.Lock()
	w.passphraseTimeoutCancel = nil
	w.passphraseTimeoutMu.Unlock()
	w.passphraseUsedMu.Unlock()
}

// Locked returns whether the account manager for a wallet is locked.
func (w *Wallet) Locked() bool {
	return w.Manager.IsLocked()
}

// holdUnlock prevents the wallet from being locked.  The heldUnlock object
// *must* be released, or the wallet will forever remain unlocked.
//
// TODO: To prevent the above scenario, perhaps closures should be passed
// to the walletLocker goroutine and disallow callers from explicitly
// handling the locking mechanism.
func (w *Wallet) holdUnlock() (heldUnlock, error) {
	w.passphraseUsedMu.RLock()
	locked := w.Manager.IsLocked()
	if locked {
		w.passphraseUsedMu.RUnlock()
		return nil, errors.E(errors.Locked)
	}
	hold := make(heldUnlock)
	go func() {
		<-hold
		w.passphraseUsedMu.RUnlock()
	}()
	return hold, nil
}

// release releases the hold on the unlocked-state of the wallet and allows the
// wallet to be locked again.  If a lock timeout has already expired, the
// wallet is locked again as soon as release is called.
func (c heldUnlock) release() {
	c <- struct{}{}
}

// ChangePrivatePassphrase attempts to change the passphrase for a wallet from
// old to new.  Changing the passphrase is synchronized with all other address
// manager locking and unlocking.  The lock state will be the same as it was
// before the password change.
func (w *Wallet) ChangePrivatePassphrase(old, new []byte) error {
	const op errors.Op = "wallet.ChangePrivatePassphrase"
	defer w.passphraseUsedMu.Unlock()
	w.passphraseUsedMu.Lock()
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		return w.Manager.ChangePassphrase(addrmgrNs, old, new, true)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// ChangePublicPassphrase modifies the public passphrase of the wallet.
func (w *Wallet) ChangePublicPassphrase(old, new []byte) error {
	const op errors.Op = "wallet.ChangePublicPassphrase"
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		return w.Manager.ChangePassphrase(addrmgrNs, old, new, false)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// CalculateAccountBalance sums the amounts of all unspent transaction
// outputs to the given account of a wallet and returns the balance.
func (w *Wallet) CalculateAccountBalance(account uint32, confirms int32) (udb.Balances, error) {
	const op errors.Op = "wallet.CalculateAccountBalance"
	var balance udb.Balances
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error

		balance, err = w.TxStore.AccountBalance(txmgrNs, addrmgrNs,
			confirms, account)
		return err
	})
	if err != nil {
		return balance, errors.E(op, err)
	}
	return balance, nil
}

// CalculateAccountBalances calculates the values for the wtxmgr struct Balance,
// which includes the total balance, the spendable balance, and the balance
// which has yet to mature.
func (w *Wallet) CalculateAccountBalances(confirms int32) (map[uint32]*udb.Balances, error) {
	const op errors.Op = "wallet.CalculateAccountBalances"
	balances := make(map[uint32]*udb.Balances)
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		return w.Manager.ForEachAccount(addrmgrNs, func(acct uint32) error {
			balance, err := w.TxStore.AccountBalance(txmgrNs, addrmgrNs,
				confirms, acct)
			if err != nil {
				return err
			}
			balances[acct] = &balance
			return nil
		})
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return balances, nil
}

// CurrentAddress gets the most recently requested payment address from a wallet.
// If the address has already been used (there is at least one transaction
// spending to it in the blockchain or dcrd mempool), the next chained address
// is returned.
func (w *Wallet) CurrentAddress(account uint32) (dcrutil.Address, error) {
	const op errors.Op = "wallet.CurrentAddress"
	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()

	data, ok := w.addressBuffers[account]
	if !ok {
		return nil, errors.E(op, errors.NotExist, errors.Errorf("no account %d", account))
	}
	buf := &data.albExternal

	childIndex := buf.lastUsed + 1 + buf.cursor
	child, err := buf.branchXpub.Child(childIndex)
	if err != nil {
		return nil, errors.E(op, err)
	}
	addr, err := compat.HD2Address(child, w.chainParams)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return addr, nil
}

// PubKeyForAddress looks up the associated public key for a P2PKH address.
func (w *Wallet) PubKeyForAddress(a dcrutil.Address) (chainec.PublicKey, error) {
	const op errors.Op = "wallet.PubKeyForAddress"
	var pubKey chainec.PublicKey
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		managedAddr, err := w.Manager.Address(addrmgrNs, a)
		if err != nil {
			return err
		}
		managedPubKeyAddr, ok := managedAddr.(udb.ManagedPubKeyAddress)
		if !ok {
			return errors.New("address does not have an associated public key")
		}
		pubKey = managedPubKeyAddr.PubKey()
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return pubKey, nil
}

// SignMessage returns the signature of a signed message using an address'
// associated private key.
func (w *Wallet) SignMessage(msg string, addr dcrutil.Address) (sig []byte, err error) {
	const op errors.Op = "wallet.SignMessage"
	var buf bytes.Buffer
	wire.WriteVarString(&buf, 0, "Decred Signed Message:\n")
	wire.WriteVarString(&buf, 0, msg)
	messageHash := chainhash.HashB(buf.Bytes())
	var privKey chainec.PrivateKey
	var done func()
	defer func() {
		if done != nil {
			done()
		}
	}()
	err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		privKey, done, err = w.Manager.PrivateKey(addrmgrNs, addr)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	pkCast, ok := privKey.(*secp256k1.PrivateKey)
	if !ok {
		return nil, errors.E(op, "unable to create secp256k1.PrivateKey from chainec.PrivateKey")
	}
	sig, err = secp256k1.SignCompact(pkCast, messageHash, true)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return sig, nil
}

// VerifyMessage verifies that sig is a valid signature of msg and was created
// using the secp256k1 private key for addr.
func VerifyMessage(msg string, addr dcrutil.Address, sig []byte, params dcrutil.AddressParams) (bool, error) {
	const op errors.Op = "wallet.VerifyMessage"
	// Validate the signature - this just shows that it was valid for any pubkey
	// at all. Whether the pubkey matches is checked below.
	var buf bytes.Buffer
	wire.WriteVarString(&buf, 0, "Decred Signed Message:\n")
	wire.WriteVarString(&buf, 0, msg)
	expectedMessageHash := chainhash.HashB(buf.Bytes())
	pk, wasCompressed, err := chainec.Secp256k1.RecoverCompact(sig,
		expectedMessageHash)
	if err != nil {
		return false, errors.E(op, err)
	}

	// Reconstruct the address from the recovered pubkey.
	var serializedPK []byte
	if wasCompressed {
		serializedPK = pk.SerializeCompressed()
	} else {
		serializedPK = pk.SerializeUncompressed()
	}
	recoveredAddr, err := dcrutil.NewAddressSecpPubKey(serializedPK, params)
	if err != nil {
		return false, errors.E(op, err)
	}

	// Return whether addresses match.
	return recoveredAddr.Address() == addr.Address(), nil
}

// HaveAddress returns whether the wallet is the owner of the address a.
func (w *Wallet) HaveAddress(a dcrutil.Address) (bool, error) {
	const op errors.Op = "wallet.HaveAddress"
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		_, err := w.Manager.Address(addrmgrNs, a)
		return err
	})
	if err != nil {
		if errors.Is(errors.NotExist, err) {
			return false, nil
		}
		return false, errors.E(op, err)
	}
	return true, nil
}

// AccountOfAddress finds the account that an address is associated with.
func (w *Wallet) AccountOfAddress(a dcrutil.Address) (uint32, error) {
	const op errors.Op = "wallet.AccountOfAddress"
	var account uint32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		account, err = w.Manager.AddrAccount(addrmgrNs, a)
		return err
	})
	if err != nil {
		return 0, errors.E(op, err)
	}
	return account, nil
}

// AddressInfo returns detailed information regarding a wallet address.
func (w *Wallet) AddressInfo(a dcrutil.Address) (udb.ManagedAddress, error) {
	const op errors.Op = "wallet.AddressInfo"
	var managedAddress udb.ManagedAddress
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		managedAddress, err = w.Manager.Address(addrmgrNs, a)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return managedAddress, nil
}

// AccountNumber returns the account number for an account name.
func (w *Wallet) AccountNumber(accountName string) (uint32, error) {
	const op errors.Op = "wallet.AccountNumber"
	var account uint32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		account, err = w.Manager.LookupAccount(addrmgrNs, accountName)
		return err
	})
	if err != nil {
		return 0, errors.E(op, err)
	}
	return account, nil
}

// AccountName returns the name of an account.
func (w *Wallet) AccountName(accountNumber uint32) (string, error) {
	const op errors.Op = "wallet.AccountName"
	var accountName string
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		accountName, err = w.Manager.AccountName(addrmgrNs, accountNumber)
		return err
	})
	if err != nil {
		return "", errors.E(op, err)
	}
	return accountName, nil
}

// AccountProperties returns the properties of an account, including address
// indexes and name. It first fetches the desynced information from the address
// manager, then updates the indexes based on the address pools.
func (w *Wallet) AccountProperties(acct uint32) (*udb.AccountProperties, error) {
	const op errors.Op = "wallet.AccountProperties"
	var props *udb.AccountProperties
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		waddrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		props, err = w.Manager.AccountProperties(waddrmgrNs, acct)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return props, nil
}

// RenameAccount sets the name for an account number to newName.
func (w *Wallet) RenameAccount(account uint32, newName string) error {
	const op errors.Op = "wallet.RenameAccount"
	var props *udb.AccountProperties
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		err := w.Manager.RenameAccount(addrmgrNs, account, newName)
		if err != nil {
			return err
		}
		props, err = w.Manager.AccountProperties(addrmgrNs, account)
		return err
	})
	if err != nil {
		return errors.E(op, err)
	}
	w.NtfnServer.notifyAccountProperties(props)
	return nil
}

// NextAccount creates the next account and returns its account number.  The
// name must be unique to the account.  In order to support automatic seed
// restoring, new accounts may not be created when all of the previous 100
// accounts have no transaction history (this is a deviation from the BIP0044
// spec, which allows no unused account gaps).
func (w *Wallet) NextAccount(name string) (uint32, error) {
	const op errors.Op = "wallet.NextAccount"
	maxEmptyAccounts := uint32(w.accountGapLimit)
	var account uint32
	var props *udb.AccountProperties
	var xpub *hdkeychain.ExtendedKey
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)

		// Ensure that there is transaction history in the last 100 accounts.
		var err error
		lastAcct, err := w.Manager.LastAccount(addrmgrNs)
		if err != nil {
			return err
		}
		canCreate := false
		for i := uint32(0); i < maxEmptyAccounts; i++ {
			a := lastAcct - i
			if a == 0 && i < maxEmptyAccounts-1 {
				// Less than 100 accounts total.
				canCreate = true
				break
			}
			props, err := w.Manager.AccountProperties(addrmgrNs, a)
			if err != nil {
				return err
			}
			if props.LastUsedExternalIndex != ^uint32(0) || props.LastUsedInternalIndex != ^uint32(0) {
				canCreate = true
				break
			}
		}
		if !canCreate {
			return errors.New("last 100 accounts have no transaction history")
		}

		account, err = w.Manager.NewAccount(addrmgrNs, name)
		if err != nil {
			return err
		}

		props, err = w.Manager.AccountProperties(addrmgrNs, account)
		if err != nil {
			return err
		}

		xpub, err = w.Manager.AccountExtendedPubKey(tx, account)
		if err != nil {
			return err
		}

		gapLimit := uint32(w.gapLimit)
		err = w.Manager.SyncAccountToAddrIndex(addrmgrNs, account,
			gapLimit, udb.ExternalBranch)
		if err != nil {
			return err
		}
		return w.Manager.SyncAccountToAddrIndex(addrmgrNs, account,
			gapLimit, udb.InternalBranch)
	})
	if err != nil {
		return 0, errors.E(op, err)
	}

	extKey, intKey, err := deriveBranches(xpub)
	if err != nil {
		return 0, errors.E(op, err)
	}
	w.addressBuffersMu.Lock()
	w.addressBuffers[account] = &bip0044AccountData{
		xpub:        xpub,
		albExternal: addressBuffer{branchXpub: extKey, lastUsed: ^uint32(0)},
		albInternal: addressBuffer{branchXpub: intKey, lastUsed: ^uint32(0)},
	}
	w.addressBuffersMu.Unlock()

	if n, err := w.NetworkBackend(); err == nil {
		errs := make(chan error, 2)
		for _, branchKey := range []*hdkeychain.ExtendedKey{extKey, intKey} {
			branchKey := branchKey
			go func() {
				addrs, err := deriveChildAddresses(branchKey, 0,
					uint32(w.gapLimit), w.chainParams)
				if err != nil {
					errs <- err
					return
				}
				errs <- n.LoadTxFilter(context.TODO(), false, addrs, nil)
			}()
		}
		for i := 0; i < cap(errs); i++ {
			err := <-errs
			if err != nil {
				return 0, errors.E(op, err)
			}
		}
	}

	w.NtfnServer.notifyAccountProperties(props)

	return account, nil
}

// MasterPubKey returns the BIP0044 master public key for the passed account.
func (w *Wallet) MasterPubKey(account uint32) (*hdkeychain.ExtendedKey, error) {
	const op errors.Op = "wallet.MasterPubKey"
	var masterPubKey string
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		masterPubKey, err = w.Manager.GetMasterPubkey(addrmgrNs, account)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	extKey, err := hdkeychain.NewKeyFromString(masterPubKey, w.chainParams)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return extKey, nil
}

// MasterPrivKey returns the extended private key for the given account. The
// account must exist and the wallet must be unlocked, otherwise this function
// fails.
func (w *Wallet) MasterPrivKey(account uint32) (*hdkeychain.ExtendedKey, error) {
	const op errors.Op = "wallet.MasterPrivKey"

	var privKey *hdkeychain.ExtendedKey
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		var err error
		privKey, err = w.Manager.AccountExtendedPrivKey(tx, account)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	return privKey, nil
}

// GetTransactionsByHashes returns all known transactions identified by a slice
// of transaction hashes.  It is possible that not all transactions are found,
// and in this case the known results will be returned along with an inventory
// vector of all missing transactions and an error with code
// NotExist.
func (w *Wallet) GetTransactionsByHashes(txHashes []*chainhash.Hash) (txs []*wire.MsgTx, notFound []*wire.InvVect, err error) {
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		for _, hash := range txHashes {
			tx, err := w.TxStore.Tx(ns, hash)
			if err != nil {
				return err
			}
			if tx == nil {
				notFound = append(notFound, wire.NewInvVect(wire.InvTypeTx, hash))
			} else {
				txs = append(txs, tx)
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	if len(notFound) != 0 {
		err = errors.E(errors.NotExist, "transaction(s) not found")
	}
	return
}

// CreditCategory describes the type of wallet transaction output.  The category
// of "sent transactions" (debits) is always "send", and is not expressed by
// this type.
//
// TODO: This is a requirement of the RPC server and should be moved.
type CreditCategory byte

// These constants define the possible credit categories.
const (
	CreditReceive CreditCategory = iota
	CreditGenerate
	CreditImmature
)

// String returns the category as a string.  This string may be used as the
// JSON string for categories as part of listtransactions and gettransaction
// RPC responses.
func (c CreditCategory) String() string {
	switch c {
	case CreditReceive:
		return "receive"
	case CreditGenerate:
		return "generate"
	case CreditImmature:
		return "immature"
	default:
		return "unknown"
	}
}

// RecvCategory returns the category of received credit outputs from a
// transaction record.  The passed block chain height is used to distinguish
// immature from mature coinbase outputs.
//
// TODO: This is intended for use by the RPC server and should be moved out of
// this package at a later time.
func RecvCategory(details *udb.TxDetails, syncHeight int32, chainParams *chaincfg.Params) CreditCategory {
	if blockchain.IsCoinBaseTx(&details.MsgTx) {
		if coinbaseMatured(chainParams, details.Block.Height, syncHeight) {
			return CreditGenerate
		}
		return CreditImmature
	}
	return CreditReceive
}

// listTransactions creates a object that may be marshalled to a response result
// for a listtransactions RPC.
//
// TODO: This should be moved to the jsonrpc package.
func listTransactions(tx walletdb.ReadTx, details *udb.TxDetails, addrMgr *udb.Manager, syncHeight int32, net *chaincfg.Params) (sends, receives []types.ListTransactionsResult) {
	addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)

	var (
		blockHashStr  string
		blockTime     int64
		confirmations int64
	)
	if details.Block.Height != -1 {
		blockHashStr = details.Block.Hash.String()
		blockTime = details.Block.Time.Unix()
		confirmations = int64(confirms(details.Block.Height, syncHeight))
	}

	txHashStr := details.Hash.String()
	received := details.Received.Unix()
	generated := blockchain.IsCoinBaseTx(&details.MsgTx)
	recvCat := RecvCategory(details, syncHeight, net).String()

	send := len(details.Debits) != 0

	txTypeStr := types.LTTTRegular
	switch details.TxType {
	case stake.TxTypeSStx:
		txTypeStr = types.LTTTTicket
	case stake.TxTypeSSGen:
		txTypeStr = types.LTTTVote
	case stake.TxTypeSSRtx:
		txTypeStr = types.LTTTRevocation
	}

	// Fee can only be determined if every input is a debit.
	var feeF64 float64
	if len(details.Debits) == len(details.MsgTx.TxIn) {
		var debitTotal dcrutil.Amount
		for _, deb := range details.Debits {
			debitTotal += deb.Amount
		}
		var outputTotal dcrutil.Amount
		for _, output := range details.MsgTx.TxOut {
			outputTotal += dcrutil.Amount(output.Value)
		}
		// Note: The actual fee is debitTotal - outputTotal.  However,
		// this RPC reports negative numbers for fees, so the inverse
		// is calculated.
		feeF64 = (outputTotal - debitTotal).ToCoin()
	}

outputs:
	for i, output := range details.MsgTx.TxOut {
		// Determine if this output is a credit.  Change outputs are skipped.
		var isCredit bool
		for _, cred := range details.Credits {
			if cred.Index == uint32(i) {
				if cred.Change {
					continue outputs
				}
				isCredit = true
				break
			}
		}

		var address string
		var accountName string
		_, addrs, _, _ := txscript.ExtractPkScriptAddrs(output.Version,
			output.PkScript, net)
		if len(addrs) == 1 {
			addr := addrs[0]
			address = addr.Address()
			account, err := addrMgr.AddrAccount(addrmgrNs, addrs[0])
			if err == nil {
				accountName, err = addrMgr.AccountName(addrmgrNs, account)
				if err != nil {
					accountName = ""
				}
			}
		}

		amountF64 := dcrutil.Amount(output.Value).ToCoin()
		result := types.ListTransactionsResult{
			// Fields left zeroed:
			//   InvolvesWatchOnly
			//   BlockIndex
			//
			// Fields set below:
			//   Account (only for non-"send" categories)
			//   Category
			//   Amount
			//   Fee
			Address:         address,
			Vout:            uint32(i),
			Confirmations:   confirmations,
			Generated:       generated,
			BlockHash:       blockHashStr,
			BlockTime:       blockTime,
			TxID:            txHashStr,
			WalletConflicts: []string{},
			Time:            received,
			TimeReceived:    received,
			TxType:          &txTypeStr,
		}

		// Add a received/generated/immature result if this is a credit.
		// If the output was spent, create a second result under the
		// send category with the inverse of the output amount.  It is
		// therefore possible that a single output may be included in
		// the results set zero, one, or two times.
		//
		// Since credits are not saved for outputs that are not
		// controlled by this wallet, all non-credits from transactions
		// with debits are grouped under the send category.

		if send {
			result.Category = "send"
			result.Amount = -amountF64
			result.Fee = &feeF64
			sends = append(sends, result)
		}
		if isCredit {
			result.Account = accountName
			result.Category = recvCat
			result.Amount = amountF64
			result.Fee = nil
			receives = append(receives, result)
		}
	}
	return sends, receives
}

// ListSinceBlock returns a slice of objects with details about transactions
// since the given block. If the block is -1 then all transactions are included.
// This is intended to be used for listsinceblock RPC replies.
func (w *Wallet) ListSinceBlock(start, end, syncHeight int32) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListSinceBlock"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			for _, detail := range details {
				sends, receives := listTransactions(tx, &detail,
					w.Manager, syncHeight, w.chainParams)
				txList = append(txList, receives...)
				txList = append(txList, sends...)
			}
			return false, nil
		}

		return w.TxStore.RangeTransactions(txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return txList, nil
}

// ListTransactions returns a slice of objects with details about a recorded
// transaction.  This is intended to be used for listtransactions RPC
// replies.
func (w *Wallet) ListTransactions(from, count int) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListTransactions"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		// Need to skip the first from transactions, and after those, only
		// include the next count transactions.
		skipped := 0
		n := 0

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			// Iterate over transactions at this height in reverse order.
			// This does nothing for unmined transactions, which are
			// unsorted, but it will process mined transactions in the
			// reverse order they were marked mined.
			for i := len(details) - 1; i >= 0; i-- {
				if n >= count {
					return true, nil
				}

				if from > skipped {
					skipped++
					continue
				}

				sends, receives := listTransactions(tx, &details[i],
					w.Manager, tipHeight, w.chainParams)
				txList = append(txList, sends...)
				txList = append(txList, receives...)

				if len(sends) != 0 || len(receives) != 0 {
					n++
				}
			}

			return false, nil
		}

		// Return newer results first by starting at mempool height and working
		// down to the genesis block.
		return w.TxStore.RangeTransactions(txmgrNs, -1, 0, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	// reverse the list so that it is sorted from old to new.
	for i, j := 0, len(txList)-1; i < j; i, j = i+1, j-1 {
		txList[i], txList[j] = txList[j], txList[i]
	}
	return txList, nil
}

// ListAddressTransactions returns a slice of objects with details about
// recorded transactions to or from any address belonging to a set.  This is
// intended to be used for listaddresstransactions RPC replies.
func (w *Wallet) ListAddressTransactions(pkHashes map[string]struct{}) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListAddressTransactions"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		rangeFn := func(details []udb.TxDetails) (bool, error) {
		loopDetails:
			for i := range details {
				detail := &details[i]

				for _, cred := range detail.Credits {
					pkScript := detail.MsgTx.TxOut[cred.Index].PkScript
					_, addrs, _, err := txscript.ExtractPkScriptAddrs(
						0, pkScript, w.chainParams)
					if err != nil || len(addrs) != 1 {
						continue
					}
					apkh, ok := addrs[0].(*dcrutil.AddressPubKeyHash)
					if !ok {
						continue
					}
					_, ok = pkHashes[string(apkh.ScriptAddress())]
					if !ok {
						continue
					}

					sends, receives := listTransactions(tx, detail,
						w.Manager, tipHeight, w.chainParams)
					txList = append(txList, receives...)
					txList = append(txList, sends...)
					continue loopDetails
				}
			}
			return false, nil
		}

		return w.TxStore.RangeTransactions(txmgrNs, 0, -1, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return txList, nil
}

// ListAllTransactions returns a slice of objects with details about a recorded
// transaction.  This is intended to be used for listalltransactions RPC
// replies.
func (w *Wallet) ListAllTransactions() ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListAllTransactions"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			// Iterate over transactions at this height in reverse
			// order.  This does nothing for unmined transactions,
			// which are unsorted, but it will process mined
			// transactions in the reverse order they were marked
			// mined.
			for i := len(details) - 1; i >= 0; i-- {
				sends, receives := listTransactions(tx, &details[i],
					w.Manager, tipHeight, w.chainParams)
				txList = append(txList, sends...)
				txList = append(txList, receives...)
			}
			return false, nil
		}

		// Return newer results first by starting at mempool height and
		// working down to the genesis block.
		return w.TxStore.RangeTransactions(txmgrNs, -1, 0, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	// reverse the list so that it is sorted from old to new.
	for i, j := 0, len(txList)-1; i < j; i, j = i+1, j-1 {
		txList[i], txList[j] = txList[j], txList[i]
	}
	return txList, nil
}

// ListTransactionDetails returns the listtransaction results for a single
// transaction.
func (w *Wallet) ListTransactionDetails(txHash *chainhash.Hash) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListTransactionDetails"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		txd, err := w.TxStore.TxDetails(txmgrNs, txHash)
		if err != nil {
			return err
		}
		sends, receives := listTransactions(dbtx, txd, w.Manager, tipHeight, w.chainParams)
		txList = make([]types.ListTransactionsResult, 0, len(sends)+len(receives))
		txList = append(txList, receives...)
		txList = append(txList, sends...)
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return txList, nil
}

// BlockIdentifier identifies a block by either a height in the main chain or a
// hash.
type BlockIdentifier struct {
	height int32
	hash   *chainhash.Hash
}

// NewBlockIdentifierFromHeight constructs a BlockIdentifier for a block height.
func NewBlockIdentifierFromHeight(height int32) *BlockIdentifier {
	return &BlockIdentifier{height: height}
}

// NewBlockIdentifierFromHash constructs a BlockIdentifier for a block hash.
func NewBlockIdentifierFromHash(hash *chainhash.Hash) *BlockIdentifier {
	return &BlockIdentifier{hash: hash}
}

// BlockInfo records info pertaining to a block.  It does not include any
// information about wallet transactions contained in the block.
type BlockInfo struct {
	Hash             chainhash.Hash
	Height           int32
	Confirmations    int32
	Header           []byte
	Timestamp        int64
	StakeInvalidated bool
}

// BlockInfo returns info regarding a block recorded by the wallet.
func (w *Wallet) BlockInfo(blockID *BlockIdentifier) (*BlockInfo, error) {
	const op errors.Op = "wallet.BlockInfo"
	var blockInfo *BlockInfo
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		blockHash := blockID.hash
		if blockHash == nil {
			hash, err := w.TxStore.GetMainChainBlockHashForHeight(txmgrNs,
				blockID.height)
			if err != nil {
				return err
			}
			blockHash = &hash
		}
		header, err := w.TxStore.GetSerializedBlockHeader(txmgrNs, blockHash)
		if err != nil {
			return err
		}
		height := udb.ExtractBlockHeaderHeight(header)
		inMainChain, invalidated := w.TxStore.BlockInMainChain(dbtx, blockHash)
		var confs int32
		if inMainChain {
			confs = confirms(height, tipHeight)
		}
		blockInfo = &BlockInfo{
			Hash:             *blockHash,
			Height:           height,
			Confirmations:    confs,
			Header:           header,
			Timestamp:        udb.ExtractBlockHeaderTime(header),
			StakeInvalidated: invalidated,
		}
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return blockInfo, nil
}

// TransactionSummary returns details about a recorded transaction that is
// relevant to the wallet in some way.
func (w *Wallet) TransactionSummary(txHash *chainhash.Hash) (txSummary *TransactionSummary, confs int32, blockHash *chainhash.Hash, err error) {
	const opf = "wallet.TransactionSummary(%v)"
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.TxStore.MainChainTip(ns)
		txDetails, err := w.TxStore.TxDetails(ns, txHash)
		if err != nil {
			return err
		}
		txSummary = new(TransactionSummary)
		*txSummary = makeTxSummary(dbtx, w, txDetails)
		confs = confirms(txDetails.Height(), tipHeight)
		if confs > 0 {
			blockHash = &txDetails.Block.Hash
		}
		return nil
	})
	if err != nil {
		op := errors.Opf(opf, txHash)
		return nil, 0, nil, errors.E(op, err)
	}
	return txSummary, confs, blockHash, nil
}

// GetTicketsResult response struct for gettickets rpc request
type GetTicketsResult struct {
	Tickets []*TicketSummary
}

// fetchTicketDetails returns the ticket details of the provided ticket hash.
func (w *Wallet) fetchTicketDetails(ns walletdb.ReadBucket, hash *chainhash.Hash) (*udb.TicketDetails, error) {
	txDetail, err := w.TxStore.TxDetails(ns, hash)
	if err != nil {
		return nil, err
	}

	if txDetail.TxType != stake.TxTypeSStx {
		return nil, errors.Errorf("%v is not a ticket", hash)
	}

	ticketDetails, err := w.TxStore.TicketDetails(ns, txDetail)
	if err != nil {
		return nil, errors.Errorf("%v while trying to get ticket"+
			" details for txhash: %v", err, hash)
	}

	return ticketDetails, nil
}

// GetTicketInfoPrecise returns the ticket summary and the corresponding block header
// for the provided ticket.  The ticket summary is comprised of the transaction
// summmary for the ticket, the spender (if already spent) and the ticket's
// current status.
//
// If the ticket is unmined, then the returned block header will be nil.
//
// The argument chainClient is always expected to be not nil in this case,
// otherwise one should use the alternative GetTicketInfo instead.  With
// the ability to use the rpc chain client, this function is able to determine
// whether a ticket has been missed or not.  Otherwise, it is just known to be
// unspent (possibly live or missed).
func (w *Wallet) GetTicketInfoPrecise(ctx context.Context, rpcCaller Caller, hash *chainhash.Hash) (*TicketSummary, *wire.BlockHeader, error) {
	const op errors.Op = "wallet.GetTicketInfoPrecise"

	var ticketSummary *TicketSummary
	var blockHeader *wire.BlockHeader

	rpc := dcrd.New(rpcCaller)
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		ticketDetails, err := w.fetchTicketDetails(txmgrNs, hash)
		if err != nil {
			return err
		}

		ticketSummary = makeTicketSummary(ctx, rpc, dbtx, w, ticketDetails)
		if ticketDetails.Ticket.Block.Height == -1 {
			// unmined tickets do not have an associated block header
			return nil
		}

		// Fetch the associated block header of the ticket.
		hBytes, err := w.TxStore.GetSerializedBlockHeader(txmgrNs,
			&ticketDetails.Ticket.Block.Hash)
		if err != nil {
			return err
		}

		blockHeader = new(wire.BlockHeader)
		err = blockHeader.FromBytes(hBytes)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	return ticketSummary, blockHeader, nil
}

// GetTicketInfo returns the ticket summary and the corresponding block header
// for the provided ticket. The ticket summary is comprised of the transaction
// summmary for the ticket, the spender (if already spent) and the ticket's
// current status.
//
// If the ticket is unmined, then the returned block header will be nil.
func (w *Wallet) GetTicketInfo(hash *chainhash.Hash) (*TicketSummary, *wire.BlockHeader, error) {
	const op errors.Op = "wallet.GetTicketInfo"

	var ticketSummary *TicketSummary
	var blockHeader *wire.BlockHeader

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		ticketDetails, err := w.fetchTicketDetails(txmgrNs, hash)
		if err != nil {
			return err
		}

		ticketSummary = makeTicketSummary(context.Background(), nil, dbtx, w, ticketDetails)
		if ticketDetails.Ticket.Block.Height == -1 {
			// unmined tickets do not have an associated block header
			return nil
		}

		// Fetch the associated block header of the ticket.
		hBytes, err := w.TxStore.GetSerializedBlockHeader(txmgrNs,
			&ticketDetails.Ticket.Block.Hash)
		if err != nil {
			return err
		}

		blockHeader = new(wire.BlockHeader)
		err = blockHeader.FromBytes(hBytes)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	return ticketSummary, blockHeader, nil
}

// GetTicketsPrecise calls function f for all tickets located in between the
// given startBlock and endBlock.  TicketSummary includes TransactionSummmary
// for the ticket and the spender (if already spent) and the ticket's current
// status. The function f also receives block header of the ticket. All
// tickets on a given call belong to the same block and at least one ticket
// is present when f is called. If the ticket is unmined, the block header will
// be nil.
//
// The function f may return an error which, if non-nil, is propagated to the
// caller.  Additionally, a boolean return value allows exiting the function
// early without reading any additional transactions when true.
//
// The arguments to f may be reused and should not be kept by the caller.
//
// The argument chainClient is always expected to be not nil in this case,
// otherwise one should use the alternative GetTickets instead.  With
// the ability to use the rpc chain client, this function is able to determine
// whether tickets have been missed or not.  Otherwise, tickets are just known
// to be unspent (possibly live or missed).
func (w *Wallet) GetTicketsPrecise(ctx context.Context, rpcCaller Caller,
	f func([]*TicketSummary, *wire.BlockHeader) (bool, error), startBlock, endBlock *BlockIdentifier) error {

	const op errors.Op = "wallet.GetTicketsPrecise"
	var start, end int32 = 0, -1

	if startBlock != nil {
		if startBlock.hash == nil {
			start = startBlock.height
		} else {
			err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
				ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
				serHeader, err := w.TxStore.GetSerializedBlockHeader(ns, startBlock.hash)
				if err != nil {
					return err
				}
				var startHeader wire.BlockHeader
				err = startHeader.Deserialize(bytes.NewReader(serHeader))
				if err != nil {
					return err
				}
				start = int32(startHeader.Height)
				return nil
			})
			if err != nil {
				return errors.E(op, err)
			}
		}
	}
	if endBlock != nil {
		if endBlock.hash == nil {
			end = endBlock.height
		} else {
			err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
				ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
				serHeader, err := w.TxStore.GetSerializedBlockHeader(ns, endBlock.hash)
				if err != nil {
					return err
				}
				var endHeader wire.BlockHeader
				err = endHeader.Deserialize(bytes.NewReader(serHeader))
				if err != nil {
					return err
				}
				end = int32(endHeader.Height)
				return nil
			})
			if err != nil {
				return errors.E(op, err)
			}
		}
	}

	rpc := dcrd.New(rpcCaller)
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		header := &wire.BlockHeader{}

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			tickets := make([]*TicketSummary, 0, len(details))

			for i := range details {
				// XXX Here is where I would look up the ticket information from the db so I can populate spenderhash and ticket status
				ticketInfo, err := w.TxStore.TicketDetails(txmgrNs, &details[i])
				if err != nil {
					return false, errors.Errorf("%v while trying to get ticket details for txhash: %v", err, &details[i].Hash)
				}
				// Continue if not a ticket
				if ticketInfo == nil {
					continue
				}
				tickets = append(tickets, makeTicketSummary(ctx, rpc, dbtx, w, ticketInfo))
			}

			if len(tickets) == 0 {
				return false, nil
			}

			if details[0].Block.Height == -1 {
				return f(tickets, nil)
			}

			headerBytes, err := w.TxStore.GetSerializedBlockHeader(txmgrNs, &details[0].Block.Hash)
			if err != nil {
				return false, err
			}
			header.FromBytes(headerBytes)
			return f(tickets, header)
		}

		return w.TxStore.RangeTransactions(txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// GetTickets calls function f for all tickets located in between the
// given startBlock and endBlock.  TicketSummary includes TransactionSummmary
// for the ticket and the spender (if already spent) and the ticket's current
// status. The function f also receives block header of the ticket. All
// tickets on a given call belong to the same block and at least one ticket
// is present when f is called. If the ticket is unmined, the block header will
// be nil.
//
// The function f may return an error which, if non-nil, is propagated to the
// caller.  Additionally, a boolean return value allows exiting the function
// early without reading any additional transactions when true.
//
// The arguments to f may be reused and should not be kept by the caller.
//
// Because this function does not have any chain client argument, tickets are
// unable to be determined whether or not they have been missed, simply unspent.
func (w *Wallet) GetTickets(f func([]*TicketSummary, *wire.BlockHeader) (bool, error), startBlock, endBlock *BlockIdentifier) error {
	const op errors.Op = "wallet.GetTickets"
	var start, end int32 = 0, -1

	if startBlock != nil {
		if startBlock.hash == nil {
			start = startBlock.height
		} else {
			err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
				ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
				serHeader, err := w.TxStore.GetSerializedBlockHeader(ns, startBlock.hash)
				if err != nil {
					return err
				}
				var startHeader wire.BlockHeader
				err = startHeader.Deserialize(bytes.NewReader(serHeader))
				if err != nil {
					return err
				}
				start = int32(startHeader.Height)
				return nil
			})
			if err != nil {
				return errors.E(op, err)
			}
		}
	}
	if endBlock != nil {
		if endBlock.hash == nil {
			end = endBlock.height
		} else {
			err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
				ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
				serHeader, err := w.TxStore.GetSerializedBlockHeader(ns, endBlock.hash)
				if err != nil {
					return err
				}
				var endHeader wire.BlockHeader
				err = endHeader.Deserialize(bytes.NewReader(serHeader))
				if err != nil {
					return err
				}
				end = int32(endHeader.Height)
				return nil
			})
			if err != nil {
				return errors.E(op, err)
			}
		}
	}

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		header := &wire.BlockHeader{}

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			tickets := make([]*TicketSummary, 0, len(details))

			for i := range details {
				// XXX Here is where I would look up the ticket information from the db so I can populate spenderhash and ticket status
				ticketInfo, err := w.TxStore.TicketDetails(txmgrNs, &details[i])
				if err != nil {
					return false, errors.Errorf("%v while trying to get ticket details for txhash: %v", err, &details[i].Hash)
				}
				// Continue if not a ticket
				if ticketInfo == nil {
					continue
				}
				tickets = append(tickets, makeTicketSummary(context.Background(), nil, dbtx, w, ticketInfo))
			}

			if len(tickets) == 0 {
				return false, nil
			}

			if details[0].Block.Height == -1 {
				return f(tickets, nil)
			}

			headerBytes, err := w.TxStore.GetSerializedBlockHeader(txmgrNs, &details[0].Block.Hash)
			if err != nil {
				return false, err
			}
			header.FromBytes(headerBytes)
			return f(tickets, header)
		}

		return w.TxStore.RangeTransactions(txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// GetTransactionsResult is the result of the wallet's GetTransactions method.
// See GetTransactions for more details.
type GetTransactionsResult struct {
	MinedTransactions   []Block
	UnminedTransactions []TransactionSummary
}

// GetTransactions runs the function f on all transactions between a starting
// and ending block.  Blocks in the block range may be specified by either a
// height or a hash.
//
// The function f may return an error which, if non-nil, is propagated to the
// caller.  Additionally, a boolean return value allows exiting the function
// early without reading any additional transactions when true.
//
// Transaction results are organized by blocks in ascending order and unmined
// transactions in an unspecified order.  Mined transactions are saved in a
// Block structure which records properties about the block. Unmined
// transactions are returned on a Block structure with height == -1.
//
// Internally this function uses the udb store RangeTransactions function,
// therefore the notes and restrictions of that function also apply here.
func (w *Wallet) GetTransactions(f func(*Block) (bool, error), startBlock, endBlock *BlockIdentifier) error {
	const op errors.Op = "wallet.GetTransactions"
	var start, end int32 = 0, -1

	if startBlock != nil {
		if startBlock.hash == nil {
			start = startBlock.height
		} else {
			err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
				ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
				serHeader, err := w.TxStore.GetSerializedBlockHeader(ns, endBlock.hash)
				if err != nil {
					return err
				}
				var startHeader wire.BlockHeader
				err = startHeader.Deserialize(bytes.NewReader(serHeader))
				if err != nil {
					return err
				}
				start = int32(startHeader.Height)
				return nil
			})
			if err != nil {
				return errors.E(op, err)
			}
		}
	}
	if endBlock != nil {
		if endBlock.hash == nil {
			end = endBlock.height
		} else {
			err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
				ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
				serHeader, err := w.TxStore.GetSerializedBlockHeader(ns, endBlock.hash)
				if err != nil {
					return err
				}
				var endHeader wire.BlockHeader
				err = endHeader.Deserialize(bytes.NewReader(serHeader))
				if err != nil {
					return err
				}
				end = int32(endHeader.Height)
				return nil
			})
			if err != nil {
				return errors.E(op, err)
			}
		}
	}

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			// TODO: probably should make RangeTransactions not reuse the
			// details backing array memory.
			dets := make([]udb.TxDetails, len(details))
			copy(dets, details)
			details = dets

			txs := make([]TransactionSummary, 0, len(details))
			for i := range details {
				txs = append(txs, makeTxSummary(dbtx, w, &details[i]))
			}

			var block *Block
			if details[0].Block.Height != -1 {
				serHeader, err := w.TxStore.GetSerializedBlockHeader(txmgrNs,
					&details[0].Block.Hash)
				if err != nil {
					return false, err
				}
				header := new(wire.BlockHeader)
				err = header.Deserialize(bytes.NewReader(serHeader))
				if err != nil {
					return false, err
				}
				block = &Block{
					Header:       header,
					Transactions: txs,
				}
			} else {
				block = &Block{
					Header:       nil,
					Transactions: txs,
				}
			}

			return f(block)
		}

		return w.TxStore.RangeTransactions(txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// AccountResult is a single account result for the AccountsResult type.
type AccountResult struct {
	udb.AccountProperties
	TotalBalance dcrutil.Amount
}

// AccountsResult is the resutl of the wallet's Accounts method.  See that
// method for more details.
type AccountsResult struct {
	Accounts           []AccountResult
	CurrentBlockHash   *chainhash.Hash
	CurrentBlockHeight int32
}

// Accounts returns the current names, numbers, and total balances of all
// accounts in the wallet.  The current chain tip is included in the result for
// atomicity reasons.
//
// TODO(jrick): Is the chain tip really needed, since only the total balances
// are included?
func (w *Wallet) Accounts() (*AccountsResult, error) {
	const op errors.Op = "wallet.Accounts"
	var (
		accounts  []AccountResult
		tipHash   chainhash.Hash
		tipHeight int32
	)
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		tipHash, tipHeight = w.TxStore.MainChainTip(txmgrNs)
		unspent, err := w.TxStore.UnspentOutputs(txmgrNs)
		if err != nil {
			return err
		}
		err = w.Manager.ForEachAccount(addrmgrNs, func(acct uint32) error {
			props, err := w.Manager.AccountProperties(addrmgrNs, acct)
			if err != nil {
				return err
			}
			accounts = append(accounts, AccountResult{
				AccountProperties: *props,
				// TotalBalance set below
			})
			return nil
		})
		if err != nil {
			return err
		}
		m := make(map[uint32]*dcrutil.Amount)
		for i := range accounts {
			a := &accounts[i]
			m[a.AccountNumber] = &a.TotalBalance
		}
		for i := range unspent {
			output := unspent[i]
			var outputAcct uint32
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(
				0, output.PkScript, w.chainParams)
			if err == nil && len(addrs) > 0 {
				outputAcct, err = w.Manager.AddrAccount(addrmgrNs, addrs[0])
			}
			if err == nil {
				amt, ok := m[outputAcct]
				if ok {
					*amt += output.Amount
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return &AccountsResult{
		Accounts:           accounts,
		CurrentBlockHash:   &tipHash,
		CurrentBlockHeight: tipHeight,
	}, nil
}

// creditSlice satisifies the sort.Interface interface to provide sorting
// transaction credits from oldest to newest.  Credits with the same receive
// time and mined in the same block are not guaranteed to be sorted by the order
// they appear in the block.  Credits from the same transaction are sorted by
// output index.
type creditSlice []*udb.Credit

func (s creditSlice) Len() int {
	return len(s)
}

func (s creditSlice) Less(i, j int) bool {
	switch {
	// If both credits are from the same tx, sort by output index.
	case s[i].OutPoint.Hash == s[j].OutPoint.Hash:
		return s[i].OutPoint.Index < s[j].OutPoint.Index

	// If both transactions are unmined, sort by their received date.
	case s[i].Height == -1 && s[j].Height == -1:
		return s[i].Received.Before(s[j].Received)

	// Unmined (newer) txs always come last.
	case s[i].Height == -1:
		return false
	case s[j].Height == -1:
		return true

	// If both txs are mined in different blocks, sort by block height.
	default:
		return s[i].Height < s[j].Height
	}
}

func (s creditSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// ListUnspent returns a slice of objects representing the unspent wallet
// transactions fitting the given criteria. The confirmations will be more than
// minconf, less than maxconf and if addresses is populated only the addresses
// contained within it will be considered.  If we know nothing about a
// transaction an empty array will be returned.
func (w *Wallet) ListUnspent(minconf, maxconf int32, addresses map[string]struct{}) ([]*types.ListUnspentResult, error) {
	const op errors.Op = "wallet.ListUnspent"
	var results []*types.ListUnspentResult
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		filter := len(addresses) != 0
		unspent, err := w.TxStore.UnspentOutputs(txmgrNs)
		if err != nil {
			return err
		}
		sort.Sort(sort.Reverse(creditSlice(unspent)))

		defaultAccountName, err := w.Manager.AccountName(
			addrmgrNs, udb.DefaultAccountNum)
		if err != nil {
			return err
		}

		for i := range unspent {
			output := unspent[i]

			details, err := w.TxStore.TxDetails(txmgrNs, &output.Hash)
			if err != nil {
				return err
			}

			// Outputs with fewer confirmations than the minimum or more
			// confs than the maximum are excluded.
			confs := confirms(output.Height, tipHeight)
			if confs < minconf || confs > maxconf {
				continue
			}

			// Only mature coinbase outputs are included.
			if output.FromCoinBase {
				if !coinbaseMatured(w.chainParams, output.Height, tipHeight) {
					continue
				}
			}

			switch details.TxRecord.TxType {
			case stake.TxTypeSStx:
				// Ticket commitment, only spendable after ticket maturity.
				if output.Index == 0 {
					if !ticketMatured(w.chainParams, details.Height(), tipHeight) {
						continue
					}
				}
				// Change outputs.
				if (output.Index > 0) && (output.Index%2 == 0) {
					if !ticketChangeMatured(w.chainParams, details.Height(), tipHeight) {
						continue
					}
				}
			case stake.TxTypeSSGen:
				// All non-OP_RETURN outputs for SSGen tx are only spendable
				// after coinbase maturity many blocks.
				if !coinbaseMatured(w.chainParams, details.Height(), tipHeight) {
					continue
				}
			case stake.TxTypeSSRtx:
				// All outputs for SSRtx tx are only spendable
				// after coinbase maturity many blocks.
				if !coinbaseMatured(w.chainParams, details.Height(), tipHeight) {
					continue
				}
			}

			// Exclude locked outputs from the result set.
			if w.LockedOutpoint(output.OutPoint) {
				continue
			}

			// Lookup the associated account for the output.  Use the
			// default account name in case there is no associated account
			// for some reason, although this should never happen.
			//
			// This will be unnecessary once transactions and outputs are
			// grouped under the associated account in the db.
			acctName := defaultAccountName
			sc, addrs, _, err := txscript.ExtractPkScriptAddrs(
				0, output.PkScript, w.chainParams)
			if err != nil {
				continue
			}
			if len(addrs) > 0 {
				acct, err := w.Manager.AddrAccount(
					addrmgrNs, addrs[0])
				if err == nil {
					s, err := w.Manager.AccountName(
						addrmgrNs, acct)
					if err == nil {
						acctName = s
					}
				}
			}

			if filter {
				for _, addr := range addrs {
					_, ok := addresses[addr.Address()]
					if ok {
						goto include
					}
				}
				continue
			}

		include:
			// At the moment watch-only addresses are not supported, so all
			// recorded outputs that are not multisig are "spendable".
			// Multisig outputs are only "spendable" if all keys are
			// controlled by this wallet.
			//
			// TODO: Each case will need updates when watch-only addrs
			// is added.  For P2PK, P2PKH, and P2SH, the address must be
			// looked up and not be watching-only.  For multisig, all
			// pubkeys must belong to the manager with the associated
			// private key (currently it only checks whether the pubkey
			// exists, since the private key is required at the moment).
			var spendable bool
		scSwitch:
			switch sc {
			case txscript.PubKeyHashTy:
				spendable = true
			case txscript.PubKeyTy:
				spendable = true
			case txscript.ScriptHashTy:
				spendable = true
			case txscript.StakeGenTy:
				spendable = true
			case txscript.StakeRevocationTy:
				spendable = true
			case txscript.StakeSubChangeTy:
				spendable = true
			case txscript.MultiSigTy:
				for _, a := range addrs {
					_, err := w.Manager.Address(addrmgrNs, a)
					if err == nil {
						continue
					}
					if errors.Is(errors.NotExist, err) {
						break scSwitch
					}
					return err
				}
				spendable = true
			}

			result := &types.ListUnspentResult{
				TxID:          output.OutPoint.Hash.String(),
				Vout:          output.OutPoint.Index,
				Tree:          output.OutPoint.Tree,
				Account:       acctName,
				ScriptPubKey:  hex.EncodeToString(output.PkScript),
				TxType:        int(details.TxType),
				Amount:        output.Amount.ToCoin(),
				Confirmations: int64(confs),
				Spendable:     spendable,
			}

			// BUG: this should be a JSON array so that all
			// addresses can be included, or removed (and the
			// caller extracts addresses from the pkScript).
			if len(addrs) > 0 {
				result.Address = addrs[0].Address()
			}

			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return results, nil
}

// DumpWIFPrivateKey returns the WIF encoded private key for a
// single wallet address.
func (w *Wallet) DumpWIFPrivateKey(addr dcrutil.Address) (string, error) {
	const op errors.Op = "wallet.DumpWIFPrivateKey"
	var privKey chainec.PrivateKey
	var done func()
	defer func() {
		if done != nil {
			done()
		}
	}()
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		privKey, done, err = w.Manager.PrivateKey(addrmgrNs, addr)
		return err
	})
	if err != nil {
		return "", errors.E(op, err)
	}
	wif := dcrutil.NewWIF(privKey, w.chainParams.PrivateKeyID, dcrec.SignatureType(privKey.GetType()))
	return wif.String(), nil
}

// ImportPrivateKey imports a private key to the wallet and writes the new
// wallet to disk.
func (w *Wallet) ImportPrivateKey(wif *dcrutil.WIF) (string, error) {
	const op errors.Op = "wallet.ImportPrivateKey"
	// Attempt to import private key into wallet.
	var addr dcrutil.Address
	var props *udb.AccountProperties
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		maddr, err := w.Manager.ImportPrivateKey(addrmgrNs, wif)
		if err == nil {
			addr = maddr.Address()
			props, err = w.Manager.AccountProperties(
				addrmgrNs, udb.ImportedAddrAccount)
		}
		return err
	})
	if err != nil {
		return "", errors.E(op, err)
	}

	if n, err := w.NetworkBackend(); err == nil {
		err := n.LoadTxFilter(context.TODO(), false, []dcrutil.Address{addr}, nil)
		if err != nil {
			return "", errors.E(op, err)
		}
	}

	addrStr := addr.Address()
	log.Infof("Imported payment address %s", addrStr)

	w.NtfnServer.notifyAccountProperties(props)

	// Return the payment address string of the imported private key.
	return addrStr, nil
}

// ImportScript imports a redeemscript to the wallet. If it also allows the
// user to specify whether or not they want the redeemscript to be rescanned,
// and how far back they wish to rescan.
func (w *Wallet) ImportScript(rs []byte) error {
	const op errors.Op = "wallet.ImportScript"
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadWriteBucket(wtxmgrNamespaceKey)

		err := w.TxStore.InsertTxScript(txmgrNs, rs)
		if err != nil {
			return err
		}

		mscriptaddr, err := w.Manager.ImportScript(addrmgrNs, rs)
		if err != nil {
			switch {
			// Don't care if it's already there.
			case errors.Is(errors.Exist, err):
				return nil
			case errors.Is(errors.Locked, err):
				log.Debugf("failed to attempt script importation " +
					"of incoming tx because addrmgr was locked")
				return err
			default:
				return err
			}
		}
		addr := mscriptaddr.Address()

		if n, err := w.NetworkBackend(); err == nil {
			err := n.LoadTxFilter(context.TODO(), false, []dcrutil.Address{addr}, nil)
			if err != nil {
				return err
			}
		}

		log.Infof("Imported script with P2SH address %v", addr)
		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// RedeemScriptCopy returns a copy of a redeem script to redeem outputs paid to
// a P2SH address.
func (w *Wallet) RedeemScriptCopy(addr dcrutil.Address) ([]byte, error) {
	const op errors.Op = "wallet.RedeemScriptCopy"
	var scriptCopy []byte
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrNamespaceKey)
		script, done, err := w.Manager.RedeemScript(ns, addr)
		if err != nil {
			return err
		}
		defer done()
		scriptCopy = make([]byte, len(script))
		copy(scriptCopy, script)
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return scriptCopy, nil
}

// StakeInfoData carries counts of ticket states and other various stake data.
type StakeInfoData struct {
	BlockHeight  int64
	TotalSubsidy dcrutil.Amount
	Sdiff        dcrutil.Amount

	OwnMempoolTix  uint32
	Unspent        uint32
	Voted          uint32
	Revoked        uint32
	UnspentExpired uint32

	PoolSize      uint32
	AllMempoolTix uint32
	Immature      uint32
	Live          uint32
	Missed        uint32
	Expired       uint32
}

func isVote(tx *wire.MsgTx) bool {
	return stake.IsSSGen(tx)
}

func isRevocation(tx *wire.MsgTx) bool {
	return stake.IsSSRtx(tx)
}

// hasVotingAuthority returns whether the 0th output of a ticket purchase can be
// spent by a vote or revocation created by this wallet.
func (w *Wallet) hasVotingAuthority(addrmgrNs walletdb.ReadBucket, ticketPurchase *wire.MsgTx) (bool, error) {
	out := ticketPurchase.TxOut[0]
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(out.Version,
		out.PkScript, w.chainParams)
	if err != nil {
		return false, err
	}
	for _, a := range addrs {
		if w.Manager.ExistsHash160(addrmgrNs, a.Hash160()[:]) {
			return true, nil
		}
	}
	return false, nil
}

// StakeInfo collects and returns staking statistics for this wallet.
func (w *Wallet) StakeInfo() (*StakeInfoData, error) {
	const op errors.Op = "wallet.StakeInfo"

	var res StakeInfoData

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		tipHash, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		res.BlockHeight = int64(tipHeight)
		if deployments.DCP0001.Active(tipHeight, w.chainParams.Net) {
			tipHeader, err := w.TxStore.GetBlockHeader(dbtx, &tipHash)
			if err != nil {
				return err
			}
			sdiff, err := w.nextRequiredDCP0001PoSDifficulty(dbtx, tipHeader, nil)
			if err != nil {
				return err
			}
			res.Sdiff = sdiff
		}
		it := w.TxStore.IterateTickets(dbtx)
		defer it.Close()
		for it.Next() {
			// Skip tickets which are not owned by this wallet.
			owned, err := w.hasVotingAuthority(addrmgrNs, &it.MsgTx)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}

			// Check for tickets in mempool
			if it.Block.Height == -1 {
				res.OwnMempoolTix++
				continue
			}

			// Check for immature tickets
			if !ticketMatured(w.chainParams, it.Block.Height, tipHeight) {
				res.Immature++
				continue
			}

			// If the ticket was spent, look up the spending tx and determine if
			// it is a vote or revocation.  If it is a vote, add the earned
			// subsidy.
			if it.SpenderHash != (chainhash.Hash{}) {
				spender, err := w.TxStore.Tx(txmgrNs, &it.SpenderHash)
				if err != nil {
					return err
				}
				switch {
				case isVote(spender):
					res.Voted++

					// Add the subsidy.
					//
					// This is not the actual subsidy that was earned by this
					// wallet, but rather the stakebase sum.  If a user uses a
					// stakepool for voting, this value will include the total
					// subsidy earned by both the user and the pool together.
					// Similarly, for stakepool wallets, this includes the
					// customer's subsidy rather than being just the subsidy
					// earned by fees.
					res.TotalSubsidy += dcrutil.Amount(spender.TxIn[0].ValueIn)

				case isRevocation(spender):
					res.Revoked++

				default:
					return errors.E(errors.IO, errors.Errorf("ticket spender %v is neither vote nor revocation", &it.SpenderHash))
				}
				continue
			}

			// Ticket is matured but unspent.  Possible states are that the
			// ticket is live, expired, or missed.
			res.Unspent++
			if ticketExpired(w.chainParams, it.Block.Height, tipHeight) {
				res.UnspentExpired++
			}
		}
		return it.Err()
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return &res, nil
}

// StakeInfoPrecise collects and returns staking statistics for this wallet.  It
// uses RPC to query further information than StakeInfo.
func (w *Wallet) StakeInfoPrecise(ctx context.Context, rpcCaller Caller) (*StakeInfoData, error) {
	const op errors.Op = "wallet.StakeInfoPrecise"

	res := &StakeInfoData{}
	rpc := dcrd.New(rpcCaller)
	var g errgroup.Group
	g.Go(func() error {
		unminedTicketCount, err := rpc.MempoolCount(ctx, "tickets")
		if err != nil {
			return err
		}
		res.AllMempoolTix = uint32(unminedTicketCount)
		return nil
	})

	// Wallet does not yet know if/when a ticket was selected.  Keep track of
	// all tickets that are either live, expired, or missed and determine their
	// states later by querying the consensus RPC server.
	var liveOrExpiredOrMissed []*chainhash.Hash

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		tipHash, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		res.BlockHeight = int64(tipHeight)
		if deployments.DCP0001.Active(tipHeight, w.chainParams.Net) {
			tipHeader, err := w.TxStore.GetBlockHeader(dbtx, &tipHash)
			if err != nil {
				return err
			}
			sdiff, err := w.nextRequiredDCP0001PoSDifficulty(dbtx, tipHeader, nil)
			if err != nil {
				return err
			}
			res.Sdiff = sdiff
		}
		it := w.TxStore.IterateTickets(dbtx)
		defer it.Close()
		for it.Next() {
			// Skip tickets which are not owned by this wallet.
			owned, err := w.hasVotingAuthority(addrmgrNs, &it.MsgTx)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}

			// Check for tickets in mempool
			if it.Block.Height == -1 {
				res.OwnMempoolTix++
				continue
			}

			// Check for immature tickets
			if !ticketMatured(w.chainParams, it.Block.Height, tipHeight) {
				res.Immature++
				continue
			}

			// If the ticket was spent, look up the spending tx and determine if
			// it is a vote or revocation.  If it is a vote, add the earned
			// subsidy.
			if it.SpenderHash != (chainhash.Hash{}) {
				spender, err := w.TxStore.Tx(txmgrNs, &it.SpenderHash)
				if err != nil {
					return err
				}
				switch {
				case isVote(spender):
					res.Voted++

					// Add the subsidy.
					//
					// This is not the actual subsidy that was earned by this
					// wallet, but rather the stakebase sum.  If a user uses a
					// stakepool for voting, this value will include the total
					// subsidy earned by both the user and the pool together.
					// Similarly, for stakepool wallets, this includes the
					// customer's subsidy rather than being just the subsidy
					// earned by fees.
					res.TotalSubsidy += dcrutil.Amount(spender.TxIn[0].ValueIn)

				case isRevocation(spender):
					res.Revoked++

					// The ticket was revoked because it was either expired or
					// missed.  Append it to the liveOrExpiredOrMissed slice to
					// check this later.
					ticketHash := it.Hash
					liveOrExpiredOrMissed = append(liveOrExpiredOrMissed, &ticketHash)

				default:
					return errors.E(errors.IO, errors.Errorf("ticket spender %v is neither vote nor revocation", &it.SpenderHash))
				}
				continue
			}

			// Ticket is matured but unspent.  Possible states are that the
			// ticket is live, expired, or missed.
			res.Unspent++
			if ticketExpired(w.chainParams, it.Block.Height, tipHeight) {
				res.UnspentExpired++
			}
			ticketHash := it.Hash
			liveOrExpiredOrMissed = append(liveOrExpiredOrMissed, &ticketHash)
		}
		if err := it.Err(); err != nil {
			return err
		}

		// Include an estimate of the live ticket pool size. The correct
		// poolsize would be the pool size to be mined into the next block,
		// which takes into account maturing stake tickets, voters, and expiring
		// tickets. There currently isn't a way to get this from the consensus
		// RPC server, so just use the current block pool size as a "good
		// enough" estimate for now.
		serHeader, err := w.TxStore.GetSerializedBlockHeader(txmgrNs, &tipHash)
		if err != nil {
			return err
		}
		var tipHeader wire.BlockHeader
		err = tipHeader.Deserialize(bytes.NewReader(serHeader))
		if err != nil {
			return err
		}
		res.PoolSize = tipHeader.PoolSize

		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	// As the wallet is unaware of when a ticket was selected or missed, this
	// info must be queried from the consensus server.  If the ticket is neither
	// live nor expired, it is assumed missed.
	live, expired, err := rpc.ExistsLiveExpiredTickets(ctx, liveOrExpiredOrMissed)
	if err != nil {
		return nil, errors.E(op, err)
	}
	for i := range liveOrExpiredOrMissed {
		switch {
		case live.Get(i):
			res.Live++
		case expired.Get(i):
			res.Expired++
		default:
			res.Missed++
		}
	}

	// Wait for MempoolCount call from beginning of function to complete.
	err = g.Wait()
	if err != nil {
		return nil, errors.E(op, err)
	}

	return res, nil
}

// LockedOutpoint returns whether an outpoint has been marked as locked and
// should not be used as an input for created transactions.
func (w *Wallet) LockedOutpoint(op wire.OutPoint) bool {
	w.lockedOutpointMu.Lock()
	_, locked := w.lockedOutpoints[op]
	w.lockedOutpointMu.Unlock()
	return locked
}

// LockOutpoint marks an outpoint as locked, that is, it should not be used as
// an input for newly created transactions.
func (w *Wallet) LockOutpoint(op wire.OutPoint) {
	w.lockedOutpointMu.Lock()
	w.lockedOutpoints[op] = struct{}{}
	w.lockedOutpointMu.Unlock()
}

// UnlockOutpoint marks an outpoint as unlocked, that is, it may be used as an
// input for newly created transactions.
func (w *Wallet) UnlockOutpoint(op wire.OutPoint) {
	w.lockedOutpointMu.Lock()
	delete(w.lockedOutpoints, op)
	w.lockedOutpointMu.Unlock()
}

// ResetLockedOutpoints resets the set of locked outpoints so all may be used
// as inputs for new transactions.
func (w *Wallet) ResetLockedOutpoints() {
	w.lockedOutpointMu.Lock()
	w.lockedOutpoints = map[wire.OutPoint]struct{}{}
	w.lockedOutpointMu.Unlock()
}

// LockedOutpoints returns a slice of currently locked outpoints.  This is
// intended to be used by marshaling the result as a JSON array for
// listlockunspent RPC results.
func (w *Wallet) LockedOutpoints() []dcrdtypes.TransactionInput {
	w.lockedOutpointMu.Lock()
	locked := make([]dcrdtypes.TransactionInput, len(w.lockedOutpoints))
	i := 0
	for op := range w.lockedOutpoints {
		locked[i] = dcrdtypes.TransactionInput{
			Txid: op.Hash.String(),
			Vout: op.Index,
		}
		i++
	}
	w.lockedOutpointMu.Unlock()
	return locked
}

// UnminedTransactions returns all unmined transactions from the wallet.
// Transactions are sorted in dependency order making it suitable to range them
// in order to broadcast at wallet startup.
func (w *Wallet) UnminedTransactions() ([]*wire.MsgTx, error) {
	const op errors.Op = "wallet.UnminedTransactions"
	var txs []*wire.MsgTx
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		txs, err = w.TxStore.UnminedTxs(txmgrNs)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return txs, nil
}

// SortedActivePaymentAddresses returns a slice of all active payment
// addresses in a wallet.
func (w *Wallet) SortedActivePaymentAddresses() ([]string, error) {
	const op errors.Op = "wallet.SortedActivePaymentAddresses"
	var addrStrs []string
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		return w.Manager.ForEachActiveAddress(addrmgrNs, func(addr dcrutil.Address) error {
			addrStrs = append(addrStrs, addr.Address())
			return nil
		})
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	sort.Strings(addrStrs)
	return addrStrs, nil
}

// confirmed checks whether a transaction at height txHeight has met minconf
// confirmations for a blockchain at height curHeight.
func confirmed(minconf, txHeight, curHeight int32) bool {
	return confirms(txHeight, curHeight) >= minconf
}

// confirms returns the number of confirmations for a transaction in a block at
// height txHeight (or -1 for an unconfirmed tx) given the chain height
// curHeight.
func confirms(txHeight, curHeight int32) int32 {
	switch {
	case txHeight == -1, txHeight > curHeight:
		return 0
	default:
		return curHeight - txHeight + 1
	}
}

// coinbaseMatured returns whether a transaction mined at txHeight has
// reached coinbase maturity in a chain with tip height curHeight.
func coinbaseMatured(params *chaincfg.Params, txHeight, curHeight int32) bool {
	return txHeight >= 0 && curHeight-txHeight+1 > int32(params.CoinbaseMaturity)
}

// ticketChangeMatured returns whether a ticket change mined at
// txHeight has reached ticket maturity in a chain with a tip height
// curHeight.
func ticketChangeMatured(params *chaincfg.Params, txHeight, curHeight int32) bool {
	return txHeight >= 0 && curHeight-txHeight+1 > int32(params.SStxChangeMaturity)
}

// ticketMatured returns whether a ticket mined at txHeight has
// reached ticket maturity in a chain with a tip height curHeight.
func ticketMatured(params *chaincfg.Params, txHeight, curHeight int32) bool {
	// dcrd has an off-by-one in the calculation of the ticket
	// maturity, which results in maturity being one block higher
	// than the params would indicate.
	return txHeight >= 0 && curHeight-txHeight > int32(params.TicketMaturity)
}

// ticketExpired returns whether a ticket mined at txHeight has
// reached ticket expiry in a chain with a tip height curHeight.
func ticketExpired(params *chaincfg.Params, txHeight, curHeight int32) bool {
	// Ticket maturity off-by-one extends to the expiry depth as well.
	return txHeight >= 0 && curHeight-txHeight > int32(params.TicketMaturity)+int32(params.TicketExpiry)
}

// AccountTotalReceivedResult is a single result for the
// Wallet.TotalReceivedForAccounts method.
type AccountTotalReceivedResult struct {
	AccountNumber    uint32
	AccountName      string
	TotalReceived    dcrutil.Amount
	LastConfirmation int32
}

// TotalReceivedForAccounts iterates through a wallet's transaction history,
// returning the total amount of decred received for all accounts.
func (w *Wallet) TotalReceivedForAccounts(minConf int32) ([]AccountTotalReceivedResult, error) {
	const op errors.Op = "wallet.TotalReceivedForAccounts"
	var results []AccountTotalReceivedResult
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		err := w.Manager.ForEachAccount(addrmgrNs, func(account uint32) error {
			accountName, err := w.Manager.AccountName(addrmgrNs, account)
			if err != nil {
				return err
			}
			results = append(results, AccountTotalReceivedResult{
				AccountNumber: account,
				AccountName:   accountName,
			})
			return nil
		})
		if err != nil {
			return err
		}

		var stopHeight int32

		if minConf > 0 {
			stopHeight = tipHeight - minConf + 1
		} else {
			stopHeight = -1
		}

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			for i := range details {
				detail := &details[i]
				for _, cred := range detail.Credits {
					pkVersion := detail.MsgTx.TxOut[cred.Index].Version
					pkScript := detail.MsgTx.TxOut[cred.Index].PkScript
					var outputAcct uint32
					_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkVersion,
						pkScript, w.chainParams)
					if err == nil && len(addrs) > 0 {
						outputAcct, err = w.Manager.AddrAccount(
							addrmgrNs, addrs[0])
					}
					if err == nil {
						acctIndex := int(outputAcct)
						if outputAcct == udb.ImportedAddrAccount {
							acctIndex = len(results) - 1
						}
						res := &results[acctIndex]
						res.TotalReceived += cred.Amount
						res.LastConfirmation = confirms(
							detail.Block.Height, tipHeight)
					}
				}
			}
			return false, nil
		}
		return w.TxStore.RangeTransactions(txmgrNs, 0, stopHeight, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return results, nil
}

// TotalReceivedForAddr iterates through a wallet's transaction history,
// returning the total amount of decred received for a single wallet
// address.
func (w *Wallet) TotalReceivedForAddr(addr dcrutil.Address, minConf int32) (dcrutil.Amount, error) {
	const op errors.Op = "wallet.TotalReceivedForAddr"
	var amount dcrutil.Amount
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		var (
			addrStr    = addr.Address()
			stopHeight int32
		)

		if minConf > 0 {
			stopHeight = tipHeight - minConf + 1
		} else {
			stopHeight = -1
		}
		rangeFn := func(details []udb.TxDetails) (bool, error) {
			for i := range details {
				detail := &details[i]
				for _, cred := range detail.Credits {
					pkVersion := detail.MsgTx.TxOut[cred.Index].Version
					pkScript := detail.MsgTx.TxOut[cred.Index].PkScript
					_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkVersion,
						pkScript, w.chainParams)
					// An error creating addresses from the output script only
					// indicates a non-standard script, so ignore this credit.
					if err != nil {
						continue
					}
					for _, a := range addrs {
						if addrStr == a.Address() {
							amount += cred.Amount
							break
						}
					}
				}
			}
			return false, nil
		}
		return w.TxStore.RangeTransactions(txmgrNs, 0, stopHeight, rangeFn)
	})
	if err != nil {
		return 0, errors.E(op, err)
	}
	return amount, nil
}

// SendOutputs creates and sends payment transactions. It returns the
// transaction hash upon success
func (w *Wallet) SendOutputs(outputs []*wire.TxOut, account uint32, minconf int32) (*chainhash.Hash, error) {
	const op errors.Op = "wallet.SendOutputs"
	relayFee := w.RelayFee()
	for _, output := range outputs {
		err := txrules.CheckOutput(output, relayFee)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}

	heldUnlock, err := w.holdUnlock()
	if err != nil {
		return nil, err
	}
	defer heldUnlock.release()
	tx, err := w.txToOutputs("wallet.SendOutputs", outputs, account, minconf, true)
	if err != nil {
		return nil, err
	}
	hash := tx.Tx.TxHash()
	return &hash, nil
}

// SignatureError records the underlying error when validating a transaction
// input signature.
type SignatureError struct {
	InputIndex uint32
	Error      error
}

// SignTransaction uses secrets of the wallet, as well as additional secrets
// passed in by the caller, to create and add input signatures to a transaction.
//
// Transaction input script validation is used to confirm that all signatures
// are valid.  For any invalid input, a SignatureError is added to the returns.
// The final error return is reserved for unexpected or fatal errors, such as
// being unable to determine a previous output script to redeem.
//
// The transaction pointed to by tx is modified by this function.
func (w *Wallet) SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScripts map[wire.OutPoint][]byte,
	additionalKeysByAddress map[string]*dcrutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]SignatureError, error) {

	const op errors.Op = "wallet.SignTransaction"

	var doneFuncs []func()
	defer func() {
		for _, f := range doneFuncs {
			f()
		}
	}()

	var signErrors []SignatureError
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		for i, txIn := range tx.TxIn {
			// For an SSGen tx, skip the first input as it is a stake base
			// and doesn't need to be signed.
			if i == 0 {
				if stake.IsSSGen(tx) {
					// Put some garbage in the signature script.
					txIn.SignatureScript = []byte{0xDE, 0xAD, 0xBE, 0xEF}
					continue
				}
			}

			prevOutScript, ok := additionalPrevScripts[txIn.PreviousOutPoint]
			if !ok {
				prevHash := &txIn.PreviousOutPoint.Hash
				prevIndex := txIn.PreviousOutPoint.Index
				txDetails, err := w.TxStore.TxDetails(txmgrNs, prevHash)
				if errors.Is(errors.NotExist, err) {
					return errors.Errorf("%v not found", &txIn.PreviousOutPoint)
				} else if err != nil {
					return err
				}
				prevOutScript = txDetails.MsgTx.TxOut[prevIndex].PkScript
			}

			// Set up our callbacks that we pass to txscript so it can
			// look up the appropriate keys and scripts by address.
			getKey := txscript.KeyClosure(func(addr dcrutil.Address) (
				chainec.PrivateKey, bool, error) {
				if len(additionalKeysByAddress) != 0 {
					addrStr := addr.Address()
					wif, ok := additionalKeysByAddress[addrStr]
					if !ok {
						return nil, false,
							errors.Errorf("no key for address (needed: %v, have %v)",
								addr.Address(), additionalKeysByAddress)
					}
					return wif.PrivKey, true, nil
				}
				address, err := w.Manager.Address(addrmgrNs, addr)
				if err != nil {
					return nil, false, err
				}

				pka, ok := address.(udb.ManagedPubKeyAddress)
				if !ok {
					return nil, false, errors.Errorf("address %v is not "+
						"a pubkey address", address.Address().Address())
				}
				key, done, err := w.Manager.PrivateKey(addrmgrNs, addr)
				if err != nil {
					return nil, false, err
				}
				doneFuncs = append(doneFuncs, done)
				return key, pka.Compressed(), nil
			})
			getScript := txscript.ScriptClosure(func(
				addr dcrutil.Address) ([]byte, error) {
				// If keys were provided then we can only use the
				// redeem scripts provided with our inputs, too.
				if len(additionalKeysByAddress) != 0 {
					addrStr := addr.Address()
					script, ok := p2shRedeemScriptsByAddress[addrStr]
					if !ok {
						return nil, errors.New("no script for " +
							"address")
					}
					return script, nil
				}

				// First check tx manager script store.
				script, err := w.TxStore.GetTxScript(txmgrNs,
					addr.ScriptAddress())
				if errors.Is(errors.NotExist, err) {
					// Then check the address manager.
					sc, done, err := w.Manager.RedeemScript(addrmgrNs, addr)
					if err != nil {
						return nil, err
					}
					script = sc
					doneFuncs = append(doneFuncs, done)
				} else if err != nil {
					return nil, err
				}
				return script, nil
			})

			// SigHashSingle inputs can only be signed if there's a
			// corresponding output. However this could be already signed,
			// so we always verify the output.
			if (hashType&txscript.SigHashSingle) !=
				txscript.SigHashSingle || i < len(tx.TxOut) {
				// Check for alternative checksig scripts and
				// set the signature suite accordingly.
				ecType := dcrec.STEcdsaSecp256k1
				class := txscript.GetScriptClass(0, prevOutScript)
				if class == txscript.PubkeyAltTy ||
					class == txscript.PubkeyHashAltTy {
					var err error
					ecType, err = txscript.ExtractPkScriptAltSigType(prevOutScript)
					if err != nil {
						return errors.New("unknown checksigalt signature " +
							"suite specified")
					}
				}

				script, err := txscript.SignTxOutput(w.ChainParams(),
					tx, i, prevOutScript, hashType, getKey,
					getScript, txIn.SignatureScript, ecType)
				// Failure to sign isn't an error, it just means that
				// the tx isn't complete.
				if err != nil {
					signErrors = append(signErrors, SignatureError{
						InputIndex: uint32(i),
						Error:      errors.E(op, err),
					})
					continue
				}
				txIn.SignatureScript = script
			}

			// Either it was already signed or we just signed it.
			// Find out if it is completely satisfied or still needs more.
			vm, err := txscript.NewEngine(prevOutScript, tx, i,
				sanityVerifyFlags, 0, nil)
			if err == nil {
				err = vm.Execute()
			}
			if err != nil {
				multisigNotEnoughSigs := false
				class, addr, _, _ := txscript.ExtractPkScriptAddrs(
					0,
					additionalPrevScripts[txIn.PreviousOutPoint],
					w.ChainParams())

				if txscript.IsErrorCode(err, txscript.ErrInvalidStackOperation) &&
					class == txscript.ScriptHashTy {
					redeemScript, _ := getScript(addr[0])
					redeemClass := txscript.GetScriptClass(
						0, redeemScript)
					if redeemClass == txscript.MultiSigTy {
						multisigNotEnoughSigs = true
					}
				}
				// Only report an error for the script engine in the event
				// that it's not a multisignature underflow, indicating that
				// we didn't have enough signatures in front of the
				// redeemScript rather than an actual error.
				if !multisigNotEnoughSigs {
					signErrors = append(signErrors, SignatureError{
						InputIndex: uint32(i),
						Error:      errors.E(op, err),
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return signErrors, nil
}

// CreateSignature returns the raw signature created by the private key of addr
// for tx's idx'th input script and the serialized compressed pubkey for the
// address.
func (w *Wallet) CreateSignature(tx *wire.MsgTx, idx uint32, addr dcrutil.Address, hashType txscript.SigHashType, prevPkScript []byte) (sig, pubkey []byte, err error) {
	const op errors.Op = "wallet.CreateSignature"
	var privKey chainec.PrivateKey
	var pubKey chainec.PublicKey
	var done func()
	defer func() {
		if done != nil {
			done()
		}
	}()

	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(waddrmgrNamespaceKey)

		var err error
		privKey, done, err = w.Manager.PrivateKey(ns, addr)
		if err != nil {
			return err
		}
		pubKey = chainec.Secp256k1.NewPublicKey(privKey.Public())
		return nil
	})
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	sig, err = txscript.RawTxInSignature(tx, int(idx), prevPkScript, hashType, privKey)
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	return sig, pubKey.SerializeCompressed(), nil
}

// isRelevantTx determines whether the transaction is relevant to the wallet and
// should be recorded in the database.
func (w *Wallet) isRelevantTx(dbtx walletdb.ReadTx, tx *wire.MsgTx) bool {
	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

	for _, in := range tx.TxIn {
		// Input is relevant if it contains a saved redeem script or spends a
		// wallet output.
		rs := txscript.MultisigRedeemScriptFromScriptSig(in.SignatureScript)
		if rs != nil && w.Manager.ExistsHash160(addrmgrNs,
			dcrutil.Hash160(rs)) {
			return true
		}
		if w.TxStore.ExistsUTXO(dbtx, &in.PreviousOutPoint) {
			return true
		}
	}
	for _, out := range tx.TxOut {
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(out.Version,
			out.PkScript, w.chainParams)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if w.Manager.ExistsHash160(addrmgrNs, a.Hash160()[:]) {
				return true
			}
		}
	}

	return false
}

// AbandonTransaction removes a transaction, identified by its hash, from
// the wallet if present.  All transaction spend chains deriving from the
// transaction's outputs are also removed.  Does not error if the transaction
// doesn't already exist unmined, but will if the transaction is marked mined in
// a block on the main chain.
//
// Purged transactions may have already been published to the network and may
// still appear in future blocks, and new transactions spending the same inputs
// as purged transactions may be rejected by full nodes due to being double
// spends.  In turn, this can cause the purged transaction to be mined later and
// replace other transactions authored by the wallet.
func (w *Wallet) AbandonTransaction(hash *chainhash.Hash) error {
	const opf = "wallet.AbandonTransaction(%v)"
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)
		details, err := w.TxStore.TxDetails(ns, hash)
		if err != nil {
			return err
		}
		if details.Block.Height != -1 {
			return errors.E(errors.Invalid, errors.Errorf("transaction %v is mined in main chain", hash))
		}
		return w.TxStore.RemoveUnconfirmed(ns, &details.MsgTx, hash)
	})
	if err != nil {
		op := errors.Opf(opf, hash)
		return errors.E(op, err)
	}
	return nil
}

// PublishTransaction saves (if relevant) and sends the transaction to the
// consensus RPC server so it can be propagated to other nodes and eventually
// mined.  If the send fails, the transaction is not added to the wallet.
func (w *Wallet) PublishTransaction(tx *wire.MsgTx, serializedTx []byte, n NetworkBackend) (*chainhash.Hash, error) {
	const opf = "wallet.PublishTransaction(%v)"

	txHash := tx.TxHash()

	var relevant bool
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		relevant = w.isRelevantTx(dbtx, tx)

		// Prevent high fee transactions from being published, if disabled and
		// the fee can be calculated.
		if relevant && !w.AllowHighFees {
			totalInput, err := w.TxStore.TotalInput(dbtx, tx)
			if err != nil {
				return err
			}
			err = w.checkHighFees(totalInput, tx)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		op := errors.Opf(opf, &txHash)
		return nil, errors.E(op, err)
	}

	var watchOutPoints []wire.OutPoint
	if relevant {
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			rec, err := udb.NewTxRecord(serializedTx, time.Now())
			if err != nil {
				return err
			}
			watchOutPoints, err = w.processTransactionRecord(dbtx, rec, nil, nil)
			return err
		})
		if err != nil {
			op := errors.Opf(opf, &txHash)
			return nil, errors.E(op, err)
		}
	}

	err = n.PublishTransactions(context.TODO(), tx)
	if err != nil {
		// Return the error and purge relevant txs from the store only if the
		// backend returns an error different than "already have transaction".
		var isDuplicateTx bool

		// Unwrap to access the underlying RPC error code.
		innerErr := err
		for {
			if e, ok := innerErr.(*dcrjson.RPCError); ok && e.Code == dcrjson.ErrRPCDuplicateTx {
				// Found an RPCError in the error stack. Verify if the error is
				// due to this exact same transaction already existing in the
				// mempool, in which case we ignore it and don't purge the tx
				// from the database.
				//
				// This is brittle, since it relies on checking the error
				// message, but ErrRPCDuplicateTx can be returned in situations
				// other than an exact copy of the tx already existing (such as
				// the new tx being a double spend) and in such cases we _do_
				// need to purge it.
				dupeMsg := fmt.Sprintf("already have transaction %v",
					tx.TxHash())
				isDuplicateTx = strings.HasSuffix(e.Message, dupeMsg)
				break
			} else if e, ok := innerErr.(*errors.Error); ok && e.Err != nil {
				innerErr = e.Err
			} else {
				break
			}
		}
		if !isDuplicateTx {
			if relevant {
				if err := w.AbandonTransaction(&txHash); err != nil {
					log.Warnf("Failed to abandon unmined transaction: %v", err)
				}
			}
			op := errors.Opf(opf, &txHash)
			return nil, errors.E(op, err)
		}
	}

	if len(watchOutPoints) > 0 {
		err := n.LoadTxFilter(context.TODO(), false, nil, watchOutPoints)
		if err != nil {
			log.Errorf("Failed to watch outpoints: %v", err)
		}
	}

	return &txHash, nil
}

// PublishUnminedTransactions rebroadcasts all unmined transactions
// to the consensus RPC server so it can be propagated to other nodes
// and eventually mined.
func (w *Wallet) PublishUnminedTransactions(ctx context.Context, p Peer) error {
	const op errors.Op = "wallet.PublishUnminedTransactions"
	unminedTxs, err := w.UnminedTransactions()
	if err != nil {
		return errors.E(op, err)
	}
	err = p.PublishTransactions(ctx, unminedTxs...)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// ChainParams returns the network parameters for the blockchain the wallet
// belongs to.
func (w *Wallet) ChainParams() *chaincfg.Params {
	return w.chainParams
}

// NeedsAccountsSync returns whether or not the wallet is void of any generated
// keys and accounts (other than the default account), and records the genesis
// block as the main chain tip.  When these are both true, an accounts sync
// should be performed to restore, per BIP0044, any generated accounts and
// addresses from a restored seed.
func (w *Wallet) NeedsAccountsSync() (bool, error) {
	_, tipHeight := w.MainChainTip()
	if tipHeight != 0 {
		return false, nil
	}

	needsSync := true
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		lastAcct, err := w.Manager.LastAccount(addrmgrNs)
		if err != nil {
			return err
		}
		if lastAcct != 0 {
			// There are more accounts than just the default account and no
			// accounts sync is required.
			needsSync = false
			return nil
		}
		// Begin iteration over all addresses in the first account.  If any
		// exist, "break out" of the loop with a special error.
		errBreak := errors.New("break")
		err = w.Manager.ForEachAccountAddress(addrmgrNs, 0, func(udb.ManagedAddress) error {
			needsSync = false
			return errBreak
		})
		if err == errBreak {
			return nil
		}
		return err
	})
	return needsSync, err
}

// Create creates an new wallet, writing it to an empty database.  If the passed
// seed is non-nil, it is used.  Otherwise, a secure random seed of the
// recommended length is generated.
func Create(db DB, pubPass, privPass, seed []byte, params *chaincfg.Params) error {
	const op errors.Op = "wallet.Create"
	// If a seed was provided, ensure that it is of valid length. Otherwise,
	// we generate a random seed for the wallet with the recommended seed
	// length.
	if seed == nil {
		hdSeed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
		if err != nil {
			return errors.E(op, err)
		}
		seed = hdSeed
	}
	if len(seed) < hdkeychain.MinSeedBytes || len(seed) > hdkeychain.MaxSeedBytes {
		return errors.E(op, hdkeychain.ErrInvalidSeedLen)
	}

	err := udb.Initialize(db.internal(), params, seed, pubPass, privPass)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// CreateWatchOnly creates a watchonly wallet on the provided db.
func CreateWatchOnly(db DB, extendedPubKey string, pubPass []byte, params *chaincfg.Params) error {
	const op errors.Op = "wallet.CreateWatchOnly"
	err := udb.InitializeWatchOnly(db.internal(), params, extendedPubKey, pubPass)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// decodeStakePoolColdExtKey decodes the string of stake pool addresses
// to search incoming tickets for. The format for the passed string is:
//   "xpub...:end"
// where xpub... is the extended public key and end is the last
// address index to scan to, exclusive. Effectively, it returns the derived
// addresses for this public key for the address indexes [0,end). The branch
// used for the derivation is always the external branch.
func decodeStakePoolColdExtKey(encStr string, params *chaincfg.Params) (map[string]struct{}, error) {
	// Default option; stake pool is disabled.
	if encStr == "" {
		return nil, nil
	}

	// Split the string.
	splStrs := strings.Split(encStr, ":")
	if len(splStrs) != 2 {
		return nil, errors.Errorf("failed to correctly parse passed stakepool " +
			"address public key and index")
	}

	// Parse the extended public key and ensure it's the right network.
	key, err := hdkeychain.NewKeyFromString(splStrs[0], params)
	if err != nil {
		return nil, err
	}

	// Parse the ending index and ensure it's valid.
	end, err := strconv.Atoi(splStrs[1])
	if err != nil {
		return nil, err
	}
	if end < 0 || end > udb.MaxAddressesPerAccount {
		return nil, errors.Errorf("pool address index is invalid (got %v)",
			end)
	}

	log.Infof("Please wait, deriving %v stake pool fees addresses "+
		"for extended public key %s", end, splStrs[0])

	// Derive from external branch
	branchKey, err := key.Child(udb.ExternalBranch)
	if err != nil {
		return nil, err
	}

	// Derive the addresses from [0, end) for this extended public key.
	addrs, err := deriveChildAddresses(branchKey, 0, uint32(end)+1, params)
	if err != nil {
		return nil, err
	}

	addrMap := make(map[string]struct{})
	for i := range addrs {
		addrMap[addrs[i].Address()] = struct{}{}
	}

	return addrMap, nil
}

// Open loads an already-created wallet from the passed database and namespaces
// configuration options and sets it up it according to the rest of options.
func Open(cfg *Config) (*Wallet, error) {
	const op errors.Op = "wallet.Open"
	// Migrate to the unified DB if necessary.
	db := cfg.DB.internal()
	needsMigration, err := udb.NeedsMigration(db)
	if err != nil {
		return nil, errors.E(op, err)
	}
	if needsMigration {
		err := udb.Migrate(db, cfg.Params)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}

	// Perform upgrades as necessary.
	err = udb.Upgrade(db, cfg.PubPassphrase, cfg.Params)
	if err != nil {
		return nil, errors.E(op, err)
	}

	w := &Wallet{
		db: db,

		// StakeOptions
		votingEnabled: cfg.VotingEnabled,
		addressReuse:  cfg.AddressReuse,
		ticketAddress: cfg.VotingAddress,
		poolAddress:   cfg.PoolAddress,
		poolFees:      cfg.PoolFees,

		// LoaderOptions
		gapLimit:                cfg.GapLimit,
		AllowHighFees:           cfg.AllowHighFees,
		accountGapLimit:         cfg.AccountGapLimit,
		disableCoinTypeUpgrades: cfg.DisableCoinTypeUpgrades,

		// Chain params
		subsidyCache: blockchain.NewSubsidyCache(0, compat.Params2to1(cfg.Params)),
		chainParams:  cfg.Params,

		lockedOutpoints: map[wire.OutPoint]struct{}{},

		addressBuffers: make(map[uint32]*bip0044AccountData),
	}

	// Open database managers
	w.Manager, w.TxStore, w.StakeMgr, err = udb.Open(db, cfg.Params, cfg.PubPassphrase)
	if err != nil {
		return nil, errors.E(op, err)
	}
	log.Infof("Opened wallet") // TODO: log balance? last sync height?

	var vb stake.VoteBits
	err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrNamespaceKey)
		lastAcct, err := w.Manager.LastAccount(ns)
		if err != nil {
			return err
		}
		for acct := uint32(0); acct <= lastAcct; acct++ {
			xpub, err := w.Manager.AccountExtendedPubKey(tx, acct)
			if err != nil {
				return err
			}
			extKey, intKey, err := deriveBranches(xpub)
			if err != nil {
				return err
			}
			props, err := w.Manager.AccountProperties(ns, acct)
			if err != nil {
				return err
			}
			w.addressBuffers[acct] = &bip0044AccountData{
				xpub: xpub,
				albExternal: addressBuffer{
					branchXpub: extKey,
					lastUsed:   props.LastUsedExternalIndex,
					cursor:     props.LastReturnedExternalIndex - props.LastUsedExternalIndex,
				},
				albInternal: addressBuffer{
					branchXpub: intKey,
					lastUsed:   props.LastUsedInternalIndex,
					cursor:     props.LastReturnedInternalIndex - props.LastUsedInternalIndex,
				},
			}
		}

		vb = w.readDBVoteBits(tx)

		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	w.NtfnServer = newNotificationServer(w)
	w.voteBits = vb

	w.stakePoolColdAddrs, err = decodeStakePoolColdExtKey(cfg.StakePoolColdExtKey,
		cfg.Params)
	if err != nil {
		return nil, errors.E(op, err)
	}
	w.stakePoolEnabled = len(w.stakePoolColdAddrs) > 0

	// Amounts
	w.ticketFeeIncrement, err = dcrutil.NewAmount(cfg.TicketFee)
	if err != nil {
		return nil, errors.E(op, err)
	}
	w.relayFee, err = dcrutil.NewAmount(cfg.RelayFee)
	if err != nil {
		return nil, errors.E(op, err)
	}

	return w, nil
}
