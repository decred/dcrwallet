// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"sync"

	"github.com/decred/dcrd/blockchain/stake/v2"
	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// TODO: It would be good to send errors during notification creation to the rpc
// server instead of just logging them here so the client is aware that wallet
// isn't working correctly and notifications are missing.

// TODO: Anything dealing with accounts here is expensive because the database
// is not organized correctly for true account support, but do the slow thing
// instead of the easy thing since the db can be fixed later, and we want the
// api correct now.

// NotificationServer is a server that interested clients may hook into to
// receive notifications of changes in a wallet.  A client is created for each
// registered notification.  Clients are guaranteed to receive messages in the
// order wallet created them, but there is no guaranteed synchronization between
// different clients.
type NotificationServer struct {
	transactions []chan *TransactionNotifications
	// Coalesce transaction notifications since wallet previously did not add
	// mined txs together.  Now it does and this can be rewritten.
	currentTxNtfn     *TransactionNotifications
	accountClients    []chan *AccountNotification
	tipChangedClients []chan *MainTipChangedNotification
	confClients       []*ConfirmationNotificationsClient
	mu                sync.Mutex // Only protects registered clients
	wallet            *Wallet    // smells like hacks
}

func newNotificationServer(wallet *Wallet) *NotificationServer {
	return &NotificationServer{
		wallet: wallet,
	}
}

func lookupInputAccount(dbtx walletdb.ReadTx, w *Wallet, details *udb.TxDetails, deb udb.DebitRecord) uint32 {
	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

	// TODO: Debits should record which account(s?) they
	// debit from so this doesn't need to be looked up.
	prevOP := &details.MsgTx.TxIn[deb.Index].PreviousOutPoint
	prev, err := w.TxStore.TxDetails(txmgrNs, &prevOP.Hash)
	if err != nil {
		log.Errorf("Cannot query previous transaction details for %v: %v", prevOP.Hash, err)
		return 0
	}
	if prev == nil {
		log.Errorf("Missing previous transaction %v", prevOP.Hash)
		return 0
	}
	prevOut := prev.MsgTx.TxOut[prevOP.Index]
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(prevOut.Version, prevOut.PkScript, w.chainParams)
	var inputAcct uint32
	if err == nil && len(addrs) > 0 {
		inputAcct, err = w.Manager.AddrAccount(addrmgrNs, addrs[0])
	}
	if err != nil {
		log.Errorf("Cannot fetch account for previous output %v: %v", prevOP, err)
		inputAcct = 0
	}
	return inputAcct
}

func lookupOutputChain(dbtx walletdb.ReadTx, w *Wallet, details *udb.TxDetails,
	cred udb.CreditRecord) (account uint32, internal bool, address dcrutil.Address,
	amount int64, outputScript []byte) {

	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

	output := details.MsgTx.TxOut[cred.Index]
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(output.Version, output.PkScript, w.chainParams)
	var ma udb.ManagedAddress
	if err == nil && len(addrs) > 0 {
		ma, err = w.Manager.Address(addrmgrNs, addrs[0])
	}
	if err != nil {
		log.Errorf("Cannot fetch account for wallet output: %v", err)
	} else {
		account = ma.Account()
		internal = ma.Internal()
		address = ma.Address()
		amount = output.Value
		outputScript = output.PkScript
	}
	return
}

func makeTxSummary(dbtx walletdb.ReadTx, w *Wallet, details *udb.TxDetails) TransactionSummary {
	serializedTx := details.SerializedTx
	if serializedTx == nil {
		var buf bytes.Buffer
		buf.Grow(details.MsgTx.SerializeSize())
		err := details.MsgTx.Serialize(&buf)
		if err != nil {
			log.Errorf("Transaction serialization: %v", err)
		}
		serializedTx = buf.Bytes()
	}
	var fee dcrutil.Amount
	if len(details.Debits) == len(details.MsgTx.TxIn) {
		for _, deb := range details.Debits {
			fee += deb.Amount
		}
		for _, txOut := range details.MsgTx.TxOut {
			fee -= dcrutil.Amount(txOut.Value)
		}
	}
	var inputs []TransactionSummaryInput
	if len(details.Debits) != 0 {
		inputs = make([]TransactionSummaryInput, len(details.Debits))
		for i, d := range details.Debits {
			inputs[i] = TransactionSummaryInput{
				Index:           d.Index,
				PreviousAccount: lookupInputAccount(dbtx, w, details, d),
				PreviousAmount:  d.Amount,
			}
		}
	}
	outputs := make([]TransactionSummaryOutput, 0, len(details.MsgTx.TxOut))
	for i := range details.MsgTx.TxOut {
		credIndex := len(outputs)
		mine := len(details.Credits) > credIndex && details.Credits[credIndex].Index == uint32(i)
		if !mine {
			continue
		}
		acct, internal, address, amount, outputScript := lookupOutputChain(dbtx, w, details, details.Credits[credIndex])
		output := TransactionSummaryOutput{
			Index:        uint32(i),
			Account:      acct,
			Internal:     internal,
			Amount:       dcrutil.Amount(amount),
			Address:      address,
			OutputScript: outputScript,
		}
		outputs = append(outputs, output)
	}

	var transactionType = TxTransactionType(&details.MsgTx)

	// Use earliest of receive time or block time if the transaction is mined.
	receiveTime := details.Received
	if details.Height() >= 0 && details.Block.Time.Before(receiveTime) {
		receiveTime = details.Block.Time
	}

	return TransactionSummary{
		Hash:        &details.Hash,
		Transaction: serializedTx,
		MyInputs:    inputs,
		MyOutputs:   outputs,
		Fee:         fee,
		Timestamp:   receiveTime.Unix(),
		Type:        transactionType,
	}
}

func makeTicketSummary(ctx context.Context, rpc *dcrd.RPC, dbtx walletdb.ReadTx, w *Wallet, details *udb.TicketDetails) *TicketSummary {
	var ticketStatus = TicketStatusLive

	ticketTransactionDetails := makeTxSummary(dbtx, w, details.Ticket)
	if details.Spender != nil {
		spenderTransactionDetails := makeTxSummary(dbtx, w, details.Spender)
		if details.Spender.TxType == stake.TxTypeSSGen {
			ticketStatus = TicketStatusVoted
		} else if details.Spender.TxType == stake.TxTypeSSRtx {
			ticketStatus = TicketStatusRevoked
		} else if rpc != nil {
			// rpc can be nil if in spv mode
			// Final check to see if ticket was missed otherwise it's live
			live, err := rpc.ExistsLiveTicket(ctx, &details.Ticket.Hash)
			if err != nil {
				log.Errorf("Unable to check if ticket was live for ticket status: %v", &details.Ticket.Hash)
				ticketStatus = TicketStatusUnknown
			} else if !live {
				ticketStatus = TicketStatusMissed
			}
		}
		return &TicketSummary{
			Ticket:  &ticketTransactionDetails,
			Spender: &spenderTransactionDetails,
			Status:  ticketStatus,
		}
	}

	if details.Ticket.Height() == int32(-1) {
		ticketStatus = TicketStatusUnmined
	} else {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		// Check if ticket age is not yet mature
		if !ticketMatured(w.chainParams, details.Ticket.Height(), tipHeight) {
			ticketStatus = TicketStatusImmature
			// Check if ticket age is over TicketExpiry limit and therefore expired
		} else if ticketExpired(w.chainParams, details.Ticket.Height(), tipHeight) {
			ticketStatus = TicketStatusExpired
		}
	}
	return &TicketSummary{
		Ticket:  &ticketTransactionDetails,
		Spender: nil,
		Status:  ticketStatus,
	}
}

func totalBalances(dbtx walletdb.ReadTx, w *Wallet, m map[uint32]dcrutil.Amount) error {
	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	unspent, err := w.TxStore.UnspentOutputs(dbtx.ReadBucket(wtxmgrNamespaceKey))
	if err != nil {
		return err
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
			_, ok := m[outputAcct]
			if ok {
				m[outputAcct] += output.Amount
			}
		}
	}
	return nil
}

func flattenBalanceMap(m map[uint32]dcrutil.Amount) []AccountBalance {
	s := make([]AccountBalance, 0, len(m))
	for k, v := range m {
		s = append(s, AccountBalance{Account: k, TotalBalance: v})
	}
	return s
}

func relevantAccounts(w *Wallet, m map[uint32]dcrutil.Amount, txs []TransactionSummary) {
	for _, tx := range txs {
		for _, d := range tx.MyInputs {
			m[d.PreviousAccount] = 0
		}
		for _, c := range tx.MyOutputs {
			m[c.Account] = 0
		}
	}
}

func (s *NotificationServer) notifyUnminedTransaction(dbtx walletdb.ReadTx, details *udb.TxDetails) {
	defer s.mu.Unlock()
	s.mu.Lock()

	// Sanity check: should not be currently coalescing a notification for
	// mined transactions at the same time that an unmined tx is notified.
	if s.currentTxNtfn != nil {
		log.Tracef("Notifying unmined tx notification while creating notification for blocks")
	}

	clients := s.transactions
	if len(clients) == 0 {
		return
	}

	unminedTxs := []TransactionSummary{makeTxSummary(dbtx, s.wallet, details)}
	unminedHashes, err := s.wallet.TxStore.UnminedTxHashes(dbtx.ReadBucket(wtxmgrNamespaceKey))
	if err != nil {
		log.Errorf("Cannot fetch unmined transaction hashes: %v", err)
		return
	}
	bals := make(map[uint32]dcrutil.Amount)
	relevantAccounts(s.wallet, bals, unminedTxs)
	err = totalBalances(dbtx, s.wallet, bals)
	if err != nil {
		log.Errorf("Cannot determine balances for relevant accounts: %v", err)
		return
	}
	n := &TransactionNotifications{
		UnminedTransactions:      unminedTxs,
		UnminedTransactionHashes: unminedHashes,
		NewBalances:              flattenBalanceMap(bals),
	}
	for _, c := range clients {
		c <- n
	}
}

func (s *NotificationServer) notifyDetachedBlock(hash *chainhash.Hash) {
	defer s.mu.Unlock()
	s.mu.Lock()

	if s.currentTxNtfn == nil {
		s.currentTxNtfn = &TransactionNotifications{}
	}
	s.currentTxNtfn.DetachedBlocks = append(s.currentTxNtfn.DetachedBlocks, hash)
}

func (s *NotificationServer) notifyMinedTransaction(dbtx walletdb.ReadTx, details *udb.TxDetails, block *udb.BlockMeta) {
	defer s.mu.Unlock()
	s.mu.Lock()

	if s.currentTxNtfn == nil {
		s.currentTxNtfn = &TransactionNotifications{}
	}
	n := len(s.currentTxNtfn.AttachedBlocks)
	if n == 0 || s.currentTxNtfn.AttachedBlocks[n-1].Header.BlockHash() != block.Hash {
		return
	}
	txs := &s.currentTxNtfn.AttachedBlocks[n-1].Transactions
	*txs = append(*txs, makeTxSummary(dbtx, s.wallet, details))
}

func (s *NotificationServer) notifyAttachedBlock(dbtx walletdb.ReadTx, block *wire.BlockHeader, blockHash *chainhash.Hash) {
	defer s.mu.Unlock()
	s.mu.Lock()

	if s.currentTxNtfn == nil {
		s.currentTxNtfn = &TransactionNotifications{}
	}

	// Add block details if it wasn't already included for previously
	// notified mined transactions.
	n := len(s.currentTxNtfn.AttachedBlocks)
	if n == 0 || s.currentTxNtfn.AttachedBlocks[n-1].Header.BlockHash() != *blockHash {
		s.currentTxNtfn.AttachedBlocks = append(s.currentTxNtfn.AttachedBlocks, Block{
			Header: block,
		})
	}
}

func (s *NotificationServer) sendAttachedBlockNotification(ctx context.Context) {
	// Avoid work if possible
	s.mu.Lock()
	if len(s.transactions) == 0 {
		s.currentTxNtfn = nil
		s.mu.Unlock()
		return
	}
	currentTxNtfn := s.currentTxNtfn
	s.currentTxNtfn = nil
	s.mu.Unlock()

	// The UnminedTransactions field is intentionally not set.  Since the
	// hashes of all detached blocks are reported, and all transactions
	// moved from a mined block back to unconfirmed are either in the
	// UnminedTransactionHashes slice or don't exist due to conflicting with
	// a mined transaction in the new best chain, there is no possiblity of
	// a new, previously unseen transaction appearing in unconfirmed.

	var (
		w             = s.wallet
		bals          = make(map[uint32]dcrutil.Amount)
		unminedHashes []*chainhash.Hash
	)
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		unminedHashes, err = w.TxStore.UnminedTxHashes(txmgrNs)
		if err != nil {
			return err
		}
		for _, b := range currentTxNtfn.AttachedBlocks {
			relevantAccounts(w, bals, b.Transactions)
		}
		return totalBalances(dbtx, w, bals)

	})
	if err != nil {
		log.Errorf("Failed to construct attached blocks notification: %v", err)
		return
	}

	currentTxNtfn.UnminedTransactionHashes = unminedHashes
	currentTxNtfn.NewBalances = flattenBalanceMap(bals)

	s.mu.Lock()
	for _, c := range s.transactions {
		c <- currentTxNtfn
	}
	s.mu.Unlock()
}

// TransactionNotifications is a notification of changes to the wallet's
// transaction set and the current chain tip that wallet is considered to be
// synced with.  All transactions added to the blockchain are organized by the
// block they were mined in.
//
// During a chain switch, all removed block hashes are included.  Detached
// blocks are sorted in the reverse order they were mined.  Attached blocks are
// sorted in the order mined.
//
// All newly added unmined transactions are included.  Removed unmined
// transactions are not explicitly included.  Instead, the hashes of all
// transactions still unmined are included.
//
// If any transactions were involved, each affected account's new total balance
// is included.
//
// TODO: Because this includes stuff about blocks and can be fired without any
// changes to transactions, it needs a better name.
type TransactionNotifications struct {
	AttachedBlocks           []Block
	DetachedBlocks           []*chainhash.Hash
	UnminedTransactions      []TransactionSummary
	UnminedTransactionHashes []*chainhash.Hash
	NewBalances              []AccountBalance
}

// Block contains the properties and all relevant transactions of an attached
// block.
type Block struct {
	Header       *wire.BlockHeader // Nil if referring to mempool
	Transactions []TransactionSummary
}

// TicketSummary contains the properties to describe a ticket's current status
type TicketSummary struct {
	Ticket  *TransactionSummary
	Spender *TransactionSummary
	Status  TicketStatus
}

// TicketStatus describes the current status a ticket can be observed to be.
type TicketStatus int8

const (
	// TicketStatusUnknown any ticket that its status was unable to be determined.
	TicketStatusUnknown TicketStatus = iota
	// TicketStatusUnmined any not yet mined ticket.
	TicketStatusUnmined
	// TicketStatusImmature any so to be live ticket.
	TicketStatusImmature
	// TicketStatusLive any currently live ticket.
	TicketStatusLive
	// TicketStatusVoted any ticket that was seen to have voted.
	TicketStatusVoted
	// TicketStatusRevoked any ticket that has been previously revoked.
	TicketStatusRevoked
	// TicketStatusMissed any ticket that has yet to be revoked, and was missed.
	TicketStatusMissed
	// TicketStatusExpired any ticket that has yet to be revoked, and was expired.
	TicketStatusExpired
)

// TransactionSummary contains a transaction relevant to the wallet and marks
// which inputs and outputs were relevant.
type TransactionSummary struct {
	Hash        *chainhash.Hash
	Transaction []byte
	MyInputs    []TransactionSummaryInput
	MyOutputs   []TransactionSummaryOutput
	Fee         dcrutil.Amount
	Timestamp   int64
	Type        TransactionType
}

// TransactionType describes the which type of transaction is has been observed to be.
// For instance, if it has a ticket as an input and a stake base reward as an output,
// it is known to be a vote.
type TransactionType int8

const (
	// TransactionTypeRegular transaction type for all regular transactions.
	TransactionTypeRegular TransactionType = iota

	// TransactionTypeCoinbase is the transaction type for all coinbase transactions.
	TransactionTypeCoinbase

	// TransactionTypeTicketPurchase transaction type for all transactions that
	// consume regular transactions as inputs and have commitments for future votes
	// as outputs.
	TransactionTypeTicketPurchase

	// TransactionTypeVote transaction type for all transactions that consume a ticket
	// and also offer a stake base reward output.
	TransactionTypeVote

	// TransactionTypeRevocation transaction type for all transactions that consume a
	// ticket, but offer no stake base reward.
	TransactionTypeRevocation
)

// TxTransactionType returns the correct TransactionType given a wire transaction
func TxTransactionType(tx *wire.MsgTx) TransactionType {
	if blockchain.IsCoinBaseTx(tx) {
		return TransactionTypeCoinbase
	} else if stake.IsSStx(tx) {
		return TransactionTypeTicketPurchase
	} else if stake.IsSSGen(tx) {
		return TransactionTypeVote
	} else if stake.IsSSRtx(tx) {
		return TransactionTypeRevocation
	} else {
		return TransactionTypeRegular
	}
}

// TransactionSummaryInput describes a transaction input that is relevant to the
// wallet.  The Index field marks the transaction input index of the transaction
// (not included here).  The PreviousAccount and PreviousAmount fields describe
// how much this input debits from a wallet account.
type TransactionSummaryInput struct {
	Index           uint32
	PreviousAccount uint32
	PreviousAmount  dcrutil.Amount
}

// TransactionSummaryOutput describes wallet properties of a transaction output
// controlled by the wallet.  The Index field marks the transaction output index
// of the transaction (not included here).
type TransactionSummaryOutput struct {
	Index        uint32
	Account      uint32
	Internal     bool
	Amount       dcrutil.Amount
	Address      dcrutil.Address
	OutputScript []byte
}

// AccountBalance associates a total (zero confirmation) balance with an
// account.  Balances for other minimum confirmation counts require more
// expensive logic and it is not clear which minimums a client is interested in,
// so they are not included.
type AccountBalance struct {
	Account      uint32
	TotalBalance dcrutil.Amount
}

// TransactionNotificationsClient receives TransactionNotifications from the
// NotificationServer over the channel C.
type TransactionNotificationsClient struct {
	C      <-chan *TransactionNotifications
	server *NotificationServer
}

// TransactionNotifications returns a client for receiving
// TransactionNotifiations notifications over a channel.  The channel is
// unbuffered.
//
// When finished, the Done method should be called on the client to disassociate
// it from the server.
func (s *NotificationServer) TransactionNotifications() TransactionNotificationsClient {
	c := make(chan *TransactionNotifications)
	s.mu.Lock()
	s.transactions = append(s.transactions, c)
	s.mu.Unlock()
	return TransactionNotificationsClient{
		C:      c,
		server: s,
	}
}

// Done deregisters the client from the server and drains any remaining
// messages.  It must be called exactly once when the client is finished
// receiving notifications.
func (c *TransactionNotificationsClient) Done() {
	go func() {
		// Drain notifications until the client channel is removed from
		// the server and closed.
		for range c.C {
		}
	}()
	go func() {
		s := c.server
		s.mu.Lock()
		clients := s.transactions
		for i, ch := range clients {
			if c.C == ch {
				clients[i] = clients[len(clients)-1]
				s.transactions = clients[:len(clients)-1]
				close(ch)
				break
			}
		}
		s.mu.Unlock()
	}()
}

// AccountNotification contains properties regarding an account, such as its
// name and the number of derived and imported keys.  When any of these
// properties change, the notification is fired.
type AccountNotification struct {
	AccountNumber    uint32
	AccountName      string
	ExternalKeyCount uint32
	InternalKeyCount uint32
	ImportedKeyCount uint32
}

func (s *NotificationServer) notifyAccountProperties(props *udb.AccountProperties) {
	defer s.mu.Unlock()
	s.mu.Lock()
	clients := s.accountClients
	if len(clients) == 0 {
		return
	}
	n := &AccountNotification{
		AccountNumber:    props.AccountNumber,
		AccountName:      props.AccountName,
		ExternalKeyCount: 0,
		InternalKeyCount: 0,
		ImportedKeyCount: props.ImportedKeyCount,
	}
	// Key counts have to be fudged for BIP0044 accounts a little bit because
	// only the last used child index is saved.  Add the gap limit since these
	// addresses have also been generated and are being watched for transaction
	// activity.
	if props.AccountNumber <= udb.MaxAccountNum {
		n.ExternalKeyCount = minUint32(hdkeychain.HardenedKeyStart,
			props.LastUsedExternalIndex+uint32(s.wallet.gapLimit))
		n.InternalKeyCount = minUint32(hdkeychain.HardenedKeyStart,
			props.LastUsedInternalIndex+uint32(s.wallet.gapLimit))
	}
	for _, c := range clients {
		c <- n
	}
}

// AccountNotificationsClient receives AccountNotifications over the channel C.
type AccountNotificationsClient struct {
	C      chan *AccountNotification
	server *NotificationServer
}

// AccountNotifications returns a client for receiving AccountNotifications over
// a channel.  The channel is unbuffered.  When finished, the client's Done
// method should be called to disassociate the client from the server.
func (s *NotificationServer) AccountNotifications() AccountNotificationsClient {
	c := make(chan *AccountNotification)
	s.mu.Lock()
	s.accountClients = append(s.accountClients, c)
	s.mu.Unlock()
	return AccountNotificationsClient{
		C:      c,
		server: s,
	}
}

// Done deregisters the client from the server and drains any remaining
// messages.  It must be called exactly once when the client is finished
// receiving notifications.
func (c *AccountNotificationsClient) Done() {
	go func() {
		for range c.C {
		}
	}()
	go func() {
		s := c.server
		s.mu.Lock()
		clients := s.accountClients
		for i, ch := range clients {
			if c.C == ch {
				clients[i] = clients[len(clients)-1]
				s.accountClients = clients[:len(clients)-1]
				close(ch)
				break
			}
		}
		s.mu.Unlock()
	}()
}

// MainTipChangedNotification describes processed changes to the main chain tip
// block.  Attached and detached blocks are sorted by increasing heights.
//
// This is intended to be a lightweight alternative to TransactionNotifications
// when only info regarding the main chain tip block changing is needed.
type MainTipChangedNotification struct {
	AttachedBlocks []*chainhash.Hash
	DetachedBlocks []*chainhash.Hash
	NewHeight      int32
}

// MainTipChangedNotificationsClient receives MainTipChangedNotifications over
// the channel C.
type MainTipChangedNotificationsClient struct {
	C      chan *MainTipChangedNotification
	server *NotificationServer
}

// MainTipChangedNotifications returns a client for receiving
// MainTipChangedNotification over a channel.  The channel is unbuffered.  When
// finished, the client's Done method should be called to disassociate the
// client from the server.
func (s *NotificationServer) MainTipChangedNotifications() MainTipChangedNotificationsClient {
	c := make(chan *MainTipChangedNotification)
	s.mu.Lock()
	s.tipChangedClients = append(s.tipChangedClients, c)
	s.mu.Unlock()
	return MainTipChangedNotificationsClient{
		C:      c,
		server: s,
	}
}

// Done deregisters the client from the server and drains any remaining
// messages.  It must be called exactly once when the client is finished
// receiving notifications.
func (c *MainTipChangedNotificationsClient) Done() {
	go func() {
		for range c.C {
		}
	}()
	go func() {
		s := c.server
		s.mu.Lock()
		clients := s.tipChangedClients
		for i, ch := range clients {
			if c.C == ch {
				clients[i] = clients[len(clients)-1]
				s.tipChangedClients = clients[:len(clients)-1]
				close(ch)
				break
			}
		}
		s.mu.Unlock()
	}()
}

func (s *NotificationServer) notifyMainChainTipChanged(n *MainTipChangedNotification) {
	s.mu.Lock()

	for _, c := range s.tipChangedClients {
		c <- n
	}

	if len(s.confClients) > 0 {
		var wg sync.WaitGroup
		wg.Add(len(s.confClients))
		for _, c := range s.confClients {
			c := c
			go func() {
				c.process(n.NewHeight)
				wg.Done()
			}()
		}
		wg.Wait()
	}

	s.mu.Unlock()
}

// ConfirmationNotifications registers a client for confirmation notifications
// from the notification server.
func (s *NotificationServer) ConfirmationNotifications(ctx context.Context) *ConfirmationNotificationsClient {
	c := &ConfirmationNotificationsClient{
		watched: make(map[chainhash.Hash]int32),
		r:       make(chan *confNtfnResult),
		ctx:     ctx,
		s:       s,
	}

	// Register with the server
	s.mu.Lock()
	s.confClients = append(s.confClients, c)
	s.mu.Unlock()

	// Cleanup when caller signals done.
	go func() {
		<-ctx.Done()

		// Remove item from notification server's slice
		s.mu.Lock()
		slice := &s.confClients
		for i, sc := range *slice {
			if c == sc {
				(*slice)[i] = (*slice)[len(*slice)-1]
				*slice = (*slice)[:len(*slice)-1]
				break
			}
		}
		s.mu.Unlock()
	}()

	return c
}

// ConfirmationNotificationsClient provides confirmation notifications of watched
// transactions until the caller's context signals done.  Callers register for
// notifications using Watch and receive notifications by calling Recv.
type ConfirmationNotificationsClient struct {
	watched map[chainhash.Hash]int32
	mu      sync.Mutex

	r   chan *confNtfnResult
	ctx context.Context
	s   *NotificationServer
}

type confNtfnResult struct {
	result []ConfirmationNotification
	err    error
}

// ConfirmationNotification describes the number of confirmations of a single
// transaction, or -1 if the transaction is unknown or removed from the wallet.
// If the transaction is mined (Confirmations >= 1), the block hash and height
// is included.  Otherwise the block hash is nil and the block height is set to
// -1.
type ConfirmationNotification struct {
	TxHash        *chainhash.Hash
	Confirmations int32
	BlockHash     *chainhash.Hash // nil when unmined
	BlockHeight   int32           // -1 when unmined
}

// Watch adds additional transactions to watch and create confirmation results
// for.  Results are immediately created with the current number of
// confirmations and are watched until stopAfter confirmations is met or the
// transaction is unknown or removed from the wallet.
func (c *ConfirmationNotificationsClient) Watch(txHashes []*chainhash.Hash, stopAfter int32) {
	w := c.s.wallet
	r := make([]ConfirmationNotification, 0, len(c.watched))
	err := walletdb.View(c.ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		// cannot range here, txHashes may be modified
		for i := 0; i < len(txHashes); {
			h := txHashes[i]
			height, err := w.TxStore.TxBlockHeight(dbtx, h)
			var confs int32
			switch {
			case errors.Is(err, errors.NotExist):
				confs = -1
			case err != nil:
				return err
			default:
				// Remove tx hash from watching list if tx block has been mined
				// and then invalidated by next block
				if tipHeight > height && height > 0 {
					txDetails, err := w.TxStore.TxDetails(txmgrNs, h)
					if err != nil {
						return err
					}
					_, invalidated := w.TxStore.BlockInMainChain(dbtx, &txDetails.Block.Hash)
					if invalidated {
						confs = -1
						break
					}
				}
				confs = confirms(height, tipHeight)
			}
			r = append(r, ConfirmationNotification{
				TxHash:        h,
				Confirmations: confs,
				BlockHeight:   -1,
			})
			if confs > 0 {
				result := &r[len(r)-1]
				height, err := w.TxStore.TxBlockHeight(dbtx, result.TxHash)
				if err != nil {
					return err
				}
				blockHash, err := w.TxStore.GetMainChainBlockHashForHeight(txmgrNs, height)
				if err != nil {
					return err
				}
				result.BlockHash = &blockHash
				result.BlockHeight = height
			}
			if confs >= stopAfter || confs == -1 {
				// Remove this hash from the slice so it is not added to the
				// watch map.  Do not increment i so this same index is used
				// next iteration with the new hash.
				s := &txHashes
				(*s)[i] = (*s)[len(*s)-1]
				*s = (*s)[:len(*s)-1]
			} else {
				i++
			}
		}
		return nil
	})
	if err != nil {
		r = nil
	}
	c.r <- &confNtfnResult{r, err}

	c.mu.Lock()
	for _, h := range txHashes {
		c.watched[*h] = stopAfter
	}
	c.mu.Unlock()
}

// Recv waits for the next notification.  Returns context.Canceled when the
// context is canceled.
func (c *ConfirmationNotificationsClient) Recv() ([]ConfirmationNotification, error) {
	select {
	case <-c.ctx.Done():
		return nil, context.Canceled
	case r := <-c.r:
		return r.result, r.err
	}
}

func (c *ConfirmationNotificationsClient) process(tipHeight int32) {
	select {
	case <-c.ctx.Done():
		return
	default:
	}

	c.mu.Lock()
	w := c.s.wallet
	r := &confNtfnResult{
		result: make([]ConfirmationNotification, 0, len(c.watched)),
	}
	var unwatch []*chainhash.Hash
	err := walletdb.View(c.ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		for txHash, stopAfter := range c.watched {
			txHash := txHash // copy
			height, err := w.TxStore.TxBlockHeight(dbtx, &txHash)
			var confs int32
			switch {
			case errors.Is(err, errors.NotExist):
				confs = -1
			case err != nil:
				return err
			default:
				// Remove tx hash from watching list if tx block has been mined
				// and then invalidated by next block
				if tipHeight > height && height > 0 {
					txDetails, err := w.TxStore.TxDetails(txmgrNs, &txHash)
					if err != nil {
						return err
					}
					_, invalidated := w.TxStore.BlockInMainChain(dbtx, &txDetails.Block.Hash)
					if invalidated {
						confs = -1
						break
					}
				}
				confs = confirms(height, tipHeight)
			}
			r.result = append(r.result, ConfirmationNotification{
				TxHash:        &txHash,
				Confirmations: confs,
				BlockHeight:   -1,
			})
			if confs > 0 {
				result := &r.result[len(r.result)-1]
				height, err := w.TxStore.TxBlockHeight(dbtx, result.TxHash)
				if err != nil {
					return err
				}
				blockHash, err := w.TxStore.GetMainChainBlockHashForHeight(txmgrNs, height)
				if err != nil {
					return err
				}
				result.BlockHash = &blockHash
				result.BlockHeight = height
			}
			if confs >= stopAfter || confs == -1 {
				unwatch = append(unwatch, &txHash)
			}
		}
		return nil
	})
	if err != nil {
		r.result = nil
		r.err = err
	}
	for _, h := range unwatch {
		delete(c.watched, *h)
	}
	c.mu.Unlock()

	select {
	case c.r <- r:
	case <-c.ctx.Done():
	}
}
