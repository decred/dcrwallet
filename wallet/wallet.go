// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/hdkeychain"
	dcrrpcclient "github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/internal/walletdb"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/jrick/bitset"
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
	initiallyUnlocked bool
	gapLimit          int
	accountGapLimit   int

	networkBackend   NetworkBackend
	networkBackendMu sync.Mutex

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

	GapLimit        int
	AccountGapLimit int

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
		if err != nil && errors.Is(errors.NotExist, err) {
			return errors.E(op, errors.NotExist, errors.Errorf("missing tx %v", outPoint.Hash))
		}
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

// StakeDifficulty is used to get the next block's stake difficulty.
func (w *Wallet) StakeDifficulty() (dcrutil.Amount, error) {
	const op errors.Op = "wallet.StakeDifficulty"
	n, err := w.NetworkBackend()
	if err != nil {
		return 0, errors.E(op, err)
	}
	ticketPrice, err := n.StakeDifficulty(context.TODO())
	if err != nil {
		return 0, errors.E(op, err)
	}
	return ticketPrice, nil
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
		return 5
	case wire.TestNet2:
		return 6
	case wire.SimNet:
		return 6
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
	}
}

// WaitForShutdown blocks until all wallet goroutines have finished executing.
func (w *Wallet) WaitForShutdown() {
	w.wg.Wait()
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
func (w *Wallet) loadActiveAddrs(dbtx walletdb.ReadTx, nb NetworkBackend) (uint64, error) {
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
				return nb.LoadTxFilter(context.TODO(), false, addrs, nil)
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
		errs <- nb.LoadTxFilter(context.TODO(), false, addrs, nil)
	}()
	for i := 0; i < cap(errs); i++ {
		err := <-errs
		if err != nil {
			return 0, err
		}
	}

	return bip0044AddrCount + importedAddrCount, nil
}

// LoadActiveDataFilters loads filters for all active addresses and unspent
// outpoints for this wallet.
func (w *Wallet) LoadActiveDataFilters(n NetworkBackend) error {
	const op errors.Op = "wallet.LoadActiveDataFilters"
	log.Infof("Loading active addresses and unspent outputs...")

	var addrCount, utxoCount uint64
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		addrCount, err = w.loadActiveAddrs(dbtx, n)
		if err != nil {
			return err
		}

		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		unspent, err := w.TxStore.UnspentOutpoints(txmgrNs)
		if err != nil {
			return err
		}
		utxoCount = uint64(len(unspent))
		err = n.LoadTxFilter(context.TODO(), false, nil, unspent)
		return err
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

// createHeaderData creates the header data to process from hex-encoded
// serialized block headers.
func createHeaderData(headers [][]byte) ([]udb.BlockHeaderData, error) {
	data := make([]udb.BlockHeaderData, len(headers))
	var decodedHeader wire.BlockHeader
	for i, header := range headers {
		var headerData udb.BlockHeaderData
		copy(headerData.SerializedHeader[:], header)
		err := decodedHeader.Deserialize(bytes.NewReader(header))
		if err != nil {
			return nil, err
		}
		headerData.BlockHash = decodedHeader.BlockHash()
		data[i] = headerData
	}
	return data, nil
}

func (w *Wallet) fetchHeaders(n NetworkBackend) (int, error) {
	fetchedHeaders := 0

	var blockLocators []*chainhash.Hash
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
		headers, err := n.GetHeaders(context.TODO(), blockLocators, &hashStop)
		if err != nil {
			return 0, err
		}

		if len(headers) == 0 {
			return fetchedHeaders, nil
		}

		headerData, err := createHeaderData(headers)
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

		fetchedHeaders += len(headers)
	}
}

// FetchHeaders fetches headers from the consensus RPC server and updates the
// main chain tip with the latest block.  The number of new headers fetched is
// returned, along with the hash of the first previously-unseen block hash now
// in the main chain.  This is the block a rescan should begin at (inclusive),
// and is only relevant when the number of fetched headers is not zero.
func (w *Wallet) FetchHeaders(n NetworkBackend) (count int, rescanFrom chainhash.Hash, rescanFromHeight int32, mainChainTipBlockHash chainhash.Hash, mainChainTipBlockHeight int32, err error) {
	const op errors.Op = "wallet.FetchHeaders"
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
			mainChainHash, err := n.GetBlockHash(context.TODO(), height)
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
		err = errors.E(op, err)
		return
	}

	log.Infof("Fetching headers")
	fetchedHeaderCount, err := w.fetchHeaders(n)
	if err != nil {
		err = errors.E(op, err)
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
			err = errors.E(op, err)
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
			txh, err := w.compressWallet("wallet.Consolidate", txr.inputs,
				txr.account, txr.address)
			heldUnlock.release()
			txr.resp <- consolidateResponse{txh, err}

		case txr := <-w.createTxRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- createTxResponse{nil, err}
				continue
			}
			tx, err := w.txToOutputs("wallet.SendOutputs", txr.outputs,
				txr.account, txr.minconf, true)
			heldUnlock.release()
			txr.resp <- createTxResponse{tx, err}

		case txr := <-w.createMultisigTxRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- createMultisigTxResponse{nil, nil, nil, err}
				continue
			}
			tx, address, redeemScript, err := w.txToMultisig("wallet.CreateMultisigTx",
				txr.account, txr.amount, txr.pubkeys, txr.nrequired, txr.minconf)
			heldUnlock.release()
			txr.resp <- createMultisigTxResponse{tx, address, redeemScript, err}

		case txr := <-w.purchaseTicketRequests:
			heldUnlock, err := w.holdUnlock()
			if err != nil {
				txr.resp <- purchaseTicketResponse{nil, err}
				continue
			}
			data, err := w.purchaseTickets("wallet.PurchaseTickets", txr)
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

// CreateMultisigTx receives a request from the RPC and ships it to txCreator to
// generate a new multisigtx.
func (w *Wallet) CreateMultisigTx(account uint32, amount dcrutil.Amount, pubkeys []*dcrutil.AddressSecpPubKey, nrequired int8, minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {
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

// PurchaseTickets receives a request from the RPC and ships it to txCreator
// to purchase a new ticket. It returns a slice of the hashes of the purchased
// tickets.
func (w *Wallet) PurchaseTickets(minBalance, spendLimit dcrutil.Amount, minConf int32, ticketAddr dcrutil.Address, account uint32, numTickets int, poolAddress dcrutil.Address,
	poolFees float64, expiry int32, txFee dcrutil.Amount, ticketFee dcrutil.Amount) ([]*chainhash.Hash, error) {

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
		case <-timeout:
		}

		// Select statement fell through by an explicit lock or the
		// timer expiring.  Lock the manager here.
		timeout = nil
		err := w.Manager.Lock()
		if err != nil && !errors.Is(errors.Locked, err) {
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
	const op errors.Op = "wallet.Unlock"
	err := make(chan error, 1)
	w.unlockRequests <- unlockRequest{
		passphrase: passphrase,
		lockAfter:  lock,
		err:        err,
	}
	e := <-err
	if e != nil {
		return errors.E(op, e)
	}
	return nil
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
		return nil, errors.E(errors.Locked)
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
	const op errors.Op = "wallet.ChangePrivatePassphrase"
	err := make(chan error, 1)
	w.changePassphrase <- changePassphraseRequest{
		old: old,
		new: new,
		err: err,
	}
	e := <-err
	if e != nil {
		return errors.E(op, e)
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
	addr, err := child.Address(w.chainParams)
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
func VerifyMessage(msg string, addr dcrutil.Address, sig []byte) (bool, error) {
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
	recoveredAddr, err := dcrutil.NewAddressSecpPubKey(serializedPK, addr.Net())
	if err != nil {
		return false, errors.E(op, err)
	}

	// Return whether addresses match.
	return recoveredAddr.EncodeAddress() == addr.EncodeAddress(), nil
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
	extKey, err := hdkeychain.NewKeyFromString(masterPubKey)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return extKey, nil
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
func listTransactions(tx walletdb.ReadTx, details *udb.TxDetails, addrMgr *udb.Manager, syncHeight int32, net *chaincfg.Params) (sends, receives []dcrjson.ListTransactionsResult) {
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
func (w *Wallet) ListSinceBlock(start, end, syncHeight int32) ([]dcrjson.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListSinceBlock"
	txList := []dcrjson.ListTransactionsResult{}
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
func (w *Wallet) ListTransactions(from, count int) ([]dcrjson.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListTransactions"
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
	for i, j := 0, len(txList)-1; i < j; {
		txList[i], txList[j] = txList[j], txList[i]
		i++
		j--
	}
	return txList, nil
}

// ListAddressTransactions returns a slice of objects with details about
// recorded transactions to or from any address belonging to a set.  This is
// intended to be used for listaddresstransactions RPC replies.
func (w *Wallet) ListAddressTransactions(pkHashes map[string]struct{}) ([]dcrjson.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListAddressTransactions"
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

					sends, receives := listTransactions(tx, detail,
						w.Manager, tipHeight, w.chainParams)
					if err != nil {
						return false, err
					}
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
func (w *Wallet) ListAllTransactions() ([]dcrjson.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListAllTransactions"
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
	for i, j := 0, len(txList)-1; i < j; {
		txList[i], txList[j] = txList[j], txList[i]
		i++
		j--
	}
	return txList, nil
}

// ListTransactionDetails returns the listtransaction results for a single
// transaction.
func (w *Wallet) ListTransactionDetails(txHash *chainhash.Hash) ([]dcrjson.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListTransactionDetails"
	txList := []dcrjson.ListTransactionsResult{}
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
		txList = make([]dcrjson.ListTransactionsResult, 0, len(sends)+len(receives))
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
	const op errors.Op = "wallet.TransactionSummary"
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
			blockHash = &txDetails.Hash
		}
		return nil
	})
	if err != nil {
		return nil, 0, nil, errors.E(op, err)
	}
	return txSummary, confs, blockHash, nil
}

// GetTicketsResult response struct for gettickets rpc request
type GetTicketsResult struct {
	Tickets []*TicketSummary
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
func (w *Wallet) GetTickets(f func([]*TicketSummary, *wire.BlockHeader) (bool, error), chainClient *dcrrpcclient.Client, startBlock, endBlock *BlockIdentifier) error {
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
				tickets = append(tickets, makeTicketSummary(chainClient, dbtx, w, ticketInfo))
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
		errors.E(op, err)
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
func (w *Wallet) ListUnspent(minconf, maxconf int32, addresses map[string]struct{}) ([]*dcrjson.ListUnspentResult, error) {
	const op errors.Op = "wallet.ListUnspent"
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
				return errors.Errorf("Couldn't get credit details")
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
					if errors.Is(errors.NotExist, err) {
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
	wif, err := dcrutil.NewWIF(privKey, w.chainParams, privKey.GetType())
	if err != nil {
		return "", errors.E(op, err)
	}
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

// RedeemScriptCopy returns a copy of a redeem script to redeem outputs payed to
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

func isTicketPurchase(tx *wire.MsgTx) bool {
	return stake.IsSStx(tx)
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
//     Missed           uint32   Number of missed tickets (failing to vote)
//     Expired          uint32   Number of expired tickets
//     Revoked          uint32   Number of missed tickets that were missed and
//                                 then revoked
//     TotalSubsidy     int64    Total amount of coins earned by stake mining
//
// Getting this information is extremely costly as in involves a massive
// number of chain server calls.
func (w *Wallet) StakeInfo(chainClient *dcrrpcclient.Client) (*StakeInfoData, error) {
	const op errors.Op = "wallet.StakeInfo"
	// This is only needed for the total count and can be optimized.
	mempoolTicketsFuture := chainClient.GetRawMempoolAsync(dcrjson.GRMTickets)

	res := &StakeInfoData{}

	// Wallet does not yet know if/when a ticket was selected.  Keep track of
	// all tickets that are either live, expired, or missed and determine their
	// states later by querying the consensus RPC server.
	var liveOrExpiredOrMissed []*chainhash.Hash

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		tipHash, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		res.BlockHeight = int64(tipHeight)
		it := w.TxStore.IterateTickets(dbtx)
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
			if !confirmed(int32(w.chainParams.TicketMaturity)+1,
				it.Block.Height, tipHeight) {
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
					// Similarily, for stakepool wallets, this includes the
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
	expiredFuture := chainClient.ExistsExpiredTicketsAsync(liveOrExpiredOrMissed)
	liveFuture := chainClient.ExistsLiveTicketsAsync(liveOrExpiredOrMissed)
	expiredBitsetHex, err := expiredFuture.Receive()
	if err != nil {
		return nil, errors.E(op, err)
	}
	liveBitsetHex, err := liveFuture.Receive()
	if err != nil {
		return nil, errors.E(op, err)
	}
	expiredBitset, err := hex.DecodeString(expiredBitsetHex)
	if err != nil {
		return nil, errors.E(op, err)
	}
	liveBitset, err := hex.DecodeString(liveBitsetHex)
	if err != nil {
		return nil, errors.E(op, err)
	}
	for i := range liveOrExpiredOrMissed {
		switch {
		case bitset.Bytes(liveBitset).Get(i):
			res.Live++
		case bitset.Bytes(expiredBitset).Get(i):
			res.Expired++
		default:
			res.Missed++
		}
	}

	// Receive the mempool tickets future called at the beginning of the
	// function and determine the total count of tickets in the mempool.
	mempoolTickets, err := mempoolTicketsFuture.Receive()
	if err != nil {
		return nil, errors.E(op, err)
	}
	res.AllMempoolTix = uint32(len(mempoolTickets))

	return res, nil
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
			addrStrs = append(addrStrs, addr.EncodeAddress())
			return nil
		})
	})
	if err != nil {
		return nil, errors.E(op, err)
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

	req := createTxRequest{
		account: account,
		outputs: outputs,
		minconf: minconf,
		resp:    make(chan createTxResponse),
	}
	w.createTxRequests <- req
	resp := <-req.resp
	if resp.err != nil {
		return nil, resp.err
	}

	hash := resp.tx.Tx.TxHash()
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
				if err != nil {
					return errors.Errorf("Cannot query previous transaction "+
						"details for %v: %v", txIn.PreviousOutPoint, err)
				}
				if txDetails == nil {
					return errors.Errorf("%v not found",
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
							errors.Errorf("no key for address (needed: %v, have %v)",
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
					return nil, false, errors.Errorf("address %v is not "+
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
						Error:      errors.E(op, err),
					})
					continue
				}
				txIn.SignatureScript = script
			}

			// Either it was already signed or we just signed it.
			// Find out if it is completely satisfied or still needs more.
			vm, err := txscript.NewEngine(prevOutScript, tx, i,
				sanityVerifyFlags, txscript.DefaultScriptVersion, nil)
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
		rs, err := txscript.MultisigRedeemScriptFromScriptSig(in.SignatureScript)
		if err == nil && rs != nil && w.Manager.ExistsHash160(addrmgrNs,
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

// PublishTransaction saves (if relevant) and sends the transaction to the
// consensus RPC server so it can be propagated to other nodes and eventually
// mined.  If the send fails, the transaction is not added to the wallet.
func (w *Wallet) PublishTransaction(tx *wire.MsgTx, serializedTx []byte, n NetworkBackend) (*chainhash.Hash, error) {
	const op errors.Op = "wallet.PublishTransaction"
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
		return nil, errors.E(op, err)
	}

	if !relevant {
		err := n.PublishTransaction(context.TODO(), tx)
		if err != nil {
			return nil, err
		}
		txHash := tx.TxHash()
		return &txHash, nil
	}

	var txHash *chainhash.Hash
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		rec, err := udb.NewTxRecord(serializedTx, time.Now())
		if err != nil {
			return err
		}
		err = w.processTransaction(dbtx, rec, nil, nil)
		if err != nil {
			return err
		}
		err = n.PublishTransaction(context.TODO(), tx)
		if err != nil {
			return err
		}
		h := tx.TxHash()
		txHash = &h
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return txHash, nil
}

// PublishUnminedTransactions rebroadcasts all unmined transactions
// to the consensus RPC server so it can be propagated to other nodes
// and eventually mined.
func (w *Wallet) PublishUnminedTransactions(ctx context.Context, backend NetworkBackend) error {
	const op errors.Op = "wallet.PublishUnminedTransactions"
	unminedTxs, err := w.UnminedTransactions()
	if err != nil {
		log.Errorf("Cannot load unmined transactions for resending: %v", err)
		return errors.E(op, err)
	}
	for _, tx := range unminedTxs {
		txHash := tx.TxHash()
		err := backend.PublishTransaction(ctx, tx)
		if err != nil {
			// TODO: Transactions should be removed if this is a double spend.
			log.Tracef("Could not resend transaction %v: %v", &txHash, err)
			continue
		}
		log.Tracef("Resent unmined transaction %v", &txHash)
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
	key, err := hdkeychain.NewKeyFromString(splStrs[0])
	if err != nil {
		return nil, err
	}
	if !key.IsForNet(params) {
		return nil, errors.Errorf("extended public key is for wrong network")
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
		addrMap[addrs[i].EncodeAddress()] = struct{}{}
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
	err = udb.Upgrade(db, cfg.PubPassphrase)
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
		gapLimit:        cfg.GapLimit,
		AllowHighFees:   cfg.AllowHighFees,
		accountGapLimit: cfg.AccountGapLimit,

		// Chain params
		subsidyCache: blockchain.NewSubsidyCache(0, cfg.Params),
		chainParams:  cfg.Params,

		lockedOutpoints: map[wire.OutPoint]struct{}{},

		initiallyUnlocked: false,

		consolidateRequests:      make(chan consolidateRequest),
		createTxRequests:         make(chan createTxRequest),
		createMultisigTxRequests: make(chan createMultisigTxRequest),
		purchaseTicketRequests:   make(chan purchaseTicketRequest),
		addressBuffers:           make(map[uint32]*bip0044AccountData),
		unlockRequests:           make(chan unlockRequest),
		lockRequests:             make(chan struct{}),
		holdUnlockRequests:       make(chan chan heldUnlock),
		lockState:                make(chan bool),
		changePassphrase:         make(chan changePassphraseRequest),
		quit:                     make(chan struct{}),
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
