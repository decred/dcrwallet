// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/apperrors"
	walletchain "github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/walletdb"
)

const (
	revocationFeePerKB dcrutil.Amount = 1e6
)

func stakeStoreError(code apperrors.Code, str string, err error) error {
	return apperrors.E{ErrorCode: code, Description: str, Err: err}
}

// sstxRecord is the structure for a stored SStx.
type sstxRecord struct {
	tx          *dcrutil.Tx
	ts          time.Time
	voteBitsSet bool   // Removed in version 3
	voteBits    uint16 // Removed in version 3
	voteBitsExt []byte // Removed in version 3
}

// ssgenRecord is the structure for a stored SSGen tx. There's no
// real reason to store the actual transaction I don't think,
// the inputs and outputs are all predetermined from the block
// height and the original SStx it references.
//
// TODO Store the extended votebits, too.
type ssgenRecord struct {
	blockHash   chainhash.Hash
	blockHeight uint32
	txHash      chainhash.Hash
	voteBits    uint16
	ts          time.Time
}

// ssrtxRecord is the structure for a stored SSRtx. While the
// ssrtx itself does not include the block hash or block height,
// we still preserve that so that we know the block ntfn that
// informed us that the sstx was missed.
type ssrtxRecord struct {
	blockHash   chainhash.Hash
	blockHeight uint32
	txHash      chainhash.Hash
	ts          time.Time
}

// TicketStatus is the current status of a stake pool ticket.
type TicketStatus uint8

const (
	// TSImmatureOrLive indicates that the ticket is either
	// live or immature.
	TSImmatureOrLive = iota

	// TSVoted indicates that the ticket was spent as a vote.
	TSVoted

	// TSMissed indicates that the ticket was spent as a
	// revocation.
	TSMissed
)

// PoolTicket is a 73-byte struct that is used to preserve user's
// ticket information when they have an account at the stake pool.
type PoolTicket struct {
	Ticket       chainhash.Hash
	HeightTicket uint32
	Status       TicketStatus
	HeightSpent  uint32
	SpentBy      chainhash.Hash
}

// StakePoolUser is a list of tickets for a given user (P2SH
// address) in the stake pool.
type StakePoolUser struct {
	Tickets        []*PoolTicket
	InvalidTickets []*chainhash.Hash
}

// StakeStore represents a safely accessible database of
// stake transactions.
type StakeStore struct {
	Params   *chaincfg.Params
	Manager  *Manager
	chainSvr *walletchain.RPCClient

	ownedSStxs map[chainhash.Hash]struct{}
	mtx        sync.RWMutex // only protects ownedSStxs
}

// StakeNotification is the data structure that contains information
// about an SStx (ticket), SSGen (vote), or SSRtx (revocation)
// produced by wallet.
type StakeNotification struct {
	TxType    int8 // These are the same as in staketx.go of stake, but int8
	TxHash    chainhash.Hash
	BlockHash chainhash.Hash // SSGen only
	Height    int32          // SSGen only
	Amount    int64          // SStx only
	SStxIn    chainhash.Hash // SSGen and SSRtx
	VoteBits  uint16         // SSGen only
}

// checkHashInStore checks if a hash exists in ownedSStxs.
func (s *StakeStore) checkHashInStore(hash *chainhash.Hash) bool {
	_, exists := s.ownedSStxs[*hash]
	return exists
}

// CheckHashInStore is the exported version of CheckHashInStore that is
// safe for concurrent access.
func (s *StakeStore) CheckHashInStore(hash *chainhash.Hash) bool {
	s.mtx.RLock()
	exists := s.checkHashInStore(hash)
	s.mtx.RUnlock()
	return exists
}

// addHashToStore adds a hash into ownedSStxs.
func (s *StakeStore) addHashToStore(hash *chainhash.Hash) {
	s.ownedSStxs[*hash] = struct{}{}
}

// insertSStx inserts an SStx into the store.
func (s *StakeStore) insertSStx(ns walletdb.ReadWriteBucket, sstx *dcrutil.Tx) error {
	// If we already have the SStx, no need to
	// try to include twice.
	exists := s.checkHashInStore(sstx.Hash())
	if exists {
		log.Tracef("Attempted to insert SStx %v into the stake store, "+
			"but the SStx already exists.", sstx.Hash())
		return nil
	}
	record := &sstxRecord{
		tx: sstx,
		ts: time.Now(),
	}

	// Add the SStx to the database.
	err := putSStxRecord(ns, record, DBVersion)
	if err != nil {
		return err
	}

	// Add the SStx's hash to the internal list in the store.
	s.addHashToStore(sstx.Hash())

	return nil
}

// InsertSStx is the exported version of insertSStx that is safe for concurrent
// access.
func (s *StakeStore) InsertSStx(ns walletdb.ReadWriteBucket, sstx *dcrutil.Tx) error {
	s.mtx.Lock()
	err := s.insertSStx(ns, sstx)
	s.mtx.Unlock()
	return err
}

// dumpSStxHashes dumps the hashes of all owned SStxs. Note
// that this doesn't use the DB.
func (s *StakeStore) dumpSStxHashes() []chainhash.Hash {
	// Copy the hash list of sstxs. You could pass the pointer
	// directly but you risk that the size of the internal
	// ownedSStxs is later modified while the end user is
	// working with the returned list.
	ownedSStxs := make([]chainhash.Hash, len(s.ownedSStxs))

	itr := 0
	for hash := range s.ownedSStxs {
		ownedSStxs[itr] = hash
		itr++
	}

	return ownedSStxs
}

// DumpSStxHashes returns the hashes of all wallet ticket purchase transactions.
func (s *StakeStore) DumpSStxHashes() ([]chainhash.Hash, error) {
	defer s.mtx.RUnlock()
	s.mtx.RLock()

	return s.dumpSStxHashes(), nil
}

// dumpSStxHashes dumps the hashes of all owned SStxs for some address.
func (s *StakeStore) dumpSStxHashesForAddress(ns walletdb.ReadBucket, addr dcrutil.Address) ([]chainhash.Hash, error) {
	// Extract the HASH160 script hash; if it's not 20 bytes
	// long, return an error.
	hash160 := addr.ScriptAddress()
	if len(hash160) != 20 {
		str := "stake store is closed"
		return nil, stakeStoreError(apperrors.ErrInput, str, nil)
	}
	_, addrIsP2SH := addr.(*dcrutil.AddressScriptHash)

	allTickets := s.dumpSStxHashes()
	var ticketsForAddr []chainhash.Hash

	// Access the database and store the result locally.
	for _, h := range allTickets {
		thisHash160, p2sh, err := fetchSStxRecordSStxTicketHash160(ns, &h, DBVersion)
		if err != nil {
			str := "failure getting ticket 0th out script hashes from db"
			return nil, stakeStoreError(apperrors.ErrDatabase, str, err)
		}
		if addrIsP2SH != p2sh {
			continue
		}

		if bytes.Equal(hash160, thisHash160) {
			ticketsForAddr = append(ticketsForAddr, h)
		}
	}

	return ticketsForAddr, nil
}

// DumpSStxHashesForAddress returns the hashes of all wallet ticket purchase
// transactions for an address.
func (s *StakeStore) DumpSStxHashesForAddress(ns walletdb.ReadBucket, addr dcrutil.Address) ([]chainhash.Hash, error) {
	defer s.mtx.RUnlock()
	s.mtx.RLock()

	return s.dumpSStxHashesForAddress(ns, addr)
}

// sstxAddress returns the address for a given ticket.
func (s *StakeStore) sstxAddress(ns walletdb.ReadBucket, hash *chainhash.Hash) (dcrutil.Address, error) {
	// Access the database and store the result locally.
	thisHash160, p2sh, err := fetchSStxRecordSStxTicketHash160(ns, hash, DBVersion)
	if err != nil {
		str := "failure getting ticket 0th out script hashes from db"
		return nil, stakeStoreError(apperrors.ErrDatabase, str, err)
	}
	var addr dcrutil.Address
	if p2sh {
		addr, err = dcrutil.NewAddressScriptHashFromHash(thisHash160, s.Params)
	} else {
		addr, err = dcrutil.NewAddressPubKeyHash(thisHash160, s.Params, chainec.ECTypeSecp256k1)
	}
	if err != nil {
		str := "failure getting ticket 0th out script hashes from db"
		return nil, stakeStoreError(apperrors.ErrDatabase, str, err)
	}

	return addr, nil
}

// SStxAddress is the exported, concurrency safe version of sstxAddress.
func (s *StakeStore) SStxAddress(ns walletdb.ReadBucket, hash *chainhash.Hash) (dcrutil.Address, error) {
	return s.sstxAddress(ns, hash)
}

// dumpSSGenHashes fetches and returns the entire list of votes generated by
// this wallet, including votes that were produced but were never included in
// the blockchain.
func (s *StakeStore) dumpSSGenHashes(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	var voteList []chainhash.Hash

	// Open the vite records database.
	bucket := ns.NestedReadBucket(ssgenRecordsBucketName)

	// Store each hash sequentially.
	err := bucket.ForEach(func(k []byte, v []byte) error {
		recs, errDeser := deserializeSSGenRecords(v)
		if errDeser != nil {
			return errDeser
		}

		for _, rec := range recs {
			voteList = append(voteList, rec.txHash)
		}
		return nil
	})
	return voteList, err
}

// DumpSSGenHashes is the exported version of dumpSSGenHashes that is safe
// for concurrent access.
func (s *StakeStore) DumpSSGenHashes(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	return s.dumpSSGenHashes(ns)
}

// dumpSSRtxTickets fetches the entire list of tickets spent as revocations
// byt this wallet.
func (s *StakeStore) dumpSSRtxTickets(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	var ticketList []chainhash.Hash

	// Open the revocation records database.
	bucket := ns.NestedReadBucket(ssrtxRecordsBucketName)

	// Store each hash sequentially.
	err := bucket.ForEach(func(k []byte, v []byte) error {
		ticket, errDeser := chainhash.NewHash(k)
		if errDeser != nil {
			return errDeser
		}

		ticketList = append(ticketList, *ticket)
		return nil
	})
	return ticketList, err
}

// DumpSSRtxTickets is the exported version of dumpSSRtxTickets that is safe
// for concurrent access.
func (s *StakeStore) DumpSSRtxTickets(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	return s.dumpSSRtxTickets(ns)
}

// insertSSGen inserts an SSGen record into the DB (keyed to the SStx it
// spends.
func insertSSGen(ns walletdb.ReadWriteBucket, blockHash *chainhash.Hash, blockHeight int64,
	ssgenHash *chainhash.Hash, voteBits uint16, sstxHash *chainhash.Hash) error {

	if blockHeight <= 0 {
		return fmt.Errorf("invalid SSGen block height")
	}

	record := &ssgenRecord{
		*blockHash,
		uint32(blockHeight),
		*ssgenHash,
		voteBits,
		time.Now(),
	}

	// Add the SSGen to the database.
	return putSSGenRecord(ns, sstxHash, record)
}

// InsertSSGen is the exported version of insertSSGen that is safe for
// concurrent access.
func (s *StakeStore) InsertSSGen(ns walletdb.ReadWriteBucket, blockHash *chainhash.Hash, blockHeight int64, ssgenHash *chainhash.Hash, voteBits uint16, sstxHash *chainhash.Hash) error {
	return insertSSGen(ns, blockHash, blockHeight, ssgenHash, voteBits, sstxHash)
}

// signVRTransaction signs a vote (SSGen) or revocation (SSRtx)
// transaction. isSSGen indicates if it is an SSGen; if it's not,
// it's an SSRtx.
func signVRTransaction(m *Manager, ns walletdb.ReadBucket, msgTx *wire.MsgTx, sstx *dcrutil.Tx, isSSGen bool) error {
	txInNumToSign := 0
	hashType := txscript.SigHashAll

	if isSSGen {
		// For an SSGen tx, skip the first input as it is a stake base
		// and doesn't need to be signed.
		msgTx.TxIn[0].SignatureScript = m.chainParams.StakeBaseSigScript
		txInNumToSign = 1
	}

	// Get the script for the OP_SSTX tagged output that we need
	// to sign.
	sstxOutScript := sstx.MsgTx().TxOut[0].PkScript

	// Create a slice of functions to run after the retreived secrets are no
	// longer needed.
	doneFuncs := make([]func(), 0, len(msgTx.TxIn))
	defer func() {
		for _, done := range doneFuncs {
			done()
		}
	}()

	// Set up our callbacks that we pass to dcrscript so it can
	// look up the appropriate keys and scripts by address.
	var getKey txscript.KeyClosure = func(addr dcrutil.Address) (chainec.PrivateKey, bool, error) {
		address, err := m.Address(ns, addr)
		if err != nil {
			return nil, false, err
		}

		pka, ok := address.(ManagedPubKeyAddress)
		if !ok {
			return nil, false, fmt.Errorf("address is not " +
				"a pubkey address")
		}

		key, done, err := m.PrivateKey(ns, addr)
		if err != nil {
			return nil, false, err
		}
		doneFuncs = append(doneFuncs, done)

		return key, pka.Compressed(), nil
	}
	var getScript txscript.ScriptClosure = func(addr dcrutil.Address) ([]byte, error) {
		script, done, err := m.RedeemScript(ns, addr)
		if err != nil {
			return nil, err
		}
		doneFuncs = append(doneFuncs, done)
		return script, nil
	}

	// Attempt to generate the signed txin.
	signedScript, err := txscript.SignTxOutput(
		m.chainParams,
		msgTx,
		txInNumToSign,
		sstxOutScript,
		hashType,
		getKey,
		getScript,
		msgTx.TxIn[txInNumToSign].SignatureScript,
		chainec.ECTypeSecp256k1,
	)
	if err != nil {
		return fmt.Errorf("failed to sign ssgen or "+
			"ssrtx, error: %v", err.Error())
	}

	msgTx.TxIn[txInNumToSign].SignatureScript = signedScript

	// Either it was already signed or we just signed it.
	// Find out if it is completely satisfied or still needs more.
	// Decred: Needed??
	flags := txscript.ScriptBip16
	engine, err := txscript.NewEngine(sstxOutScript,
		msgTx,
		txInNumToSign,
		flags,
		txscript.DefaultScriptVersion,
		nil)
	if err != nil {
		return fmt.Errorf("failed to generate signature script engine for "+
			"ssgen or ssrtx, error: %v", err.Error())
	}
	err = engine.Execute()
	if err != nil {
		return fmt.Errorf("failed to generate correct signature script for "+
			"ssgen or ssrtx: %v", err.Error())
	}

	return nil
}

var subsidyCache *blockchain.SubsidyCache
var initSudsidyCacheOnce sync.Once

// generateVoteScript generates a voting script from the passed VoteBits, for
// use in a vote.
func generateVoteScript(voteBits stake.VoteBits) ([]byte, error) {
	toPush := make([]byte, 2+len(voteBits.ExtendedBits))
	binary.LittleEndian.PutUint16(toPush[0:2], voteBits.Bits)
	copy(toPush[2:], voteBits.ExtendedBits[:])

	blockVBScript, err := txscript.GenerateProvablyPruneableOut(toPush)
	if err != nil {
		return nil, err
	}

	return blockVBScript, nil
}

// generateVoteNtfn creates a new SSGen given a header hash, height, sstx
// tx hash, and votebits and returns a notification.
func (s *StakeStore) generateVoteNtfn(ns walletdb.ReadWriteBucket, waddrmgrNs walletdb.ReadBucket, blockHash *chainhash.Hash, height int64, sstxHash *chainhash.Hash, defaultVoteBits stake.VoteBits, stakePoolEnabled, allowHighFees bool) (*StakeNotification, error) {
	// 1. Fetch the SStx, then calculate all the values we'll need later for
	// the generation of the SSGen tx outputs.
	sstxRecord, err := fetchSStxRecord(ns, sstxHash, DBVersion)
	if err != nil {
		return nil, err
	}
	sstx := sstxRecord.tx
	sstxMsgTx := sstx.MsgTx()

	// The legacy wallet didn't store anything about the voteBits to use.
	// In the case we're loading a legacy wallet and the voteBits are
	// unset, just use the default voteBits as set by the user.
	voteBits := defaultVoteBits

	if stakePoolEnabled && sstxRecord.voteBitsSet {
		voteBits.Bits = sstxRecord.voteBits
		// This is not correct and therefore commented out.  For now, this will set
		// extended voteBits to whatever the default is to the wallet.
		// voteBits.ExtendedBits = sstxRecord.voteBitsExt
	}

	// Store the sstx pubkeyhashes and amounts as found in the transaction
	// outputs.
	// TODO Get information on the allowable fee range for the vote
	// and check to make sure we don't overflow that.
	ssgenPayTypes, ssgenPkhs, sstxAmts, _, _, _ :=
		stake.TxSStxStakeOutputInfo(sstxMsgTx)

	// Get the current reward.
	initSudsidyCacheOnce.Do(func() {
		subsidyCache = blockchain.NewSubsidyCache(height, s.Params)
	})
	stakeVoteSubsidy := blockchain.CalcStakeVoteSubsidy(subsidyCache,
		height, s.Params)

	// Calculate the output values from this data.
	ssgenCalcAmts := stake.CalculateRewards(sstxAmts,
		sstxMsgTx.TxOut[0].Value,
		stakeVoteSubsidy)

	subsidyCache = blockchain.NewSubsidyCache(height, s.Params)
	// 2. Add all transaction inputs to a new transaction after performing
	// some validity checks. First, add the stake base, then the OP_SSTX
	// tagged output.
	msgTx := wire.NewMsgTx()

	// Stakebase.
	stakeBaseOutPoint := wire.NewOutPoint(&chainhash.Hash{},
		uint32(0xFFFFFFFF),
		wire.TxTreeRegular)
	txInStakeBase := wire.NewTxIn(stakeBaseOutPoint, []byte{})
	msgTx.AddTxIn(txInStakeBase)

	// Add the subsidy amount into the input.
	msgTx.TxIn[0].ValueIn = stakeVoteSubsidy

	// SStx tagged output as an OutPoint.
	prevOut := wire.NewOutPoint(sstxHash,
		0, // Index 0
		1) // Tree stake
	txIn := wire.NewTxIn(prevOut, []byte{})
	msgTx.AddTxIn(txIn)

	// 3. Add the OP_RETURN null data pushes of the block header hash,
	// the block height, and votebits, then add all the OP_SSGEN tagged
	// outputs.
	//
	// Block reference output.
	blockRefScript, err := txscript.GenerateSSGenBlockRef(*blockHash,
		uint32(height))
	if err != nil {
		return nil, err
	}
	blockRefOut := wire.NewTxOut(0, blockRefScript)
	msgTx.AddTxOut(blockRefOut)

	// Votebits output.
	blockVBScript, err := generateVoteScript(voteBits)
	if err != nil {
		return nil, err
	}
	blockVBOut := wire.NewTxOut(0, blockVBScript)
	msgTx.AddTxOut(blockVBOut)

	// Add all the SSGen-tagged transaction outputs to the transaction after
	// performing some validity checks.
	for i, ssgenPkh := range ssgenPkhs {
		// Create a new script which pays to the provided address specified in
		// the original ticket tx.
		var ssgenOutScript []byte
		switch ssgenPayTypes[i] {
		case false: // P2PKH
			ssgenOutScript, err = txscript.PayToSSGenPKHDirect(ssgenPkh)
			if err != nil {
				return nil, err
			}
		case true: // P2SH
			ssgenOutScript, err = txscript.PayToSSGenSHDirect(ssgenPkh)
			if err != nil {
				return nil, err
			}
		}

		// Add the txout to our SSGen tx.
		txOut := wire.NewTxOut(ssgenCalcAmts[i], ssgenOutScript)

		msgTx.AddTxOut(txOut)
	}

	// Check to make sure our SSGen was created correctly.
	_, err = stake.IsSSGen(msgTx)
	if err != nil {
		return nil, err
	}

	// Sign the transaction.
	err = signVRTransaction(s.Manager, waddrmgrNs, msgTx, sstx, true)
	if err != nil {
		return nil, err
	}

	// Store the information about the SSGen.
	hash := msgTx.TxHash()
	err = insertSSGen(ns,
		blockHash,
		height,
		&hash,
		voteBits.Bits,
		sstx.Hash())
	if err != nil {
		return nil, err
	}

	// Send the transaction.
	ssgenSha, err := s.chainSvr.SendRawTransaction(msgTx, allowHighFees)
	if err != nil {
		return nil, err
	}

	log.Debugf("Generated SSGen %v, voting on block %v at height %v. "+
		"The ticket used to generate the SSGen was %v.",
		ssgenSha, blockHash, height, sstxHash)

	// Generate a notification to return.
	ntfn := &StakeNotification{
		TxType:    int8(stake.TxTypeSSGen),
		TxHash:    *ssgenSha,
		BlockHash: *blockHash,
		Height:    int32(height),
		Amount:    0,
		SStxIn:    *sstx.Hash(),
		VoteBits:  voteBits.Bits,
	}

	return ntfn, nil
}

func (s *StakeStore) generateVoteTx(ns walletdb.ReadBucket, waddrmgrNs walletdb.ReadBucket, blockHash *chainhash.Hash, height int64, sstxHash *chainhash.Hash, voteBits stake.VoteBits) (*wire.MsgTx, error) {
	// 1. Fetch the SStx, then calculate all the values we'll need later for
	// the generation of the SSGen tx outputs.
	sstxRecord, err := fetchSStxRecord(ns, sstxHash, DBVersion)
	if err != nil {
		return nil, err
	}
	sstx := sstxRecord.tx
	sstxMsgTx := sstx.MsgTx()

	// Store the sstx pubkeyhashes and amounts as found in the transaction
	// outputs.
	// TODO Get information on the allowable fee range for the vote
	// and check to make sure we don't overflow that.
	ssgenPayTypes, ssgenPkhs, sstxAmts, _, _, _ :=
		stake.TxSStxStakeOutputInfo(sstxMsgTx)

	// Get the current reward.
	initSudsidyCacheOnce.Do(func() {
		subsidyCache = blockchain.NewSubsidyCache(height, s.Params)
	})
	stakeVoteSubsidy := blockchain.CalcStakeVoteSubsidy(subsidyCache,
		height, s.Params)

	// Calculate the output values from this data.
	ssgenCalcAmts := stake.CalculateRewards(sstxAmts,
		sstxMsgTx.TxOut[0].Value,
		stakeVoteSubsidy)

	subsidyCache = blockchain.NewSubsidyCache(height, s.Params)
	// 2. Add all transaction inputs to a new transaction after performing
	// some validity checks. First, add the stake base, then the OP_SSTX
	// tagged output.
	msgTx := wire.NewMsgTx()

	// Stakebase.
	stakeBaseOutPoint := wire.NewOutPoint(&chainhash.Hash{},
		uint32(0xFFFFFFFF),
		wire.TxTreeRegular)
	txInStakeBase := wire.NewTxIn(stakeBaseOutPoint, []byte{})
	msgTx.AddTxIn(txInStakeBase)

	// Add the subsidy amount into the input.
	msgTx.TxIn[0].ValueIn = stakeVoteSubsidy

	// SStx tagged output as an OutPoint.
	prevOut := wire.NewOutPoint(sstxHash,
		0, // Index 0
		1) // Tree stake
	txIn := wire.NewTxIn(prevOut, []byte{})
	msgTx.AddTxIn(txIn)

	// 3. Add the OP_RETURN null data pushes of the block header hash,
	// the block height, and votebits, then add all the OP_SSGEN tagged
	// outputs.
	//
	// Block reference output.
	blockRefScript, err := txscript.GenerateSSGenBlockRef(*blockHash,
		uint32(height))
	if err != nil {
		return nil, err
	}
	blockRefOut := wire.NewTxOut(0, blockRefScript)
	msgTx.AddTxOut(blockRefOut)

	// Votebits output.
	blockVBScript, err := generateVoteScript(voteBits)
	if err != nil {
		return nil, err
	}
	blockVBOut := wire.NewTxOut(0, blockVBScript)
	msgTx.AddTxOut(blockVBOut)

	// Add all the SSGen-tagged transaction outputs to the transaction after
	// performing some validity checks.
	for i, ssgenPkh := range ssgenPkhs {
		// Create a new script which pays to the provided address specified in
		// the original ticket tx.
		var ssgenOutScript []byte
		switch ssgenPayTypes[i] {
		case false: // P2PKH
			ssgenOutScript, err = txscript.PayToSSGenPKHDirect(ssgenPkh)
			if err != nil {
				return nil, err
			}
		case true: // P2SH
			ssgenOutScript, err = txscript.PayToSSGenSHDirect(ssgenPkh)
			if err != nil {
				return nil, err
			}
		}

		// Add the txout to our SSGen tx.
		txOut := wire.NewTxOut(ssgenCalcAmts[i], ssgenOutScript)

		msgTx.AddTxOut(txOut)
	}

	// Check to make sure our SSGen was created correctly.
	_, err = stake.IsSSGen(msgTx)
	if err != nil {
		return nil, err
	}

	// Sign the transaction.
	err = signVRTransaction(s.Manager, waddrmgrNs, msgTx, sstx, true)
	return msgTx, err
}

// GenerateVoteTx creates a new vote transaction given a header hash, height,
// sstx tx hash, and votebits.  The vote is not stored in the database.
func (s *StakeStore) GenerateVoteTx(ns walletdb.ReadBucket, waddrmgrNs walletdb.ReadBucket, blockHash *chainhash.Hash, height int64, sstxHash *chainhash.Hash, voteBits stake.VoteBits) (*wire.MsgTx, error) {
	return s.generateVoteTx(ns, waddrmgrNs, blockHash, height, sstxHash, voteBits)
}

// insertSSRtx inserts an SSRtx record into the DB (keyed to the SStx it
// spends.
func (s *StakeStore) insertSSRtx(ns walletdb.ReadWriteBucket, blockHash *chainhash.Hash, blockHeight int64, ssrtxHash *chainhash.Hash, sstxHash *chainhash.Hash) error {

	if blockHeight <= 0 {
		return fmt.Errorf("invalid SSRtx block height")
	}

	record := &ssrtxRecord{
		*blockHash,
		uint32(blockHeight),
		*ssrtxHash,
		time.Now(),
	}

	// Add the SSRtx to the database.
	return putSSRtxRecord(ns, sstxHash, record)
}

// InsertSSRtx is the exported version of insertSSRtx that is safe for
// concurrent access.
func (s *StakeStore) InsertSSRtx(ns walletdb.ReadWriteBucket, blockHash *chainhash.Hash, blockHeight int64, ssrtxHash *chainhash.Hash, sstxHash *chainhash.Hash) error {
	return s.insertSSRtx(ns, blockHash, blockHeight, ssrtxHash, sstxHash)
}

// GetSSRtxs gets a list of SSRtxs that have been generated for some stake
// ticket.
func (s *StakeStore) getSSRtxs(ns walletdb.ReadBucket, sstxHash *chainhash.Hash) ([]*ssrtxRecord, error) {
	return fetchSSRtxRecords(ns, sstxHash)
}

// GenerateRevocation generates a revocation (SSRtx), signs it, and
// submits it by SendRawTransaction. It also stores a record of it
// in the local database.
func (s *StakeStore) generateRevocation(ns walletdb.ReadWriteBucket, waddrmgrNs walletdb.ReadBucket, blockHash *chainhash.Hash,
	height int64, sstxHash *chainhash.Hash, feePerKb dcrutil.Amount, allowHighFees bool) (*StakeNotification, error) {

	// 1. Fetch the SStx, then calculate all the values we'll need later for
	// the generation of the SSRtx tx outputs.
	sstxRecord, err := fetchSStxRecord(ns, sstxHash, DBVersion)
	if err != nil {
		return nil, err
	}
	sstx := sstxRecord.tx

	// Store the sstx pubkeyhashes and amounts as found in the transaction
	// outputs.
	// TODO Get information on the allowable fee range for the revocation
	// and check to make sure we don't overflow that.
	sstxPayTypes, sstxPkhs, sstxAmts, _, _, _ :=
		stake.TxSStxStakeOutputInfo(sstx.MsgTx())
	ssrtxCalcAmts := stake.CalculateRewards(sstxAmts, sstx.MsgTx().TxOut[0].Value,
		int64(0))

	// Calculate the fee to use for this revocation based on the fee
	// per KB that is standard for mainnet.
	revocationSizeEst := estimateSSRtxTxSize(1, len(sstxPkhs))
	revocationFee := txrules.FeeForSerializeSize(revocationFeePerKB,
		revocationSizeEst)

	// 2. Add the only input.
	msgTx := wire.NewMsgTx()

	// SStx tagged output as an OutPoint; reference this as
	// the only input.
	prevOut := wire.NewOutPoint(sstxHash,
		0, // Index 0
		1) // Tree stake
	txIn := wire.NewTxIn(prevOut, []byte{})
	msgTx.AddTxIn(txIn)

	// 3. Add all the OP_SSRTX tagged outputs.

	// Add all the SSRtx-tagged transaction outputs to the transaction after
	// performing some validity checks.
	feeAdded := false
	for i, sstxPkh := range sstxPkhs {
		// Create a new script which pays to the provided address specified in
		// the original ticket tx.
		var ssrtxOutScript []byte
		switch sstxPayTypes[i] {
		case false: // P2PKH
			ssrtxOutScript, err = txscript.PayToSSRtxPKHDirect(sstxPkh)
			if err != nil {
				return nil, err
			}
		case true: // P2SH
			ssrtxOutScript, err = txscript.PayToSSRtxSHDirect(sstxPkh)
			if err != nil {
				return nil, err
			}
		}

		// Add a fee from an output that has enough.
		amt := ssrtxCalcAmts[i]
		if !feeAdded && ssrtxCalcAmts[i] >= int64(revocationFee) {
			amt -= int64(revocationFee)
			feeAdded = true
		}

		// Add the txout to our SSRtx tx.
		txOut := wire.NewTxOut(amt, ssrtxOutScript)
		msgTx.AddTxOut(txOut)
	}

	// Check to make sure our SSRtx was created correctly.
	_, err = stake.IsSSRtx(msgTx)
	if err != nil {
		return nil, err
	}

	// Sign the transaction.
	err = signVRTransaction(s.Manager, waddrmgrNs, msgTx, sstx, false)
	if err != nil {
		return nil, err
	}

	// Store the information about the SSRtx.
	hash := msgTx.TxHash()
	err = s.insertSSRtx(ns,
		blockHash,
		height,
		&hash,
		sstx.Hash())
	if err != nil {
		return nil, err
	}

	// Send the transaction.
	ssrtxHash, err := s.chainSvr.SendRawTransaction(msgTx, allowHighFees)
	if err != nil {
		return nil, err
	}

	log.Debugf("Generated SSRtx %v. The ticket used to "+
		"generate the SSRtx was %v.", ssrtxHash, sstx.Hash())

	// Generate a notification to return.
	ntfn := &StakeNotification{
		TxType:    int8(stake.TxTypeSSRtx),
		TxHash:    *ssrtxHash,
		BlockHash: chainhash.Hash{},
		Height:    0,
		Amount:    0,
		SStxIn:    *sstx.Hash(),
		VoteBits:  0,
	}

	return ntfn, nil
}

// HandleWinningTicketsNtfn scans the list of eligible tickets and, if any
// of these tickets in the sstx store match these tickets, spends them as
// votes.
func (s *StakeStore) HandleWinningTicketsNtfn(ns walletdb.ReadWriteBucket, waddrmgrNs walletdb.ReadBucket, blockHash *chainhash.Hash, blockHeight int64, tickets []*chainhash.Hash, defaultVoteBits stake.VoteBits, stakePoolEnabled, allowHighFees bool) ([]*StakeNotification, error) {
	// Go through the list of tickets and see any of the
	// ones we own match those eligible.
	var ticketsToPull []*chainhash.Hash

	s.mtx.RLock()
	for _, ticket := range tickets {
		if s.checkHashInStore(ticket) {
			ticketsToPull = append(ticketsToPull, ticket)
		}
	}
	defer s.mtx.RUnlock()

	// No matching tickets (boo!), return.
	if len(ticketsToPull) == 0 {
		return nil, nil
	}

	ntfns := make([]*StakeNotification, len(ticketsToPull))
	voteErrors := make([]error, len(ticketsToPull))
	// Matching tickets (yay!), generate some SSGen.
	for i, ticket := range ticketsToPull {
		ntfns[i], voteErrors[i] = s.generateVoteNtfn(ns, waddrmgrNs, blockHash,
			blockHeight, ticket, defaultVoteBits, stakePoolEnabled, allowHighFees)
	}

	errStr := ""
	for i, err := range voteErrors {
		if err != nil {
			errStr += fmt.Sprintf("Error encountered attempting to create "+
				"vote using ticket %v: ", ticketsToPull[i])
			errStr += err.Error()
			errStr += "\n"
		}
	}

	if errStr != "" {
		return nil, fmt.Errorf("%v", errStr)
	}

	return ntfns, nil
}

// HandleMissedTicketsNtfn scans the list of missed tickets and, if any
// of these tickets in the sstx store match these tickets, spends them as
// SSRtx.
func (s *StakeStore) HandleMissedTicketsNtfn(ns walletdb.ReadWriteBucket, waddrmgrNs walletdb.ReadBucket, blockHash *chainhash.Hash,
	blockHeight int64, tickets []*chainhash.Hash, feePerKb dcrutil.Amount, allowHighFees bool) ([]*StakeNotification, error) {

	// Go through the list of tickets and see any of the
	// ones we own match those eligible.
	var ticketsToPull []*chainhash.Hash

	s.mtx.RLock()
	for _, ticket := range tickets {
		if s.checkHashInStore(ticket) {
			ticketsToPull = append(ticketsToPull, ticket)
		}
	}
	s.mtx.RUnlock()

	// No matching tickets, return.
	if len(ticketsToPull) == 0 {
		return nil, nil
	}

	ntfns := make([]*StakeNotification, len(ticketsToPull))
	revocationErrors := make([]error, len(ticketsToPull))
	// Matching tickets, generate some SSRtx.
	for i, ticket := range ticketsToPull {
		ntfns[i], revocationErrors[i] = s.generateRevocation(ns, waddrmgrNs,
			blockHash, blockHeight, ticket, feePerKb, allowHighFees)
	}

	errStr := ""
	for i, err := range revocationErrors {
		if err != nil {
			errStr += fmt.Sprintf("Error encountered attempting to create "+
				"revocation using ticket %v: ", ticketsToPull[i])
			errStr += err.Error()
			errStr += "\n"
		}
	}

	if errStr != "" {
		return nil, fmt.Errorf("%v", errStr)
	}

	return ntfns, nil
}

// updateStakePoolUserTickets updates a stake pool ticket for a given user.
// If the ticket does not currently exist in the database, it adds it. If it
// does exist (the ticket hash exists), it replaces the old record.
func (s *StakeStore) updateStakePoolUserTickets(ns walletdb.ReadWriteBucket, waddrmgrNs walletdb.ReadBucket, user dcrutil.Address, ticket *PoolTicket) error {
	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	return updateStakePoolUserTickets(ns, *scriptHash, ticket)
}

// UpdateStakePoolUserTickets is the exported and concurrency safe form of
// updateStakePoolUserTickets.
func (s *StakeStore) UpdateStakePoolUserTickets(ns walletdb.ReadWriteBucket, waddrmgrNs walletdb.ReadBucket, user dcrutil.Address, ticket *PoolTicket) error {
	return s.updateStakePoolUserTickets(ns, waddrmgrNs, user, ticket)
}

// removeStakePoolUserInvalTickets prepares the user.Address and asks stakedb
// to remove the formerly invalid tickets.
func (s *StakeStore) removeStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address,
	ticket *chainhash.Hash) error {

	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	return removeStakePoolInvalUserTickets(ns, *scriptHash, ticket)
}

// RemoveStakePoolUserInvalTickets is the exported and concurrency safe form of
// removetStakePoolUserInvalTickets.
func (s *StakeStore) RemoveStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address,
	ticket *chainhash.Hash) error {
	return s.removeStakePoolUserInvalTickets(ns, user, ticket)
}

// updateStakePoolUserInvalTickets updates the list of invalid stake pool
// tickets for a given user. If the ticket does not currently exist in the
// database, it adds it.
func (s *StakeStore) updateStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address, ticket *chainhash.Hash) error {
	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	return updateStakePoolInvalUserTickets(ns, *scriptHash, ticket)
}

// UpdateStakePoolUserInvalTickets is the exported and concurrency safe form of
// updateStakePoolUserInvalTickets.
func (s *StakeStore) UpdateStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address, ticket *chainhash.Hash) error {
	return s.updateStakePoolUserInvalTickets(ns, user, ticket)
}

func stakePoolUserInfo(ns walletdb.ReadBucket, user dcrutil.Address) (*StakePoolUser, error) {
	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return nil, stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	stakePoolUser := new(StakePoolUser)

	// Catch missing user errors below and blank out the stake
	// pool user information for the section if the user has
	// no entries.
	missingValidTickets, missingInvalidTickets := false, false

	userTickets, fetchErrVal := fetchStakePoolUserTickets(ns, *scriptHash)
	if fetchErrVal != nil {
		stakeMgrErr, is := fetchErrVal.(apperrors.E)
		if is {
			missingValidTickets = stakeMgrErr.ErrorCode ==
				apperrors.ErrPoolUserTicketsNotFound
		} else {
			return nil, fetchErrVal
		}
	}
	if missingValidTickets {
		userTickets = make([]*PoolTicket, 0)
	}

	invalTickets, fetchErrInval := fetchStakePoolUserInvalTickets(ns,
		*scriptHash)
	if fetchErrInval != nil {
		stakeMgrErr, is := fetchErrInval.(apperrors.E)
		if is {
			missingInvalidTickets = stakeMgrErr.ErrorCode ==
				apperrors.ErrPoolUserInvalTcktsNotFound
		} else {
			return nil, fetchErrInval
		}
	}
	if missingInvalidTickets {
		invalTickets = make([]*chainhash.Hash, 0)
	}

	stakePoolUser.Tickets = userTickets
	stakePoolUser.InvalidTickets = invalTickets

	return stakePoolUser, nil
}

// StakePoolUserInfo returns the stake pool user information for a given stake
// pool user, keyed to their P2SH voting address.
func (s *StakeStore) StakePoolUserInfo(ns walletdb.ReadBucket, user dcrutil.Address) (*StakePoolUser, error) {
	return stakePoolUserInfo(ns, user)
}

// loadManager returns a new stake manager that results from loading it from
// the passed opened database.  The public passphrase is required to decrypt the
// public keys.
func (s *StakeStore) loadOwnedSStxs(ns walletdb.ReadBucket) error {
	// Regenerate the list of tickets.
	// Perform all database lookups in a read-only view.
	ticketList := make(map[chainhash.Hash]struct{})

	// Open the sstx records database.
	bucket := ns.NestedReadBucket(sstxRecordsBucketName)

	// Store each key sequentially.
	err := bucket.ForEach(func(k []byte, v []byte) error {
		var errNewHash error
		var hash *chainhash.Hash

		hash, errNewHash = chainhash.NewHash(k)
		if errNewHash != nil {
			return errNewHash
		}
		ticketList[*hash] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}

	s.ownedSStxs = ticketList
	return nil
}

// SetChainSvr is used to set the chainSvr to a given pointer. Should
// be called after chainSvr is initialized in wallet.
func (s *StakeStore) SetChainSvr(chainSvr *walletchain.RPCClient) {
	s.chainSvr = chainSvr
}

// newStakeStore initializes a new stake store with the given parameters.
func newStakeStore(params *chaincfg.Params, manager *Manager) *StakeStore {
	return &StakeStore{
		Params:     params,
		Manager:    manager,
		chainSvr:   nil,
		ownedSStxs: make(map[chainhash.Hash]struct{}),
	}
}

// openStakeStore loads an existing stake manager from the given namespace,
// waddrmgr, and network parameters.
//
// A ManagerError with an error code of ErrNoExist will be returned if the
// passed manager does not exist in the specified namespace.
func openStakeStore(ns walletdb.ReadBucket, manager *Manager, params *chaincfg.Params) (*StakeStore, error) {
	// Return an error if the manager has NOT already been created in the
	// given database namespace.
	exists := stakeStoreExists(ns)
	if !exists {
		str := "the specified stake store/manager does not exist in db"
		return nil, stakeStoreError(apperrors.ErrNoExist, str, nil)
	}

	ss := newStakeStore(params, manager)

	err := ss.loadOwnedSStxs(ns)
	if err != nil {
		return nil, err
	}

	return ss, nil
}
