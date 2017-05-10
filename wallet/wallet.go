// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/bitset"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
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

	walletDbWatchingOnlyName = "wowallet.db"
)

var (
	// SimulationPassphrase is the password for a wallet created for simnet
	// with --createtemp.
	SimulationPassphrase = []byte("password")
)

// ErrNotSynced describes an error where an operation cannot complete
// due wallet being out of sync (and perhaps currently syncing with)
// the remote chain server.
var ErrNotSynced = errors.New("wallet is not synchronized with the chain server")

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

// VotingInfo is a container for the current height, hash, and list
// of eligible tickets.
type VotingInfo struct {
	BlockHash   *chainhash.Hash
	BlockHeight int64
	Tickets     []*chainhash.Hash
}

// Wallet is a structure containing all the components for a
// complete wallet.  It contains the Armory-style key store
// addresses and keys),
type Wallet struct {
	publicPassphrase []byte

	// Data stores
	db       walletdb.DB
	Manager  *udb.Manager
	TxStore  *udb.Store
	StakeMgr *udb.StakeStore

	// Handlers for stake system.
	stakeSettingsLock       sync.Mutex
	voteBits                stake.VoteBits
	ticketPurchasingEnabled bool
	votingEnabled           bool
	CurrentVotingInfo       *VotingInfo
	balanceToMaintain       dcrutil.Amount
	poolAddress             dcrutil.Address
	poolFees                float64
	stakePoolEnabled        bool
	stakePoolColdAddrs      map[string]struct{}

	// Start up flags/settings
	initiallyUnlocked bool
	gapLimit          int

	chainClient     *chain.RPCClient
	chainClientLock sync.Mutex

	lockedOutpoints map[wire.OutPoint]struct{}

	relayFee               dcrutil.Amount
	relayFeeMu             sync.Mutex
	ticketFeeIncrementLock sync.Mutex
	ticketFeeIncrement     dcrutil.Amount
	DisallowFree           bool
	AllowHighFees          bool

	// Channel for transaction creation requests.
	consolidateRequests      chan consolidateRequest
	createTxRequests         chan createTxRequest
	createMultisigTxRequests chan createMultisigTxRequest

	// Channels for stake tx creation requests.
	createSStxRequests     chan createSStxRequest
	createSSGenRequests    chan createSSGenRequest
	createSSRtxRequests    chan createSSRtxRequest
	purchaseTicketRequests chan purchaseTicketRequest

	// Internal address handling.
	addressReuse     bool
	ticketAddress    dcrutil.Address
	addressBuffers   map[uint32]*bip0044AccountData
	addressBuffersMu sync.Mutex

	// Channels for the manager locker.
	unlockRequests     chan unlockRequest
	lockRequests       chan struct{}
	holdUnlockRequests chan chan heldUnlock
	lockState          chan bool
	changePassphrase   chan changePassphraseRequest

	// Information for reorganization handling.
	reorganizingLock sync.Mutex
	reorganizeToHash chainhash.Hash
	sideChain        []sideChainBlock
	reorganizing     bool

	NtfnServer *NotificationServer

	chainParams *chaincfg.Params

	wg sync.WaitGroup

	started bool
	quit    chan struct{}
	quitMu  sync.Mutex
}

// newWallet creates a new Wallet structure with the provided address manager
// and transaction store.
func newWallet(votingEnabled bool, addressReuse bool, ticketAddress dcrutil.Address,
	poolAddress dcrutil.Address, pf float64, relayFee, ticketFee dcrutil.Amount,
	gapLimit int, stakePoolColdAddrs map[string]struct{}, AllowHighFees bool,
	mgr *udb.Manager, txs *udb.Store, smgr *udb.StakeStore, db *walletdb.DB,
	params *chaincfg.Params) *Wallet {

	w := &Wallet{
		db:                       *db,
		Manager:                  mgr,
		TxStore:                  txs,
		StakeMgr:                 smgr,
		votingEnabled:            votingEnabled,
		lockedOutpoints:          map[wire.OutPoint]struct{}{},
		relayFee:                 relayFee,
		ticketFeeIncrement:       ticketFee,
		AllowHighFees:            AllowHighFees,
		consolidateRequests:      make(chan consolidateRequest),
		createTxRequests:         make(chan createTxRequest),
		createMultisigTxRequests: make(chan createMultisigTxRequest),
		createSStxRequests:       make(chan createSStxRequest),
		createSSGenRequests:      make(chan createSSGenRequest),
		createSSRtxRequests:      make(chan createSSRtxRequest),
		purchaseTicketRequests:   make(chan purchaseTicketRequest),
		addressReuse:             addressReuse,
		ticketAddress:            ticketAddress,
		addressBuffers:           make(map[uint32]*bip0044AccountData),
		poolAddress:              poolAddress,
		poolFees:                 pf,
		gapLimit:                 gapLimit,
		stakePoolEnabled:         len(stakePoolColdAddrs) > 0,
		stakePoolColdAddrs:       stakePoolColdAddrs,
		initiallyUnlocked:        false,
		unlockRequests:           make(chan unlockRequest),
		lockRequests:             make(chan struct{}),
		holdUnlockRequests:       make(chan chan heldUnlock),
		lockState:                make(chan bool),
		changePassphrase:         make(chan changePassphraseRequest),
		chainParams:              params,
		quit:                     make(chan struct{}),
	}

	// TODO: remove newWallet, stick the above in Open, and don't ignore this
	// error.
	var vb stake.VoteBits
	walletdb.View(w.db, func(tx walletdb.ReadTx) error {
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
				albExternal: addressBuffer{
					branchXpub: extKey,
					lastUsed:   props.LastUsedExternalIndex,
				},
				albInternal: addressBuffer{
					branchXpub: intKey,
					lastUsed:   props.LastUsedInternalIndex,
				},
			}
		}

		vb = w.readDBVoteBits(tx)

		return nil
	})

	w.NtfnServer = newNotificationServer(w)
	w.voteBits = vb
	return w
}

// StakeDifficulty is used to get the current stake difficulty from the daemon.
func (w *Wallet) StakeDifficulty() (dcrutil.Amount, error) {
	chainClient, err := w.requireChainClient()
	if err != nil {
		return 0, err
	}

	sdResp, err := chainClient.GetStakeDifficulty()
	if err != nil {
		return 0, err
	}

	sd, err := dcrutil.NewAmount(sdResp.NextStakeDifficulty)
	if err != nil {
		return 0, err
	}

	return sd, nil
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

// TicketPurchasingEnabled returns whether the wallet is configured to purchase tickets.
func (w *Wallet) TicketPurchasingEnabled() bool {
	w.stakeSettingsLock.Lock()
	enabled := w.ticketPurchasingEnabled
	w.stakeSettingsLock.Unlock()
	return enabled
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
		return 4
	case wire.TestNet2:
		return 5
	case wire.SimNet:
		return 5
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
	return choices, voteBits, err
}

// SetAgendaChoices sets the choices for agendas defined by the supported stake
// version.  If a choice is set multiple times, the last takes preference.  The
// new votebits after each change is made are returned.
func (w *Wallet) SetAgendaChoices(choices ...AgendaChoice) (voteBits uint16, err error) {
	version, deployments := CurrentAgendas(w.chainParams)
	if len(deployments) == 0 {
		const str = "no agendas to set for this network"
		return 0, apperrors.E{ErrorCode: apperrors.ErrInput, Description: str, Err: nil}
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
				str := "no agenda with ID " + c.AgendaID
				return apperrors.E{ErrorCode: apperrors.ErrInput, Description: str, Err: nil}
			}

			var matchingChoice *chaincfg.Choice
			for i := range matchingAgenda.Choices {
				if matchingAgenda.Choices[i].Id == c.ChoiceID {
					matchingChoice = &matchingAgenda.Choices[i]
					break
				}
			}
			if matchingChoice == nil {
				str := "agenda " + c.AgendaID + " has no choice ID " + c.ChoiceID
				return apperrors.E{ErrorCode: apperrors.ErrInput, Description: str, Err: nil}
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
		return 0, err
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

// SetTicketPurchasingEnabled is used to enable or disable ticket purchasing in the
// wallet.
func (w *Wallet) SetTicketPurchasingEnabled(flag bool) {
	w.stakeSettingsLock.Lock()
	w.ticketPurchasingEnabled = flag
	w.stakeSettingsLock.Unlock()
}

// SetCurrentVotingInfo is used to set the current tickets eligible
// to vote on the top block, along with that block's hash and height.
func (w *Wallet) SetCurrentVotingInfo(blockHash *chainhash.Hash, blockHeight int64, tickets []*chainhash.Hash) {
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	w.CurrentVotingInfo = &VotingInfo{blockHash, blockHeight, tickets}
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

// SetInitiallyUnlocked sets whether or not the wallet is initially unlocked.
// This allows the user to resync accounts, dictating some of the start up
// syncing behaviour. It should only be called before the wallet RPC servers
// are accessible. It is not safe for concurrent access.
func (w *Wallet) SetInitiallyUnlocked(set bool) {
	w.initiallyUnlocked = set
}

// Start starts the goroutines necessary to manage a wallet.
func (w *Wallet) Start() {
	w.quitMu.Lock()
	select {
	case <-w.quit:
		// Restart the wallet goroutines after shutdown finishes.
		w.WaitForShutdown()
		w.quit = make(chan struct{})
	default:
		// Ignore when the wallet is still running.
		if w.started {
			w.quitMu.Unlock()
			return
		}
		w.started = true
	}
	w.quitMu.Unlock()

	w.wg.Add(2)
	go w.txCreator()
	go w.walletLocker()
}

// SynchronizeRPC associates the wallet with the consensus RPC client,
// synchronizes the wallet with the latest changes to the blockchain, and
// continuously updates the wallet through RPC notifications.
//
// This method is unstable and will be removed when all syncing logic is moved
// outside of the wallet package.
func (w *Wallet) SynchronizeRPC(chainClient *chain.RPCClient) {
	w.quitMu.Lock()
	select {
	case <-w.quit:
		w.quitMu.Unlock()
		return
	default:
	}
	w.quitMu.Unlock()

	// TODO: Ignoring the new client when one is already set breaks callers
	// who are replacing the client, perhaps after a disconnect.
	w.chainClientLock.Lock()
	if w.chainClient != nil {
		w.chainClientLock.Unlock()
		return
	}
	w.chainClient = chainClient
	w.chainClientLock.Unlock()

	w.StakeMgr.SetChainSvr(chainClient)

	// TODO: It would be preferable to either run these goroutines
	// separately from the wallet (use wallet mutator functions to
	// make changes from the RPC client) and not have to stop and
	// restart them each time the client disconnects and reconnets.
	w.wg.Add(2)
	go w.handleChainNotifications(chainClient)
	go w.handleChainVotingNotifications(chainClient)

	// Request notifications for winning tickets.
	err := chainClient.NotifyWinningTickets()
	if err != nil {
		log.Error("Unable to request transaction updates for "+
			"winning tickets. Error: ", err.Error())
	}

	// Request notifications for spent and missed tickets.
	err = chainClient.NotifySpentAndMissedTickets()
	if err != nil {
		log.Error("Unable to request transaction updates for spent "+
			"and missed tickets. Error: ", err.Error())
	}

	ticketPurchasingEnabled := w.TicketPurchasingEnabled()
	if ticketPurchasingEnabled {
		vb := w.VoteBits()
		log.Infof("Wallet ticket purchasing enabled: vote bits = %#04x, "+
			"extended vote bits = %x", vb.Bits, vb.ExtendedBits)
	}
	if w.votingEnabled {
		log.Infof("Wallet voting enabled")
	}
	if ticketPurchasingEnabled || w.votingEnabled {
		log.Infof("Please ensure your wallet remains unlocked so it may " +
			"create stake transactions")
	}
}

// requireChainClient marks that a wallet method can only be completed when the
// consensus RPC server is set.  This function and all functions that call it
// are unstable and will need to be moved when the syncing code is moved out of
// the wallet.
func (w *Wallet) requireChainClient() (*chain.RPCClient, error) {
	w.chainClientLock.Lock()
	chainClient := w.chainClient
	w.chainClientLock.Unlock()
	if chainClient == nil {
		return nil, errors.New("blockchain RPC is inactive")
	}
	return chainClient, nil
}

// ChainClient returns the optional consensus RPC client associated with the
// wallet.
//
// This function is unstable and will be removed once sync logic is moved out of
// the wallet.
func (w *Wallet) ChainClient() *chain.RPCClient {
	w.chainClientLock.Lock()
	chainClient := w.chainClient
	w.chainClientLock.Unlock()
	return chainClient
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

// quitChan atomically reads the quit channel.
func (w *Wallet) quitChan() <-chan struct{} {
	w.quitMu.Lock()
	c := w.quit
	w.quitMu.Unlock()
	return c
}

// Stop signals all wallet goroutines to shutdown.
func (w *Wallet) Stop() {
	w.quitMu.Lock()
	quit := w.quit
	w.quitMu.Unlock()

	select {
	case <-quit:
	default:
		close(quit)
		w.chainClientLock.Lock()
		if w.chainClient != nil {
			w.chainClient.Stop()
			w.chainClient = nil
		}
		w.chainClientLock.Unlock()
	}
}

// ShuttingDown returns whether the wallet is currently in the process of
// shutting down or not.
func (w *Wallet) ShuttingDown() bool {
	select {
	case <-w.quitChan():
		return true
	default:
		return false
	}
}

// WaitForShutdown blocks until all wallet goroutines have finished executing.
func (w *Wallet) WaitForShutdown() {
	w.chainClientLock.Lock()
	if w.chainClient != nil {
		w.chainClient.WaitForShutdown()
	}
	w.chainClientLock.Unlock()
	w.wg.Wait()
}

// SynchronizingToNetwork returns whether the wallet is currently synchronizing
// with the Bitcoin network.
func (w *Wallet) SynchronizingToNetwork() bool {
	// At the moment, RPC is the only synchronization method.  In the
	// future, when SPV is added, a separate check will also be needed, or
	// SPV could always be enabled if RPC was not explicitly specified when
	// creating the wallet.
	w.chainClientLock.Lock()
	syncing := w.chainClient != nil
	w.chainClientLock.Unlock()
	return syncing
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

// loadActiveAddrs loads the consensus RPC server with active addresses for
// transaction notifications.  For logging purposes, it returns the total number
// of addresses loaded.
func (w *Wallet) loadActiveAddrs(dbtx walletdb.ReadTx, chainClient *chain.RPCClient) (uint64, error) {
	pool := sync.Pool{New: func() interface{} { return make([]dcrutil.Address, 0, 256) }}
	recycleAddrs := func(addrs []dcrutil.Address) { pool.Put(addrs[:0]) }
	getAddrs := func() []dcrutil.Address { return pool.Get().([]dcrutil.Address) }

	loadBranchAddrs := func(branchKey *hdkeychain.ExtendedKey, n uint32, errs chan<- error) {
		jobs := n/256 + 1
		jobErrs := make(chan error, jobs)
		for child := uint32(0); child < n; child += 256 {
			go func(child uint32) {
				addrs := getAddrs()
				stop := minUint32(n, child+256)
				for ; child < stop; child++ {
					addr, err := deriveChildAddress(branchKey, child, w.chainParams)
					if err == hdkeychain.ErrInvalidChild {
						continue
					}
					if err != nil {
						jobErrs <- err
						return
					}
					addrs = append(addrs, addr)
				}
				future := chainClient.LoadTxFilterAsync(false, addrs, nil)
				recycleAddrs(addrs)
				jobErrs <- future.Receive()
			}(child)
		}
		for i := 0; i < cap(jobErrs); i++ {
			err := <-jobErrs
			if err != nil {
				errs <- err
				return
			}
		}
		errs <- nil
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
		extn := minUint32(props.LastUsedExternalIndex+1+gapLimit, hdkeychain.HardenedKeyStart)
		intn := minUint32(props.LastUsedInternalIndex+1+gapLimit, hdkeychain.HardenedKeyStart)
		// pre-cache the pubkey results so concurrent access does not race.
		extKey.ECPubKey()
		intKey.ECPubKey()
		go loadBranchAddrs(extKey, extn, errs)
		go loadBranchAddrs(intKey, intn, errs)
		bip0044AddrCount += uint64(extn) + uint64(intn)
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
		errs <- chainClient.LoadTxFilter(false, addrs, nil)
	}()
	for i := 0; i < cap(errs); i++ {
		err := <-errs
		if err != nil {
			return 0, err
		}
	}

	return bip0044AddrCount + importedAddrCount, nil
}

// LoadActiveDataFilters loads the consensus RPC server's websocket client
// transaction filter with all active addresses and unspent outpoints for this
// wallet.
func (w *Wallet) LoadActiveDataFilters(chainClient *chain.RPCClient) error {
	log.Infof("Loading active addresses and unspent outputs...")

	var addrCount, utxoCount uint64
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		addrCount, err = w.loadActiveAddrs(dbtx, chainClient)
		if err != nil {
			return err
		}

		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		unspent, err := w.TxStore.UnspentOutpoints(txmgrNs)
		if err != nil {
			return err
		}
		utxoCount = uint64(len(unspent))
		err = chainClient.LoadTxFilter(false, nil, unspent)
		return err
	})
	if err != nil {
		return err
	}

	log.Infof("Registered for transaction notifications for %v address(es) "+
		"and %v output(s)", addrCount, utxoCount)
	return nil
}

// createHeaderData creates the header data to process from hex-encoded
// serialized block headers.
func createHeaderData(headers []string) ([]udb.BlockHeaderData, error) {
	data := make([]udb.BlockHeaderData, len(headers))
	hexbuf := make([]byte, len(udb.RawBlockHeader{})*2)
	var decodedHeader wire.BlockHeader
	for i, header := range headers {
		var headerData udb.BlockHeaderData
		copy(hexbuf, header)
		_, err := hex.Decode(headerData.SerializedHeader[:], hexbuf)
		if err != nil {
			return nil, err
		}
		r := bytes.NewReader(headerData.SerializedHeader[:])
		err = decodedHeader.Deserialize(r)
		if err != nil {
			return nil, err
		}
		headerData.BlockHash = decodedHeader.BlockHash()
		data[i] = headerData
	}
	return data, nil
}

func (w *Wallet) fetchHeaders(chainClient *chain.RPCClient) (int, error) {
	fetchedHeaders := 0

	var blockLocators []chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		blockLocators = w.TxStore.BlockLocators(txmgrNs)
		return nil
	})
	if err != nil {
		return 0, err
	}

	// Fetch and process headers until no more are returned.
	hashStop := chainhash.Hash{}
	for {
		response, err := chainClient.GetHeaders(blockLocators, &hashStop)
		if err != nil {
			return 0, err
		}

		if len(response.Headers) == 0 {
			return fetchedHeaders, nil
		}

		headerData, err := createHeaderData(response.Headers)
		if err != nil {
			return 0, err
		}

		err = walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
			addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
			txmgrNs := tx.ReadWriteBucket(wtxmgrNamespaceKey)
			err := w.TxStore.InsertMainChainHeaders(txmgrNs, addrmgrNs,
				headerData)
			if err != nil {
				return err
			}
			blockLocators = w.TxStore.BlockLocators(txmgrNs)
			return nil
		})
		if err != nil {
			return 0, err
		}

		fetchedHeaders += len(response.Headers)
	}
}

// FetchHeaders fetches headers from the consensus RPC server and updates the
// main chain tip with the latest block.  The number of new headers fetched is
// returned, along with the hash of the first previously-unseen block hash now
// in the main chain.  This is the block a rescan should begin at (inclusive),
// and is only relevant when the number of fetched headers is not zero.
func (w *Wallet) FetchHeaders(chainClient *chain.RPCClient) (count int, rescanFrom chainhash.Hash, rescanFromHeight int32,
	mainChainTipBlockHash chainhash.Hash, mainChainTipBlockHeight int32, err error) {

	// Unfortunately, getheaders is broken and needs a workaround when wallet's
	// previous main chain is now a side chain.  Until this is fixed, do what
	// wallet had previously been doing by querying blocks on its main chain, in
	// reverse order, stopping at the first block that is found and that exists
	// on the actual main chain.
	//
	// See https://github.com/decred/dcrd/issues/427 for details.  This hack
	// should be dumped once fixed.
	var (
		commonAncestor       chainhash.Hash
		commonAncestorHeight int32
	)
	err = walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadWriteBucket(wtxmgrNamespaceKey)

		commonAncestor, commonAncestorHeight = w.TxStore.MainChainTip(txmgrNs)
		hash, height := commonAncestor, commonAncestorHeight

		for height != 0 {
			mainChainHash, err := chainClient.GetBlockHash(int64(height))
			if err == nil && hash == *mainChainHash {
				// found it
				break
			}

			height--
			hash, err = w.TxStore.GetMainChainBlockHashForHeight(txmgrNs, height)
			if err != nil {
				return err
			}
		}

		// No rollback necessary when already on the main chain.
		if height == commonAncestorHeight {
			return nil
		}

		// Remove blocks after the side chain fork point.  Block locators should
		// now begin here, avoiding any issues with calling getheaders with
		// side chain hashes.
		return w.TxStore.Rollback(txmgrNs, addrmgrNs, height+1)
	})
	if err != nil {
		return
	}

	log.Infof("Fetching headers")
	fetchedHeaderCount, err := w.fetchHeaders(chainClient)
	if err != nil {
		return
	}
	log.Infof("Fetched %v new header(s)", fetchedHeaderCount)

	var rescanStart chainhash.Hash
	var rescanStartHeight int32

	if fetchedHeaderCount != 0 {
		// Find the common ancestor of the previous tip before fetching headers,
		// and the new main chain.  Headers are never pruned so the parents can
		// be iterated in reverse until the common ancestor is found.  This is
		// the starting point for the rescan.
		err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
			txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
			for {
				hash, err := w.TxStore.GetMainChainBlockHashForHeight(
					txmgrNs, commonAncestorHeight)
				if err != nil {
					return err
				}
				if commonAncestor == hash {
					break
				}
				header, err := w.TxStore.GetSerializedBlockHeader(
					txmgrNs, &commonAncestor)
				if err != nil {
					return err
				}
				copy(commonAncestor[:], udb.ExtractBlockHeaderParentHash(header))
				commonAncestorHeight--
			}
			mainChainTipBlockHash, mainChainTipBlockHeight = w.TxStore.MainChainTip(txmgrNs)

			rescanStartHeight = commonAncestorHeight + 1
			rescanStart, err = w.TxStore.GetMainChainBlockHashForHeight(
				txmgrNs, rescanStartHeight)
			return err
		})
		if err != nil {
			return
		}
	} else {
		// fetchedHeaderCount is 0 so the current mainChainTip is the commonAncestor
		mainChainTipBlockHash = commonAncestor
		mainChainTipBlockHeight = commonAncestorHeight
	}

	return fetchedHeaderCount, rescanStart, rescanStartHeight, mainChainTipBlockHash,
		mainChainTipBlockHeight, nil
}

// syncWithChain brings the wallet up to date with the current chain server
// connection.  It creates a rescan request and blocks until the rescan has
// finished.
func (w *Wallet) syncWithChain(chainClient *chain.RPCClient) error {
	// Request notifications for connected and disconnected blocks.
	err := chainClient.NotifyBlocks()
	if err != nil {
		return err
	}

	// Discover any addresses for this wallet that have not yet been created.
	err = w.DiscoverActiveAddresses(chainClient, w.initiallyUnlocked)
	if err != nil {
		return err
	}

	// Load transaction filters with all active addresses and watched outpoints.
	err = w.LoadActiveDataFilters(chainClient)
	if err != nil {
		return err
	}

	// Fetch headers for unseen blocks in the main chain, determine whether a
	// rescan is necessary, and when to begin it.
	fetchedHeaderCount, rescanStart, _, _, _, err := w.FetchHeaders(chainClient)
	if err != nil {
		return err
	}

	// Rescan when necessary.
	if fetchedHeaderCount != 0 {
		err = <-w.Rescan(chainClient, &rescanStart)
		if err != nil {
			return err
		}
	}

	w.resendUnminedTxs(chainClient)

	// Send winning and missed ticket notifications out so that the wallet
	// can immediately vote and redeem any tickets it may have missed on
	// startup.
	// TODO A proper pass through for dcrrpcclient for these cmds.
	if w.initiallyUnlocked {
		_, err = chainClient.RawRequest("rebroadcastwinners", nil)
		if err != nil {
			return err
		}
		_, err = chainClient.RawRequest("rebroadcastmissed", nil)
		if err != nil {
			return err
		}
	}

	log.Infof("Blockchain sync completed, wallet ready for general usage.")

	return nil
}

type (
	consolidateRequest struct {
		inputs  int
		account uint32
		address dcrutil.Address
		resp    chan consolidateResponse
	}
	createTxRequest struct {
		account uint32
		outputs []*wire.TxOut
		minconf int32
		resp    chan createTxResponse
	}
	createMultisigTxRequest struct {
		account   uint32
		amount    dcrutil.Amount
		pubkeys   []*dcrutil.AddressSecpPubKey
		nrequired int8
		minconf   int32
		resp      chan createMultisigTxResponse
	}
	createSStxRequest struct {
		usedInputs []udb.Credit
		pair       map[string]dcrutil.Amount
		couts      []dcrjson.SStxCommitOut
		inputs     []dcrjson.SStxInput
		minconf    int32
		resp       chan createSStxResponse
	}
	createSSGenRequest struct {
		tickethash chainhash.Hash
		blockhash  chainhash.Hash
		height     int64
		votebits   uint16
		resp       chan createSSGenResponse
	}
	createSSRtxRequest struct {
		tickethash chainhash.Hash
		resp       chan createSSRtxResponse
	}
	purchaseTicketRequest struct {
		minBalance  dcrutil.Amount
		spendLimit  dcrutil.Amount
		minConf     int32
		ticketAddr  dcrutil.Address
		account     uint32
		numTickets  int
		poolAddress dcrutil.Address
		poolFees    float64
		expiry      int32
		txFee       dcrutil.Amount
		ticketFee   dcrutil.Amount
		resp        chan purchaseTicketResponse
	}

	consolidateResponse struct {
		txHash *chainhash.Hash
		err    error
	}
	createTxResponse struct {
		tx  *txauthor.AuthoredTx
		err error
	}
	createMultisigTxResponse struct {
		tx           *CreatedTx
		address      dcrutil.Address
		redeemScript []byte
		err          error
	}
	createSStxResponse struct {
		tx  *CreatedTx
		err error
	}
	createSSGenResponse struct {
		tx  *CreatedTx
		err error
	}
	createSSRtxResponse struct {
		tx  *CreatedTx
		err error
	}
	purchaseTicketResponse struct {
		data []*chainhash.Hash
		err  error
	}
)

// txCreator is responsible for the input selection and creation of
// transactions.  These functions are the responsibility of this method
// (designed to be run as its own goroutine) since input selection must be
// serialized, or else it is possible to create double spends by choosing the
// same inputs for multiple transactions.  Along with input selection, this
// method is also responsible for the signing of transactions, since we don't
// want to end up in a situation where we run out of inputs as multiple
// transactions are being created.  In this situation, it would then be possible
// for both requests, rather than just one, to fail due to not enough available
// inputs.
func (w *Wallet) txCreator() {
	quit := w.quitChan()
out:
	for {
		select {
		case txr := <-w.consolidateRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- consolidateResponse{nil, err}
				continue
			}
			txh, err := w.compressWallet(txr.inputs, txr.account, txr.address)
			heldUnlock.release()
			txr.resp <- consolidateResponse{txh, err}

		case txr := <-w.createTxRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- createTxResponse{nil, err}
				continue
			}
			tx, err := w.txToOutputs(txr.outputs, txr.account,
				txr.minconf, true)
			heldUnlock.release()
			txr.resp <- createTxResponse{tx, err}

		case txr := <-w.createMultisigTxRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- createMultisigTxResponse{nil, nil, nil, err}
				continue
			}
			tx, address, redeemScript, err := w.txToMultisig(txr.account,
				txr.amount, txr.pubkeys, txr.nrequired, txr.minconf)
			heldUnlock.release()
			txr.resp <- createMultisigTxResponse{tx, address, redeemScript, err}

		case txr := <-w.createSStxRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- createSStxResponse{nil, err}
				continue
			}
			tx, err := w.txToSStx(txr.pair,
				txr.usedInputs,
				txr.inputs,
				txr.couts,
				udb.DefaultAccountNum,
				txr.minconf)
			heldUnlock.release()
			txr.resp <- createSStxResponse{tx, err}

		case txr := <-w.createSSGenRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- createSSGenResponse{nil, err}
				continue
			}
			tx, err := w.txToSSGen(txr.tickethash,
				txr.blockhash,
				txr.height,
				txr.votebits)
			heldUnlock.release()
			txr.resp <- createSSGenResponse{tx, err}

		case txr := <-w.createSSRtxRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- createSSRtxResponse{nil, err}
				continue
			}
			tx, err := w.txToSSRtx(txr.tickethash)
			heldUnlock.release()
			txr.resp <- createSSRtxResponse{tx, err}

		case txr := <-w.purchaseTicketRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- purchaseTicketResponse{nil, err}
				continue
			}
			data, err := w.purchaseTickets(txr)
			heldUnlock.release()
			txr.resp <- purchaseTicketResponse{data, err}

		case <-quit:
			break out
		}
	}
	w.wg.Done()
}

// Consolidate consolidates as many UTXOs as are passed in the inputs argument.
// If that many UTXOs can not be found, it will use the maximum it finds. This
// will only compress UTXOs in the default account
func (w *Wallet) Consolidate(inputs int, account uint32,
	address dcrutil.Address) (*chainhash.Hash, error) {
	req := consolidateRequest{
		inputs:  inputs,
		account: account,
		address: address,
		resp:    make(chan consolidateResponse),
	}
	w.consolidateRequests <- req
	resp := <-req.resp
	return resp.txHash, resp.err
}

// CreateSimpleTx creates a new signed transaction spending unspent P2PKH
// outputs with at laest minconf confirmations spending to any number of
// address/amount pairs.  Change and an appropriate transaction fee are
// automatically included, if necessary.  All transaction creation through this
// function is serialized to prevent the creation of many transactions which
// spend the same outputs.
func (w *Wallet) CreateSimpleTx(account uint32, outputs []*wire.TxOut,
	minconf int32) (*txauthor.AuthoredTx, error) {

	req := createTxRequest{
		account: account,
		outputs: outputs,
		minconf: minconf,
		resp:    make(chan createTxResponse),
	}
	w.createTxRequests <- req
	resp := <-req.resp
	return resp.tx, resp.err
}

// CreateMultisigTx receives a request from the RPC and ships it to txCreator to
// generate a new multisigtx.
func (w *Wallet) CreateMultisigTx(account uint32, amount dcrutil.Amount,
	pubkeys []*dcrutil.AddressSecpPubKey, nrequired int8,
	minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {

	req := createMultisigTxRequest{
		account:   account,
		amount:    amount,
		pubkeys:   pubkeys,
		nrequired: nrequired,
		minconf:   minconf,
		resp:      make(chan createMultisigTxResponse),
	}
	w.createMultisigTxRequests <- req
	resp := <-req.resp
	return resp.tx, resp.address, resp.redeemScript, resp.err
}

// CreateSStxTx receives a request from the RPC and ships it to txCreator to
// generate a new SStx.
func (w *Wallet) CreateSStxTx(pair map[string]dcrutil.Amount,
	usedInputs []udb.Credit,
	inputs []dcrjson.SStxInput,
	couts []dcrjson.SStxCommitOut,
	minconf int32) (*CreatedTx, error) {

	req := createSStxRequest{
		usedInputs: usedInputs,
		pair:       pair,
		inputs:     inputs,
		couts:      couts,
		minconf:    minconf,
		resp:       make(chan createSStxResponse),
	}
	w.createSStxRequests <- req
	resp := <-req.resp
	return resp.tx, resp.err
}

// CreateSSGenTx receives a request from the RPC and ships it to txCreator to
// generate a new SSGen.
func (w *Wallet) CreateSSGenTx(ticketHash chainhash.Hash,
	blockHash chainhash.Hash,
	height int64,
	voteBits uint16) (*CreatedTx, error) {

	req := createSSGenRequest{
		tickethash: ticketHash,
		blockhash:  blockHash,
		height:     height,
		votebits:   voteBits,
		resp:       make(chan createSSGenResponse),
	}
	w.createSSGenRequests <- req
	resp := <-req.resp
	return resp.tx, resp.err
}

// CreateSSRtx receives a request from the RPC and ships it to txCreator to
// generate a new SSRtx.
func (w *Wallet) CreateSSRtx(ticketHash chainhash.Hash) (*CreatedTx, error) {

	req := createSSRtxRequest{
		tickethash: ticketHash,
		resp:       make(chan createSSRtxResponse),
	}
	w.createSSRtxRequests <- req
	resp := <-req.resp
	return resp.tx, resp.err
}

// PurchaseTickets receives a request from the RPC and ships it to txCreator
// to purchase a new ticket. It returns a slice of the hashes of the purchased
// tickets.
func (w *Wallet) PurchaseTickets(minBalance, spendLimit dcrutil.Amount,
	minConf int32, ticketAddr dcrutil.Address, account uint32,
	numTickets int, poolAddress dcrutil.Address, poolFees float64,
	expiry int32, txFee dcrutil.Amount, ticketFee dcrutil.Amount) ([]*chainhash.Hash,
	error) {

	req := purchaseTicketRequest{
		minBalance:  minBalance,
		spendLimit:  spendLimit,
		minConf:     minConf,
		ticketAddr:  ticketAddr,
		account:     account,
		numTickets:  numTickets,
		poolAddress: poolAddress,
		poolFees:    poolFees,
		expiry:      expiry,
		txFee:       txFee,
		ticketFee:   ticketFee,
		resp:        make(chan purchaseTicketResponse),
	}
	w.purchaseTicketRequests <- req
	resp := <-req.resp
	return resp.data, resp.err
}

type (
	unlockRequest struct {
		passphrase []byte
		lockAfter  <-chan time.Time // nil prevents the timeout.
		err        chan error
	}

	changePassphraseRequest struct {
		old, new []byte
		err      chan error
	}

	// heldUnlock is a tool to prevent the wallet from automatically
	// locking after some timeout before an operation which needed
	// the unlocked wallet has finished.  Any aquired heldUnlock
	// *must* be released (preferably with a defer) or the wallet
	// will forever remain unlocked.
	heldUnlock chan struct{}
)

// walletLocker manages the locked/unlocked state of a wallet.
func (w *Wallet) walletLocker() {
	var timeout <-chan time.Time
	holdChan := make(heldUnlock)
	quit := w.quitChan()
out:
	for {
		select {
		case req := <-w.unlockRequests:
			hadTimeout := timeout != nil
			var wasLocked bool
			err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
				wasLocked = w.Manager.IsLocked()
				addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
				return w.Manager.Unlock(addrmgrNs, req.passphrase)
			})
			if err != nil {
				if !wasLocked {
					log.Info("The wallet has been locked due to an incorrect passphrase.")
				}
				req.err <- err
				continue
			}
			// When the wallet was already unlocked without any timeout, do not
			// set the timeout and instead wait until an explicit lock is
			// performed.  Read the timeout in a new goroutine so that callers
			// won't deadlock on the send.
			if !wasLocked && !hadTimeout && req.lockAfter != nil {
				go func() { <-req.lockAfter }()
			} else {
				timeout = req.lockAfter
			}
			switch {
			case (wasLocked || hadTimeout) && timeout == nil:
				log.Info("The wallet has been unlocked without a time limit")
			case (wasLocked || !hadTimeout) && timeout != nil:
				log.Info("The wallet has been temporarily unlocked")
			}
			req.err <- nil
			continue

		case req := <-w.changePassphrase:
			err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
				addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
				return w.Manager.ChangePassphrase(addrmgrNs, req.old,
					req.new, true)
			})
			req.err <- err
			continue

		case req := <-w.holdUnlockRequests:
			if w.Manager.IsLocked() {
				close(req)
				continue
			}

			req <- holdChan
			<-holdChan // Block until the lock is released.

			// If, after holding onto the unlocked wallet for some
			// time, the timeout has expired, lock it now instead
			// of hoping it gets unlocked next time the top level
			// select runs.
			select {
			case <-timeout:
				// Let the top level select fallthrough so the
				// wallet is locked.
			default:
				continue
			}

		case w.lockState <- w.Manager.IsLocked():
			continue

		case <-quit:
			break out

		case <-w.lockRequests:
			timeout = nil
		case <-timeout:
		}

		// Select statement fell through by an explicit lock or the
		// timer expiring.  Lock the manager here.
		timeout = nil
		err := w.Manager.Lock()
		if err != nil && !apperrors.IsError(err, apperrors.ErrLocked) {
			log.Errorf("Could not lock wallet: %v", err)
		} else {
			log.Info("The wallet has been locked.")
		}
	}
	w.wg.Done()
}

// Unlock unlocks the wallet's address manager and relocks it after timeout has
// expired.  If the wallet is already unlocked and the new passphrase is
// correct, the current timeout is replaced with the new one.  The wallet will
// be locked if the passphrase is incorrect or any other error occurs during the
// unlock.
func (w *Wallet) Unlock(passphrase []byte, lock <-chan time.Time) error {
	err := make(chan error, 1)
	w.unlockRequests <- unlockRequest{
		passphrase: passphrase,
		lockAfter:  lock,
		err:        err,
	}

	return <-err
}

// Lock locks the wallet's address manager.
func (w *Wallet) Lock() {
	w.lockRequests <- struct{}{}
}

// Locked returns whether the account manager for a wallet is locked.
func (w *Wallet) Locked() bool {
	return <-w.lockState
}

// holdUnlock prevents the wallet from being locked.  The heldUnlock object
// *must* be released, or the wallet will forever remain unlocked.
//
// TODO: To prevent the above scenario, perhaps closures should be passed
// to the walletLocker goroutine and disallow callers from explicitly
// handling the locking mechanism.
func (w *Wallet) holdUnlock() (heldUnlock, error) {
	req := make(chan heldUnlock)
	w.holdUnlockRequests <- req
	hl, ok := <-req
	if !ok {
		// TODO(davec): This should be defined and exported from
		// udb.
		return nil, apperrors.E{
			ErrorCode:   apperrors.ErrLocked,
			Description: "address manager is locked",
		}
	}
	return hl, nil
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
	err := make(chan error, 1)
	w.changePassphrase <- changePassphraseRequest{
		old: old,
		new: new,
		err: err,
	}
	return <-err
}

// ChangePublicPassphrase modifies the public passphrase of the wallet.
func (w *Wallet) ChangePublicPassphrase(old, new []byte) error {
	return walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		return w.Manager.ChangePassphrase(addrmgrNs, old, new, false)
	})
}

// CalculateAccountBalance sums the amounts of all unspent transaction
// outputs to the given account of a wallet and returns the balance.
func (w *Wallet) CalculateAccountBalance(account uint32, confirms int32) (udb.Balances, error) {
	var balance udb.Balances
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error

		balance, err = w.TxStore.AccountBalance(txmgrNs, addrmgrNs,
			confirms, account)
		return err
	})
	return balance, err
}

// CalculateAccountBalances calculates the values for the wtxmgr struct Balance,
// which includes the total balance, the spendable balance, and the balance
// which has yet to mature.
func (w *Wallet) CalculateAccountBalances(confirms int32) (map[uint32]*udb.Balances, error) {
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

	return balances, err
}

// CurrentAddress gets the most recently requested payment address from a wallet.
// If the address has already been used (there is at least one transaction
// spending to it in the blockchain or dcrd mempool), the next chained address
// is returned.
func (w *Wallet) CurrentAddress(account uint32) (dcrutil.Address, error) {
	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()

	data, ok := w.addressBuffers[account]
	if !ok {
		const str = "account not found"
		return nil, apperrors.E{ErrorCode: apperrors.ErrAccountNotFound, Description: str, Err: nil}
	}
	buf := &data.albExternal

	childIndex := buf.lastUsed + buf.cursor
	child, err := buf.branchXpub.Child(childIndex)
	if err != nil {
		const str = "failed to derive child key"
		return nil, apperrors.E{ErrorCode: apperrors.ErrKeyChain, Description: str, Err: nil}
	}
	return child.Address(w.chainParams)
}

// PubKeyForAddress looks up the associated public key for a P2PKH address.
func (w *Wallet) PubKeyForAddress(a dcrutil.Address) (chainec.PublicKey, error) {
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
	return pubKey, err
}

// SignMessage returns the signature of a signed message using an address'
// associated private key.
func (w *Wallet) SignMessage(msg string, addr dcrutil.Address) (sig []byte, err error) {
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
		return nil, err
	}
	pkCast, ok := privKey.(*secp256k1.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("Unable to create secp256k1.PrivateKey" +
			"from chainec.PrivateKey")
	}
	return secp256k1.SignCompact(secp256k1.S256(), pkCast, messageHash, true)
}

// existsAddressOnChain checks the chain on daemon to see if the given address
// has been used before on the main chain.
func (w *Wallet) existsAddressOnChain(address dcrutil.Address) (bool, error) {
	chainClient, err := w.requireChainClient()
	if err != nil {
		return false, err
	}
	exists, err := chainClient.ExistsAddress(address)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// ExistsAddressOnChain is the exported version of existsAddressOnChain that is
// safe for concurrent access.
func (w *Wallet) ExistsAddressOnChain(address dcrutil.Address) (bool, error) {
	return w.existsAddressOnChain(address)
}

// HaveAddress returns whether the wallet is the owner of the address a.
func (w *Wallet) HaveAddress(a dcrutil.Address) (bool, error) {
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		_, err := w.Manager.Address(addrmgrNs, a)
		return err
	})
	if err == nil {
		return true, nil
	}
	if apperrors.IsError(err, apperrors.ErrAddressNotFound) {
		return false, nil
	}
	return false, err
}

// AccountOfAddress finds the account that an address is associated with.
func (w *Wallet) AccountOfAddress(a dcrutil.Address) (uint32, error) {
	var account uint32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		account, err = w.Manager.AddrAccount(addrmgrNs, a)
		return err
	})
	return account, err
}

// AddressInfo returns detailed information regarding a wallet address.
func (w *Wallet) AddressInfo(a dcrutil.Address) (udb.ManagedAddress, error) {
	var managedAddress udb.ManagedAddress
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		managedAddress, err = w.Manager.Address(addrmgrNs, a)
		return err
	})
	return managedAddress, err
}

// AccountNumber returns the account number for an account name.
func (w *Wallet) AccountNumber(accountName string) (uint32, error) {
	var account uint32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		account, err = w.Manager.LookupAccount(addrmgrNs, accountName)
		return err
	})
	return account, err
}

// AccountName returns the name of an account.
func (w *Wallet) AccountName(accountNumber uint32) (string, error) {
	var accountName string
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		accountName, err = w.Manager.AccountName(addrmgrNs, accountNumber)
		return err
	})
	return accountName, err
}

// AccountProperties returns the properties of an account, including address
// indexes and name. It first fetches the desynced information from the address
// manager, then updates the indexes based on the address pools.
func (w *Wallet) AccountProperties(acct uint32) (*udb.AccountProperties, error) {
	var props *udb.AccountProperties
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		waddrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		props, err = w.Manager.AccountProperties(waddrmgrNs, acct)
		return err
	})
	return props, err
}

// RenameAccount sets the name for an account number to newName.
func (w *Wallet) RenameAccount(account uint32, newName string) error {
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
	if err == nil {
		w.NtfnServer.notifyAccountProperties(props)
	}
	return err
}

const maxEmptyAccounts = 100

// NextAccount creates the next account and returns its account number.  The
// name must be unique to the account.  In order to support automatic seed
// restoring, new accounts may not be created when all of the previous 100
// accounts have no transaction history (this is a deviation from the BIP0044
// spec, which allows no unused account gaps).
func (w *Wallet) NextAccount(name string) (uint32, error) {
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
			if props.LastUsedExternalIndex != 0 || props.LastUsedInternalIndex != 0 {
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
		return 0, err
	}

	extKey, intKey, err := deriveBranches(xpub)
	if err != nil {
		return 0, err
	}
	w.addressBuffersMu.Lock()
	w.addressBuffers[account] = &bip0044AccountData{
		albExternal: addressBuffer{branchXpub: extKey},
		albInternal: addressBuffer{branchXpub: intKey},
	}
	w.addressBuffersMu.Unlock()

	client := w.ChainClient()
	if client != nil {
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
				errs <- client.LoadTxFilter(false, addrs, nil)
			}()
		}
		for i := 0; i < cap(errs); i++ {
			err := <-errs
			if err != nil {
				return 0, err
			}
		}
	}

	w.NtfnServer.notifyAccountProperties(props)

	return account, nil
}

// MasterPubKey returns the BIP0044 master public key for the passed account.
//
// TODO: This should not be returning the key as a string.
func (w *Wallet) MasterPubKey(account uint32) (string, error) {
	var masterPubKey string
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		masterPubKey, err = w.Manager.GetMasterPubkey(addrmgrNs, account)
		return err
	})
	return masterPubKey, err
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
func RecvCategory(details *udb.TxDetails, syncHeight int32,
	chainParams *chaincfg.Params) CreditCategory {
	if blockchain.IsCoinBaseTx(&details.MsgTx) {
		if confirmed(int32(chainParams.CoinbaseMaturity), details.Block.Height,
			syncHeight) {
			return CreditGenerate
		}
		return CreditImmature
	}
	return CreditReceive
}

// listTransactions creates a object that may be marshalled to a response result
// for a listtransactions RPC.
//
// TODO: This should be moved to the legacyrpc package.
func listTransactions(tx walletdb.ReadTx, details *udb.TxDetails, addrMgr *udb.Manager,
	syncHeight int32, net *chaincfg.Params) []dcrjson.ListTransactionsResult {

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

	results := []dcrjson.ListTransactionsResult{}
	txHashStr := details.Hash.String()
	received := details.Received.Unix()
	generated := blockchain.IsCoinBaseTx(&details.MsgTx)
	recvCat := RecvCategory(details, syncHeight, net).String()

	send := len(details.Debits) != 0

	txTypeStr := dcrjson.LTTTRegular
	switch details.TxType {
	case stake.TxTypeSStx:
		txTypeStr = dcrjson.LTTTTicket
	case stake.TxTypeSSGen:
		txTypeStr = dcrjson.LTTTVote
	case stake.TxTypeSSRtx:
		txTypeStr = dcrjson.LTTTRevocation
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
		// Determine if this output is a credit, and if so, determine
		// its spentness.
		var isCredit bool
		var spentCredit bool
		for _, cred := range details.Credits {
			if cred.Index == uint32(i) {
				// Change outputs are ignored.
				if cred.Change {
					continue outputs
				}

				isCredit = true
				spentCredit = cred.Spent
				break
			}
		}

		var address string
		var accountName string
		_, addrs, _, _ := txscript.ExtractPkScriptAddrs(output.Version,
			output.PkScript, net)
		if len(addrs) == 1 {
			addr := addrs[0]
			address = addr.EncodeAddress()
			account, err := addrMgr.AddrAccount(addrmgrNs, addrs[0])
			if err == nil {
				accountName, err = addrMgr.AccountName(addrmgrNs, account)
				if err != nil {
					accountName = ""
				}
			}
		}

		amountF64 := dcrutil.Amount(output.Value).ToCoin()
		result := dcrjson.ListTransactionsResult{
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

		if send || spentCredit {
			result.Category = "send"
			result.Amount = -amountF64
			result.Fee = &feeF64
			results = append(results, result)
		}
		if isCredit {
			result.Account = accountName
			result.Category = recvCat
			result.Amount = amountF64
			result.Fee = nil
			results = append(results, result)
		}
	}
	return results
}

// ListSinceBlock returns a slice of objects with details about transactions
// since the given block. If the block is -1 then all transactions are included.
// This is intended to be used for listsinceblock RPC replies.
func (w *Wallet) ListSinceBlock(start, end, syncHeight int32) ([]dcrjson.ListTransactionsResult, error) {
	txList := []dcrjson.ListTransactionsResult{}
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			for _, detail := range details {
				jsonResults := listTransactions(tx, &detail,
					w.Manager, syncHeight, w.chainParams)
				txList = append(txList, jsonResults...)
			}
			return false, nil
		}

		return w.TxStore.RangeTransactions(txmgrNs, start, end, rangeFn)
	})
	return txList, err
}

// ListTransactions returns a slice of objects with details about a recorded
// transaction.  This is intended to be used for listtransactions RPC
// replies.
func (w *Wallet) ListTransactions(from, count int) ([]dcrjson.ListTransactionsResult, error) {
	txList := []dcrjson.ListTransactionsResult{}
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

				jsonResults := listTransactions(tx, &details[i],
					w.Manager, tipHeight, w.chainParams)
				txList = append(txList, jsonResults...)

				if len(jsonResults) > 0 {
					n++
				}
			}

			return false, nil
		}

		// Return newer results first by starting at mempool height and working
		// down to the genesis block.
		return w.TxStore.RangeTransactions(txmgrNs, -1, 0, rangeFn)
	})
	return txList, err
}

// ListAddressTransactions returns a slice of objects with details about
// recorded transactions to or from any address belonging to a set.  This is
// intended to be used for listaddresstransactions RPC replies.
func (w *Wallet) ListAddressTransactions(pkHashes map[string]struct{}) ([]dcrjson.ListTransactionsResult, error) {
	txList := []dcrjson.ListTransactionsResult{}
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
						txscript.DefaultScriptVersion, pkScript, w.chainParams)
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

					jsonResults := listTransactions(tx, detail,
						w.Manager, tipHeight, w.chainParams)
					if err != nil {
						return false, err
					}
					txList = append(txList, jsonResults...)
					continue loopDetails
				}
			}
			return false, nil
		}

		return w.TxStore.RangeTransactions(txmgrNs, 0, -1, rangeFn)
	})
	return txList, err
}

// ListAllTransactions returns a slice of objects with details about a recorded
// transaction.  This is intended to be used for listalltransactions RPC
// replies.
func (w *Wallet) ListAllTransactions() ([]dcrjson.ListTransactionsResult, error) {
	txList := []dcrjson.ListTransactionsResult{}
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
				jsonResults := listTransactions(tx, &details[i],
					w.Manager, tipHeight, w.chainParams)
				txList = append(txList, jsonResults...)
			}
			return false, nil
		}

		// Return newer results first by starting at mempool height and
		// working down to the genesis block.
		return w.TxStore.RangeTransactions(txmgrNs, -1, 0, rangeFn)
	})
	return txList, err
}

// BlockIdentifier identifies a block by either a height or a hash.
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

// GetTransactionsResult is the result of the wallet's GetTransactions method.
// See GetTransactions for more details.
type GetTransactionsResult struct {
	MinedTransactions   []Block
	UnminedTransactions []TransactionSummary
}

// GetTransactions returns transaction results between a starting and ending
// block.  Blocks in the block range may be specified by either a height or a
// hash.
//
// Because this is a possibly lenghtly operation, a cancel channel is provided
// to cancel the task.  If this channel unblocks, the results created thus far
// will be returned.
//
// Transaction results are organized by blocks in ascending order and unmined
// transactions in an unspecified order.  Mined transactions are saved in a
// Block structure which records properties about the block.
func (w *Wallet) GetTransactions(startBlock, endBlock *BlockIdentifier, cancel <-chan struct{}) (*GetTransactionsResult, error) {
	var start, end int32 = 0, -1

	w.chainClientLock.Lock()
	chainClient := w.chainClient
	w.chainClientLock.Unlock()

	// TODO: Fetching block heights by their hashes is inherently racy
	// because not all block headers are saved but when they are for SPV the
	// db can be queried directly without this.
	var startResp, endResp dcrrpcclient.FutureGetBlockVerboseResult
	if startBlock != nil {
		if startBlock.hash == nil {
			start = startBlock.height
		} else {
			if chainClient == nil {
				return nil, errors.New("no chain server client")
			}
			startResp = chainClient.GetBlockVerboseAsync(startBlock.hash, false)
		}
	}
	if endBlock != nil {
		if endBlock.hash == nil {
			end = endBlock.height
		} else {
			if chainClient == nil {
				return nil, errors.New("no chain server client")
			}
			endResp = chainClient.GetBlockVerboseAsync(endBlock.hash, false)
		}
	}
	if startResp != nil {
		resp, err := startResp.Receive()
		if err != nil {
			return nil, err
		}
		start = int32(resp.Height)
	}
	if endResp != nil {
		resp, err := endResp.Receive()
		if err != nil {
			return nil, err
		}
		end = int32(resp.Height)
	}

	var res GetTransactionsResult
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

			if details[0].Block.Height != -1 {
				blockHash := details[0].Block.Hash
				res.MinedTransactions = append(res.MinedTransactions, Block{
					Hash:         &blockHash,
					Height:       details[0].Block.Height,
					Timestamp:    details[0].Block.Time.Unix(),
					Transactions: txs,
				})
			} else {
				res.UnminedTransactions = txs
			}

			select {
			case <-cancel:
				return true, nil
			default:
				return false, nil
			}
		}

		return w.TxStore.RangeTransactions(txmgrNs, start, end, rangeFn)
	})
	return &res, err
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
				txscript.DefaultScriptVersion, output.PkScript, w.chainParams)
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
	return &AccountsResult{
		Accounts:           accounts,
		CurrentBlockHash:   &tipHash,
		CurrentBlockHeight: tipHeight,
	}, err
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
func (w *Wallet) ListUnspent(minconf, maxconf int32, addresses map[string]struct{}) ([]*dcrjson.ListUnspentResult, error) {
	var results []*dcrjson.ListUnspentResult
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
				return fmt.Errorf("Couldn't get credit details")
			}

			// Outputs with fewer confirmations than the minimum or more
			// confs than the maximum are excluded.
			confs := confirms(output.Height, tipHeight)
			if confs < minconf || confs > maxconf {
				continue
			}

			// Only mature coinbase outputs are included.
			if output.FromCoinBase {
				target := int32(w.ChainParams().CoinbaseMaturity)
				if !confirmed(target, output.Height, tipHeight) {
					continue
				}
			}

			switch details.TxRecord.TxType {
			case stake.TxTypeSStx:
				// Ticket commitment, only spendable after ticket maturity.
				// You can only spent it after TM many blocks has gone past, so
				// ticket maturity + 1??? Check this DECRED TODO
				if output.Index == 0 {
					if !confirmed(int32(w.chainParams.TicketMaturity+1),
						details.Height(), tipHeight) {
						continue
					}
				}
				// Change outputs.
				if (output.Index > 0) && (output.Index%2 == 0) {
					if !confirmed(int32(w.chainParams.SStxChangeMaturity),
						details.Height(), tipHeight) {
						continue
					}
				}
			case stake.TxTypeSSGen:
				// All non-OP_RETURN outputs for SSGen tx are only spendable
				// after coinbase maturity many blocks.
				if !confirmed(int32(w.chainParams.CoinbaseMaturity),
					details.Height(), tipHeight) {
					continue
				}
			case stake.TxTypeSSRtx:
				// All outputs for SSRtx tx are only spendable
				// after coinbase maturity many blocks.
				if !confirmed(int32(w.chainParams.CoinbaseMaturity),
					details.Height(), tipHeight) {
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
				txscript.DefaultScriptVersion, output.PkScript, w.chainParams)
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
					_, ok := addresses[addr.EncodeAddress()]
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
					if apperrors.IsError(err, apperrors.ErrAddressNotFound) {
						break scSwitch
					}
					return err
				}
				spendable = true
			}

			result := &dcrjson.ListUnspentResult{
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
				result.Address = addrs[0].EncodeAddress()
			}

			results = append(results, result)
		}
		return nil
	})
	return results, err
}

// DumpWIFPrivateKey returns the WIF encoded private key for a
// single wallet address.
func (w *Wallet) DumpWIFPrivateKey(addr dcrutil.Address) (string, error) {
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
		return "", err
	}
	wif, err := dcrutil.NewWIF(privKey, w.chainParams, privKey.GetType())
	if err != nil {
		return "", err
	}
	return wif.String(), nil
}

// ImportPrivateKey imports a private key to the wallet and writes the new
// wallet to disk.
func (w *Wallet) ImportPrivateKey(wif *dcrutil.WIF) (string, error) {
	chainClient, err := w.requireChainClient()
	if err != nil {
		return "", err
	}

	// Attempt to import private key into wallet.
	var addr dcrutil.Address
	var props *udb.AccountProperties
	err = walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
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
		return "", err
	}

	err = chainClient.LoadTxFilter(false, []dcrutil.Address{addr}, nil)
	if err != nil {
		return "", fmt.Errorf("Failed to subscribe for address ntfns for "+
			"address %s: %s", addr.EncodeAddress(), err)
	}

	addrStr := addr.EncodeAddress()
	log.Infof("Imported payment address %s", addrStr)

	w.NtfnServer.notifyAccountProperties(props)

	// Return the payment address string of the imported private key.
	return addrStr, nil
}

// ImportScript imports a redeemscript to the wallet. If it also allows the
// user to specify whether or not they want the redeemscript to be rescanned,
// and how far back they wish to rescan.
func (w *Wallet) ImportScript(rs []byte) error {
	chainClient, err := w.requireChainClient()
	if err != nil {
		return err
	}

	return walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadWriteBucket(wtxmgrNamespaceKey)

		err := w.TxStore.InsertTxScript(txmgrNs, rs)
		if err != nil {
			return err
		}

		// Get current block's height and hash.
		mscriptaddr, err := w.Manager.ImportScript(addrmgrNs, rs)
		if err != nil {
			switch {
			// Don't care if it's already there.
			case apperrors.IsError(err, apperrors.ErrDuplicateAddress):
				return nil
			case apperrors.IsError(err, apperrors.ErrLocked):
				log.Debugf("failed to attempt script importation " +
					"of incoming tx because addrmgr was locked")
				return err
			default:
				return err
			}
		}
		err = chainClient.LoadTxFilter(false,
			[]dcrutil.Address{mscriptaddr.Address()}, nil)
		if err != nil {
			return fmt.Errorf("Failed to subscribe for address ntfns for "+
				"address %s: %s", mscriptaddr.Address().EncodeAddress(),
				err.Error())
		}

		log.Infof("Redeem script hash %x (address %v) successfully added.",
			mscriptaddr.Address().ScriptAddress(),
			mscriptaddr.Address().EncodeAddress())

		return nil
	})
}

// RedeemScriptCopy returns a copy of a redeem script to redeem outputs payed to
// a P2SH address.
func (w *Wallet) RedeemScriptCopy(addr dcrutil.Address) ([]byte, error) {
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
	return scriptCopy, err
}

// StakeInfoData is a struct containing the data that would be returned from
// a StakeInfo request to the wallet.
type StakeInfoData struct {
	BlockHeight   int64
	PoolSize      uint32
	AllMempoolTix uint32
	OwnMempoolTix uint32
	Immature      uint32
	Live          uint32
	Voted         uint32
	Missed        uint32
	Revoked       uint32
	Expired       uint32
	TotalSubsidy  dcrutil.Amount
}

// hashInPointerSlice returns whether a hash exists in a slice of hash pointers
// or not.
func hashInPointerSlice(h chainhash.Hash, list []*chainhash.Hash) bool {
	for _, hash := range list {
		if h == *hash {
			return true
		}
	}

	return false
}

func firstError(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// StakeInfo collects and returns staking statistics for this wallet to the end
// user. This includes:
//
//     PoolSize         uint32   Number of live tickets in the ticket pool
//     AllMempoolTix    uint32   Number of tickets currently in the mempool
//     OwnMempoolTix    uint32   Number of tickets in mempool that are from
//                                 this wallet
//     Immature         uint32   Number of tickets from this wallet that are in
//                                 the blockchain but which are not yet mature
//     Live             uint32   Number of mature, active tickets owned by this
//                                 wallet
//     Voted            uint32   Number of votes cast by this wallet
//     Missed           uint32   Number of missed tickets (failing to vote or
//                                 expired)
//     Revoked          uint32   Number of missed tickets that were missed and
//                                 then revoked
//     TotalSubsidy     int64    Total amount of coins earned by stake mining
//
// Getting this information is extremely costly as in involves a massive
// number of chain server calls.
func (w *Wallet) StakeInfo(chainClient *dcrrpcclient.Client) (*StakeInfoData, error) {
	var resp *StakeInfoData
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		stakemgrNs := tx.ReadBucket(wstakemgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		mempoolTicketsFuture := chainClient.GetRawMempoolAsync(dcrjson.GRMTickets)
		missedTicketsFuture := chainClient.MissedTicketsAsync()

		var (
			poolSize                                         uint32
			ticketHashPtrs, voteHashPtrs, revocationHashPtrs []*chainhash.Hash
			liveTicketsFuture                                dcrrpcclient.FutureExistsLiveTicketsResult
			expiredTicketsFuture                             dcrrpcclient.FutureExistsExpiredTicketsResult

			err1, err2, err3, err4, err5 error // for concurrent work
			wg                           sync.WaitGroup
		)

		// DB calls in the same transaction MUST NOT be run concurrently.  bolt
		// bucket usage is not concurrent safe due to subbucket caching and
		// statistics counting.
		//
		// Therefore load up initial info out of the db serially and then spawn
		// goroutines to use the results concurrently.
		wg.Add(4)
		ticketHashes, err := w.StakeMgr.DumpSStxHashes()
		if err != nil {
			return err
		}
		go func() {
			ticketHashPtrs = make([]*chainhash.Hash, len(ticketHashes))
			for i := range ticketHashes {
				ticketHashPtrs[i] = &ticketHashes[i]
			}
			liveTicketsFuture = chainClient.ExistsLiveTicketsAsync(ticketHashPtrs)
			wg.Done()
		}()
		revocationHashes, err := w.StakeMgr.DumpSSRtxTickets(stakemgrNs)
		if err != nil {
			return err
		}
		go func() {
			revocationHashPtrs = make([]*chainhash.Hash, len(revocationHashes))
			for i := range revocationHashes {
				revocationHashPtrs[i] = &revocationHashes[i]
			}
			expiredTicketsFuture = chainClient.ExistsExpiredTicketsAsync(revocationHashPtrs)
			wg.Done()
		}()
		voteHashes, err := w.StakeMgr.DumpSSGenHashes(stakemgrNs)
		if err != nil {
			return err
		}
		go func() {
			voteHashPtrs = make([]*chainhash.Hash, len(voteHashes))
			for i := range voteHashes {
				voteHashPtrs[i] = &voteHashes[i]
			}
			wg.Done()
		}()
		tipHash, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		serHeader, err := w.TxStore.GetSerializedBlockHeader(txmgrNs, &tipHash)
		if err != nil {
			return err
		}
		go func() {
			defer wg.Done()
			var tipHeader wire.BlockHeader
			err1 = tipHeader.Deserialize(bytes.NewReader(serHeader))
			if err1 != nil {
				return
			}
			// Get the poolsize estimate from the current best block.
			// The correct poolsize would be the pool size to be mined
			// into the next block, which takes into account maturing
			// stake tickets, voters, and expiring tickets. There
			// currently isn't a way to get this from the RPC, so
			// just use the current block pool size as a "good
			// enough" estimate for now.
			poolSize = tipHeader.PoolSize
		}()
		wg.Wait()
		if err1 != nil {
			return err1
		}

		var (
			mempoolTicketCount      uint32
			ownedMempoolTicketCount uint32
			missedCount             uint32
			liveCount               uint32
			immatureCount           uint32
			expiredCount            uint32
			ownedMempoolTickets     []*chainhash.Hash
			totalSubsidy            dcrutil.Amount
			maybeImmature           = make(chan *chainhash.Hash, len(ticketHashPtrs))
		)
		wg.Add(5)
		go func() {
			// This looks extremely suspicious (why would another view need to
			// be opened when we're already inside of a view?) but see the above
			// comment regarding bolt concurrent usage.  Opening another view
			// transaction means that database calls can safely be run
			// concurrently with the already opened view.
			err1 = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
				txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
				for _, voteHash := range voteHashPtrs {
					msgTx, err := w.TxStore.Tx(txmgrNs, voteHash)
					if err != nil || msgTx == nil {
						log.Tracef("Failed to find vote in blockchain while generating "+
							"stake info (hash %v, err %s)", voteHash, err)
						continue
					}
					totalSubsidy += dcrutil.Amount(msgTx.TxIn[0].ValueIn)
				}
				return nil
			})
			wg.Done()
		}()
		go func() {
			var mempoolTickets []*chainhash.Hash
			mempoolTickets, err2 = mempoolTicketsFuture.Receive()
			mempoolTicketCount = uint32(len(mempoolTickets))
			ownedMempoolTickets = make([]*chainhash.Hash, 0, mempoolTicketCount)
			for i := range ticketHashes {
				ticketHash := &ticketHashes[i]
				if hashInPointerSlice(*ticketHash, mempoolTickets) {
					ownedMempoolTickets = append(ownedMempoolTickets, ticketHash)
				}
			}
			ownedMempoolTicketCount = uint32(len(ownedMempoolTickets))

			ticketMaturity := int64(w.ChainParams().TicketMaturity)
			for ticketHash := range maybeImmature {
				// Skip tickets that aren't in the blockchain.
				if hashInPointerSlice(*ticketHash, ownedMempoolTickets) {
					continue
				}

				height, err := w.TxStore.TxBlockHeight(tx, ticketHash)
				if err != nil {
					log.Tracef("Failed to find ticket in blockchain while generating "+
						"stake info (hash %v, err %s)", ticketHash, err)
					continue
				}

				if height != -1 && int64(tipHeight-height) < ticketMaturity {
					immatureCount++
				}
			}

			wg.Done()
		}()
		go func() {
			var allMissedTickets []*chainhash.Hash
			allMissedTickets, err3 = missedTicketsFuture.Receive()
			for _, ticketHash := range ticketHashes {
				found := false
				if hashInPointerSlice(ticketHash, allMissedTickets) {
					// Increment missedNum if the missed ticket doesn't have a
					// revoked associated with it
					for _, revocationHash := range revocationHashes {
						if ticketHash == revocationHash {
							found = true
							break
						}
					}
					if !found {
						missedCount++
					}
				}
			}
			missedCount += uint32(len(revocationHashes))
			wg.Done()
		}()
		go func() {
			defer func() {
				close(maybeImmature)
				wg.Done()
			}()
			exitsLiveTicketsHex, err := liveTicketsFuture.Receive()
			if err != nil {
				err4 = fmt.Errorf("existslivetickets: %v", err)
				return
			}
			existsLiveTicketsBitset, err := hex.DecodeString(exitsLiveTicketsHex)
			if err != nil {
				err4 = fmt.Errorf("existslivetickets: %v", err)
				return
			}
			for i, ticketHash := range ticketHashPtrs {
				if bitset.Bytes(existsLiveTicketsBitset).Get(i) {
					liveCount++
				} else {
					maybeImmature <- ticketHash
				}
			}
		}()
		go func() {
			defer wg.Done()
			existsExpiredTicketsHex, err := expiredTicketsFuture.Receive()
			if err != nil {
				err5 = fmt.Errorf("existsexpiredtickets: %v", err)
				return
			}
			existsExpiredTicketsBitset, err := hex.DecodeString(existsExpiredTicketsHex)
			if err != nil {
				err5 = fmt.Errorf("existsexpiredtickets: %v", err)
				return
			}
			for i := range revocationHashes {
				if bitset.Bytes(existsExpiredTicketsBitset).Get(i) {
					expiredCount++
				}
			}
		}()
		wg.Wait()
		err = firstError(err1, err2, err3, err4, err5)
		if err != nil {
			return err
		}

		// Do not count expired tickets with missed.
		missedCount -= expiredCount

		resp = &StakeInfoData{
			BlockHeight:   int64(tipHeight),
			PoolSize:      poolSize,
			AllMempoolTix: mempoolTicketCount,
			OwnMempoolTix: ownedMempoolTicketCount,
			Immature:      immatureCount,
			Live:          liveCount,
			Voted:         uint32(len(voteHashes)),
			TotalSubsidy:  totalSubsidy,
			Missed:        missedCount,
			Revoked:       uint32(len(revocationHashes)),
			Expired:       expiredCount,
		}
		return nil
	})
	return resp, err
}

// LockedOutpoint returns whether an outpoint has been marked as locked and
// should not be used as an input for created transactions.
func (w *Wallet) LockedOutpoint(op wire.OutPoint) bool {
	_, locked := w.lockedOutpoints[op]
	return locked
}

// LockOutpoint marks an outpoint as locked, that is, it should not be used as
// an input for newly created transactions.
func (w *Wallet) LockOutpoint(op wire.OutPoint) {
	w.lockedOutpoints[op] = struct{}{}
}

// UnlockOutpoint marks an outpoint as unlocked, that is, it may be used as an
// input for newly created transactions.
func (w *Wallet) UnlockOutpoint(op wire.OutPoint) {
	delete(w.lockedOutpoints, op)
}

// ResetLockedOutpoints resets the set of locked outpoints so all may be used
// as inputs for new transactions.
func (w *Wallet) ResetLockedOutpoints() {
	w.lockedOutpoints = map[wire.OutPoint]struct{}{}
}

// LockedOutpoints returns a slice of currently locked outpoints.  This is
// intended to be used by marshaling the result as a JSON array for
// listlockunspent RPC results.
func (w *Wallet) LockedOutpoints() []dcrjson.TransactionInput {
	locked := make([]dcrjson.TransactionInput, len(w.lockedOutpoints))
	i := 0
	for op := range w.lockedOutpoints {
		locked[i] = dcrjson.TransactionInput{
			Txid: op.Hash.String(),
			Vout: op.Index,
		}
		i++
	}
	return locked
}

// resendUnminedTxs iterates through all transactions that spend from wallet
// credits that are not known to have been mined into a block, and attempts
// to send each to the chain server for relay.
func (w *Wallet) resendUnminedTxs(chainClient *chain.RPCClient) {
	var txs []*wire.MsgTx
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		txs, err = w.TxStore.UnminedTxs(txmgrNs)
		return err
	})
	if err != nil {
		log.Errorf("Cannot load unmined transactions for resending: %v", err)
		return
	}

	for _, tx := range txs {
		resp, err := chainClient.SendRawTransaction(tx, w.AllowHighFees)
		if err != nil {
			// TODO(jrick): Check error for if this tx is a double spend,
			// remove it if so.
			log.Tracef("Could not resend transaction %v: %v",
				tx.TxHash(), err)
			continue
		}
		log.Tracef("Resent unmined transaction %v", resp)
	}
}

// SortedActivePaymentAddresses returns a slice of all active payment
// addresses in a wallet.
func (w *Wallet) SortedActivePaymentAddresses() ([]string, error) {
	var addrStrs []string
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		return w.Manager.ForEachActiveAddress(addrmgrNs, func(addr dcrutil.Address) error {
			addrStrs = append(addrStrs, addr.EncodeAddress())
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	sort.Sort(sort.StringSlice(addrStrs))
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
	return results, err
}

// TotalReceivedForAddr iterates through a wallet's transaction history,
// returning the total amount of decred received for a single wallet
// address.
func (w *Wallet) TotalReceivedForAddr(addr dcrutil.Address, minConf int32) (dcrutil.Amount, error) {
	var amount dcrutil.Amount
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		var (
			addrStr    = addr.EncodeAddress()
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
						if addrStr == a.EncodeAddress() {
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
	return amount, err
}

// SendOutputs creates and sends payment transactions. It returns the
// transaction hash upon success
func (w *Wallet) SendOutputs(outputs []*wire.TxOut, account uint32,
	minconf int32) (*chainhash.Hash, error) {

	relayFee := w.RelayFee()
	for _, output := range outputs {
		err := txrules.CheckOutput(output, relayFee)
		if err != nil {
			return nil, err
		}
	}

	// Create transaction, replying with an error if the creation
	// was not successful.
	createdTx, err := w.CreateSimpleTx(account, outputs, minconf)
	if err != nil {
		return nil, err
	}

	// TODO: The record already has the serialized tx, so no need to
	// serialize it again.
	hash := createdTx.Tx.TxHash()
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
func (w *Wallet) SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType,
	additionalPrevScripts map[wire.OutPoint][]byte,
	additionalKeysByAddress map[string]*dcrutil.WIF,
	p2shRedeemScriptsByAddress map[string][]byte) ([]SignatureError, error) {

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
				isSSGen, err := stake.IsSSGen(tx)
				if err == nil && isSSGen {
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
				if err != nil {
					return fmt.Errorf("Cannot query previous transaction "+
						"details for %v: %v", txIn.PreviousOutPoint, err)
				}
				if txDetails == nil {
					return fmt.Errorf("%v not found",
						txIn.PreviousOutPoint)
				}
				prevOutScript = txDetails.MsgTx.TxOut[prevIndex].PkScript
			}

			// Set up our callbacks that we pass to txscript so it can
			// look up the appropriate keys and scripts by address.
			getKey := txscript.KeyClosure(func(addr dcrutil.Address) (
				chainec.PrivateKey, bool, error) {
				if len(additionalKeysByAddress) != 0 {
					addrStr := addr.EncodeAddress()
					wif, ok := additionalKeysByAddress[addrStr]
					if !ok {
						return nil, false,
							fmt.Errorf("no key for address (needed: %v, have %v)",
								addr.EncodeAddress(), additionalKeysByAddress)
					}
					return wif.PrivKey, true, nil
				}
				address, err := w.Manager.Address(addrmgrNs, addr)
				if err != nil {
					return nil, false, err
				}

				pka, ok := address.(udb.ManagedPubKeyAddress)
				if !ok {
					return nil, false, fmt.Errorf("address %v is not "+
						"a pubkey address", address.Address().EncodeAddress())
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
					addrStr := addr.EncodeAddress()
					script, ok := p2shRedeemScriptsByAddress[addrStr]
					if !ok {
						return nil, errors.New("no script for " +
							"address")
					}
					return script, nil
				}

				// First check tx manager script store.
				scrTxStore, err := w.TxStore.GetTxScript(txmgrNs,
					addr.ScriptAddress())
				if err != nil {
					return nil, err
				}
				if scrTxStore != nil {
					return scrTxStore, nil
				}

				// Then check the address manager.
				script, done, err := w.Manager.RedeemScript(addrmgrNs, addr)
				if err != nil {
					return nil, err
				}
				doneFuncs = append(doneFuncs, done)
				return script, nil
			})

			// SigHashSingle inputs can only be signed if there's a
			// corresponding output. However this could be already signed,
			// so we always verify the output.
			if (hashType&txscript.SigHashSingle) !=
				txscript.SigHashSingle || i < len(tx.TxOut) {
				// Check for alternative checksig scripts and
				// set the signature suite accordingly.
				ecType := chainec.ECTypeSecp256k1
				class := txscript.GetScriptClass(txscript.DefaultScriptVersion, prevOutScript)
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
						Error:      err,
					})
					continue
				}
				txIn.SignatureScript = script
			}

			// Either it was already signed or we just signed it.
			// Find out if it is completely satisfied or still needs more.
			vm, err := txscript.NewEngine(prevOutScript, tx, i,
				txscript.StandardVerifyFlags, txscript.DefaultScriptVersion, nil)
			if err == nil {
				err = vm.Execute()
			}
			if err != nil {
				multisigNotEnoughSigs := false
				class, addr, _, _ := txscript.ExtractPkScriptAddrs(
					txscript.DefaultScriptVersion,
					additionalPrevScripts[txIn.PreviousOutPoint],
					w.ChainParams())

				if err == txscript.ErrStackUnderflow &&
					class == txscript.ScriptHashTy {
					redeemScript, _ := getScript(addr[0])
					redeemClass := txscript.GetScriptClass(
						txscript.DefaultScriptVersion, redeemScript)
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
						Error:      err,
					})
				}
			}
		}
		return nil
	})
	return signErrors, err
}

// PublishTransaction sends the transaction to the consensus RPC server so it
// can be propigated to other nodes and eventually mined.
//
// This function is unstable and will be removed once syncing code is moved out
// of the wallet.
func (w *Wallet) PublishTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	server, err := w.requireChainClient()
	if err != nil {
		return nil, err
	}

	return server.SendRawTransaction(tx, w.AllowHighFees)
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
		if apperrors.Wraps(err, errBreak) {
			return nil
		}
		return err
	})
	return needsSync, err
}

// Create creates an new wallet, writing it to an empty database.  If the passed
// seed is non-nil, it is used.  Otherwise, a secure random seed of the
// recommended length is generated.
func Create(db walletdb.DB, pubPass, privPass, seed []byte, params *chaincfg.Params) error {
	// If a seed was provided, ensure that it is of valid length. Otherwise,
	// we generate a random seed for the wallet with the recommended seed
	// length.
	if seed == nil {
		hdSeed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
		if err != nil {
			return err
		}
		seed = hdSeed
	}
	if len(seed) < hdkeychain.MinSeedBytes || len(seed) > hdkeychain.MaxSeedBytes {
		return hdkeychain.ErrInvalidSeedLen
	}

	return udb.Initialize(db, params, seed, pubPass, privPass)
}

// CreateWatchOnly creates a watchonly wallet on the provided db.
func CreateWatchOnly(db walletdb.DB, extendedPubKey string, pubPass []byte, params *chaincfg.Params) error {
	return udb.InitializeWatchOnly(db, params, extendedPubKey, pubPass)
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
		return nil, fmt.Errorf("failed to correctly parse passed stakepool " +
			"address public key and index")
	}

	// Parse the extended public key and ensure it's the right network.
	key, err := hdkeychain.NewKeyFromString(splStrs[0])
	if err != nil {
		return nil, err
	}
	if !key.IsForNet(params) {
		return nil, fmt.Errorf("extended public key is for wrong network")
	}

	// Parse the ending index and ensure it's valid.
	end, err := strconv.Atoi(splStrs[1])
	if err != nil {
		return nil, err
	}
	if end < 0 || end > udb.MaxAddressesPerAccount {
		return nil, fmt.Errorf("pool address index is invalid (got %v)",
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
		addrMap[addrs[i].EncodeAddress()] = struct{}{}
	}

	return addrMap, nil
}

// Open loads an already-created wallet from the passed database and namespaces.
func Open(db walletdb.DB, pubPass []byte, votingEnabled bool, addressReuse bool,
	pruneTickets bool, ticketAddress string, poolAddress string, poolFees float64,
	ticketFee float64, gapLimit int, stakePoolColdExtKey string, allowHighFees bool,
	relayFee float64, params *chaincfg.Params) (*Wallet, error) {

	// Migrate to the unified DB if necessary.
	needsMigration, err := udb.NeedsMigration(db)
	if err != nil {
		return nil, err
	}
	if needsMigration {
		err := udb.Migrate(db, params)
		if err != nil {
			return nil, err
		}
	}

	// Perform upgrades as necessary.
	err = udb.Upgrade(db, pubPass)
	if err != nil {
		return nil, err
	}

	// Open database managers
	addrMgr, txMgr, smgr, err := udb.Open(db, params, pubPass)
	if err != nil {
		return nil, err
	}

	// If configured to prune old tickets from transaction history, do so
	// now.  This step is always skipped on simnet because adjustment times
	// are shorter.
	if pruneTickets && params != &chaincfg.SimNetParams {
		err := walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
			bucket := tx.ReadWriteBucket(wtxmgrNamespaceKey)
			return txMgr.PruneOldTickets(bucket)
		})
		if err != nil {
			return nil, err
		}
	}

	var ticketAddr dcrutil.Address
	if ticketAddress != "" {
		ticketAddr, err = dcrutil.DecodeAddress(ticketAddress, params)
		if err != nil {
			return nil, fmt.Errorf("ticket address could not parse: %v",
				err.Error())
		}
	}

	var poolAddr dcrutil.Address
	if poolAddress != "" {
		poolAddr, err = dcrutil.DecodeAddress(poolAddress, params)
		if err != nil {
			return nil, fmt.Errorf("pool address could not parse: %v",
				err.Error())
		}
	}
	stakePoolColdAddrs, err := decodeStakePoolColdExtKey(stakePoolColdExtKey,
		params)
	if err != nil {
		return nil, err
	}

	ticketFeeAmt, err := dcrutil.NewAmount(ticketFee)
	if err != nil {
		return nil, err
	}

	relayFeeAmt, err := dcrutil.NewAmount(relayFee)
	if err != nil {
		return nil, err
	}

	log.Infof("Opened wallet") // TODO: log balance? last sync height?

	w := newWallet(
		votingEnabled,
		addressReuse,
		ticketAddr,
		poolAddr,
		poolFees,
		relayFeeAmt,
		ticketFeeAmt,
		gapLimit,
		stakePoolColdAddrs,
		allowHighFees,
		addrMgr,
		txMgr,
		smgr,
		&db,
		params,
	)

	return w, nil
}
