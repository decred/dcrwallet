// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrwallet/v2/deployments"
	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/internal/compat"
	"decred.org/dcrwallet/v2/rpc/client/dcrd"
	"decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v2/validate"
	"decred.org/dcrwallet/v2/wallet/txrules"
	"decred.org/dcrwallet/v2/wallet/txsizes"
	"decred.org/dcrwallet/v2/wallet/udb"
	"decred.org/dcrwallet/v2/wallet/walletdb"

	"github.com/decred/dcrd/blockchain/stake/v4"
	blockchain "github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/dcrutil/v4"
	gcs2 "github.com/decred/dcrd/gcs/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
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

// The assumed output script version is defined to assist with refactoring to
// use actual script versions.
const scriptVersionAssumed = 0

// StakeDifficultyInfo is a container for stake difficulty information updates.
type StakeDifficultyInfo struct {
	BlockHash       *chainhash.Hash
	BlockHeight     int64
	StakeDifficulty int64
}

// outpoint is a wire.OutPoint without specifying a tree.  It is used as the key
// for the lockedOutpoints map.
type outpoint struct {
	hash  chainhash.Hash
	index uint32
}

// Wallet is a structure containing all the components for a
// complete wallet.  It contains the Armory-style key store
// addresses and keys),
type Wallet struct {
	// disapprovePercent is an atomic. It sets the percentage of blocks to
	// disapprove on simnet or testnet.
	disapprovePercent uint32

	// Data stores
	db       walletdb.DB
	manager  *udb.Manager
	txStore  *udb.Store
	stakeMgr *udb.StakeStore

	// Handlers for stake system.
	stakeSettingsLock  sync.Mutex
	defaultVoteBits    stake.VoteBits
	votingEnabled      bool
	poolAddress        stdaddr.StakeAddress
	poolFees           float64
	manualTickets      bool
	stakePoolEnabled   bool
	stakePoolColdAddrs map[string]struct{}
	subsidyCache       *blockchain.SubsidyCache
	tspends            map[chainhash.Hash]wire.MsgTx
	tspendPolicy       map[chainhash.Hash]stake.TreasuryVoteT
	tspendKeyPolicy    map[string]stake.TreasuryVoteT // keyed by politeia key
	vspTSpendPolicy    map[udb.VSPTSpend]stake.TreasuryVoteT
	vspTSpendKeyPolicy map[udb.VSPTreasuryKey]stake.TreasuryVoteT

	// Start up flags/settings
	gapLimit        uint32
	accountGapLimit int

	// initialHeight is the wallet's tip height prior to syncing with the
	// network. Useful for calculating or estimating headers fetch progress
	// during sync if the target header height is known or can be estimated.
	initialHeight int32

	networkBackend   NetworkBackend
	networkBackendMu sync.Mutex

	lockedOutpoints  map[outpoint]struct{}
	lockedOutpointMu sync.Mutex

	relayFee                dcrutil.Amount
	relayFeeMu              sync.Mutex
	allowHighFees           bool
	disableCoinTypeUpgrades bool
	recentlyPublished       map[chainhash.Hash]struct{}
	recentlyPublishedMu     sync.Mutex

	// Internal address handling.
	addressReuse     bool
	ticketAddress    stdaddr.StakeAddress
	addressBuffers   map[uint32]*bip0044AccountData
	addressBuffersMu sync.Mutex

	// Passphrase unlock
	passphraseUsedMu        sync.RWMutex
	passphraseTimeoutMu     sync.Mutex
	passphraseTimeoutCancel chan struct{}

	// Mix rate limiting
	mixSems mixSemaphores

	NtfnServer *NotificationServer

	chainParams *chaincfg.Params
}

// Config represents the configuration options needed to initialize a wallet.
type Config struct {
	DB DB

	PubPassphrase []byte

	VotingEnabled bool
	AddressReuse  bool
	VotingAddress stdaddr.StakeAddress
	PoolAddress   stdaddr.StakeAddress
	PoolFees      float64

	GapLimit                uint32
	AccountGapLimit         int
	MixSplitLimit           int
	DisableCoinTypeUpgrades bool

	StakePoolColdExtKey string
	ManualTickets       bool
	AllowHighFees       bool
	RelayFee            dcrutil.Amount
	Params              *chaincfg.Params
}

// DisapprovePercent returns the wallet's block disapproval percentage.
func (w *Wallet) DisapprovePercent() uint32 {
	return atomic.LoadUint32(&w.disapprovePercent)
}

// SetDisapprovePercent sets the wallet's block disapproval percentage. Do not
// set on mainnet.
func (w *Wallet) SetDisapprovePercent(percent uint32) {
	atomic.StoreUint32(&w.disapprovePercent, percent)
}

// FetchOutput fetches the associated transaction output given an outpoint.
// It cannot be used to fetch multi-signature outputs.
func (w *Wallet) FetchOutput(ctx context.Context, outPoint *wire.OutPoint) (*wire.TxOut, error) {
	const op errors.Op = "wallet.FetchOutput"

	var out *wire.TxOut
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		outTx, err := w.txStore.Tx(txmgrNs, &outPoint.Hash)
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

// VotingEnabled returns whether the wallet is configured to vote tickets.
func (w *Wallet) VotingEnabled() bool {
	w.stakeSettingsLock.Lock()
	enabled := w.votingEnabled
	w.stakeSettingsLock.Unlock()
	return enabled
}

// AddTSpend adds a tspend to the cache.
func (w *Wallet) AddTSpend(tx wire.MsgTx) error {
	hash := tx.TxHash()
	log.Infof("TSpend arrived: %v", hash)
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	if _, ok := w.tspends[hash]; ok {
		return fmt.Errorf("tspend already cached")
	}
	w.tspends[hash] = tx
	return nil
}

// GetAllTSpends returns all tspends currently in the cache.
// Note: currently the tspend list does not get culled.
func (w *Wallet) GetAllTSpends(ctx context.Context) []*wire.MsgTx {
	_, height := w.MainChainTip(ctx)
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	txs := make([]*wire.MsgTx, 0, len(w.tspends))
	for k := range w.tspends {
		v := w.tspends[k]
		if uint32(height) > v.Expiry {
			delete(w.tspends, k)
			continue
		}
		txs = append(txs, &v)
	}
	return txs
}

func voteVersion(params *chaincfg.Params) uint32 {
	switch params.Net {
	case wire.MainNet:
		return 9
	case 0x48e7a065: // TestNet2
		return 6
	case wire.TestNet3:
		return 10
	case wire.SimNet:
		return 10
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
		choiceID := udb.DefaultAgendaPreference(dbtx, version, d.Vote.Id)
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

func (w *Wallet) readDBTicketVoteBits(dbtx walletdb.ReadTx, ticketHash *chainhash.Hash) (stake.VoteBits, bool) {
	version, deployments := CurrentAgendas(w.chainParams)
	tvb := stake.VoteBits{
		Bits:         0x0001,
		ExtendedBits: make([]byte, 4),
	}
	binary.LittleEndian.PutUint32(tvb.ExtendedBits, version)

	if len(deployments) == 0 {
		return tvb, false
	}

	var hasSavedPrefs bool
	for i := range deployments {
		d := &deployments[i]
		choiceID := udb.TicketAgendaPreference(dbtx, ticketHash, version, d.Vote.Id)
		if choiceID == "" {
			continue
		}
		hasSavedPrefs = true
		for j := range d.Vote.Choices {
			choice := &d.Vote.Choices[j]
			if choiceID == choice.Id {
				tvb.Bits |= choice.Bits
				break
			}
		}
	}
	return tvb, hasSavedPrefs
}

func (w *Wallet) readDBTreasuryPolicies(dbtx walletdb.ReadTx) (
	map[chainhash.Hash]stake.TreasuryVoteT, map[udb.VSPTSpend]stake.TreasuryVoteT, error) {
	a, err := udb.TSpendPolicies(dbtx)
	if err != nil {
		return nil, nil, err
	}
	b, err := udb.VSPTSpendPolicies(dbtx)
	return a, b, err
}

func (w *Wallet) readDBTreasuryKeyPolicies(dbtx walletdb.ReadTx) (
	map[string]stake.TreasuryVoteT, map[udb.VSPTreasuryKey]stake.TreasuryVoteT, error) {
	a, err := udb.TreasuryKeyPolicies(dbtx)
	if err != nil {
		return nil, nil, err
	}
	b, err := udb.VSPTreasuryKeyPolicies(dbtx)
	return a, b, err
}

// VoteBits returns the vote bits that are described by the currently set agenda
// preferences.  The previous block valid bit is always set, and must be unset
// elsewhere if the previous block's regular transactions should be voted
// against.
func (w *Wallet) VoteBits() stake.VoteBits {
	w.stakeSettingsLock.Lock()
	vb := w.defaultVoteBits
	w.stakeSettingsLock.Unlock()
	return vb
}

// AgendaChoice describes a user's choice for a consensus deployment agenda.
type AgendaChoice struct {
	AgendaID string
	ChoiceID string
}

// AgendaChoices returns the choice IDs for every agenda of the supported stake
// version.  Abstains are included.  Returns choice IDs set for the specified
// non-nil ticket hash, or the default choice IDs if the ticket hash is nil or
// there are no choices set for the ticket.
func (w *Wallet) AgendaChoices(ctx context.Context, ticketHash *chainhash.Hash) (choices []AgendaChoice, voteBits uint16, err error) {
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

	var ownTicket bool
	var hasSavedPrefs bool

	voteBits = 1
	err = walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		if ticketHash != nil {
			ownTicket = w.txStore.OwnTicket(tx, ticketHash)
			ownTicket = ownTicket || w.stakeMgr.OwnTicket(ticketHash)
			if !ownTicket {
				return nil
			}
		}

		for i := range deployments {
			agenda := &deployments[i].Vote
			var choice string
			if ticketHash == nil {
				choice = udb.DefaultAgendaPreference(tx, version, agenda.Id)
			} else {
				choice = udb.TicketAgendaPreference(tx, ticketHash, version, agenda.Id)
			}
			if choice == "" {
				continue
			}
			hasSavedPrefs = true
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
	if ticketHash != nil && !ownTicket {
		return nil, 0, errors.E(errors.NotExist, "ticket not found")
	}
	if ticketHash != nil && !hasSavedPrefs {
		// no choices set for ticket hash, return default choices.
		return w.AgendaChoices(ctx, nil)
	}
	return choices, voteBits, nil
}

// SetAgendaChoices sets the choices for agendas defined by the supported stake
// version.  If a choice is set multiple times, the last takes preference.  The
// new votebits after each change is made are returned.
// If a ticketHash is provided, agenda choices are only set for that ticket and
// the new votebits for that ticket is returned.
func (w *Wallet) SetAgendaChoices(ctx context.Context, ticketHash *chainhash.Hash, choices ...AgendaChoice) (voteBits uint16, err error) {
	const op errors.Op = "wallet.SetAgendaChoices"
	version, deployments := CurrentAgendas(w.chainParams)
	if len(deployments) == 0 {
		return 0, errors.E("no agendas to set for this network")
	}

	if ticketHash != nil {
		// validate ticket ownership
		var ownTicket bool
		err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
			ownTicket = w.txStore.OwnTicket(dbtx, ticketHash) || w.stakeMgr.OwnTicket(ticketHash)
			return nil
		})
		if err != nil {
			return 0, errors.E(op, err)
		}
		if !ownTicket {
			return 0, errors.E(errors.NotExist, "ticket not found")
		}
	}

	type maskChoice struct {
		mask uint16
		bits uint16
	}
	var appliedChoices []maskChoice

	err = walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
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

			var err error
			if ticketHash == nil {
				err = udb.SetDefaultAgendaPreference(tx, version, c.AgendaID, c.ChoiceID)
			} else {
				err = udb.SetTicketAgendaPreference(tx, ticketHash, version, c.AgendaID, c.ChoiceID)
			}
			if err != nil {
				return err
			}
			appliedChoices = append(appliedChoices, maskChoice{
				mask: matchingAgenda.Mask,
				bits: matchingChoice.Bits,
			})
			if ticketHash != nil {
				// No need to check that this ticket has prefs set,
				// we just saved the per-ticket vote bits.
				ticketVoteBits, _ := w.readDBTicketVoteBits(tx, ticketHash)
				voteBits = ticketVoteBits.Bits
			}
		}
		return nil
	})
	if err != nil {
		return 0, errors.E(op, err)
	}

	// With the DB update successful, modify the default votebits cached by the
	// wallet structure. Per-ticket votebits are not cached.
	if ticketHash == nil {
		w.stakeSettingsLock.Lock()
		for _, c := range appliedChoices {
			w.defaultVoteBits.Bits &^= c.mask // Clear all bits from this agenda
			w.defaultVoteBits.Bits |= c.bits  // Set bits for this choice
		}
		voteBits = w.defaultVoteBits.Bits
		w.stakeSettingsLock.Unlock()
	}

	return voteBits, nil
}

// TreasuryKeyPolicyForTicket returns all of the treasury key policies set for a
// single ticket. It does not consider the global wallet setting.
func (w *Wallet) TreasuryKeyPolicyForTicket(ticketHash *chainhash.Hash) map[string]string {
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	policies := make(map[string]string)
	for key, value := range w.vspTSpendKeyPolicy {
		if key.Ticket.IsEqual(ticketHash) {
			var choice string
			switch value {
			case stake.TreasuryVoteYes:
				choice = "yes"
			case stake.TreasuryVoteNo:
				choice = "no"
			default:
				choice = "abstain"
			}
			policies[key.TreasuryKey] = choice
		}
	}
	return policies
}

// TreasuryKeyPolicy returns a vote policy for provided Pi key. If there is
// no policy this method returns TreasuryVoteInvalid.
// A non-nil ticket hash may be used by a VSP to return per-ticket policies.
func (w *Wallet) TreasuryKeyPolicy(pikey []byte, ticket *chainhash.Hash) stake.TreasuryVoteT {
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	// Zero value is abstain/invalid, just return as is.
	if ticket != nil {
		return w.vspTSpendKeyPolicy[udb.VSPTreasuryKey{
			Ticket:      *ticket,
			TreasuryKey: string(pikey),
		}]
	}
	return w.tspendKeyPolicy[string(pikey)]
}

// TSpendPolicyForTicket returns all of the tspend policies set for a single
// ticket. It does not consider the global wallet setting.
func (w *Wallet) TSpendPolicyForTicket(ticketHash *chainhash.Hash) map[string]string {
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	policies := make(map[string]string)
	for key, value := range w.vspTSpendPolicy {
		if key.Ticket.IsEqual(ticketHash) {
			var choice string
			switch value {
			case stake.TreasuryVoteYes:
				choice = "yes"
			case stake.TreasuryVoteNo:
				choice = "no"
			default:
				choice = "abstain"
			}
			policies[key.TSpend.String()] = choice
		}
	}
	return policies
}

// TSpendPolicy returns a vote policy for a tspend.  If a policy is set for a
// particular tspend transaction, that policy is returned.  Otherwise, if the
// tspend is known, any policy for the treasury key which signs the tspend is
// returned.
// A non-nil ticket hash may be used by a VSP to return per-ticket policies.
func (w *Wallet) TSpendPolicy(tspendHash, ticketHash *chainhash.Hash) stake.TreasuryVoteT {
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	// Policy preferences for specific tspends override key policies.
	if ticketHash != nil {
		policy, ok := w.vspTSpendPolicy[udb.VSPTSpend{
			Ticket: *ticketHash,
			TSpend: *tspendHash,
		}]
		if ok {
			return policy
		}
	}
	if policy, ok := w.tspendPolicy[*tspendHash]; ok {
		return policy
	}

	// If this tspend is known, the pi key can be extracted from it and its
	// policy is returned.
	tspend, ok := w.tspends[*tspendHash]
	if !ok {
		return 0 // invalid/abstain
	}
	pikey := tspend.TxIn[0].SignatureScript[66 : 66+secp256k1.PubKeyBytesLenCompressed]

	// Zero value means abstain, just return as is.
	if ticketHash != nil {
		policy, ok := w.vspTSpendKeyPolicy[udb.VSPTreasuryKey{
			Ticket:      *ticketHash,
			TreasuryKey: string(pikey),
		}]
		if ok {
			return policy
		}
	}
	return w.tspendKeyPolicy[string(pikey)]
}

// TreasuryKeyPolicy records the voting policy for treasury spend transactions
// by a particular key, and possibly for a particular ticket being voted on by a
// VSP.
type TreasuryKeyPolicy struct {
	PiKey  []byte
	Ticket *chainhash.Hash // nil unless for per-ticket VSP policies
	Policy stake.TreasuryVoteT
}

// TreasuryKeyPolicies returns all configured policies for treasury keys.
func (w *Wallet) TreasuryKeyPolicies() []TreasuryKeyPolicy {
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	policies := make([]TreasuryKeyPolicy, 0, len(w.tspendKeyPolicy))
	for pikey, policy := range w.tspendKeyPolicy {
		policies = append(policies, TreasuryKeyPolicy{
			PiKey:  []byte(pikey),
			Policy: policy,
		})
	}
	for tuple, policy := range w.vspTSpendKeyPolicy {
		ticketHash := tuple.Ticket // copy
		pikey := []byte(tuple.TreasuryKey)
		policies = append(policies, TreasuryKeyPolicy{
			PiKey:  pikey,
			Ticket: &ticketHash,
			Policy: policy,
		})
	}
	return policies
}

// SetTreasuryKeyPolicy sets a tspend vote policy for a specific Politeia
// instance key.
// A non-nil ticket hash may be used by a VSP to set per-ticket policies.
func (w *Wallet) SetTreasuryKeyPolicy(ctx context.Context, pikey []byte,
	policy stake.TreasuryVoteT, ticketHash *chainhash.Hash) error {

	switch policy {
	case stake.TreasuryVoteInvalid, stake.TreasuryVoteNo, stake.TreasuryVoteYes:
	default:
		err := errors.Errorf("invalid treasury vote policy %#x", policy)
		return errors.E(errors.Invalid, err)
	}

	defer w.stakeSettingsLock.Unlock()
	w.stakeSettingsLock.Lock()

	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		if ticketHash != nil {
			return udb.SetVSPTreasuryKeyPolicy(dbtx, ticketHash,
				pikey, policy)
		}
		return udb.SetTreasuryKeyPolicy(dbtx, pikey, policy)
	})
	if err != nil {
		return err
	}

	if ticketHash != nil {
		k := udb.VSPTreasuryKey{
			Ticket:      *ticketHash,
			TreasuryKey: string(pikey),
		}
		if policy == stake.TreasuryVoteInvalid {
			delete(w.vspTSpendKeyPolicy, k)
			return nil
		}

		w.vspTSpendKeyPolicy[k] = policy
		return nil
	}

	if policy == stake.TreasuryVoteInvalid {
		delete(w.tspendKeyPolicy, string(pikey))
		return nil
	}

	w.tspendKeyPolicy[string(pikey)] = policy
	return nil
}

// SetTSpendPolicy sets a tspend vote policy for a specific tspend transaction
// hash.
// A non-nil ticket hash may be used by a VSP to set per-ticket policies.
func (w *Wallet) SetTSpendPolicy(ctx context.Context, tspendHash *chainhash.Hash,
	policy stake.TreasuryVoteT, ticketHash *chainhash.Hash) error {

	switch policy {
	case stake.TreasuryVoteInvalid, stake.TreasuryVoteNo, stake.TreasuryVoteYes:
	default:
		err := errors.Errorf("invalid treasury vote policy %#x", policy)
		return errors.E(errors.Invalid, err)
	}

	defer w.stakeSettingsLock.Unlock()
	w.stakeSettingsLock.Lock()

	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		if ticketHash != nil {
			return udb.SetVSPTSpendPolicy(dbtx, ticketHash,
				tspendHash, policy)
		}
		return udb.SetTSpendPolicy(dbtx, tspendHash, policy)
	})
	if err != nil {
		return err
	}

	if ticketHash != nil {
		k := udb.VSPTSpend{
			Ticket: *ticketHash,
			TSpend: *tspendHash,
		}
		if policy == stake.TreasuryVoteInvalid {
			delete(w.vspTSpendPolicy, k)
			return nil
		}

		w.vspTSpendPolicy[k] = policy
		return nil
	}

	if policy == stake.TreasuryVoteInvalid {
		delete(w.tspendPolicy, *tspendHash)
		return nil
	}

	w.tspendPolicy[*tspendHash] = policy
	return nil
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

// InitialHeight is the wallet's tip height prior to syncing with the network.
func (w *Wallet) InitialHeight() int32 {
	return w.initialHeight
}

// MainChainTip returns the hash and height of the tip-most block in the main
// chain that the wallet is synchronized to.
func (w *Wallet) MainChainTip(ctx context.Context) (hash chainhash.Hash, height int32) {
	// TODO: after the main chain tip is successfully updated in the db, it
	// should be saved in memory.  This will speed up access to it, and means
	// there won't need to be an ignored error here for ergonomic access to the
	// hash and height.
	walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		hash, height = w.txStore.MainChainTip(dbtx)
		return nil
	})
	return
}

// BlockInMainChain returns whether hash is a block hash of any block in the
// wallet's main chain.  If the block is in the main chain, invalidated reports
// whether a child block in the main chain stake invalidates the queried block.
func (w *Wallet) BlockInMainChain(ctx context.Context, hash *chainhash.Hash) (haveBlock, invalidated bool, err error) {
	const op errors.Op = "wallet.BlockInMainChain"
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		haveBlock, invalidated = w.txStore.BlockInMainChain(dbtx, hash)
		return nil
	})
	if err != nil {
		return false, false, errors.E(op, err)
	}
	return haveBlock, invalidated, nil
}

// BlockHeader returns the block header for a block by it's identifying hash, if
// it is recorded by the wallet.
func (w *Wallet) BlockHeader(ctx context.Context, blockHash *chainhash.Hash) (*wire.BlockHeader, error) {
	var header *wire.BlockHeader
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		header, err = w.txStore.GetBlockHeader(dbtx, blockHash)
		return err
	})
	return header, err
}

// CFilterV2 returns the version 2 regular compact filter for a block along
// with the key required to query it for matches against committed scripts.
func (w *Wallet) CFilterV2(ctx context.Context, blockHash *chainhash.Hash) ([gcs2.KeySize]byte, *gcs2.FilterV2, error) {
	var f *gcs2.FilterV2
	var key [gcs2.KeySize]byte
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		key, f, err = w.txStore.CFilterV2(dbtx, blockHash)
		return err
	})
	return key, f, err
}

// RangeCFiltersV2 calls the function `f` for the set of version 2 committed
// filters for the main chain within the specificed block range.
//
// The default behavior for an unspecified range is to loop over the entire
// main chain.
//
// The function `f` may return true for the first argument to indicate no more
// items should be fetched. Any returned errors by `f` also cause the loop to
// fail.
//
// Note that the filter passed to `f` is safe for use after `f` returns.
func (w *Wallet) RangeCFiltersV2(ctx context.Context, startBlock, endBlock *BlockIdentifier, f func(chainhash.Hash, [gcs2.KeySize]byte, *gcs2.FilterV2) (bool, error)) error {
	const op errors.Op = "wallet.RangeCFiltersV2"

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		start, end, err := w.blockRange(dbtx, startBlock, endBlock)
		if err != nil {
			return err
		}

		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		rangeFn := func(block *udb.Block) (bool, error) {
			key, filter, err := w.txStore.CFilterV2(dbtx, &block.Hash)
			if err != nil {
				return false, err
			}

			return f(block.Hash, key, filter)
		}

		return w.txStore.RangeBlocks(txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// watchHDAddrs loads the network backend's transaction filter with all HD
// addresses for transaction notifications.
//
// This method does nothing if the wallet's rescan point is behind the main
// chain tip block and firstWatch is false.  That is, it does not watch any
// addresses if the wallet's transactions are not synced with the best known
// block.  There is no reason to watch addresses if there is a known possibility
// of not having all relevant transactions.
func (w *Wallet) watchHDAddrs(ctx context.Context, firstWatch bool, n NetworkBackend) (count uint64, err error) {
	if !firstWatch {
		rp, err := w.RescanPoint(ctx)
		if err != nil {
			return 0, err
		}
		if rp != nil {
			return 0, nil
		}
	}

	// Read branch keys and child counts for all derived and imported
	// HD accounts.
	type hdAccount struct {
		externalKey, internalKey                   *hdkeychain.ExtendedKey
		externalCount, internalCount               uint32
		lastWatchedExternal, lastWatchedInternal   uint32
		lastReturnedExternal, lastReturnedInternal uint32
		lastUsedExternal, lastUsedInternal         uint32
	}
	hdAccounts := make(map[uint32]hdAccount)
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

		var err error
		lastAcct, err := w.manager.LastAccount(addrmgrNs)
		if err != nil {
			return err
		}
		lastImportedAcct, err := w.manager.LastImportedAccount(dbtx)
		if err != nil {
			return err
		}

		loadAccount := func(acct uint32) error {
			props, err := w.manager.AccountProperties(addrmgrNs, acct)
			if err != nil {
				return err
			}
			hdAccounts[acct] = hdAccount{
				externalCount:        minUint32(props.LastReturnedExternalIndex+w.gapLimit, hdkeychain.HardenedKeyStart-1),
				internalCount:        minUint32(props.LastReturnedInternalIndex+w.gapLimit, hdkeychain.HardenedKeyStart-1),
				lastReturnedExternal: props.LastReturnedExternalIndex,
				lastReturnedInternal: props.LastReturnedInternalIndex,
				lastUsedExternal:     props.LastUsedExternalIndex,
				lastUsedInternal:     props.LastUsedInternalIndex,
			}
			return nil
		}
		for acct := uint32(0); acct <= lastAcct; acct++ {
			err := loadAccount(acct)
			if err != nil {
				return err
			}
		}
		for acct := uint32(udb.ImportedAddrAccount + 1); acct <= lastImportedAcct; acct++ {
			err := loadAccount(acct)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	w.addressBuffersMu.Lock()
	for acct, ad := range w.addressBuffers {
		hd := hdAccounts[acct]

		// Update the in-memory address tracking with the latest last
		// used index retreived from the db.
		// Because the cursor may be advanced ahead of what the database
		// would otherwise record as the last returned address, due to
		// delayed db updates during some operations, a delta is
		// calculated between the in-memory and db last returned
		// indexes, and this delta is added back to the new cursor.
		//
		// This is calculated as:
		//   delta := ad.albExternal.lastUsed + ad.albExternal.cursor - hd.lastReturnedExternal
		//   ad.albExternal.cursor = hd.lastReturnedExternal - hd.lastUsedExternal + delta
		// which simplifies to the calculation below.  An additional clamp
		// is added to prevent the cursors from going negative.
		if hd.lastUsedExternal+1 > ad.albExternal.lastUsed+1 {
			ad.albExternal.cursor += ad.albExternal.lastUsed - hd.lastUsedExternal
			if ad.albExternal.cursor > ^uint32(0)>>1 {
				ad.albExternal.cursor = 0
			}
			ad.albExternal.lastUsed = hd.lastUsedExternal
		}
		if hd.lastUsedInternal+1 > ad.albInternal.lastUsed+1 {
			ad.albInternal.cursor += ad.albInternal.lastUsed - hd.lastUsedInternal
			if ad.albInternal.cursor > ^uint32(0)>>1 {
				ad.albInternal.cursor = 0
			}
			ad.albInternal.lastUsed = hd.lastUsedInternal
		}

		hd.externalKey = ad.albExternal.branchXpub
		hd.internalKey = ad.albInternal.branchXpub
		if firstWatch {
			ad.albExternal.lastWatched = hd.externalCount
			ad.albInternal.lastWatched = hd.internalCount
		} else {
			hd.lastWatchedExternal = ad.albExternal.lastWatched
			hd.lastWatchedInternal = ad.albInternal.lastWatched
		}
		hdAccounts[acct] = hd
	}
	w.addressBuffersMu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchAddrs := make(chan []stdaddr.Address, runtime.NumCPU())
	watchError := make(chan error)
	go func() {
		for addrs := range watchAddrs {
			count += uint64(len(addrs))
			err := n.LoadTxFilter(ctx, false, addrs, nil)
			if err != nil {
				watchError <- err
				cancel()
				return
			}
		}
		watchError <- nil
	}()
	var deriveError error
	loadBranchAddrs := func(branchKey *hdkeychain.ExtendedKey, start, end uint32) {
		const step = 256
		for ; start <= end; start += step {
			addrs := make([]stdaddr.Address, 0, step)
			stop := minUint32(end+1, start+step)
			for child := start; child < stop; child++ {
				addr, err := deriveChildAddress(branchKey, child, w.chainParams)
				if errors.Is(err, hdkeychain.ErrInvalidChild) {
					continue
				}
				if err != nil {
					deriveError = err
					return
				}
				addrs = append(addrs, addr)
			}
			select {
			case watchAddrs <- addrs:
			case <-ctx.Done():
				return
			}
		}
	}
	for _, hd := range hdAccounts {
		loadBranchAddrs(hd.externalKey, hd.lastWatchedExternal, hd.externalCount)
		loadBranchAddrs(hd.internalKey, hd.lastWatchedInternal, hd.internalCount)
		if ctx.Err() != nil || deriveError != nil {
			break
		}
	}
	close(watchAddrs)
	if deriveError != nil {
		return 0, deriveError
	}
	err = <-watchError
	if err != nil {
		return 0, err
	}

	w.addressBuffersMu.Lock()
	for acct, hd := range hdAccounts {
		ad := w.addressBuffers[acct]
		if ad.albExternal.lastWatched < hd.externalCount {
			ad.albExternal.lastWatched = hd.externalCount
		}
		if ad.albInternal.lastWatched < hd.internalCount {
			ad.albInternal.lastWatched = hd.internalCount
		}
	}
	w.addressBuffersMu.Unlock()

	return count, nil
}

// CoinType returns the active BIP0044 coin type. For watching-only wallets,
// which do not save the coin type keys, this method will return an error with
// code errors.WatchingOnly.
func (w *Wallet) CoinType(ctx context.Context) (uint32, error) {
	const op errors.Op = "wallet.CoinType"
	var coinType uint32
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		var err error
		coinType, err = w.manager.CoinType(tx)
		return err
	})
	if err != nil {
		return coinType, errors.E(op, err)
	}
	return coinType, nil
}

// CoinTypePrivKey returns the BIP0044 coin type private key.
func (w *Wallet) CoinTypePrivKey(ctx context.Context) (*hdkeychain.ExtendedKey, error) {
	const op errors.Op = "wallet.CoinTypePrivKey"
	var coinTypePrivKey *hdkeychain.ExtendedKey
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		var err error
		coinTypePrivKey, err = w.manager.CoinTypePrivKey(tx)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return coinTypePrivKey, nil
}

// LoadActiveDataFilters loads filters for all active addresses and unspent
// outpoints for this wallet.
func (w *Wallet) LoadActiveDataFilters(ctx context.Context, n NetworkBackend, reload bool) (err error) {
	const op errors.Op = "wallet.LoadActiveDataFilters"
	log.Infof("Loading active addresses and unspent outputs...")

	if reload {
		err := n.LoadTxFilter(ctx, true, nil, nil)
		if err != nil {
			return err
		}
	}

	buf := make([]wire.OutPoint, 0, 64)
	defer func() {
		if len(buf) > 0 && err == nil {
			err = n.LoadTxFilter(ctx, false, nil, buf)
			if err != nil {
				return
			}
		}
	}()
	watchOutPoint := func(op *wire.OutPoint) (err error) {
		buf = append(buf, *op)
		if len(buf) == cap(buf) {
			err = n.LoadTxFilter(ctx, false, nil, buf)
			buf = buf[:0]
		}
		return
	}

	hdAddrCount, err := w.watchHDAddrs(ctx, true, n)
	if err != nil {
		return err
	}
	log.Infof("Registered for transaction notifications for %v HD address(es)", hdAddrCount)

	// Watch individually-imported addresses (which must each be read out of
	// the DB).
	abuf := make([]stdaddr.Address, 0, 256)
	var importedAddrCount int
	watchAddress := func(a udb.ManagedAddress) error {
		addr := a.Address()
		abuf = append(abuf, addr)
		if len(abuf) == cap(abuf) {
			importedAddrCount += len(abuf)
			err := n.LoadTxFilter(ctx, false, abuf, nil)
			abuf = abuf[:0]
			return err
		}
		return nil
	}
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		return w.manager.ForEachAccountAddress(addrmgrNs, udb.ImportedAddrAccount, watchAddress)
	})
	if err != nil {
		return err
	}
	if len(abuf) != 0 {
		importedAddrCount += len(abuf)
		err := n.LoadTxFilter(ctx, false, abuf, nil)
		if err != nil {
			return err
		}
	}
	if importedAddrCount > 0 {
		log.Infof("Registered for transaction notifications for %v imported address(es)", importedAddrCount)
	}

	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		err := w.txStore.ForEachUnspentOutpoint(dbtx, watchOutPoint)
		if err != nil {
			return err
		}

		_, height := w.txStore.MainChainTip(dbtx)
		tickets, err := w.txStore.UnspentTickets(dbtx, height, true)
		if err != nil {
			return err
		}
		for i := range tickets {
			op := wire.OutPoint{
				Hash:  tickets[i],
				Index: 0,
				Tree:  wire.TxTreeStake,
			}
			err = watchOutPoint(&op)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}
	log.Infof("Registered for transaction notifications for all relevant outputs")

	return nil
}

// CommittedTickets takes a list of tickets and returns a filtered list of
// tickets that are controlled by this wallet.
func (w *Wallet) CommittedTickets(ctx context.Context, tickets []*chainhash.Hash) ([]*chainhash.Hash, []stdaddr.Address, error) {
	const op errors.Op = "wallet.CommittedTickets"
	hashes := make([]*chainhash.Hash, 0, len(tickets))
	addresses := make([]stdaddr.Address, 0, len(tickets))
	// Verify we own this ticket
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		for _, v := range tickets {
			// Make sure ticket exists
			tx, err := w.txStore.Tx(txmgrNs, v)
			if err != nil {
				log.Debugf("%v", err)
				continue
			}
			if !stake.IsSStx(tx) {
				continue
			}

			// Commitment outputs are at alternating output
			// indexes, starting at 1.
			var bestAddr stdaddr.StakeAddress
			var bestAmount dcrutil.Amount

			for i := 1; i < len(tx.TxOut); i += 2 {
				scr := tx.TxOut[i].PkScript
				addr, err := stake.AddrFromSStxPkScrCommitment(scr,
					w.chainParams)
				if err != nil {
					log.Debugf("%v", err)
					break
				}
				if _, ok := addr.(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0); !ok {
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

			// We only support Hash160 addresses.
			var hash160 []byte
			switch bestAddr := bestAddr.(type) {
			case stdaddr.Hash160er:
				hash160 = bestAddr.Hash160()[:]
			}
			if hash160 == nil || !w.manager.ExistsHash160(
				addrmgrNs, hash160) {
				log.Debugf("not our address: hash160=%x", hash160)
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

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		missing = w.txStore.IsMissingMainChainCFilters(dbtx)
		if missing {
			height, err = w.txStore.MissingCFiltersHeight(dbtx)
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
		err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
			ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
			var err error
			missing = w.txStore.IsMissingMainChainCFilters(dbtx)
			if !missing {
				return nil
			}
			hash, err := w.txStore.GetMainChainBlockHashForHeight(ns, height)
			if err != nil {
				return err
			}
			_, _, err = w.txStore.CFilterV2(dbtx, &hash)
			if err == nil {
				height += span
				cont = true
				return nil
			}
			storage = storage[:cap(storage)]
			hashes, err = w.txStore.GetMainChainBlockHashes(ns, &hash, true, storage)
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

		filterData, err := p.CFiltersV2(ctx, get)
		if err != nil {
			return err
		}

		// Validate the newly received filters against the previously
		// stored block header using the corresponding inclusion proof
		// returned by the peer.
		filters := make([]*gcs2.FilterV2, len(filterData))
		err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
			for i, cf := range filterData {
				header, err := w.txStore.GetBlockHeader(dbtx, get[i])
				if err != nil {
					return err
				}
				err = validate.CFilterV2HeaderCommitment(w.chainParams.Net,
					header, cf.Filter, cf.ProofIndex, cf.Proof)
				if err != nil {
					return err
				}

				filters[i] = cf.Filter
			}
			return nil
		})
		if err != nil {
			return err
		}

		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			_, _, err := w.txStore.CFilterV2(dbtx, get[len(get)-1])
			if err == nil {
				cont = true
				return nil
			}
			return w.txStore.InsertMissingCFilters(dbtx, get, filters)
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
func (w *Wallet) BlockLocators(ctx context.Context, sidechain []*BlockNode) ([]*chainhash.Hash, error) {
	const op errors.Op = "wallet.BlockLocators"
	var locators []*chainhash.Hash
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
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
		hash, height = w.txStore.MainChainTip(dbtx)
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
			hash, err := w.txStore.GetMainChainBlockHashForHeight(ns, height)
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

// Consolidate consolidates as many UTXOs as are passed in the inputs argument.
// If that many UTXOs can not be found, it will use the maximum it finds. This
// will only compress UTXOs in the default account
func (w *Wallet) Consolidate(ctx context.Context, inputs int, account uint32, address stdaddr.Address) (*chainhash.Hash, error) {
	return w.compressWallet(ctx, "wallet.Consolidate", inputs, account, address)
}

// CreateMultisigTx creates and signs a multisig transaction.
func (w *Wallet) CreateMultisigTx(ctx context.Context, account uint32, amount dcrutil.Amount,
	pubkeys [][]byte, nrequired int8, minconf int32) (*CreatedTx, stdaddr.Address, []byte, error) {
	return w.txToMultisig(ctx, "wallet.CreateMultisigTx", account, amount, pubkeys, nrequired, minconf)
}

// PurchaseTicketsRequest describes the parameters for purchasing tickets.
type PurchaseTicketsRequest struct {
	Count         int
	SourceAccount uint32
	VotingAddress stdaddr.StakeAddress
	MinConf       int32
	Expiry        int32
	VotingAccount uint32 // Used when VotingAddress == nil, or CSPPServer != ""
	DontSignTx    bool

	// Mixed split buying through CoinShuffle++
	CSPPServer         string
	DialCSPPServer     DialFunc
	MixedAccount       uint32
	MixedAccountBranch uint32
	MixedSplitAccount  uint32
	ChangeAccount      uint32

	// VSP ticket buying; not currently usable with CoinShuffle++.
	VSPAddress stdaddr.StakeAddress
	VSPFees    float64

	// VSPServer methods
	// XXX this should be an interface

	// VSPFeeProcessFunc Process the fee price for the vsp to register a ticket
	// so we can reserve the amount.
	VSPFeeProcess func(context.Context) (float64, error)
	// VSPFeePaymentProcess processes the payment of the vsp fee and returns
	// the paid fee tx.
	VSPFeePaymentProcess func(context.Context, *chainhash.Hash, *wire.MsgTx) error

	// extraSplitOutput is an additional transaction output created during
	// UTXO contention, to be used as the input to pay a VSP fee
	// transaction, in order that both VSP fees and a single ticket purchase
	// may be created by spending distinct outputs.  After purchaseTickets
	// reentry, this output is locked and UTXO selection is only used to
	// fund the split transaction for a ticket purchase, without reserving
	// any additional outputs to pay the VSP fee.
	extraSplitOutput *Input
}

// PurchaseTicketsResponse describes the response for purchasing tickets request.
type PurchaseTicketsResponse struct {
	TicketHashes []*chainhash.Hash
	Tickets      []*wire.MsgTx
	SplitTx      *wire.MsgTx
}

// PurchaseTickets purchases tickets, returning purchase tickets response.
func (w *Wallet) PurchaseTickets(ctx context.Context, n NetworkBackend,
	req *PurchaseTicketsRequest) (*PurchaseTicketsResponse, error) {

	const op errors.Op = "wallet.PurchaseTickets"

	resp, err := w.purchaseTickets(ctx, op, n, req)
	if err == nil || !errors.Is(err, errVSPFeeRequiresUTXOSplit) || req.DontSignTx {
		return resp, err
	}

	// Do not attempt to split utxos for a fee payment when spending from
	// the mixed account.  This error is rather unlikely anyways, as mixed
	// accounts probably have very many outputs.
	if req.CSPPServer != "" && req.MixedAccount == req.SourceAccount {
		return nil, errors.E(op, errors.InsufficientBalance)
	}

	feePercent, err := req.VSPFeeProcess(ctx)
	if err != nil {
		return nil, err
	}
	sdiff, err := w.NextStakeDifficulty(ctx)
	if err != nil {
		return nil, err
	}
	_, height := w.MainChainTip(ctx)
	dcp0010Active := true
	switch n := n.(type) {
	case *dcrd.RPC:
		dcp0010Active, err = deployments.DCP0010Active(ctx,
			height, w.chainParams, n)
		if err != nil {
			return nil, err
		}
	}
	relayFee := w.RelayFee()
	vspFee := txrules.StakePoolTicketFee(sdiff, relayFee, height,
		feePercent, w.chainParams, dcp0010Active)
	a := &authorTx{
		outputs:            make([]*wire.TxOut, 0, 2),
		account:            req.SourceAccount,
		changeAccount:      req.SourceAccount, // safe-ish; this is not mixed.
		minconf:            req.MinConf,
		randomizeChangeIdx: true,
		txFee:              relayFee,
	}
	addr, err := w.NewInternalAddress(ctx, req.SourceAccount)
	if err != nil {
		return nil, err
	}
	version, script := addr.(Address).PaymentScript()
	a.outputs = append(a.outputs, &wire.TxOut{Version: version, PkScript: script})
	txsize := txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHInputSize},
		a.outputs, txsizes.P2PKHPkScriptSize)
	txfee := txrules.FeeForSerializeSize(relayFee, txsize)
	a.outputs[0].Value = int64(vspFee + 2*txfee)
	err = w.authorTx(ctx, op, a)
	if err != nil {
		return nil, err
	}
	err = w.recordAuthoredTx(ctx, op, a)
	if err != nil {
		return nil, err
	}
	err = w.publishAndWatch(ctx, op, nil, a.atx.Tx, a.watch)
	if err != nil {
		return nil, err
	}
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, update := range a.changeSourceUpdates {
			err := update(dbtx)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	req.MinConf = 0
	req.Count = 1
	var index uint32 = 0
	if a.atx.ChangeIndex == 0 {
		index = 1
	}
	req.extraSplitOutput = &Input{
		OutPoint: wire.OutPoint{
			Hash:  a.atx.Tx.TxHash(),
			Index: index,
			Tree:  0,
		},
		PrevOut: *a.atx.Tx.TxOut[index],
	}
	return w.purchaseTickets(ctx, op, n, req)
}

// Unlock unlocks the wallet, allowing access to private keys and secret scripts.
// An unlocked wallet will be locked before returning with a Passphrase error if
// the passphrase is incorrect.
// If the wallet is currently unlocked without any timeout, timeout is ignored
// and read in a background goroutine to avoid blocking sends.
// If the wallet is locked and a non-nil timeout is provided, the wallet will be
// locked in the background after reading from the channel.
// If the wallet is already unlocked with a previous timeout, the new timeout
// replaces the prior.
func (w *Wallet) Unlock(ctx context.Context, passphrase []byte, timeout <-chan time.Time) error {
	const op errors.Op = "wallet.Unlock"

	w.passphraseUsedMu.RLock()
	wasLocked := w.manager.IsLocked()
	err := w.manager.UnlockedWithPassphrase(passphrase)
	w.passphraseUsedMu.RUnlock()
	switch {
	case errors.Is(err, errors.WatchingOnly):
		return errors.E(op, err)
	case errors.Is(err, errors.Passphrase):
		w.Lock()
		if !wasLocked {
			log.Info("The wallet has been locked due to an incorrect passphrase.")
		}
		return errors.E(op, err)
	default:
		return errors.E(op, err)
	case errors.Is(err, errors.Locked):
		defer w.passphraseUsedMu.RUnlock()
		w.passphraseUsedMu.RLock()
		err = walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
			addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
			return w.manager.Unlock(addrmgrNs, passphrase)
		})
		if err != nil {
			return errors.E(op, errors.Passphrase, err)
		}
	case err == nil:
	}
	w.replacePassphraseTimeout(wasLocked, timeout)
	return nil
}

// SetAccountPassphrase individually-encrypts or changes the passphrase for
// account private keys.
//
// If the passphrase has zero length, the private keys are re-encrypted with the
// manager's global passphrase.
func (w *Wallet) SetAccountPassphrase(ctx context.Context, account uint32, passphrase []byte) error {
	return walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		return w.manager.SetAccountPassphrase(dbtx, account, passphrase)
	})
}

// UnlockAccount decrypts a uniquely-encrypted account's private keys.
func (w *Wallet) UnlockAccount(ctx context.Context, account uint32, passphrase []byte) error {
	return walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		return w.manager.UnlockAccount(dbtx, account, passphrase)
	})
}

// LockAccount locks an individually-encrypted account by removing private key
// access until unlocked again.
func (w *Wallet) LockAccount(ctx context.Context, account uint32) error {
	return walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		return w.manager.LockAccount(dbtx, account)
	})
}

// AccountUnlocked returns whether an individually-encrypted account is unlocked.
func (w *Wallet) AccountUnlocked(ctx context.Context, account uint32) (bool, error) {
	const op errors.Op = "wallet.AccountUnlocked"
	var unlocked bool
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var encrypted bool
		encrypted, unlocked = w.manager.AccountHasPassphrase(dbtx, account)
		if !encrypted {
			const s = "account is not individually encrypted"
			return errors.E(errors.Invalid, s)
		}
		return nil
	})
	if err != nil {
		return false, errors.E(op, err)
	}
	return unlocked, nil
}

func (w *Wallet) AccountHasPassphrase(ctx context.Context, account uint32) (bool, error) {
	const op errors.Op = "wallet.AccountHasPassphrase"
	var encrypted bool
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		encrypted, _ = w.manager.AccountHasPassphrase(dbtx, account)
		return nil
	})
	if err != nil {
		return false, errors.E(op, err)
	}
	return encrypted, nil
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
	_ = w.manager.Lock()
	w.passphraseTimeoutCancel = nil
	w.passphraseTimeoutMu.Unlock()
	w.passphraseUsedMu.Unlock()
}

// Locked returns whether the account manager for a wallet is locked.
func (w *Wallet) Locked() bool {
	return w.manager.IsLocked()
}

// Unlocked returns whether the account manager for a wallet is unlocked.
func (w *Wallet) Unlocked() bool {
	return !w.Locked()
}

// WatchingOnly returns whether the wallet only contains public keys.
func (w *Wallet) WatchingOnly() bool {
	return w.manager.WatchingOnly()
}

// ChangePrivatePassphrase attempts to change the passphrase for a wallet from
// old to new.  Changing the passphrase is synchronized with all other address
// manager locking and unlocking.  The lock state will be the same as it was
// before the password change.
func (w *Wallet) ChangePrivatePassphrase(ctx context.Context, old, new []byte) error {
	const op errors.Op = "wallet.ChangePrivatePassphrase"
	defer w.passphraseUsedMu.Unlock()
	w.passphraseUsedMu.Lock()
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		return w.manager.ChangePassphrase(addrmgrNs, old, new, true)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// ChangePublicPassphrase modifies the public passphrase of the wallet.
func (w *Wallet) ChangePublicPassphrase(ctx context.Context, old, new []byte) error {
	const op errors.Op = "wallet.ChangePublicPassphrase"
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		return w.manager.ChangePassphrase(addrmgrNs, old, new, false)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// Balances describes a breakdown of an account's balances in various
// categories.
type Balances struct {
	Account                 uint32
	ImmatureCoinbaseRewards dcrutil.Amount
	ImmatureStakeGeneration dcrutil.Amount
	LockedByTickets         dcrutil.Amount
	Spendable               dcrutil.Amount
	Total                   dcrutil.Amount
	VotingAuthority         dcrutil.Amount
	Unconfirmed             dcrutil.Amount
}

// AccountBalance returns the balance breakdown for a single account.
func (w *Wallet) AccountBalance(ctx context.Context, account uint32, confirms int32) (Balances, error) {
	const op errors.Op = "wallet.CalculateAccountBalance"
	var balance Balances
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		balance, err = w.txStore.AccountBalance(dbtx,
			confirms, account)
		return err
	})
	if err != nil {
		return balance, errors.E(op, err)
	}
	return balance, nil
}

// AccountBalances returns the balance breakdowns for a each account.
func (w *Wallet) AccountBalances(ctx context.Context, confirms int32) ([]Balances, error) {
	const op errors.Op = "wallet.CalculateAccountBalances"
	var balances []Balances
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		return w.manager.ForEachAccount(addrmgrNs, func(acct uint32) error {
			balance, err := w.txStore.AccountBalance(dbtx,
				confirms, acct)
			if err != nil {
				return err
			}
			balances = append(balances, balance)
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
func (w *Wallet) CurrentAddress(account uint32) (stdaddr.Address, error) {
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

// SignHashes returns signatures of signed transaction hashes using an
// address' associated private key.
func (w *Wallet) SignHashes(ctx context.Context, hashes [][]byte, addr stdaddr.Address) ([][]byte,
	[]byte, error) {

	var privKey *secp256k1.PrivateKey
	var done func()
	defer func() {
		if done != nil {
			done()
		}
	}()
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		privKey, done, err = w.manager.PrivateKey(addrmgrNs, addr)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	signatures := make([][]byte, len(hashes))
	for i, hash := range hashes {
		sig := ecdsa.Sign(privKey, hash)
		signatures[i] = sig.Serialize()
	}

	return signatures, privKey.PubKey().SerializeCompressed(), nil
}

// SignMessage returns the signature of a signed message using an address'
// associated private key.
func (w *Wallet) SignMessage(ctx context.Context, msg string, addr stdaddr.Address) (sig []byte, err error) {
	const op errors.Op = "wallet.SignMessage"
	var buf bytes.Buffer
	wire.WriteVarString(&buf, 0, "Decred Signed Message:\n")
	wire.WriteVarString(&buf, 0, msg)
	messageHash := chainhash.HashB(buf.Bytes())
	var privKey *secp256k1.PrivateKey
	var done func()
	defer func() {
		if done != nil {
			done()
		}
	}()
	err = walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		privKey, done, err = w.manager.PrivateKey(addrmgrNs, addr)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	sig = ecdsa.SignCompact(privKey, messageHash, true)
	return sig, nil
}

// VerifyMessage verifies that sig is a valid signature of msg and was created
// using the secp256k1 private key for addr.
func VerifyMessage(msg string, addr stdaddr.Address, sig []byte, params stdaddr.AddressParams) (bool, error) {
	const op errors.Op = "wallet.VerifyMessage"
	// Validate the signature - this just shows that it was valid for any pubkey
	// at all. Whether the pubkey matches is checked below.
	var buf bytes.Buffer
	wire.WriteVarString(&buf, 0, "Decred Signed Message:\n")
	wire.WriteVarString(&buf, 0, msg)
	expectedMessageHash := chainhash.HashB(buf.Bytes())
	pk, wasCompressed, err := ecdsa.RecoverCompact(sig, expectedMessageHash)
	if err != nil {
		return false, errors.E(op, err)
	}

	// Reconstruct the pubkey hash address from the recovered pubkey.
	var pkHash []byte
	if wasCompressed {
		pkHash = stdaddr.Hash160(pk.SerializeCompressed())
	} else {
		pkHash = stdaddr.Hash160(pk.SerializeUncompressed())
	}
	address, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pkHash, params)
	if err != nil {
		return false, errors.E(op, err)
	}

	// Return whether addresses match.
	return address.String() == addr.String(), nil
}

// HaveAddress returns whether the wallet is the owner of the address a.
func (w *Wallet) HaveAddress(ctx context.Context, a stdaddr.Address) (bool, error) {
	const op errors.Op = "wallet.HaveAddress"
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		_, err := w.manager.Address(addrmgrNs, a)
		return err
	})
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return false, nil
		}
		return false, errors.E(op, err)
	}
	return true, nil
}

// AccountNumber returns the account number for an account name.
func (w *Wallet) AccountNumber(ctx context.Context, accountName string) (uint32, error) {
	const op errors.Op = "wallet.AccountNumber"
	var account uint32
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		account, err = w.manager.LookupAccount(addrmgrNs, accountName)
		return err
	})
	if err != nil {
		return 0, errors.E(op, err)
	}
	return account, nil
}

// AccountName returns the name of an account.
func (w *Wallet) AccountName(ctx context.Context, accountNumber uint32) (string, error) {
	const op errors.Op = "wallet.AccountName"
	var accountName string
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		accountName, err = w.manager.AccountName(addrmgrNs, accountNumber)
		return err
	})
	if err != nil {
		return "", errors.E(op, err)
	}
	return accountName, nil
}

// RenameAccount sets the name for an account number to newName.
func (w *Wallet) RenameAccount(ctx context.Context, account uint32, newName string) error {
	const op errors.Op = "wallet.RenameAccount"
	var props *udb.AccountProperties
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		err := w.manager.RenameAccount(addrmgrNs, account, newName)
		if err != nil {
			return err
		}
		props, err = w.manager.AccountProperties(addrmgrNs, account)
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
func (w *Wallet) NextAccount(ctx context.Context, name string) (uint32, error) {
	const op errors.Op = "wallet.NextAccount"
	maxEmptyAccounts := uint32(w.accountGapLimit)
	var account uint32
	var props *udb.AccountProperties
	var xpub *hdkeychain.ExtendedKey
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)

		// Ensure that there is transaction history in the last 100 accounts.
		var err error
		lastAcct, err := w.manager.LastAccount(addrmgrNs)
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
			props, err := w.manager.AccountProperties(addrmgrNs, a)
			if err != nil {
				return err
			}
			if props.LastUsedExternalIndex != ^uint32(0) || props.LastUsedInternalIndex != ^uint32(0) {
				canCreate = true
				break
			}
		}
		if !canCreate {
			return errors.Errorf("last %d accounts have no transaction history",
				maxEmptyAccounts)
		}

		account, err = w.manager.NewAccount(addrmgrNs, name)
		if err != nil {
			return err
		}

		props, err = w.manager.AccountProperties(addrmgrNs, account)
		if err != nil {
			return err
		}

		xpub, err = w.manager.AccountExtendedPubKey(tx, account)
		if err != nil {
			return err
		}

		err = w.manager.SyncAccountToAddrIndex(addrmgrNs, account,
			w.gapLimit, udb.ExternalBranch)
		if err != nil {
			return err
		}
		return w.manager.SyncAccountToAddrIndex(addrmgrNs, account,
			w.gapLimit, udb.InternalBranch)
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
					w.gapLimit, w.chainParams)
				if err != nil {
					errs <- err
					return
				}
				errs <- n.LoadTxFilter(ctx, false, addrs, nil)
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

// AccountXpub returns a BIP0044 account's extended public key.
func (w *Wallet) AccountXpub(ctx context.Context, account uint32) (*hdkeychain.ExtendedKey, error) {
	const op errors.Op = "wallet.AccountXpub"

	var pubKey *hdkeychain.ExtendedKey
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		var err error
		pubKey, err = w.manager.AccountExtendedPubKey(tx, account)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	return pubKey, nil
}

// AccountXpriv returns a BIP0044 account's extended private key.  The account
// must exist and the wallet must be unlocked, otherwise this function fails.
func (w *Wallet) AccountXpriv(ctx context.Context, account uint32) (*hdkeychain.ExtendedKey, error) {
	const op errors.Op = "wallet.AccountXpriv"

	var privKey *hdkeychain.ExtendedKey
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		var err error
		privKey, err = w.manager.AccountExtendedPrivKey(tx, account)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	return privKey, nil
}

// TxBlock returns the hash and height of a block which mines a transaction.
func (w *Wallet) TxBlock(ctx context.Context, hash *chainhash.Hash) (chainhash.Hash, int32, error) {
	var blockHash chainhash.Hash
	var height int32
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		height, err = w.txStore.TxBlockHeight(dbtx, hash)
		if err != nil {
			return err
		}
		if height == -1 {
			return nil
		}
		blockHash, err = w.txStore.GetMainChainBlockHashForHeight(ns, height)
		return err
	})
	if err != nil {
		return blockHash, 0, err
	}
	return blockHash, height, nil
}

// TxConfirms returns the current number of block confirmations a transaction.
func (w *Wallet) TxConfirms(ctx context.Context, hash *chainhash.Hash) (int32, error) {
	var tip, txheight int32
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		_, tip = w.txStore.MainChainTip(dbtx)
		var err error
		txheight, err = w.txStore.TxBlockHeight(dbtx, hash)
		return err
	})
	if err != nil {
		return 0, err
	}
	return confirms(txheight, tip), nil
}

// GetTransactionsByHashes returns all known transactions identified by a slice
// of transaction hashes.  It is possible that not all transactions are found,
// and in this case the known results will be returned along with an inventory
// vector of all missing transactions and an error with code
// NotExist.
func (w *Wallet) GetTransactionsByHashes(ctx context.Context, txHashes []*chainhash.Hash) (txs []*wire.MsgTx, notFound []*wire.InvVect, err error) {
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		for _, hash := range txHashes {
			tx, err := w.txStore.Tx(ns, hash)
			switch {
			case err != nil && !errors.Is(err, errors.NotExist):
				return err
			case tx == nil:
				notFound = append(notFound, wire.NewInvVect(wire.InvTypeTx, hash))
			default:
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

// recvCategory returns the category of received credit outputs from a
// transaction record.  The passed block chain height is used to distinguish
// immature from mature coinbase outputs.
//
// TODO: This is intended for use by the RPC server and should be moved out of
// this package at a later time.
func recvCategory(details *udb.TxDetails, syncHeight int32, chainParams *chaincfg.Params) CreditCategory {
	if compat.IsEitherCoinBaseTx(&details.MsgTx) {
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
	generated := compat.IsEitherCoinBaseTx(&details.MsgTx)
	recvCat := recvCategory(details, syncHeight, net).String()

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
		_, addrs := stdscript.ExtractAddrs(output.Version, output.PkScript, net)
		if len(addrs) == 1 {
			addr := addrs[0]
			address = addr.String()
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
func (w *Wallet) ListSinceBlock(ctx context.Context, start, end, syncHeight int32) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListSinceBlock"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			for _, detail := range details {
				sends, receives := listTransactions(tx, &detail,
					w.manager, syncHeight, w.chainParams)
				txList = append(txList, receives...)
				txList = append(txList, sends...)
			}
			return false, nil
		}

		return w.txStore.RangeTransactions(ctx, txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return txList, nil
}

// ListTransactions returns a slice of objects with details about a recorded
// transaction.  This is intended to be used for listtransactions RPC
// replies.
func (w *Wallet) ListTransactions(ctx context.Context, from, count int) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListTransactions"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.txStore.MainChainTip(dbtx)

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

				sends, receives := listTransactions(dbtx, &details[i],
					w.manager, tipHeight, w.chainParams)
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
		return w.txStore.RangeTransactions(ctx, txmgrNs, -1, 0, rangeFn)
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
func (w *Wallet) ListAddressTransactions(ctx context.Context, pkHashes map[string]struct{}) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListAddressTransactions"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.txStore.MainChainTip(dbtx)
		rangeFn := func(details []udb.TxDetails) (bool, error) {
		loopDetails:
			for i := range details {
				detail := &details[i]

				for _, cred := range detail.Credits {
					if detail.MsgTx.TxOut[cred.Index].Version != scriptVersionAssumed {
						continue
					}
					pkh := stdscript.ExtractPubKeyHashV0(detail.MsgTx.TxOut[cred.Index].PkScript)
					if _, ok := pkHashes[string(pkh)]; !ok {
						continue
					}

					sends, receives := listTransactions(dbtx, detail,
						w.manager, tipHeight, w.chainParams)
					txList = append(txList, receives...)
					txList = append(txList, sends...)
					continue loopDetails
				}
			}
			return false, nil
		}

		return w.txStore.RangeTransactions(ctx, txmgrNs, 0, -1, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return txList, nil
}

// ListAllTransactions returns a slice of objects with details about a recorded
// transaction.  This is intended to be used for listalltransactions RPC
// replies.
func (w *Wallet) ListAllTransactions(ctx context.Context) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListAllTransactions"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.txStore.MainChainTip(dbtx)

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			// Iterate over transactions at this height in reverse
			// order.  This does nothing for unmined transactions,
			// which are unsorted, but it will process mined
			// transactions in the reverse order they were marked
			// mined.
			for i := len(details) - 1; i >= 0; i-- {
				sends, receives := listTransactions(dbtx, &details[i],
					w.manager, tipHeight, w.chainParams)
				txList = append(txList, sends...)
				txList = append(txList, receives...)
			}
			return false, nil
		}

		// Return newer results first by starting at mempool height and
		// working down to the genesis block.
		return w.txStore.RangeTransactions(ctx, txmgrNs, -1, 0, rangeFn)
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
func (w *Wallet) ListTransactionDetails(ctx context.Context, txHash *chainhash.Hash) ([]types.ListTransactionsResult, error) {
	const op errors.Op = "wallet.ListTransactionDetails"
	txList := []types.ListTransactionsResult{}
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.txStore.MainChainTip(dbtx)

		txd, err := w.txStore.TxDetails(txmgrNs, txHash)
		if err != nil {
			return err
		}
		sends, receives := listTransactions(dbtx, txd, w.manager, tipHeight, w.chainParams)
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
func (w *Wallet) BlockInfo(ctx context.Context, blockID *BlockIdentifier) (*BlockInfo, error) {
	const op errors.Op = "wallet.BlockInfo"
	var blockInfo *BlockInfo
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.txStore.MainChainTip(dbtx)
		blockHash := blockID.hash
		if blockHash == nil {
			hash, err := w.txStore.GetMainChainBlockHashForHeight(txmgrNs,
				blockID.height)
			if err != nil {
				return err
			}
			blockHash = &hash
		}
		header, err := w.txStore.GetSerializedBlockHeader(txmgrNs, blockHash)
		if err != nil {
			return err
		}
		height := udb.ExtractBlockHeaderHeight(header)
		inMainChain, invalidated := w.txStore.BlockInMainChain(dbtx, blockHash)
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
func (w *Wallet) TransactionSummary(ctx context.Context, txHash *chainhash.Hash) (txSummary *TransactionSummary, confs int32, blockHash *chainhash.Hash, err error) {
	const opf = "wallet.TransactionSummary(%v)"
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.txStore.MainChainTip(dbtx)
		txDetails, err := w.txStore.TxDetails(ns, txHash)
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

// fetchTicketDetails returns the ticket details of the provided ticket hash.
func (w *Wallet) fetchTicketDetails(ns walletdb.ReadBucket, hash *chainhash.Hash) (*udb.TicketDetails, error) {
	txDetail, err := w.txStore.TxDetails(ns, hash)
	if err != nil {
		return nil, err
	}

	if txDetail.TxType != stake.TxTypeSStx {
		return nil, errors.Errorf("%v is not a ticket", hash)
	}

	ticketDetails, err := w.txStore.TicketDetails(ns, txDetail)
	if err != nil {
		return nil, errors.Errorf("%v while trying to get ticket"+
			" details for txhash: %v", err, hash)
	}

	return ticketDetails, nil
}

// TicketSummary contains the properties to describe a ticket's current status
type TicketSummary struct {
	Ticket  *TransactionSummary
	Spender *TransactionSummary
	Status  TicketStatus
}

// TicketStatus describes the current status a ticket can be observed to be.
type TicketStatus uint

//go:generate stringer -type=TicketStatus -linecomment

const (
	// TicketStatusUnknown any ticket that its status was unable to be determined.
	TicketStatusUnknown TicketStatus = iota // unknown

	// TicketStatusUnmined any not yet mined ticket.
	TicketStatusUnmined // unmined

	// TicketStatusImmature any so to be live ticket.
	TicketStatusImmature // immature

	// TicketStatusLive any currently live ticket.
	TicketStatusLive // live

	// TicketStatusVoted any ticket that was seen to have voted.
	TicketStatusVoted // voted

	// TicketStatusMissed any ticket that has yet to be revoked, and was missed.
	TicketStatusMissed // missed

	// TicketStatusExpired any ticket that has yet to be revoked, and was expired.
	// In SPV mode, this status may be used by unspent tickets definitely
	// past the expiry period, even if the ticket was actually missed rather than
	// expiring.
	TicketStatusExpired // expired

	// TicketStatusUnspent is a matured ticket that has not been spent.  It
	// is only used under SPV mode where it is unknown if a ticket is live,
	// was missed, or expired.
	TicketStatusUnspent // unspent

	// TicketStatusRevoked any ticket that has been previously revoked.
	TicketStatusRevoked // revoked
)

func makeTicketSummary(ctx context.Context, rpc *dcrd.RPC, dbtx walletdb.ReadTx,
	w *Wallet, details *udb.TicketDetails) *TicketSummary {

	ticketHeight := details.Ticket.Height()
	_, tipHeight := w.txStore.MainChainTip(dbtx)

	ticketTransactionDetails := makeTxSummary(dbtx, w, details.Ticket)
	summary := &TicketSummary{
		Ticket: &ticketTransactionDetails,
		Status: TicketStatusLive,
	}
	if rpc == nil {
		summary.Status = TicketStatusUnspent
	}
	// Check if ticket is unmined or immature
	switch {
	case ticketHeight == int32(-1):
		summary.Status = TicketStatusUnmined
	case !ticketMatured(w.chainParams, ticketHeight, tipHeight):
		summary.Status = TicketStatusImmature
	}

	if details.Spender != nil {
		spenderTransactionDetails := makeTxSummary(dbtx, w, details.Spender)
		summary.Spender = &spenderTransactionDetails

		switch details.Spender.TxType {
		case stake.TxTypeSSGen:
			summary.Status = TicketStatusVoted
		case stake.TxTypeSSRtx:
			summary.Status = TicketStatusRevoked
		}
		return summary
	}

	if rpc != nil && summary.Status == TicketStatusLive {
		// In RPC mode, find if unspent ticket was expired or missed.
		hashes := []*chainhash.Hash{&details.Ticket.Hash}
		live, expired, err := rpc.ExistsLiveExpiredTickets(ctx, hashes)
		switch {
		case err != nil:
			log.Errorf("Unable to check if ticket was live "+
				"for ticket status: %v", &details.Ticket.Hash)
			summary.Status = TicketStatusUnknown
		case expired.Get(0):
			summary.Status = TicketStatusExpired
		case !live.Get(0):
			summary.Status = TicketStatusMissed
		}
	} else if rpc == nil {
		// In SPV mode, use expired status when the ticket is certainly
		// past the expiry period (even though it is possible that the
		// ticket was missed rather than expiring).
		if ticketExpired(w.chainParams, ticketHeight, tipHeight) {
			summary.Status = TicketStatusExpired
		}
	}

	return summary
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
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
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
		hBytes, err := w.txStore.GetSerializedBlockHeader(txmgrNs,
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
func (w *Wallet) GetTicketInfo(ctx context.Context, hash *chainhash.Hash) (*TicketSummary, *wire.BlockHeader, error) {
	const op errors.Op = "wallet.GetTicketInfo"

	var ticketSummary *TicketSummary
	var blockHeader *wire.BlockHeader

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
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
		hBytes, err := w.txStore.GetSerializedBlockHeader(txmgrNs,
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

// blockRange returns the start and ending heights for the given set of block
// identifiers or the default values used to range over the entire chain
// (including unmined transactions).
func (w *Wallet) blockRange(dbtx walletdb.ReadTx, startBlock, endBlock *BlockIdentifier) (int32, int32, error) {
	var start, end int32 = 0, -1
	ns := dbtx.ReadBucket(wtxmgrNamespaceKey)

	switch {
	case startBlock == nil:
		// Hardcoded default start of 0.
	case startBlock.hash == nil:
		start = startBlock.height
	default:
		serHeader, err := w.txStore.GetSerializedBlockHeader(ns, startBlock.hash)
		if err != nil {
			return 0, 0, err
		}
		var startHeader wire.BlockHeader
		err = startHeader.Deserialize(bytes.NewReader(serHeader))
		if err != nil {
			return 0, 0, err
		}
		start = int32(startHeader.Height)
	}

	switch {
	case endBlock == nil:
		// Hardcoded default end of -1.
	case endBlock.hash == nil:
		end = endBlock.height
	default:
		serHeader, err := w.txStore.GetSerializedBlockHeader(ns, endBlock.hash)
		if err != nil {
			return 0, 0, err
		}
		var endHeader wire.BlockHeader
		err = endHeader.Deserialize(bytes.NewReader(serHeader))
		if err != nil {
			return 0, 0, err
		}
		end = int32(endHeader.Height)
	}

	return start, end, nil
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
	f func([]*TicketSummary, *wire.BlockHeader) (bool, error),
	startBlock, endBlock *BlockIdentifier) error {

	const op errors.Op = "wallet.GetTicketsPrecise"

	rpc := dcrd.New(rpcCaller)
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		start, end, err := w.blockRange(dbtx, startBlock, endBlock)
		if err != nil {
			return err
		}

		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		header := &wire.BlockHeader{}

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			tickets := make([]*TicketSummary, 0, len(details))

			for i := range details {
				ticketInfo, err := w.txStore.TicketDetails(txmgrNs, &details[i])
				if err != nil {
					return false, errors.Errorf("%v while trying to get "+
						"ticket details for txhash: %v", err, &details[i].Hash)
				}
				// Continue if not a ticket
				if ticketInfo == nil {
					continue
				}
				summary := makeTicketSummary(ctx, rpc, dbtx, w, ticketInfo)
				tickets = append(tickets, summary)
			}

			if len(tickets) == 0 {
				return false, nil
			}

			if details[0].Block.Height == -1 {
				return f(tickets, nil)
			}

			blockHash := &details[0].Block.Hash
			headerBytes, err := w.txStore.GetSerializedBlockHeader(txmgrNs, blockHash)
			if err != nil {
				return false, err
			}
			header.FromBytes(headerBytes)
			return f(tickets, header)
		}

		return w.txStore.RangeTransactions(ctx, txmgrNs, start, end, rangeFn)
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
func (w *Wallet) GetTickets(ctx context.Context,
	f func([]*TicketSummary, *wire.BlockHeader) (bool, error),
	startBlock, endBlock *BlockIdentifier) error {

	const op errors.Op = "wallet.GetTickets"

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		start, end, err := w.blockRange(dbtx, startBlock, endBlock)
		if err != nil {
			return err
		}

		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		header := &wire.BlockHeader{}

		rangeFn := func(details []udb.TxDetails) (bool, error) {
			tickets := make([]*TicketSummary, 0, len(details))

			for i := range details {
				ticketInfo, err := w.txStore.TicketDetails(txmgrNs, &details[i])
				if err != nil {
					return false, errors.Errorf("%v while trying to get "+
						"ticket details for txhash: %v", err, &details[i].Hash)
				}
				// Continue if not a ticket
				if ticketInfo == nil {
					continue
				}
				summary := makeTicketSummary(ctx, nil, dbtx, w, ticketInfo)
				tickets = append(tickets, summary)
			}

			if len(tickets) == 0 {
				return false, nil
			}

			if details[0].Block.Height == -1 {
				return f(tickets, nil)
			}

			blockHash := &details[0].Block.Hash
			headerBytes, err := w.txStore.GetSerializedBlockHeader(txmgrNs, blockHash)
			if err != nil {
				return false, err
			}
			header.FromBytes(headerBytes)
			return f(tickets, header)
		}

		return w.txStore.RangeTransactions(ctx, txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
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
func (w *Wallet) GetTransactions(ctx context.Context, f func(*Block) (bool, error), startBlock, endBlock *BlockIdentifier) error {
	const op errors.Op = "wallet.GetTransactions"

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		start, end, err := w.blockRange(dbtx, startBlock, endBlock)
		if err != nil {
			return err
		}

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
				serHeader, err := w.txStore.GetSerializedBlockHeader(txmgrNs,
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

		return w.txStore.RangeTransactions(ctx, txmgrNs, start, end, rangeFn)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// Spender queries for the transaction and input index which spends a Credit.
// If the output is not a Credit, an error with code ErrInput is returned.  If
// the output is unspent, the ErrNoExist code is used.
func (w *Wallet) Spender(ctx context.Context, out *wire.OutPoint) (*wire.MsgTx, uint32, error) {
	var spender *wire.MsgTx
	var spenderIndex uint32
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		spender, spenderIndex, err = w.txStore.Spender(dbtx, out)
		return err
	})
	return spender, spenderIndex, err
}

// UnspentOutput returns information about an unspent received transaction
// output. Returns error NotExist if the specified outpoint cannot be found or
// has been spent by a mined transaction. Mined transactions that are spent by
// a mempool transaction are not affected by this.
func (w *Wallet) UnspentOutput(ctx context.Context, op wire.OutPoint, includeMempool bool) (*udb.Credit, error) {
	var utxo *udb.Credit
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		utxo, err = w.txStore.UnspentOutput(txmgrNs, op, includeMempool)
		return err
	})
	return utxo, err
}

// AccountProperties contains properties associated with each account, such as
// the account name, number, and the nubmer of derived and imported keys.  If no
// address usage has been recorded on any of the external or internal branches,
// the child index is ^uint32(0).
type AccountProperties struct {
	AccountNumber             uint32
	AccountName               string
	AccountType               uint8
	LastUsedExternalIndex     uint32
	LastUsedInternalIndex     uint32
	LastReturnedExternalIndex uint32
	LastReturnedInternalIndex uint32
	ImportedKeyCount          uint32
	AccountEncrypted          bool
	AccountUnlocked           bool
}

// AccountResult is a single account result for the AccountsResult type.
type AccountResult struct {
	AccountProperties
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
func (w *Wallet) Accounts(ctx context.Context) (*AccountsResult, error) {
	const op errors.Op = "wallet.Accounts"
	var (
		accounts  []AccountResult
		tipHash   chainhash.Hash
		tipHeight int32
	)

	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

		tipHash, tipHeight = w.txStore.MainChainTip(dbtx)
		unspent, err := w.txStore.UnspentOutputs(dbtx)
		if err != nil {
			return err
		}
		err = w.manager.ForEachAccount(addrmgrNs, func(acct uint32) error {
			props, err := w.manager.AccountProperties(addrmgrNs, acct)
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
			_, addrs := stdscript.ExtractAddrs(scriptVersionAssumed, output.PkScript, w.chainParams)
			if len(addrs) == 0 {
				continue
			}
			outputAcct, err := w.manager.AddrAccount(addrmgrNs, addrs[0])
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
func (w *Wallet) ListUnspent(ctx context.Context, minconf, maxconf int32, addresses map[string]struct{}, accountName string) ([]*types.ListUnspentResult, error) {
	const op errors.Op = "wallet.ListUnspent"
	var results []*types.ListUnspentResult
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.txStore.MainChainTip(dbtx)

		filter := len(addresses) != 0
		unspent, err := w.txStore.UnspentOutputs(dbtx)
		if err != nil {
			return err
		}
		sort.Sort(sort.Reverse(creditSlice(unspent)))

		defaultAccountName, err := w.manager.AccountName(
			addrmgrNs, udb.DefaultAccountNum)
		if err != nil {
			return err
		}

		for i := range unspent {
			output := unspent[i]

			details, err := w.txStore.TxDetails(txmgrNs, &output.Hash)
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
			if w.LockedOutpoint(&output.OutPoint.Hash, output.OutPoint.Index) {
				continue
			}

			// Lookup the associated account for the output.  Use the
			// default account name in case there is no associated account
			// for some reason, although this should never happen.
			//
			// This will be unnecessary once transactions and outputs are
			// grouped under the associated account in the db.
			acctName := defaultAccountName
			sc, addrs := stdscript.ExtractAddrs(scriptVersionAssumed, output.PkScript, w.chainParams)
			if len(addrs) > 0 {
				acct, err := w.manager.AddrAccount(
					addrmgrNs, addrs[0])
				if err == nil {
					s, err := w.manager.AccountName(
						addrmgrNs, acct)
					if err == nil {
						acctName = s
					}
				}
			}
			if accountName != "" && accountName != acctName {
				continue
			}
			if filter {
				for _, addr := range addrs {
					_, ok := addresses[addr.String()]
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
			var redeemScript []byte
		scSwitch:
			switch sc {
			case stdscript.STPubKeyHashEcdsaSecp256k1:
				spendable = true
			case stdscript.STPubKeyEcdsaSecp256k1:
				spendable = true
			case stdscript.STScriptHash:
				spendable = true
				if len(addrs) != 1 {
					return errors.Errorf("invalid address count for pay-to-script-hash output")
				}
				redeemScript, err = w.manager.RedeemScript(addrmgrNs, addrs[0])
				if err != nil {
					return err
				}
			case stdscript.STStakeGenPubKeyHash, stdscript.STStakeGenScriptHash:
				spendable = true
			case stdscript.STStakeRevocationPubKeyHash, stdscript.STStakeRevocationScriptHash:
				spendable = true
			case stdscript.STStakeChangePubKeyHash, stdscript.STStakeChangeScriptHash:
				spendable = true
			case stdscript.STMultiSig:
				for _, a := range addrs {
					_, err := w.manager.Address(addrmgrNs, a)
					if err == nil {
						continue
					}
					if errors.Is(err, errors.NotExist) {
						break scSwitch
					}
					return err
				}
				spendable = true
			}

			// If address decoding failed, the output is not spendable
			// regardless of detected script type.
			spendable = spendable && len(addrs) > 0

			result := &types.ListUnspentResult{
				TxID:          output.OutPoint.Hash.String(),
				Vout:          output.OutPoint.Index,
				Tree:          output.OutPoint.Tree,
				Account:       acctName,
				ScriptPubKey:  hex.EncodeToString(output.PkScript),
				RedeemScript:  hex.EncodeToString(redeemScript),
				TxType:        int(details.TxType),
				Amount:        output.Amount.ToCoin(),
				Confirmations: int64(confs),
				Spendable:     spendable,
			}

			// BUG: this should be a JSON array so that all
			// addresses can be included, or removed (and the
			// caller extracts addresses from the pkScript).
			if len(addrs) > 0 {
				result.Address = addrs[0].String()
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

func (w *Wallet) LoadPrivateKey(ctx context.Context, addr stdaddr.Address) (key *secp256k1.PrivateKey,
	zero func(), err error) {

	const op errors.Op = "wallet.LoadPrivateKey"
	err = walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		key, zero, err = w.manager.PrivateKey(addrmgrNs, addr)
		return err
	})
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	return key, zero, nil
}

// DumpWIFPrivateKey returns the WIF encoded private key for a
// single wallet address.
func (w *Wallet) DumpWIFPrivateKey(ctx context.Context, addr stdaddr.Address) (string, error) {
	const op errors.Op = "wallet.DumpWIFPrivateKey"
	privKey, zero, err := w.LoadPrivateKey(ctx, addr)
	if err != nil {
		return "", errors.E(op, err)
	}
	defer zero()
	wif, err := dcrutil.NewWIF(privKey.Serialize(), w.chainParams.PrivateKeyID,
		dcrec.STEcdsaSecp256k1)
	if err != nil {
		return "", errors.E(op, err)
	}
	return wif.String(), nil
}

// ImportPrivateKey imports a private key to the wallet and writes the new
// wallet to disk.
func (w *Wallet) ImportPrivateKey(ctx context.Context, wif *dcrutil.WIF) (string, error) {
	const op errors.Op = "wallet.ImportPrivateKey"
	// Attempt to import private key into wallet.
	var addr stdaddr.Address
	var props *udb.AccountProperties
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		maddr, err := w.manager.ImportPrivateKey(addrmgrNs, wif)
		if err == nil {
			addr = maddr.Address()
			props, err = w.manager.AccountProperties(
				addrmgrNs, udb.ImportedAddrAccount)
		}
		return err
	})
	if err != nil {
		return "", errors.E(op, err)
	}

	if n, err := w.NetworkBackend(); err == nil {
		err := n.LoadTxFilter(ctx, false, []stdaddr.Address{addr}, nil)
		if err != nil {
			return "", errors.E(op, err)
		}
	}

	addrStr := addr.String()
	log.Infof("Imported payment address %s", addrStr)

	w.NtfnServer.notifyAccountProperties(props)

	// Return the payment address string of the imported private key.
	return addrStr, nil
}

// ImportScript imports a redeemscript to the wallet. If it also allows the
// user to specify whether or not they want the redeemscript to be rescanned,
// and how far back they wish to rescan.
func (w *Wallet) ImportScript(ctx context.Context, rs []byte) error {
	const op errors.Op = "wallet.ImportScript"
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		mscriptaddr, err := w.manager.ImportScript(addrmgrNs, rs)
		if err != nil {
			return err
		}

		addr := mscriptaddr.Address()
		if n, err := w.NetworkBackend(); err == nil {
			addrs := []stdaddr.Address{addr}
			err := n.LoadTxFilter(ctx, false, addrs, nil)
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

// VotingXprivFromSeed derives a voting xpriv from a byte seed.
func (w *Wallet) VotingXprivFromSeed(seed []byte) (*hdkeychain.ExtendedKey, error) {
	return votingXprivFromSeed(seed, w.ChainParams())
}

// votingXprivFromSeed derives a voting xpriv from a byte seed. The key is at
// the same path as the zeroth slip0044 account key for seed.
func votingXprivFromSeed(seed []byte, params *chaincfg.Params) (*hdkeychain.ExtendedKey, error) {
	const op errors.Op = "wallet.VotingXprivFromSeed"

	seedSize := len(seed)
	if seedSize < hdkeychain.MinSeedBytes || seedSize > hdkeychain.MaxSeedBytes {
		return nil, errors.E(errors.Invalid, errors.New("invalid seed length"))
	}

	// Generate the BIP0044 HD key structure to ensure the provided seed
	// can generate the required structure with no issues.
	coinTypeLegacyKeyPriv, coinTypeSLIP0044KeyPriv, acctKeyLegacyPriv, acctKeySLIP0044Priv, err := udb.HDKeysFromSeed(seed, params)
	if err != nil {
		return nil, err
	}
	coinTypeLegacyKeyPriv.Zero()
	coinTypeSLIP0044KeyPriv.Zero()
	acctKeyLegacyPriv.Zero()

	return acctKeySLIP0044Priv, nil
}

// ImportVotingAccount imports a voting account to the wallet. A password and
// unique name must be supplied. The xpriv must be for the current running
// network.
func (w *Wallet) ImportVotingAccount(ctx context.Context, xpriv *hdkeychain.ExtendedKey,
	passphrase []byte, name string) (uint32, error) {
	const op errors.Op = "wallet.ImportVotingAccount"
	var accountN uint32
	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		accountN, err = w.manager.ImportVotingAccount(dbtx, xpriv, passphrase, name)
		if err != nil {
			return err
		}
		return err
	})
	if err != nil {
		return 0, errors.E(op, err)
	}

	xpub := xpriv.Neuter()

	extKey, intKey, err := deriveBranches(xpub)
	if err != nil {
		return 0, errors.E(op, err)
	}

	// Internal addresses are used in signing messages and are not expected
	// to be found used on chain.
	if n, err := w.NetworkBackend(); err == nil {
		extAddrs, err := deriveChildAddresses(extKey, 0, w.gapLimit, w.chainParams)
		if err != nil {
			return 0, errors.E(op, err)
		}
		err = n.LoadTxFilter(ctx, false, extAddrs, nil)
		if err != nil {
			return 0, errors.E(op, err)
		}
	}

	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()
	albExternal := addressBuffer{
		branchXpub:  extKey,
		lastUsed:    ^uint32(0),
		cursor:      0,
		lastWatched: w.gapLimit - 1,
	}
	albInternal := albExternal
	albInternal.branchXpub = intKey
	w.addressBuffers[accountN] = &bip0044AccountData{
		xpub:        xpub,
		albExternal: albExternal,
		albInternal: albInternal,
	}

	return accountN, nil
}

func (w *Wallet) ImportXpubAccount(ctx context.Context, name string, xpub *hdkeychain.ExtendedKey) error {
	const op errors.Op = "wallet.ImportXpubAccount"
	if xpub.IsPrivate() {
		return errors.E(op, "extended key must be an xpub")
	}

	extKey, intKey, err := deriveBranches(xpub)
	if err != nil {
		return errors.E(op, err)
	}

	if n, err := w.NetworkBackend(); err == nil {
		extAddrs, err := deriveChildAddresses(extKey, 0, w.gapLimit, w.chainParams)
		if err != nil {
			return errors.E(op, err)
		}
		intAddrs, err := deriveChildAddresses(intKey, 0, w.gapLimit, w.chainParams)
		if err != nil {
			return errors.E(op, err)
		}
		watch := append(extAddrs, intAddrs...)
		err = n.LoadTxFilter(ctx, false, watch, nil)
		if err != nil {
			return errors.E(op, err)
		}
	}

	var account uint32
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
		err := w.manager.ImportXpubAccount(ns, name, xpub)
		if err != nil {
			return err
		}
		account, err = w.manager.LookupAccount(ns, name)
		return err
	})
	if err != nil {
		return errors.E(op, err)
	}

	defer w.addressBuffersMu.Unlock()
	w.addressBuffersMu.Lock()
	albExternal := addressBuffer{
		branchXpub:  extKey,
		lastUsed:    ^uint32(0),
		cursor:      0,
		lastWatched: w.gapLimit - 1,
	}
	albInternal := albExternal
	albInternal.branchXpub = intKey
	w.addressBuffers[account] = &bip0044AccountData{
		xpub:        xpub,
		albExternal: albExternal,
		albInternal: albInternal,
	}

	return nil
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
	return stake.IsSSGen(tx, true) // Yes treasury XXX this is not right when treasury activates
}

func isRevocation(tx *wire.MsgTx) bool {
	// isAutoRevocationsEnabled is set false to keep manually generated
	// revocations still considered to be valid revocations.  Enabling this
	// flag would cause legacy revocations to not be flagged as such.
	// Enabling the flag only adds additional checks, so it's not necessary
	// to make the call twice with the flag enabled and disabled.
	return stake.IsSSRtx(tx, false)
}

func isTreasurySpend(tx *wire.MsgTx) bool {
	return stake.IsTSpend(tx)
}

// hasVotingAuthority returns whether the 0th output of a ticket purchase can be
// spent by a vote or revocation created by this wallet.
func (w *Wallet) hasVotingAuthority(addrmgrNs walletdb.ReadBucket, ticketPurchase *wire.MsgTx) (
	mine, havePrivKey bool, err error) {
	out := ticketPurchase.TxOut[0]
	_, addrs := stdscript.ExtractAddrs(out.Version, out.PkScript, w.chainParams)
	for _, a := range addrs {
		var hash160 *[20]byte
		switch a := a.(type) {
		case stdaddr.Hash160er:
			hash160 = a.Hash160()
		default:
			continue
		}
		if w.manager.ExistsHash160(addrmgrNs, hash160[:]) {
			haveKey, err := w.manager.HavePrivateKey(addrmgrNs, a)
			return true, haveKey, err
		}
	}
	return false, false, nil
}

// StakeInfo collects and returns staking statistics for this wallet.
func (w *Wallet) StakeInfo(ctx context.Context) (*StakeInfoData, error) {
	const op errors.Op = "wallet.StakeInfo"

	var res StakeInfoData

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		tipHash, tipHeight := w.txStore.MainChainTip(dbtx)
		res.BlockHeight = int64(tipHeight)
		if deployments.DCP0001.Active(tipHeight, w.chainParams.Net) {
			tipHeader, err := w.txStore.GetBlockHeader(dbtx, &tipHash)
			if err != nil {
				return err
			}
			sdiff, err := w.nextRequiredDCP0001PoSDifficulty(dbtx, tipHeader, nil)
			if err != nil {
				return err
			}
			res.Sdiff = sdiff
		}
		it := w.txStore.IterateTickets(dbtx)
		defer it.Close()
		for it.Next() {
			// Skip tickets which are not owned by this wallet.
			owned, _, err := w.hasVotingAuthority(addrmgrNs, &it.MsgTx)
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
				spender, err := w.txStore.Tx(txmgrNs, &it.SpenderHash)
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
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		tipHash, tipHeight := w.txStore.MainChainTip(dbtx)
		res.BlockHeight = int64(tipHeight)
		if deployments.DCP0001.Active(tipHeight, w.chainParams.Net) {
			tipHeader, err := w.txStore.GetBlockHeader(dbtx, &tipHash)
			if err != nil {
				return err
			}
			sdiff, err := w.nextRequiredDCP0001PoSDifficulty(dbtx, tipHeader, nil)
			if err != nil {
				return err
			}
			res.Sdiff = sdiff
		}
		it := w.txStore.IterateTickets(dbtx)
		defer it.Close()
		for it.Next() {
			// Skip tickets which are not owned by this wallet.
			owned, _, err := w.hasVotingAuthority(addrmgrNs, &it.MsgTx)
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
				spender, err := w.txStore.Tx(txmgrNs, &it.SpenderHash)
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
		serHeader, err := w.txStore.GetSerializedBlockHeader(txmgrNs, &tipHash)
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
func (w *Wallet) LockedOutpoint(txHash *chainhash.Hash, index uint32) bool {
	op := outpoint{*txHash, index}
	w.lockedOutpointMu.Lock()
	_, locked := w.lockedOutpoints[op]
	w.lockedOutpointMu.Unlock()
	return locked
}

// LockOutpoint marks an outpoint as locked, that is, it should not be used as
// an input for newly created transactions.
func (w *Wallet) LockOutpoint(txHash *chainhash.Hash, index uint32) {
	op := outpoint{*txHash, index}
	w.lockedOutpointMu.Lock()
	w.lockedOutpoints[op] = struct{}{}
	w.lockedOutpointMu.Unlock()
}

// UnlockOutpoint marks an outpoint as unlocked, that is, it may be used as an
// input for newly created transactions.
func (w *Wallet) UnlockOutpoint(txHash *chainhash.Hash, index uint32) {
	op := outpoint{*txHash, index}
	w.lockedOutpointMu.Lock()
	delete(w.lockedOutpoints, op)
	w.lockedOutpointMu.Unlock()
}

// ResetLockedOutpoints resets the set of locked outpoints so all may be used
// as inputs for new transactions.
func (w *Wallet) ResetLockedOutpoints() {
	w.lockedOutpointMu.Lock()
	w.lockedOutpoints = make(map[outpoint]struct{})
	w.lockedOutpointMu.Unlock()
}

// LockedOutpoints returns a slice of currently locked outpoints.  This is
// intended to be used by marshaling the result as a JSON array for
// listlockunspent RPC results.
func (w *Wallet) LockedOutpoints(ctx context.Context, accountName string) ([]dcrdtypes.TransactionInput, error) {
	w.lockedOutpointMu.Lock()
	allLocked := make([]outpoint, len(w.lockedOutpoints))
	i := 0
	for op := range w.lockedOutpoints {
		allLocked[i] = op
		i++
	}
	w.lockedOutpointMu.Unlock()

	allAccts := accountName == "" || accountName == "*"
	acctLocked := make([]dcrdtypes.TransactionInput, 0, len(allLocked))
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		// Outpoints are not validated before they're locked, so
		// invalid outpoints may be locked. Simply ignore.
		for i := range allLocked {
			op := &allLocked[i]
			details, err := w.txStore.TxDetails(txmgrNs, &op.hash)
			if err != nil {
				if errors.Is(err, errors.NotExist) {
					// invalid txid
					continue
				}
				return err
			}
			if int(op.index) >= len(details.MsgTx.TxOut) {
				// valid txid, invalid vout
				continue
			}
			output := details.MsgTx.TxOut[op.index]

			if !allAccts {
				// Lookup the associated account for the output.
				_, addrs := stdscript.ExtractAddrs(output.Version, output.PkScript, w.chainParams)
				if len(addrs) == 0 {
					continue
				}
				var opAcct string
				acct, err := w.manager.AddrAccount(addrmgrNs, addrs[0])
				if err == nil {
					s, err := w.manager.AccountName(addrmgrNs, acct)
					if err == nil {
						opAcct = s
					}
				}
				if opAcct != accountName {
					continue
				}
			}

			var tree int8
			if details.TxType != stake.TxTypeRegular {
				tree = 1
			}
			acctLocked = append(acctLocked, dcrdtypes.TransactionInput{
				Amount: dcrutil.Amount(output.Value).ToCoin(),
				Txid:   op.hash.String(),
				Vout:   op.index,
				Tree:   tree,
			})
		}
		return nil
	})

	return acctLocked, err
}

// UnminedTransactions returns all unmined transactions from the wallet.
// Transactions are sorted in dependency order making it suitable to range them
// in order to broadcast at wallet startup.  This method skips over any
// transactions that are recorded as unpublished.
func (w *Wallet) UnminedTransactions(ctx context.Context) ([]*wire.MsgTx, error) {
	const op errors.Op = "wallet.UnminedTransactions"
	var recs []*udb.TxRecord
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		recs, err = w.txStore.UnminedTxs(dbtx)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	txs := make([]*wire.MsgTx, 0, len(recs))
	for i := range recs {
		if recs[i].Unpublished {
			continue
		}
		txs = append(txs, &recs[i].MsgTx)
	}
	return txs, nil
}

// SortedActivePaymentAddresses returns a slice of all active payment
// addresses in a wallet.
func (w *Wallet) SortedActivePaymentAddresses(ctx context.Context) ([]string, error) {
	const op errors.Op = "wallet.SortedActivePaymentAddresses"
	var addrStrs []string
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		return w.manager.ForEachActiveAddress(addrmgrNs, func(addr stdaddr.Address) error {
			addrStrs = append(addrStrs, addr.String())
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
func (w *Wallet) TotalReceivedForAccounts(ctx context.Context, minConf int32) ([]AccountTotalReceivedResult, error) {
	const op errors.Op = "wallet.TotalReceivedForAccounts"
	var results []AccountTotalReceivedResult
	resultIdxs := make(map[uint32]int)
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.txStore.MainChainTip(dbtx)

		err := w.manager.ForEachAccount(addrmgrNs, func(account uint32) error {
			accountName, err := w.manager.AccountName(addrmgrNs, account)
			if err != nil {
				return err
			}
			resultIdxs[account] = len(resultIdxs)
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
					_, addrs := stdscript.ExtractAddrs(pkVersion, pkScript, w.chainParams)
					if len(addrs) == 0 {
						continue
					}
					outputAcct, err := w.manager.AddrAccount(addrmgrNs, addrs[0])
					if err == nil {
						acctIndex := resultIdxs[outputAcct]
						res := &results[acctIndex]
						res.TotalReceived += cred.Amount
						res.LastConfirmation = confirms(
							detail.Block.Height, tipHeight)
					}
				}
			}
			return false, nil
		}
		return w.txStore.RangeTransactions(ctx, txmgrNs, 0, stopHeight, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return results, nil
}

// TotalReceivedForAddr iterates through a wallet's transaction history,
// returning the total amount of decred received for a single wallet
// address.
func (w *Wallet) TotalReceivedForAddr(ctx context.Context, addr stdaddr.Address, minConf int32) (dcrutil.Amount, error) {
	const op errors.Op = "wallet.TotalReceivedForAddr"
	var amount dcrutil.Amount
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.txStore.MainChainTip(dbtx)

		var (
			addrStr    = addr.String()
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
					_, addrs := stdscript.ExtractAddrs(pkVersion, pkScript, w.chainParams)
					for _, a := range addrs { // no addresses means non-standard credit, ignored
						if addrStr == a.String() {
							amount += cred.Amount
							break
						}
					}
				}
			}
			return false, nil
		}
		return w.txStore.RangeTransactions(ctx, txmgrNs, 0, stopHeight, rangeFn)
	})
	if err != nil {
		return 0, errors.E(op, err)
	}
	return amount, nil
}

// SendOutputs creates and sends payment transactions. It returns the
// transaction hash upon success
func (w *Wallet) SendOutputs(ctx context.Context, outputs []*wire.TxOut, account, changeAccount uint32, minconf int32) (*chainhash.Hash, error) {
	const op errors.Op = "wallet.SendOutputs"
	relayFee := w.RelayFee()
	for _, output := range outputs {
		err := txrules.CheckOutput(output, relayFee)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}

	a := &authorTx{
		outputs:            outputs,
		account:            account,
		changeAccount:      changeAccount,
		minconf:            minconf,
		randomizeChangeIdx: true,
		txFee:              relayFee,
		dontSignTx:         false,
		isTreasury:         false,
	}
	err := w.authorTx(ctx, op, a)
	if err != nil {
		return nil, err
	}
	err = w.recordAuthoredTx(ctx, op, a)
	if err != nil {
		return nil, err
	}
	err = w.publishAndWatch(ctx, op, nil, a.atx.Tx, a.watch)
	if err != nil {
		return nil, err
	}
	hash := a.atx.Tx.TxHash()
	return &hash, nil
}

// transaction hash upon success
func (w *Wallet) SendOutputsToTreasury(ctx context.Context, outputs []*wire.TxOut, account, changeAccount uint32, minconf int32) (*chainhash.Hash, error) {
	const op errors.Op = "wallet.SendOutputsToTreasury"
	relayFee := w.RelayFee()
	for _, output := range outputs {
		err := txrules.CheckOutput(output, relayFee)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}

	a := &authorTx{
		outputs:            outputs,
		account:            account,
		changeAccount:      changeAccount,
		minconf:            minconf,
		randomizeChangeIdx: false,
		txFee:              relayFee,
		dontSignTx:         false,
		isTreasury:         true,
	}
	err := w.authorTx(ctx, op, a)
	if err != nil {
		return nil, err
	}
	err = w.recordAuthoredTx(ctx, op, a)
	if err != nil {
		return nil, err
	}
	err = w.publishAndWatch(ctx, op, nil, a.atx.Tx, a.watch)
	if err != nil {
		return nil, err
	}
	hash := a.atx.Tx.TxHash()
	return &hash, nil
}

// SignatureError records the underlying error when validating a transaction
// input signature.
type SignatureError struct {
	InputIndex uint32
	Error      error
}

type sigDataSource struct {
	key    func(stdaddr.Address) ([]byte, dcrec.SignatureType, bool, error)
	script func(stdaddr.Address) ([]byte, error)
}

func (s sigDataSource) GetKey(a stdaddr.Address) ([]byte, dcrec.SignatureType, bool, error) {
	return s.key(a)
}
func (s sigDataSource) GetScript(a stdaddr.Address) ([]byte, error) { return s.script(a) }

// SignTransaction uses secrets of the wallet, as well as additional secrets
// passed in by the caller, to create and add input signatures to a transaction.
//
// Transaction input script validation is used to confirm that all signatures
// are valid.  For any invalid input, a SignatureError is added to the returns.
// The final error return is reserved for unexpected or fatal errors, such as
// being unable to determine a previous output script to redeem.
//
// The transaction pointed to by tx is modified by this function.
func (w *Wallet) SignTransaction(ctx context.Context, tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScripts map[wire.OutPoint][]byte,
	additionalKeysByAddress map[string]*dcrutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]SignatureError, error) {

	const op errors.Op = "wallet.SignTransaction"

	var doneFuncs []func()
	defer func() {
		for _, f := range doneFuncs {
			f()
		}
	}()

	var signErrors []SignatureError
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		for i, txIn := range tx.TxIn {
			// For an SSGen tx, skip the first input as it is a stake base
			// and doesn't need to be signed.  The transaction is expected
			// to already contain the consensus-validated stakebase script.
			if i == 0 && stake.IsSSGen(tx, true) { // Yes treasury
				continue
			}

			prevOutScript, ok := additionalPrevScripts[txIn.PreviousOutPoint]
			if !ok {
				prevHash := &txIn.PreviousOutPoint.Hash
				prevIndex := txIn.PreviousOutPoint.Index
				txDetails, err := w.txStore.TxDetails(txmgrNs, prevHash)
				if errors.Is(err, errors.NotExist) {
					return errors.Errorf("%v not found", &txIn.PreviousOutPoint)
				} else if err != nil {
					return err
				}
				prevOutScript = txDetails.MsgTx.TxOut[prevIndex].PkScript
			}

			// Set up our callbacks that we pass to txscript so it can
			// look up the appropriate keys and scripts by address.
			var source sigDataSource
			source.key = func(addr stdaddr.Address) ([]byte, dcrec.SignatureType, bool, error) {
				if len(additionalKeysByAddress) != 0 {
					addrStr := addr.String()
					wif, ok := additionalKeysByAddress[addrStr]
					if !ok {
						return nil, 0, false,
							errors.Errorf("no key for address (needed: %v, have %v)",
								addr, additionalKeysByAddress)
					}
					return wif.PrivKey(), dcrec.STEcdsaSecp256k1, true, nil
				}

				key, done, err := w.manager.PrivateKey(addrmgrNs, addr)
				if err != nil {
					return nil, 0, false, err
				}
				doneFuncs = append(doneFuncs, done)
				return key.Serialize(), dcrec.STEcdsaSecp256k1, true, nil
			}
			source.script = func(addr stdaddr.Address) ([]byte, error) {
				// If keys were provided then we can only use the
				// redeem scripts provided with our inputs, too.
				if len(additionalKeysByAddress) != 0 {
					addrStr := addr.String()
					script, ok := p2shRedeemScriptsByAddress[addrStr]
					if !ok {
						return nil, errors.New("no script for " +
							"address")
					}
					return script, nil
				}

				return w.manager.RedeemScript(addrmgrNs, addr)
			}

			// SigHashSingle inputs can only be signed if there's a
			// corresponding output. However this could be already signed,
			// so we always verify the output.
			if (hashType&txscript.SigHashSingle) !=
				txscript.SigHashSingle || i < len(tx.TxOut) {

				script, err := sign.SignTxOutput(w.ChainParams(),
					tx, i, prevOutScript, hashType, source, source, txIn.SignatureScript, true) // Yes treasury
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
				sanityVerifyFlags, scriptVersionAssumed, nil)
			if err == nil {
				err = vm.Execute()
			}
			if err != nil {
				var multisigNotEnoughSigs bool
				if errors.Is(err, txscript.ErrInvalidStackOperation) {
					pkScript := additionalPrevScripts[txIn.PreviousOutPoint]
					class, addr := stdscript.ExtractAddrs(scriptVersionAssumed, pkScript, w.ChainParams())
					if class == stdscript.STScriptHash && len(addr) > 0 {
						redeemScript, _ := source.script(addr[0])
						if stdscript.IsMultiSigScriptV0(redeemScript) {
							multisigNotEnoughSigs = true
						}
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
func (w *Wallet) CreateSignature(ctx context.Context, tx *wire.MsgTx, idx uint32, addr stdaddr.Address,
	hashType txscript.SigHashType, prevPkScript []byte) (sig, pubkey []byte, err error) {
	const op errors.Op = "wallet.CreateSignature"
	var privKey *secp256k1.PrivateKey
	var pubKey *secp256k1.PublicKey
	var done func()
	defer func() {
		if done != nil {
			done()
		}
	}()

	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(waddrmgrNamespaceKey)

		var err error
		privKey, done, err = w.manager.PrivateKey(ns, addr)
		if err != nil {
			return err
		}
		pubKey = privKey.PubKey()
		return nil
	})
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	sig, err = sign.RawTxInSignature(tx, int(idx), prevPkScript, hashType,
		privKey.Serialize(), dcrec.STEcdsaSecp256k1)
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
		rs := stdscript.MultiSigRedeemScriptFromScriptSigV0(in.SignatureScript)
		if rs != nil && w.manager.ExistsHash160(addrmgrNs,
			dcrutil.Hash160(rs)) {
			return true
		}
		if w.txStore.ExistsUTXO(dbtx, &in.PreviousOutPoint) {
			return true
		}
	}
	for _, out := range tx.TxOut {
		_, addrs := stdscript.ExtractAddrs(out.Version, out.PkScript, w.chainParams)
		for _, a := range addrs {
			var hash160 *[20]byte
			switch a := a.(type) {
			case stdaddr.Hash160er:
				hash160 = a.Hash160()
			default:
				continue
			}
			if w.manager.ExistsHash160(addrmgrNs, hash160[:]) {
				return true
			}
		}
	}

	return false
}

// DetermineRelevantTxs splits the given transactions into slices of relevant
// and non-wallet-relevant transactions (respectively).
func (w *Wallet) DetermineRelevantTxs(ctx context.Context, txs ...*wire.MsgTx) ([]*wire.MsgTx, []*wire.MsgTx, error) {
	var relevant, nonRelevant []*wire.MsgTx
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		for _, tx := range txs {
			switch w.isRelevantTx(dbtx, tx) {
			case true:
				relevant = append(relevant, tx)
			default:
				nonRelevant = append(nonRelevant, tx)
			}
		}
		return nil
	})
	return relevant, nonRelevant, err
}

func (w *Wallet) appendRelevantOutpoints(relevant []wire.OutPoint, dbtx walletdb.ReadTx, tx *wire.MsgTx) []wire.OutPoint {
	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

	if relevant == nil {
		relevant = make([]wire.OutPoint, 0, len(tx.TxOut))
	}

	txHash := tx.TxHash()
	op := wire.OutPoint{
		Hash: txHash,
	}
	isTicket := stake.DetermineTxType(tx, true, false) == stake.TxTypeSStx
	var watchedTicketOutputZero bool
	for i, out := range tx.TxOut {
		if isTicket && i > 0 && i&1 == 1 && !watchedTicketOutputZero {
			addr, err := stake.AddrFromSStxPkScrCommitment(out.PkScript, w.chainParams)
			if err != nil {
				continue
			}
			var hash160 *[20]byte
			if addr, ok := addr.(stdaddr.Hash160er); ok {
				hash160 = addr.Hash160()
			}
			if hash160 != nil && w.manager.ExistsHash160(addrmgrNs, hash160[:]) {
				op.Index = 0
				op.Tree = wire.TxTreeStake
				relevant = append(relevant, op)
				watchedTicketOutputZero = true
				continue
			}
		}

		class, addrs := stdscript.ExtractAddrs(out.Version, out.PkScript, w.chainParams)
		tree := wire.TxTreeRegular
		if _, isStake := txrules.StakeSubScriptType(class); isStake {
			tree = wire.TxTreeStake
		}

		for _, a := range addrs {
			var hash160 *[20]byte
			if a, ok := a.(stdaddr.Hash160er); ok {
				hash160 = a.Hash160()
			}
			if hash160 != nil && w.manager.ExistsHash160(addrmgrNs, hash160[:]) {
				op.Index = uint32(i)
				op.Tree = tree
				relevant = append(relevant, op)
				if isTicket && i == 0 {
					watchedTicketOutputZero = true
				}
				break
			}
		}
	}

	return relevant
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
func (w *Wallet) AbandonTransaction(ctx context.Context, hash *chainhash.Hash) error {
	const opf = "wallet.AbandonTransaction(%v)"
	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)
		details, err := w.txStore.TxDetails(ns, hash)
		if err != nil {
			return err
		}
		if details.Block.Height != -1 {
			return errors.E(errors.Invalid, errors.Errorf("transaction %v is mined in main chain", hash))
		}
		return w.txStore.RemoveUnconfirmed(ns, &details.MsgTx, hash)
	})
	if err != nil {
		op := errors.Opf(opf, hash)
		return errors.E(op, err)
	}
	w.NtfnServer.notifyRemovedTransaction(*hash)
	return nil
}

// AllowsHighFees returns whether the wallet is configured to allow or prevent
// the creation and publishing of transactions with very large fees.
func (w *Wallet) AllowsHighFees() bool {
	return w.allowHighFees
}

// PublishTransaction saves (if relevant) and sends the transaction to the
// consensus RPC server so it can be propagated to other nodes and eventually
// mined.  If the send fails, the transaction is not added to the wallet.
//
// This method does not check if a transaction pays high fees or not, and it is
// the caller's responsibility to check this using either the current wallet
// policy or other configuration parameters.  See txrules.TxPaysHighFees for a
// check for insanely high transaction fees.
func (w *Wallet) PublishTransaction(ctx context.Context, tx *wire.MsgTx, n NetworkBackend) (*chainhash.Hash, error) {
	const opf = "wallet.PublishTransaction(%v)"

	txHash := tx.TxHash()

	var relevant bool
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		relevant = w.isRelevantTx(dbtx, tx)
		return nil
	})
	if err != nil {
		op := errors.Opf(opf, &txHash)
		return nil, errors.E(op, err)
	}

	var watchOutPoints []wire.OutPoint
	if relevant {
		txBuf := new(bytes.Buffer)
		txBuf.Grow(tx.SerializeSize())
		if err = tx.Serialize(txBuf); err != nil {
			op := errors.Opf(opf, &txHash)
			return nil, errors.E(op, err)
		}

		w.lockedOutpointMu.Lock()
		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			rec, err := udb.NewTxRecord(txBuf.Bytes(), time.Now())
			if err != nil {
				return err
			}
			watchOutPoints, err = w.processTransactionRecord(ctx, dbtx, rec, nil, nil)
			return err
		})
		w.lockedOutpointMu.Unlock()
		if err != nil {
			op := errors.Opf(opf, &txHash)
			return nil, errors.E(op, err)
		}
	}

	err = n.PublishTransactions(ctx, tx)
	if err != nil {
		if relevant {
			if err := w.AbandonTransaction(ctx, &txHash); err != nil {
				log.Warnf("Failed to abandon unmined transaction: %v", err)
			}
		}
		op := errors.Opf(opf, &txHash)
		return nil, errors.E(op, err)
	}

	if len(watchOutPoints) > 0 {
		err := n.LoadTxFilter(ctx, false, nil, watchOutPoints)
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
	unminedTxs, err := w.UnminedTransactions(ctx)
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
func (w *Wallet) NeedsAccountsSync(ctx context.Context) (bool, error) {
	_, tipHeight := w.MainChainTip(ctx)
	if tipHeight != 0 {
		return false, nil
	}

	needsSync := true
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		lastAcct, err := w.manager.LastAccount(addrmgrNs)
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
		err = w.manager.ForEachAccountAddress(addrmgrNs, 0, func(udb.ManagedAddress) error {
			needsSync = false
			return errBreak
		})
		if errors.Is(err, errBreak) {
			return nil
		}
		return err
	})
	return needsSync, err
}

// ValidatePreDCP0005CFilters verifies that all stored cfilters prior to the
// DCP0005 activation height are the expected ones.
//
// Verification is done by hashing all stored cfilter data and comparing the
// resulting hash to a known, hardcoded hash.
func (w *Wallet) ValidatePreDCP0005CFilters(ctx context.Context) error {
	const op errors.Op = "wallet.ValidatePreDCP0005CFilters"

	// Hardcoded activation heights for mainnet and testnet3. Simnet
	// already follows DCP0005 rules.
	var end *BlockIdentifier
	switch w.chainParams.Net {
	case wire.MainNet:
		end = NewBlockIdentifierFromHeight(validate.DCP0005ActiveHeightMainNet - 1)
	case wire.TestNet3:
		end = NewBlockIdentifierFromHeight(validate.DCP0005ActiveHeightTestNet3 - 1)
	default:
		return errors.E(op, "The current network does not have pre-DCP0005 cfilters")
	}

	// Sum up all the cfilter data.
	hasher := blake256.New()
	rangeFn := func(_ chainhash.Hash, _ [gcs2.KeySize]byte, filter *gcs2.FilterV2) (bool, error) {
		_, err := hasher.Write(filter.Bytes())
		return false, err
	}

	err := w.RangeCFiltersV2(ctx, nil, end, rangeFn)
	if err != nil {
		return errors.E(op, err)
	}

	// Verify against the hardcoded hash.
	var cfsethash chainhash.Hash
	err = cfsethash.SetBytes(hasher.Sum(nil))
	if err != nil {
		return errors.E(op, err)
	}
	err = validate.PreDCP0005CFilterHash(w.chainParams.Net, &cfsethash)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// ImportCFiltersV2 imports the provided v2 cfilters starting at the specified
// block height. Headers for all the provided filters must have already been
// imported into the wallet, otherwise this method fails. Existing filters for
// the respective blocks are overridden.
//
// Note: No validation is performed on the contents of the imported filters.
// Importing filters that do not correspond to the actual contents of a block
// might cause the wallet to miss relevant transactions.
func (w *Wallet) ImportCFiltersV2(ctx context.Context, startBlockHeight int32, filterData [][]byte) error {
	const op errors.Op = "wallet.ImportCFiltersV2"
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		return w.txStore.ImportCFiltersV2(tx, startBlockHeight, filterData)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// Create creates an new wallet, writing it to an empty database.  If the passed
// seed is non-nil, it is used.  Otherwise, a secure random seed of the
// recommended length is generated.
func Create(ctx context.Context, db DB, pubPass, privPass, seed []byte, params *chaincfg.Params) error {
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

	err := udb.Initialize(ctx, db.internal(), params, seed, pubPass, privPass)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// CreateWatchOnly creates a watchonly wallet on the provided db.
func CreateWatchOnly(ctx context.Context, db DB, extendedPubKey string, pubPass []byte, params *chaincfg.Params) error {
	const op errors.Op = "wallet.CreateWatchOnly"
	err := udb.InitializeWatchOnly(ctx, db.internal(), params, extendedPubKey, pubPass)
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
		addrMap[addrs[i].String()] = struct{}{}
	}

	return addrMap, nil
}

// GapLimit returns the currently used gap limit.
func (w *Wallet) GapLimit() uint32 {
	return w.gapLimit
}

// ManualTickets returns whether network syncers should avoid adding ticket
// transactions to the wallet, instead requiring the wallet administrator to
// manually record any tickets.  This can be used to prevent wallets from voting
// using tickets bought by others but which reuse our voting address.
func (w *Wallet) ManualTickets() bool {
	return w.manualTickets
}

// Open loads an already-created wallet from the passed database and namespaces
// configuration options and sets it up it according to the rest of options.
func Open(ctx context.Context, cfg *Config) (*Wallet, error) {
	const op errors.Op = "wallet.Open"
	// Migrate to the unified DB if necessary.
	db := cfg.DB.internal()
	needsMigration, err := udb.NeedsMigration(ctx, db)
	if err != nil {
		return nil, errors.E(op, err)
	}
	if needsMigration {
		err := udb.Migrate(ctx, db, cfg.Params)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}

	// Perform upgrades as necessary.
	err = udb.Upgrade(ctx, db, cfg.PubPassphrase, cfg.Params)
	if err != nil {
		return nil, errors.E(op, err)
	}

	w := &Wallet{
		db: db,

		// StakeOptions
		votingEnabled:      cfg.VotingEnabled,
		addressReuse:       cfg.AddressReuse,
		ticketAddress:      cfg.VotingAddress,
		poolAddress:        cfg.PoolAddress,
		poolFees:           cfg.PoolFees,
		tspends:            make(map[chainhash.Hash]wire.MsgTx),
		tspendPolicy:       make(map[chainhash.Hash]stake.TreasuryVoteT),
		tspendKeyPolicy:    make(map[string]stake.TreasuryVoteT),
		vspTSpendPolicy:    make(map[udb.VSPTSpend]stake.TreasuryVoteT),
		vspTSpendKeyPolicy: make(map[udb.VSPTreasuryKey]stake.TreasuryVoteT),

		// LoaderOptions
		gapLimit:                cfg.GapLimit,
		allowHighFees:           cfg.AllowHighFees,
		accountGapLimit:         cfg.AccountGapLimit,
		disableCoinTypeUpgrades: cfg.DisableCoinTypeUpgrades,
		manualTickets:           cfg.ManualTickets,

		// Chain params
		subsidyCache: blockchain.NewSubsidyCache(cfg.Params),
		chainParams:  cfg.Params,

		lockedOutpoints: make(map[outpoint]struct{}),

		recentlyPublished: make(map[chainhash.Hash]struct{}),

		addressBuffers: make(map[uint32]*bip0044AccountData),

		mixSems: newMixSemaphores(cfg.MixSplitLimit),
	}

	// Open database managers
	w.manager, w.txStore, w.stakeMgr, err = udb.Open(ctx, db, cfg.Params, cfg.PubPassphrase)
	if err != nil {
		return nil, errors.E(op, err)
	}
	log.Infof("Opened wallet") // TODO: log balance? last sync height?

	var vb stake.VoteBits
	var tspendPolicy map[chainhash.Hash]stake.TreasuryVoteT
	var treasuryKeyPolicy map[string]stake.TreasuryVoteT
	var vspTSpendPolicy map[udb.VSPTSpend]stake.TreasuryVoteT
	var vspTreasuryKeyPolicy map[udb.VSPTreasuryKey]stake.TreasuryVoteT
	err = walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrNamespaceKey)
		lastAcct, err := w.manager.LastAccount(ns)
		if err != nil {
			return err
		}
		lastImported, err := w.manager.LastImportedAccount(tx)
		if err != nil {
			return err
		}
		addAccountBuffers := func(acct uint32) error {
			xpub, err := w.manager.AccountExtendedPubKey(tx, acct)
			if err != nil {
				return err
			}
			extKey, intKey, err := deriveBranches(xpub)
			if err != nil {
				return err
			}
			props, err := w.manager.AccountProperties(ns, acct)
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
			return nil
		}
		for acct := uint32(0); acct <= lastAcct; acct++ {
			if err := addAccountBuffers(acct); err != nil {
				return err
			}
		}
		for acct := uint32(udb.ImportedAddrAccount + 1); acct <= lastImported; acct++ {
			if err := addAccountBuffers(acct); err != nil {
				return err
			}
		}

		vb = w.readDBVoteBits(tx)

		tspendPolicy, vspTSpendPolicy, err = w.readDBTreasuryPolicies(tx)
		if err != nil {
			return err
		}
		treasuryKeyPolicy, vspTreasuryKeyPolicy, err = w.readDBTreasuryKeyPolicies(tx)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	w.NtfnServer = newNotificationServer(w)
	w.defaultVoteBits = vb
	w.tspendPolicy = tspendPolicy
	w.tspendKeyPolicy = treasuryKeyPolicy
	w.vspTSpendPolicy = vspTSpendPolicy
	w.vspTSpendKeyPolicy = vspTreasuryKeyPolicy

	w.stakePoolColdAddrs, err = decodeStakePoolColdExtKey(cfg.StakePoolColdExtKey,
		cfg.Params)
	if err != nil {
		return nil, errors.E(op, err)
	}
	w.stakePoolEnabled = len(w.stakePoolColdAddrs) > 0

	// Amounts
	w.relayFee = cfg.RelayFee

	// Record current tip as initialHeight.
	_, w.initialHeight = w.MainChainTip(ctx)

	return w, nil
}

// getCoinjoinTxsSumbByAcct returns a map with key representing the account and
// the sum of coinjoin output transactions from the account.
func (w *Wallet) getCoinjoinTxsSumbByAcct(ctx context.Context) (map[uint32]int, error) {
	const op errors.Op = "wallet.getCoinjoinTxsSumbByAcct"
	coinJoinTxsByAcctSum := make(map[uint32]int)
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		// Get current block.  The block height used for calculating
		// the number of tx confirmations.
		_, tipHeight := w.txStore.MainChainTip(dbtx)
		rangeFn := func(details []udb.TxDetails) (bool, error) {
			for _, detail := range details {
				if detail.TxType != stake.TxTypeRegular {
					continue
				}
				isMixedTx, mixDenom, _ := PossibleCoinJoin(&detail.MsgTx)
				if !isMixedTx {
					continue
				}
				for _, output := range detail.MsgTx.TxOut {
					if mixDenom != output.Value {
						continue
					}
					_, addrs := stdscript.ExtractAddrs(output.Version, output.PkScript, w.chainParams)
					if len(addrs) == 1 {
						acct, err := w.manager.AddrAccount(addrmgrNs, addrs[0])
						// mixed output belongs to wallet.
						if err == nil {
							coinJoinTxsByAcctSum[acct]++
						}
					}
				}
			}
			return false, nil
		}
		return w.txStore.RangeTransactions(ctx, txmgrNs, 0, tipHeight, rangeFn)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	return coinJoinTxsByAcctSum, nil
}

// GetCoinjoinTxsSumbByAcct gets all coinjoin outputs sum by accounts. This is done
// in case we need to guess a mixed account on wallet recovery.
func (w *Wallet) GetCoinjoinTxsSumbByAcct(ctx context.Context) (map[uint32]int, error) {
	const op errors.Op = "wallet.GetCoinjoinTxsSumbByAcct"
	allTxsByAcct, err := w.getCoinjoinTxsSumbByAcct(ctx)
	if err != nil {
		return nil, errors.E(op, err)
	}

	return allTxsByAcct, nil
}

// GetVSPTicketsByFeeStatus returns the ticket hashes of tickets with the
// informed fee status.
func (w *Wallet) GetVSPTicketsByFeeStatus(ctx context.Context, feeStatus int) ([]chainhash.Hash, error) {
	const op errors.Op = "wallet.GetVSPTicketsByFeeStatus"
	tickets := map[chainhash.Hash]*udb.VSPTicket{}
	var err error
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		tickets, err = udb.GetVSPTicketsByFeeStatus(dbtx, feeStatus)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	response := make([]chainhash.Hash, len(tickets))
	i := 0
	for hash := range tickets {
		copy(response[i][:], hash[:])
		i++
	}

	return response, nil
}

// SetPublished sets the informed hash as true or false.
func (w *Wallet) SetPublished(ctx context.Context, hash *chainhash.Hash, published bool) error {
	var err error
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		hash := hash
		err := w.txStore.SetPublished(dbtx, hash, published)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

type VSPTicket struct {
	FeeHash     chainhash.Hash
	FeeTxStatus uint32
	VSPHostID   uint32
	Host        string
	PubKey      []byte
}

// VSPTicketInfo returns the various information for a given vsp ticket
func (w *Wallet) VSPTicketInfo(ctx context.Context, ticketHash *chainhash.Hash) (*VSPTicket, error) {
	var data *udb.VSPTicket
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		data, err = udb.GetVSPTicket(dbtx, *ticketHash)
		if err != nil {
			return err
		}
		return nil
	})
	if err == nil && data == nil {
		err = errors.E(errors.NotExist)
		return nil, err
	} else if data == nil {
		return nil, err
	}
	convertedData := &VSPTicket{
		FeeHash:     data.FeeHash,
		FeeTxStatus: data.FeeTxStatus,
		VSPHostID:   data.VSPHostID,
		Host:        data.Host,
		PubKey:      data.PubKey,
	}
	return convertedData, err
}

// VSPFeeHashForTicket returns the hash of the fee transaction associated with a
// VSP payment.
func (w *Wallet) VSPFeeHashForTicket(ctx context.Context, ticketHash *chainhash.Hash) (chainhash.Hash, error) {
	var feeHash chainhash.Hash
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		data, err := udb.GetVSPTicket(dbtx, *ticketHash)
		if err != nil {
			return err
		}
		feeHash = data.FeeHash
		return nil
	})
	if err == nil && feeHash == (chainhash.Hash{}) {
		err = errors.E(errors.NotExist)
	}
	return feeHash, err
}

// VSPHostForTicket returns the current vsp host associated with VSP Ticket.
func (w *Wallet) VSPHostForTicket(ctx context.Context, ticketHash *chainhash.Hash) (string, error) {
	var host string
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		data, err := udb.GetVSPTicket(dbtx, *ticketHash)
		if err != nil {
			return err
		}
		host = data.Host
		return nil
	})
	if err == nil && host == "" {
		err = errors.E(errors.NotExist)
	}
	return host, err
}

// IsVSPTicketConfirmed returns whether or not a VSP ticket has been confirmed
// by a VSP.
func (w *Wallet) IsVSPTicketConfirmed(ctx context.Context, ticketHash *chainhash.Hash) (bool, error) {
	confirmed := false
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		data, err := udb.GetVSPTicket(dbtx, *ticketHash)
		if err != nil {
			return err
		}
		if data.FeeTxStatus == uint32(udb.VSPFeeProcessConfirmed) {
			confirmed = true
		}
		return nil
	})
	return confirmed, err
}

// UpdateVspTicketFeeToPaid updates a vsp ticket fee status to paid.
// This is needed when finishing the fee payment on VSPs Process.
func (w *Wallet) UpdateVspTicketFeeToPaid(ctx context.Context, ticketHash, feeHash *chainhash.Hash, host string, pubkey []byte) error {
	var err error
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		err = udb.SetVSPTicket(dbtx, ticketHash, &udb.VSPTicket{
			FeeHash:     *feeHash,
			FeeTxStatus: uint32(udb.VSPFeeProcessPaid),
			Host:        host,
			PubKey:      pubkey,
		})
		return err
	})

	return err
}

func (w *Wallet) UpdateVspTicketFeeToStarted(ctx context.Context, ticketHash, feeHash *chainhash.Hash, host string, pubkey []byte) error {
	var err error
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		err = udb.SetVSPTicket(dbtx, ticketHash, &udb.VSPTicket{
			FeeHash:     *feeHash,
			FeeTxStatus: uint32(udb.VSPFeeProcessStarted),
			Host:        host,
			PubKey:      pubkey,
		})
		return err
	})

	return err
}

func (w *Wallet) UpdateVspTicketFeeToErrored(ctx context.Context, ticketHash *chainhash.Hash, host string, pubkey []byte) error {
	var err error
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		err = udb.SetVSPTicket(dbtx, ticketHash, &udb.VSPTicket{
			FeeHash:     chainhash.Hash{},
			FeeTxStatus: uint32(udb.VSPFeeProcessErrored),
			Host:        host,
			PubKey:      pubkey,
		})
		return err
	})

	return err
}

func (w *Wallet) UpdateVspTicketFeeToConfirmed(ctx context.Context, ticketHash, feeHash *chainhash.Hash, host string, pubkey []byte) error {
	var err error
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		err = udb.SetVSPTicket(dbtx, ticketHash, &udb.VSPTicket{
			FeeHash:     *feeHash,
			FeeTxStatus: uint32(udb.VSPFeeProcessConfirmed),
			Host:        host,
			PubKey:      pubkey,
		})
		return err
	})

	return err
}

// ForUnspentUnexpiredTickets performs a function on every unexpired and unspent
// ticket from the wallet.
func (w *Wallet) ForUnspentUnexpiredTickets(ctx context.Context,
	f func(hash *chainhash.Hash) error) error {

	params := w.ChainParams()

	iter := func(ticketSummaries []*TicketSummary, _ *wire.BlockHeader) (bool, error) {
		for _, ticketSummary := range ticketSummaries {
			switch ticketSummary.Status {
			case TicketStatusLive:
			case TicketStatusImmature:
			case TicketStatusUnspent:
			default:
				continue
			}

			ticketHash := *ticketSummary.Ticket.Hash
			err := f(&ticketHash)
			if err != nil {
				return false, err
			}
		}

		return false, nil
	}

	const requiredConfs = 6 + 2
	_, blockHeight := w.MainChainTip(ctx)
	startBlockNum := blockHeight -
		int32(params.TicketExpiry+uint32(params.TicketMaturity)-requiredConfs)
	startBlock := NewBlockIdentifierFromHeight(startBlockNum)
	endBlock := NewBlockIdentifierFromHeight(blockHeight)
	return w.GetTickets(ctx, iter, startBlock, endBlock)
}
