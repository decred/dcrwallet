// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrwallet/v5/chain"
	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/p2p"
	"decred.org/dcrwallet/v5/rpc/client/dcrd"
	"decred.org/dcrwallet/v5/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v5/spv"
	"decred.org/dcrwallet/v5/version"
	"decred.org/dcrwallet/v5/wallet"
	"decred.org/dcrwallet/v5/wallet/txauthor"
	"decred.org/dcrwallet/v5/wallet/txrules"
	"decred.org/dcrwallet/v5/wallet/txsizes"
	"decred.org/dcrwallet/v5/wallet/udb"
	"github.com/decred/dcrd/blockchain/stake/v5"
	blockchain "github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/crypto/rand"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
	"golang.org/x/sync/errgroup"
)

// API version constants
const (
	jsonrpcSemverString = "10.0.0"
	jsonrpcSemverMajor  = 10
	jsonrpcSemverMinor  = 0
	jsonrpcSemverPatch  = 0
)

const (
	// sstxCommitmentString is the string to insert when a verbose
	// transaction output's pkscript type is a ticket commitment.
	sstxCommitmentString = "sstxcommitment"

	// The assumed output script version is defined to assist with refactoring
	// to use actual script versions.
	scriptVersionAssumed = 0
)

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

// the registered rpc handlers
var handlers = map[string]handler{
	"abandontransaction":        {fn: (*Server).abandonTransaction},
	"accountaddressindex":       {fn: (*Server).accountAddressIndex},
	"accountsyncaddressindex":   {fn: (*Server).accountSyncAddressIndex},
	"accountunlocked":           {fn: (*Server).accountUnlocked},
	"addmultisigaddress":        {fn: (*Server).addMultiSigAddress},
	"addtransaction":            {fn: (*Server).addTransaction},
	"auditreuse":                {fn: (*Server).auditReuse},
	"consolidate":               {fn: (*Server).consolidate},
	"createmultisig":            {fn: (*Server).createMultiSig},
	"createnewaccount":          {fn: (*Server).createNewAccount},
	"createrawtransaction":      {fn: (*Server).createRawTransaction},
	"createsignature":           {fn: (*Server).createSignature},
	"debuglevel":                {fn: (*Server).debugLevel},
	"disapprovepercent":         {fn: (*Server).disapprovePercent},
	"discoverusage":             {fn: (*Server).discoverUsage},
	"dumpprivkey":               {fn: (*Server).dumpPrivKey},
	"fundrawtransaction":        {fn: (*Server).fundRawTransaction},
	"getaccount":                {fn: (*Server).getAccount},
	"getaccountaddress":         {fn: (*Server).getAccountAddress},
	"getaddressesbyaccount":     {fn: (*Server).getAddressesByAccount},
	"getbalance":                {fn: (*Server).getBalance},
	"getbestblock":              {fn: (*Server).getBestBlock},
	"getbestblockhash":          {fn: (*Server).getBestBlockHash},
	"getblockcount":             {fn: (*Server).getBlockCount},
	"getblockhash":              {fn: (*Server).getBlockHash},
	"getblockheader":            {fn: (*Server).getBlockHeader},
	"getblock":                  {fn: (*Server).getBlock},
	"getcoinjoinsbyacct":        {fn: (*Server).getcoinjoinsbyacct},
	"getcurrentnet":             {fn: (*Server).getCurrentNet},
	"getinfo":                   {fn: (*Server).getInfo},
	"getmasterpubkey":           {fn: (*Server).getMasterPubkey},
	"getmultisigoutinfo":        {fn: (*Server).getMultisigOutInfo},
	"getnewaddress":             {fn: (*Server).getNewAddress},
	"getpeerinfo":               {fn: (*Server).getPeerInfo},
	"getrawchangeaddress":       {fn: (*Server).getRawChangeAddress},
	"getreceivedbyaccount":      {fn: (*Server).getReceivedByAccount},
	"getreceivedbyaddress":      {fn: (*Server).getReceivedByAddress},
	"getstakeinfo":              {fn: (*Server).getStakeInfo},
	"gettickets":                {fn: (*Server).getTickets},
	"gettransaction":            {fn: (*Server).getTransaction},
	"gettxout":                  {fn: (*Server).getTxOut},
	"getunconfirmedbalance":     {fn: (*Server).getUnconfirmedBalance},
	"getvotechoices":            {fn: (*Server).getVoteChoices},
	"getwalletfee":              {fn: (*Server).getWalletFee},
	"help":                      {fn: (*Server).help},
	"getcfilterv2":              {fn: (*Server).getCFilterV2},
	"importcfiltersv2":          {fn: (*Server).importCFiltersV2},
	"importprivkey":             {fn: (*Server).importPrivKey},
	"importpubkey":              {fn: (*Server).importPubKey},
	"importscript":              {fn: (*Server).importScript},
	"importxpub":                {fn: (*Server).importXpub},
	"listaccounts":              {fn: (*Server).listAccounts},
	"listaddresstransactions":   {fn: (*Server).listAddressTransactions},
	"listalltransactions":       {fn: (*Server).listAllTransactions},
	"listlockunspent":           {fn: (*Server).listLockUnspent},
	"listreceivedbyaccount":     {fn: (*Server).listReceivedByAccount},
	"listreceivedbyaddress":     {fn: (*Server).listReceivedByAddress},
	"listsinceblock":            {fn: (*Server).listSinceBlock},
	"listtransactions":          {fn: (*Server).listTransactions},
	"listunspent":               {fn: (*Server).listUnspent},
	"lockaccount":               {fn: (*Server).lockAccount},
	"lockunspent":               {fn: (*Server).lockUnspent},
	"mixaccount":                {fn: (*Server).mixAccount},
	"mixoutput":                 {fn: (*Server).mixOutput},
	"purchaseticket":            {fn: (*Server).purchaseTicket},
	"processunmanagedticket":    {fn: (*Server).processUnmanagedTicket},
	"redeemmultisigout":         {fn: (*Server).redeemMultiSigOut},
	"redeemmultisigouts":        {fn: (*Server).redeemMultiSigOuts},
	"renameaccount":             {fn: (*Server).renameAccount},
	"rescanwallet":              {fn: (*Server).rescanWallet},
	"sendfrom":                  {fn: (*Server).sendFrom},
	"sendfromtreasury":          {fn: (*Server).sendFromTreasury},
	"sendmany":                  {fn: (*Server).sendMany},
	"sendrawtransaction":        {fn: (*Server).sendRawTransaction},
	"sendtoaddress":             {fn: (*Server).sendToAddress},
	"sendtomultisig":            {fn: (*Server).sendToMultiSig},
	"sendtotreasury":            {fn: (*Server).sendToTreasury},
	"setaccountpassphrase":      {fn: (*Server).setAccountPassphrase},
	"setdisapprovepercent":      {fn: (*Server).setDisapprovePercent},
	"settreasurypolicy":         {fn: (*Server).setTreasuryPolicy},
	"settspendpolicy":           {fn: (*Server).setTSpendPolicy},
	"settxfee":                  {fn: (*Server).setTxFee},
	"setvotechoice":             {fn: (*Server).setVoteChoice},
	"signmessage":               {fn: (*Server).signMessage},
	"signrawtransaction":        {fn: (*Server).signRawTransaction},
	"signrawtransactions":       {fn: (*Server).signRawTransactions},
	"spendoutputs":              {fn: (*Server).spendOutputs},
	"sweepaccount":              {fn: (*Server).sweepAccount},
	"syncstatus":                {fn: (*Server).syncStatus},
	"ticketinfo":                {fn: (*Server).ticketInfo},
	"treasurypolicy":            {fn: (*Server).treasuryPolicy},
	"tspendpolicy":              {fn: (*Server).tspendPolicy},
	"unlockaccount":             {fn: (*Server).unlockAccount},
	"validateaddress":           {fn: (*Server).validateAddress},
	"validatepredcp0005cf":      {fn: (*Server).validatePreDCP0005CF},
	"verifymessage":             {fn: (*Server).verifyMessage},
	"version":                   {fn: (*Server).version},
	"walletinfo":                {fn: (*Server).walletInfo},
	"walletislocked":            {fn: (*Server).walletIsLocked},
	"walletlock":                {fn: (*Server).walletLock},
	"walletpassphrase":          {fn: (*Server).walletPassphrase},
	"walletpassphrasechange":    {fn: (*Server).walletPassphraseChange},
	"walletpubpassphrasechange": {fn: (*Server).walletPubPassphraseChange},

	// Unimplemented/unsupported RPCs which may be found in other
	// cryptocurrency wallets.
	"backupwallet":         {fn: unimplemented, noHelp: true},
	"getwalletinfo":        {fn: unimplemented, noHelp: true},
	"importwallet":         {fn: unimplemented, noHelp: true},
	"listaddressgroupings": {fn: unimplemented, noHelp: true},
	"dumpwallet":           {fn: unsupported, noHelp: true},
	"encryptwallet":        {fn: unsupported, noHelp: true},
	"move":                 {fn: unsupported, noHelp: true},
	"setaccount":           {fn: unsupported, noHelp: true},
}

// unimplemented handles an unimplemented RPC request with the
// appropriate error.
func unimplemented(*Server, context.Context, any) (any, error) {
	return nil, &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCUnimplemented,
		Message: "Method unimplemented",
	}
}

// unsupported handles a standard bitcoind RPC request which is
// unsupported by dcrwallet due to design differences.
func unsupported(*Server, context.Context, any) (any, error) {
	return nil, &dcrjson.RPCError{
		Code:    -1,
		Message: "Request unsupported by dcrwallet",
	}
}

// lazyHandler is a closure over a requestHandler or passthrough request with
// the RPC server's wallet and chain server variables as part of the closure
// context.
type lazyHandler func() (any, *dcrjson.RPCError)

// lazyApplyHandler looks up the best request handler func for the method,
// returning a closure that will execute it with the (required) wallet and
// (optional) consensus RPC server.  If no handlers are found and the
// chainClient is not nil, the returned handler performs RPC passthrough.
func lazyApplyHandler(s *Server, ctx context.Context, request *dcrjson.Request) lazyHandler {
	handlerData, ok := handlers[request.Method]
	if !ok {
		return func() (any, *dcrjson.RPCError) {
			// Attempt RPC passthrough if possible
			n, ok := s.walletLoader.NetworkBackend()
			if !ok {
				return nil, errRPCClientNotConnected
			}
			chainSyncer, ok := n.(*chain.Syncer)
			if !ok {
				return nil, rpcErrorf(dcrjson.ErrRPCClientNotConnected, "RPC passthrough requires dcrd RPC synchronization")
			}
			var resp json.RawMessage
			var params = make([]any, len(request.Params))
			for i := range request.Params {
				params[i] = request.Params[i]
			}
			err := chainSyncer.RPC().Call(ctx, request.Method, &resp, params...)
			if ctx.Err() != nil {
				log.Warnf("Canceled RPC method %v invoked by %v: %v", request.Method, remoteAddr(ctx), err)
				return nil, &dcrjson.RPCError{
					Code:    dcrjson.ErrRPCMisc,
					Message: ctx.Err().Error(),
				}
			}
			if err != nil {
				return nil, convertError(err)
			}
			return resp, nil
		}
	}

	return func() (any, *dcrjson.RPCError) {
		params, err := dcrjson.ParseParams(types.Method(request.Method), request.Params)
		if err != nil {
			return nil, dcrjson.ErrRPCInvalidRequest
		}

		defer func() {
			if err := ctx.Err(); err != nil {
				log.Warnf("Canceled RPC method %v invoked by %v: %v", request.Method, remoteAddr(ctx), err)
			}
		}()
		resp, err := handlerData.fn(s, ctx, params)
		if err != nil {
			return nil, convertError(err)
		}
		return resp, nil
	}
}

// makeResponse makes the JSON-RPC response struct for the result and error
// returned by a requestHandler.  The returned response is not ready for
// marshaling and sending off to a client, but must be
func makeResponse(id, result any, err error) dcrjson.Response {
	idPtr := idPointer(id)
	if err != nil {
		return dcrjson.Response{
			ID:    idPtr,
			Error: convertError(err),
		}
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return dcrjson.Response{
			ID: idPtr,
			Error: &dcrjson.RPCError{
				Code:    dcrjson.ErrRPCInternal.Code,
				Message: "Unexpected error marshalling result",
			},
		}
	}
	return dcrjson.Response{
		ID:     idPtr,
		Result: json.RawMessage(resultBytes),
	}
}

// abandonTransaction removes an unconfirmed transaction and all dependent
// transactions from the wallet.
func (s *Server) abandonTransaction(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.AbandonTransactionCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	hash, err := chainhash.NewHashFromStr(cmd.Hash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	err = w.AbandonTransaction(ctx, hash)
	return nil, err
}

// accountAddressIndex returns the next address index for the passed
// account and branch.
func (s *Server) accountAddressIndex(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.AccountAddressIndexCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	extChild, intChild, err := w.BIP0044BranchNextIndexes(ctx, account)
	if err != nil {
		return nil, err
	}
	switch uint32(cmd.Branch) {
	case udb.ExternalBranch:
		return extChild, nil
	case udb.InternalBranch:
		return intChild, nil
	default:
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "invalid branch %v", cmd.Branch)
	}
}

// accountSyncAddressIndex synchronizes the address manager and local address
// pool for some account and branch to the passed index. If the current pool
// index is beyond the passed index, an error is returned. If the passed index
// is the same as the current pool index, nothing is returned. If the syncing
// is successful, nothing is returned.
func (s *Server) accountSyncAddressIndex(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.AccountSyncAddressIndexCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	branch := uint32(cmd.Branch)
	index := uint32(cmd.Index)

	if index >= hdkeychain.HardenedKeyStart {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
			"child index %d exceeds the maximum child index for an account", index)
	}

	// Additional addresses need to be watched.  Since addresses are derived
	// based on the last used address, this RPC no longer changes the child
	// indexes that new addresses are derived from.
	return nil, w.SyncLastReturnedAddress(ctx, account, branch, index)
}

// walletPubKeys decodes each encoded key or address to a public key.  If the
// address is P2PKH, the wallet is queried for the public key.
func walletPubKeys(ctx context.Context, w *wallet.Wallet, keys []string) ([][]byte, error) {
	pubKeys := make([][]byte, len(keys))

	for i, key := range keys {
		addr, err := decodeAddress(key, w.ChainParams())
		if err != nil {
			return nil, err
		}
		switch addr := addr.(type) {
		case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
			pubKeys[i] = addr.SerializedPubKey()
			continue
		}

		a, err := w.KnownAddress(ctx, addr)
		if err != nil {
			if errors.Is(err, errors.NotExist) {
				return nil, errAddressNotInWallet
			}
			return nil, err
		}
		var pubKey []byte
		switch a := a.(type) {
		case wallet.PubKeyHashAddress:
			pubKey = a.PubKey()
		default:
			err = errors.New("address has no associated public key")
			return nil, rpcError(dcrjson.ErrRPCInvalidAddressOrKey, err)
		}
		pubKeys[i] = pubKey
	}

	return pubKeys, nil
}

// addMultiSigAddress handles an addmultisigaddress request by adding a
// multisig address to the given wallet.
func (s *Server) addMultiSigAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.AddMultisigAddressCmd)
	// If an account is specified, ensure that is the imported account.
	if cmd.Account != nil && *cmd.Account != udb.ImportedAddrAccountName {
		return nil, errNotImportedAccount
	}

	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	pubKeyAddrs, err := walletPubKeys(ctx, w, cmd.Keys)
	if err != nil {
		return nil, err
	}
	script, err := stdscript.MultiSigScriptV0(cmd.NRequired, pubKeyAddrs...)
	if err != nil {
		return nil, err
	}

	err = w.ImportScript(ctx, script)
	if err != nil && !errors.Is(err, errors.Exist) {
		return nil, err
	}

	return stdaddr.NewAddressScriptHashV0(script, w.ChainParams())
}

func (s *Server) addTransaction(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.AddTransactionCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	blockHash, err := chainhash.NewHashFromStr(cmd.BlockHash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	tx := new(wire.MsgTx)
	err = tx.Deserialize(hex.NewDecoder(strings.NewReader(cmd.Transaction)))
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDeserialization, err)
	}

	err = w.AddTransaction(ctx, tx, blockHash)
	return nil, err
}

// auditReuse returns an object keying reused addresses to two or more outputs
// referencing them.
func (s *Server) auditReuse(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.AuditReuseCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var since int32
	if cmd.Since != nil {
		since = *cmd.Since
	}

	reuse := make(map[string][]string)
	inRange := make(map[string]struct{})
	params := w.ChainParams()
	err := w.GetTransactions(ctx, func(b *wallet.Block) (bool, error) {
		for _, tx := range b.Transactions {
			// Votes and revocations are skipped because they must
			// only pay to addresses previously committed to by
			// ticket purchases, and this "address reuse" is
			// expected.
			switch tx.Type {
			case wallet.TransactionTypeVote, wallet.TransactionTypeRevocation:
				continue
			}
			for _, out := range tx.MyOutputs {
				addr := out.Address.String()
				outpoints := reuse[addr]
				outpoint := wire.OutPoint{Hash: *tx.Hash, Index: out.Index}
				reuse[addr] = append(outpoints, outpoint.String())
				if b.Header == nil || int32(b.Header.Height) >= since {
					inRange[addr] = struct{}{}
				}
			}
			if tx.Type != wallet.TransactionTypeTicketPurchase {
				continue
			}
			ticket := new(wire.MsgTx)
			err := ticket.Deserialize(bytes.NewReader(tx.Transaction))
			if err != nil {
				return false, err
			}
			for i := 1; i < len(ticket.TxOut); i += 2 { // iterate commitments
				out := ticket.TxOut[i]
				addr, err := stake.AddrFromSStxPkScrCommitment(out.PkScript, params)
				if err != nil {
					return false, err
				}
				have, err := w.HaveAddress(ctx, addr)
				if err != nil {
					return false, err
				}
				if !have {
					continue
				}
				s := addr.String()
				outpoints := reuse[s]
				outpoint := wire.OutPoint{Hash: *tx.Hash, Index: uint32(i)}
				reuse[s] = append(outpoints, outpoint.String())
				if b.Header == nil || int32(b.Header.Height) >= since {
					inRange[s] = struct{}{}
				}
			}
		}
		return false, nil
	}, nil, nil)
	if err != nil {
		return nil, err
	}
	for s, outpoints := range reuse {
		if len(outpoints) <= 1 {
			delete(reuse, s)
			continue
		}
		if _, ok := inRange[s]; !ok {
			delete(reuse, s)
		}
	}
	return reuse, nil
}

// consolidate handles a consolidate request by returning attempting to compress
// as many inputs as given and then returning the txHash and error.
func (s *Server) consolidate(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ConsolidateCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account := uint32(udb.DefaultAccountNum)
	var err error
	if cmd.Account != nil {
		account, err = w.AccountNumber(ctx, *cmd.Account)
		if err != nil {
			if errors.Is(err, errors.NotExist) {
				return nil, errAccountNotFound
			}
			return nil, err
		}
	}

	// Set change address if specified.
	var changeAddr stdaddr.Address
	if cmd.Address != nil {
		if *cmd.Address != "" {
			addr, err := decodeAddress(*cmd.Address, w.ChainParams())
			if err != nil {
				return nil, err
			}
			changeAddr = addr
		}
	}

	// TODO In the future this should take the optional account and
	// only consolidate UTXOs found within that account.
	txHash, err := w.Consolidate(ctx, cmd.Inputs, account, changeAddr)
	if err != nil {
		return nil, err
	}

	return txHash.String(), nil
}

// createMultiSig handles an createmultisig request by returning a
// multisig address for the given inputs.
func (s *Server) createMultiSig(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.CreateMultisigCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	pubKeys, err := walletPubKeys(ctx, w, cmd.Keys)
	if err != nil {
		return nil, err
	}
	script, err := stdscript.MultiSigScriptV0(cmd.NRequired, pubKeys...)
	if err != nil {
		return nil, err
	}

	address, err := stdaddr.NewAddressScriptHashV0(script, w.ChainParams())
	if err != nil {
		return nil, err
	}

	return types.CreateMultiSigResult{
		Address:      address.String(),
		RedeemScript: hex.EncodeToString(script),
	}, nil
}

// createRawTransaction handles createrawtransaction commands.
func (s *Server) createRawTransaction(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.CreateRawTransactionCmd)

	// Validate expiry, if given.
	if cmd.Expiry != nil && *cmd.Expiry < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "Expiry out of range")
	}

	// Validate the locktime, if given.
	if cmd.LockTime != nil &&
		(*cmd.LockTime < 0 ||
			*cmd.LockTime > int64(wire.MaxTxInSequenceNum)) {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "Locktime out of range")
	}

	// Add all transaction inputs to a new transaction after performing
	// some validity checks.
	mtx := wire.NewMsgTx()
	for _, input := range cmd.Inputs {
		txHash, err := chainhash.NewHashFromStr(input.Txid)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}

		switch input.Tree {
		case wire.TxTreeRegular, wire.TxTreeStake:
		default:
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Tx tree must be regular or stake")
		}

		amt, err := dcrutil.NewAmount(input.Amount)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
		if amt < 0 {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Positive input amount is required")
		}

		prevOut := wire.NewOutPoint(txHash, input.Vout, input.Tree)
		txIn := wire.NewTxIn(prevOut, int64(amt), nil)
		if cmd.LockTime != nil && *cmd.LockTime != 0 {
			txIn.Sequence = wire.MaxTxInSequenceNum - 1
		}
		mtx.AddTxIn(txIn)
	}

	// Add all transaction outputs to the transaction after performing
	// some validity checks.
	for encodedAddr, amount := range cmd.Amounts {
		// Decode the provided address.  This also ensures the network encoded
		// with the address matches the network the server is currently on.
		addr, err := stdaddr.DecodeAddress(encodedAddr, s.activeNet)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey,
				"Address %q: %v", encodedAddr, err)
		}

		// Ensure the address is one of the supported types.
		switch addr.(type) {
		case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
		case *stdaddr.AddressScriptHashV0:
		default:
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey,
				"Invalid type: %T", addr)
		}

		// Create a new script which pays to the provided address.
		vers, pkScript := addr.PaymentScript()

		atomic, err := dcrutil.NewAmount(amount)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code,
				"New amount: %v", err)
		}
		// Ensure amount is in the valid range for monetary amounts.
		if atomic <= 0 || atomic > dcrutil.MaxAmount {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Amount outside valid range: %v", atomic)
		}

		txOut := &wire.TxOut{
			Value:    int64(atomic),
			Version:  vers,
			PkScript: pkScript,
		}
		mtx.AddTxOut(txOut)
	}

	// Set the Locktime, if given.
	if cmd.LockTime != nil {
		mtx.LockTime = uint32(*cmd.LockTime)
	}

	// Set the Expiry, if given.
	if cmd.Expiry != nil {
		mtx.Expiry = uint32(*cmd.Expiry)
	}

	// Return the serialized and hex-encoded transaction.
	sb := new(strings.Builder)
	err := mtx.Serialize(hex.NewEncoder(sb))
	if err != nil {
		return nil, err
	}
	return sb.String(), nil
}

// createSignature creates a signature using the private key of a wallet
// address for a transaction input script. The serialized compressed public
// key of the address is also returned.
func (s *Server) createSignature(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.CreateSignatureCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	serializedTx, err := hex.DecodeString(cmd.SerializedTransaction)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	var tx wire.MsgTx
	err = tx.Deserialize(bytes.NewReader(serializedTx))
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDeserialization, err)
	}

	if cmd.InputIndex >= len(tx.TxIn) {
		return nil, rpcErrorf(dcrjson.ErrRPCMisc,
			"transaction input %d does not exist", cmd.InputIndex)
	}

	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		return nil, err
	}

	hashType := txscript.SigHashType(cmd.HashType)
	prevOutScript, err := hex.DecodeString(cmd.PreviousPkScript)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	sig, pubkey, err := w.CreateSignature(ctx, &tx, uint32(cmd.InputIndex),
		addr, hashType, prevOutScript)
	if err != nil {
		return nil, err
	}

	return &types.CreateSignatureResult{
		Signature: hex.EncodeToString(sig),
		PublicKey: hex.EncodeToString(pubkey),
	}, nil
}

func (s *Server) debugLevel(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.DebugLevelCmd)

	if cmd.LevelSpec == "show" {
		return fmt.Sprintf("Supported subsystems %v",
			s.cfg.Loggers.Subsystems()), nil
	}

	err := s.cfg.Loggers.SetLevels(cmd.LevelSpec)
	if err != nil {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
			"invalid debug level %v: %v", cmd.LevelSpec, err)
	}

	return "Done.", nil
}

// disapprovePercent returns the wallets current disapprove percentage.
func (s *Server) disapprovePercent(ctx context.Context, _ any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}
	return w.DisapprovePercent(), nil
}

func (s *Server) discoverUsage(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.DiscoverUsageCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, ok := s.walletLoader.NetworkBackend()
	if !ok {
		return nil, errNoNetwork
	}

	startBlock := w.ChainParams().GenesisHash
	if cmd.StartBlock != nil {
		h, err := chainhash.NewHashFromStr(*cmd.StartBlock)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "startblock: %v", err)
		}
		startBlock = *h
	}
	discoverAccounts := cmd.DiscoverAccounts != nil && *cmd.DiscoverAccounts

	gapLimit := w.GapLimit()
	if cmd.GapLimit != nil {
		gapLimit = *cmd.GapLimit
	}

	err := w.DiscoverActiveAddresses(ctx, n, &startBlock, discoverAccounts, gapLimit)
	return nil, err
}

// dumpPrivKey handles a dumpprivkey request with the private key
// for a single address, or an appropriate error if the wallet
// is locked.
func (s *Server) dumpPrivKey(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.DumpPrivKeyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		return nil, err
	}

	key, err := w.DumpWIFPrivateKey(ctx, addr)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return nil, errWalletUnlockNeeded
		}
		return nil, err
	}
	return key, nil
}

func (s *Server) fundRawTransaction(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.FundRawTransactionCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var (
		changeAddress string
		feeRate       = w.RelayFee()
		confs         = int32(1)
	)
	if cmd.Options != nil {
		opts := cmd.Options
		var err error
		if opts.ChangeAddress != nil {
			changeAddress = *opts.ChangeAddress
		}
		if opts.FeeRate != nil {
			feeRate, err = dcrutil.NewAmount(*opts.FeeRate)
			if err != nil {
				return nil, err
			}
		}
		if opts.ConfTarget != nil {
			confs = *opts.ConfTarget
			if confs < 0 {
				return nil, errors.New("confs must be non-negative")
			}
		}
	}

	tx := new(wire.MsgTx)
	err := tx.Deserialize(hex.NewDecoder(strings.NewReader(cmd.HexString)))
	if err != nil {
		return nil, err
	}
	// Existing inputs are problematic.  Without information about
	// how large the input scripts will be once signed, the wallet is
	// unable to perform correct fee estimation.  If fundrawtransaction
	// is changed later to work on a PSDT structure that includes this
	// information, this functionality may be enabled.  For now, prevent
	// the method from continuing.
	if len(tx.TxIn) != 0 {
		return nil, errors.New("transaction must not already have inputs")
	}

	accountNum, err := w.AccountNumber(ctx, cmd.FundAccount)
	if err != nil {
		return nil, err
	}

	// Because there are no other inputs, a new transaction can be created.
	var changeSource txauthor.ChangeSource
	if changeAddress != "" {
		var err error
		changeSource, err = makeScriptChangeSource(changeAddress, w.ChainParams())
		if err != nil {
			return nil, err
		}
	}
	atx, err := w.NewUnsignedTransaction(ctx, tx.TxOut, feeRate, accountNum, confs,
		wallet.OutputSelectionAlgorithmDefault, changeSource, nil)
	if err != nil {
		return nil, err
	}

	// Include chosen inputs and change output (if any) in decoded
	// transaction.
	tx.TxIn = atx.Tx.TxIn
	if atx.ChangeIndex >= 0 {
		tx.TxOut = append(tx.TxOut, atx.Tx.TxOut[atx.ChangeIndex])
	}

	// Determine the absolute fee of the funded transaction
	fee := atx.TotalInput
	for i := range tx.TxOut {
		fee -= dcrutil.Amount(tx.TxOut[i].Value)
	}

	b := new(strings.Builder)
	b.Grow(2 * tx.SerializeSize())
	err = tx.Serialize(hex.NewEncoder(b))
	if err != nil {
		return nil, err
	}
	res := &types.FundRawTransactionResult{
		Hex: b.String(),
		Fee: fee.ToCoin(),
	}
	return res, nil
}

// getAddressesByAccount handles a getaddressesbyaccount request by returning
// all addresses for an account, or an error if the requested account does
// not exist.
func (s *Server) getAddressesByAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetAddressesByAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	if cmd.Account == "imported" {
		addrs, err := w.ImportedAddresses(ctx, cmd.Account)
		if err != nil {
			return nil, err
		}
		return knownAddressMarshaler(addrs), nil
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	xpub, err := w.AccountXpub(ctx, account)
	if err != nil {
		return nil, err
	}
	extBranch, err := xpub.Child(0)
	if err != nil {
		return nil, err
	}
	intBranch, err := xpub.Child(1)
	if err != nil {
		return nil, err
	}
	endExt, endInt, err := w.BIP0044BranchNextIndexes(ctx, account)
	if err != nil {
		return nil, err
	}
	params := w.ChainParams()
	addrs := make([]string, 0, endExt+endInt)
	appendAddrs := func(branchKey *hdkeychain.ExtendedKey, n uint32) error {
		for i := uint32(0); i < n; i++ {
			child, err := branchKey.Child(i)
			if errors.Is(err, hdkeychain.ErrInvalidChild) {
				continue
			}
			if err != nil {
				return err
			}
			pkh := dcrutil.Hash160(child.SerializedPubKey())
			addr, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(
				pkh, params)
			addrs = append(addrs, addr.String())
		}
		return nil
	}
	err = appendAddrs(extBranch, endExt)
	if err != nil {
		return nil, err
	}
	err = appendAddrs(intBranch, endInt)
	if err != nil {
		return nil, err
	}
	return addressStringsMarshaler(addrs), nil
}

// getBalance handles a getbalance request by returning the balance for an
// account (wallet), or an error if the requested account does not
// exist.
func (s *Server) getBalance(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetBalanceCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	minConf := int32(*cmd.MinConf)
	if minConf < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "minconf must be non-negative")
	}

	accountName := "*"
	if cmd.Account != nil {
		accountName = *cmd.Account
	}

	blockHash, _ := w.MainChainTip(ctx)
	result := types.GetBalanceResult{
		BlockHash: blockHash.String(),
	}

	if accountName == "*" {
		balances, err := w.AccountBalances(ctx, int32(*cmd.MinConf))
		if err != nil {
			return nil, err
		}

		var (
			totImmatureCoinbase dcrutil.Amount
			totImmatureStakegen dcrutil.Amount
			totLocked           dcrutil.Amount
			totSpendable        dcrutil.Amount
			totUnconfirmed      dcrutil.Amount
			totVotingAuthority  dcrutil.Amount
			cumTot              dcrutil.Amount
		)

		balancesLen := uint32(len(balances))
		result.Balances = make([]types.GetAccountBalanceResult, 0, balancesLen)

		for _, bal := range balances {
			accountName, err := w.AccountName(ctx, bal.Account)
			if err != nil {
				// Expect account lookup to succeed
				if errors.Is(err, errors.NotExist) {
					return nil, rpcError(dcrjson.ErrRPCInternal.Code, err)
				}
				return nil, err
			}

			totImmatureCoinbase += bal.ImmatureCoinbaseRewards
			totImmatureStakegen += bal.ImmatureStakeGeneration
			totLocked += bal.LockedByTickets
			totSpendable += bal.Spendable
			totUnconfirmed += bal.Unconfirmed
			totVotingAuthority += bal.VotingAuthority
			cumTot += bal.Total

			json := types.GetAccountBalanceResult{
				AccountName:             accountName,
				ImmatureCoinbaseRewards: bal.ImmatureCoinbaseRewards.ToCoin(),
				ImmatureStakeGeneration: bal.ImmatureStakeGeneration.ToCoin(),
				LockedByTickets:         bal.LockedByTickets.ToCoin(),
				Spendable:               bal.Spendable.ToCoin(),
				Total:                   bal.Total.ToCoin(),
				Unconfirmed:             bal.Unconfirmed.ToCoin(),
				VotingAuthority:         bal.VotingAuthority.ToCoin(),
			}

			result.Balances = append(result.Balances, json)
		}

		result.TotalImmatureCoinbaseRewards = totImmatureCoinbase.ToCoin()
		result.TotalImmatureStakeGeneration = totImmatureStakegen.ToCoin()
		result.TotalLockedByTickets = totLocked.ToCoin()
		result.TotalSpendable = totSpendable.ToCoin()
		result.TotalUnconfirmed = totUnconfirmed.ToCoin()
		result.TotalVotingAuthority = totVotingAuthority.ToCoin()
		result.CumulativeTotal = cumTot.ToCoin()
	} else {
		account, err := w.AccountNumber(ctx, accountName)
		if err != nil {
			if errors.Is(err, errors.NotExist) {
				return nil, errAccountNotFound
			}
			return nil, err
		}

		bal, err := w.AccountBalance(ctx, account, int32(*cmd.MinConf))
		if err != nil {
			// Expect account lookup to succeed
			if errors.Is(err, errors.NotExist) {
				return nil, rpcError(dcrjson.ErrRPCInternal.Code, err)
			}
			return nil, err
		}
		json := types.GetAccountBalanceResult{
			AccountName:             accountName,
			ImmatureCoinbaseRewards: bal.ImmatureCoinbaseRewards.ToCoin(),
			ImmatureStakeGeneration: bal.ImmatureStakeGeneration.ToCoin(),
			LockedByTickets:         bal.LockedByTickets.ToCoin(),
			Spendable:               bal.Spendable.ToCoin(),
			Total:                   bal.Total.ToCoin(),
			Unconfirmed:             bal.Unconfirmed.ToCoin(),
			VotingAuthority:         bal.VotingAuthority.ToCoin(),
		}
		result.Balances = append(result.Balances, json)
	}

	return result, nil
}

// getBestBlock handles a getbestblock request by returning a JSON object
// with the height and hash of the most recently processed block.
func (s *Server) getBestBlock(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	hash, height := w.MainChainTip(ctx)
	result := &dcrdtypes.GetBestBlockResult{
		Hash:   hash.String(),
		Height: int64(height),
	}
	return result, nil
}

// getBestBlockHash handles a getbestblockhash request by returning the hash
// of the most recently processed block.
func (s *Server) getBestBlockHash(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	hash, _ := w.MainChainTip(ctx)
	return hash.String(), nil
}

// getBlockCount handles a getblockcount request by returning the chain height
// of the most recently processed block.
func (s *Server) getBlockCount(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	_, height := w.MainChainTip(ctx)
	return height, nil
}

// getBlockHash handles a getblockhash request by returning the main chain hash
// for a block at some height.
func (s *Server) getBlockHash(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetBlockHashCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	height := int32(cmd.Index)
	id := wallet.NewBlockIdentifierFromHeight(height)
	info, err := w.BlockInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	return info.Hash.String(), nil
}

// getBlockHeader implements the getblockheader command.
func (s *Server) getBlockHeader(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetBlockHeaderCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Attempt RPC passthrough if connected to DCRD.
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		var resp json.RawMessage
		err := chainSyncer.RPC().Call(ctx, "getblockheader", &resp, cmd.Hash, cmd.Verbose)
		return resp, err
	}

	blockHash, err := chainhash.NewHashFromStr(cmd.Hash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	blockHeader, err := w.BlockHeader(ctx, blockHash)
	if err != nil {
		return nil, err
	}

	// When the verbose flag isn't set, simply return the serialized block
	// header as a hex-encoded string.
	if cmd.Verbose == nil || !*cmd.Verbose {
		var headerBuf bytes.Buffer
		err := blockHeader.Serialize(&headerBuf)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Could not serialize block header: %v", err)
		}
		return hex.EncodeToString(headerBuf.Bytes()), nil
	}

	// The verbose flag is set, so generate the JSON object and return it.

	// Get next block hash unless there are none.
	var nextHashString string
	confirmations := int64(-1)
	mainChainHasBlock, _, err := w.BlockInMainChain(ctx, blockHash)
	if err != nil {
		return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Error checking if block is in mainchain: %v", err)
	}
	if mainChainHasBlock {
		blockHeight := int32(blockHeader.Height)
		_, bestHeight := w.MainChainTip(ctx)
		if blockHeight < bestHeight {
			nextBlockID := wallet.NewBlockIdentifierFromHeight(blockHeight + 1)
			nextBlockInfo, err := w.BlockInfo(ctx, nextBlockID)
			if err != nil {
				return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Info not found for next block: %v", err)
			}
			nextHashString = nextBlockInfo.Hash.String()
		}
		confirmations = int64(confirms(blockHeight, bestHeight))
	}

	// Calculate past median time. Look at the last 11 blocks, starting
	// with the requested block, which is consistent with dcrd.
	iBlkHeader := blockHeader // start with the block header for the requested block
	timestamps := make([]int64, 0, 11)
	for i := 0; i < cap(timestamps); i++ {
		timestamps = append(timestamps, iBlkHeader.Timestamp.Unix())
		if iBlkHeader.Height == 0 {
			break
		}
		iBlkHeader, err = w.BlockHeader(ctx, &iBlkHeader.PrevBlock)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Info not found for previous block: %v", err)
		}
	}
	slices.Sort(timestamps)
	medianTime := timestamps[len(timestamps)/2]

	// Determine the PoW hash.  When the v1 PoW hash differs from the
	// block hash, this is assumed to be v2 (DCP0011).  More advanced
	// selection logic will be necessary if the PoW hash changes again in
	// the future.
	powHash := blockHeader.PowHashV1()
	if powHash != *blockHash {
		powHash = blockHeader.PowHashV2()
	}

	return &dcrdtypes.GetBlockHeaderVerboseResult{
		Hash:          blockHash.String(),
		PowHash:       powHash.String(),
		Confirmations: confirmations,
		Version:       blockHeader.Version,
		MerkleRoot:    blockHeader.MerkleRoot.String(),
		StakeRoot:     blockHeader.StakeRoot.String(),
		VoteBits:      blockHeader.VoteBits,
		FinalState:    hex.EncodeToString(blockHeader.FinalState[:]),
		Voters:        blockHeader.Voters,
		FreshStake:    blockHeader.FreshStake,
		Revocations:   blockHeader.Revocations,
		PoolSize:      blockHeader.PoolSize,
		Bits:          strconv.FormatInt(int64(blockHeader.Bits), 16),
		SBits:         dcrutil.Amount(blockHeader.SBits).ToCoin(),
		Height:        blockHeader.Height,
		Size:          blockHeader.Size,
		Time:          blockHeader.Timestamp.Unix(),
		MedianTime:    medianTime,
		Nonce:         blockHeader.Nonce,
		ExtraData:     hex.EncodeToString(blockHeader.ExtraData[:]),
		StakeVersion:  blockHeader.StakeVersion,
		Difficulty:    difficultyRatio(blockHeader.Bits, w.ChainParams()),
		ChainWork:     "", // unset because wallet is not equipped to easily calculate the cummulative chainwork
		PreviousHash:  blockHeader.PrevBlock.String(),
		NextHash:      nextHashString,
	}, nil
}

// getBlock implements the getblock command.
func (s *Server) getBlock(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetBlockCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	// Attempt RPC passthrough if connected to DCRD.
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		var resp json.RawMessage
		err := chainSyncer.RPC().Call(ctx, "getblock", &resp, cmd.Hash, cmd.Verbose, cmd.VerboseTx)
		return resp, err
	}

	blockHash, err := chainhash.NewHashFromStr(cmd.Hash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	blocks, err := n.Blocks(ctx, []*chainhash.Hash{blockHash})
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		// Should never happen but protects against a possible panic on
		// the following code.
		return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Network returned 0 blocks")
	}

	blk := blocks[0]

	// When the verbose flag isn't set, simply return the
	// network-serialized block as a hex-encoded string.
	if cmd.Verbose == nil || !*cmd.Verbose {
		b := new(strings.Builder)
		b.Grow(2 * blk.SerializeSize())
		err = blk.Serialize(hex.NewEncoder(b))
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Could not serialize block: %v", err)
		}
		return b.String(), nil
	}

	// Get next block hash unless there are none.
	var nextHashString string
	blockHeader := &blk.Header
	confirmations := int64(-1)
	mainChainHasBlock, _, err := w.BlockInMainChain(ctx, blockHash)
	if err != nil {
		return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Error checking if block is in mainchain: %v", err)
	}
	if mainChainHasBlock {
		blockHeight := int32(blockHeader.Height)
		_, bestHeight := w.MainChainTip(ctx)
		if blockHeight < bestHeight {
			nextBlockID := wallet.NewBlockIdentifierFromHeight(blockHeight + 1)
			nextBlockInfo, err := w.BlockInfo(ctx, nextBlockID)
			if err != nil {
				return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Info not found for next block: %v", err)
			}
			nextHashString = nextBlockInfo.Hash.String()
		}
		confirmations = int64(confirms(blockHeight, bestHeight))
	}

	// Calculate past median time. Look at the last 11 blocks, starting
	// with the requested block, which is consistent with dcrd.
	timestamps := make([]int64, 0, 11)
	for iBlkHeader := blockHeader; ; {
		timestamps = append(timestamps, iBlkHeader.Timestamp.Unix())
		if iBlkHeader.Height == 0 || len(timestamps) == cap(timestamps) {
			break
		}
		iBlkHeader, err = w.BlockHeader(ctx, &iBlkHeader.PrevBlock)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Info not found for previous block: %v", err)
		}
	}
	slices.Sort(timestamps)
	medianTime := timestamps[len(timestamps)/2]

	// Determine the PoW hash.  When the v1 PoW hash differs from the
	// block hash, this is assumed to be v2 (DCP0011).  More advanced
	// selection logic will be necessary if the PoW hash changes again in
	// the future.
	powHash := blockHeader.PowHashV1()
	if powHash != *blockHash {
		powHash = blockHeader.PowHashV2()
	}

	sbitsFloat := float64(blockHeader.SBits) / dcrutil.AtomsPerCoin
	blockReply := dcrdtypes.GetBlockVerboseResult{
		Hash:          cmd.Hash,
		PoWHash:       powHash.String(),
		Version:       blockHeader.Version,
		MerkleRoot:    blockHeader.MerkleRoot.String(),
		StakeRoot:     blockHeader.StakeRoot.String(),
		PreviousHash:  blockHeader.PrevBlock.String(),
		MedianTime:    medianTime,
		Nonce:         blockHeader.Nonce,
		VoteBits:      blockHeader.VoteBits,
		FinalState:    hex.EncodeToString(blockHeader.FinalState[:]),
		Voters:        blockHeader.Voters,
		FreshStake:    blockHeader.FreshStake,
		Revocations:   blockHeader.Revocations,
		PoolSize:      blockHeader.PoolSize,
		Time:          blockHeader.Timestamp.Unix(),
		StakeVersion:  blockHeader.StakeVersion,
		Confirmations: confirmations,
		Height:        int64(blockHeader.Height),
		Size:          int32(blk.Header.Size),
		Bits:          strconv.FormatInt(int64(blockHeader.Bits), 16),
		SBits:         sbitsFloat,
		Difficulty:    difficultyRatio(blockHeader.Bits, w.ChainParams()),
		ChainWork:     "", // unset because wallet is not equipped to easily calculate the cummulative chainwork
		ExtraData:     hex.EncodeToString(blockHeader.ExtraData[:]),
		NextHash:      nextHashString,
	}

	// The coinbase must be version 3 once the treasury agenda is active.
	isTreasuryEnabled := blk.Transactions[0].Version >= wire.TxVersionTreasury

	if cmd.VerboseTx == nil || !*cmd.VerboseTx {
		transactions := blk.Transactions
		txNames := make([]string, len(transactions))
		for i, tx := range transactions {
			txNames[i] = tx.TxHash().String()
		}
		blockReply.Tx = txNames

		stransactions := blk.STransactions
		stxNames := make([]string, len(stransactions))
		for i, tx := range stransactions {
			stxNames[i] = tx.TxHash().String()
		}
		blockReply.STx = stxNames
	} else {
		txns := blk.Transactions
		rawTxns := make([]dcrdtypes.TxRawResult, len(txns))
		for i, tx := range txns {
			rawTxn, err := createTxRawResult(w.ChainParams(), tx, uint32(i), blockHeader, confirmations, isTreasuryEnabled)
			if err != nil {
				return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Could not create transaction: %v", err)
			}
			rawTxns[i] = *rawTxn
		}
		blockReply.RawTx = rawTxns

		stxns := blk.STransactions
		rawSTxns := make([]dcrdtypes.TxRawResult, len(stxns))
		for i, tx := range stxns {
			rawSTxn, err := createTxRawResult(w.ChainParams(), tx, uint32(i), blockHeader, confirmations, isTreasuryEnabled)
			if err != nil {
				return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code, "Could not create stake transaction: %v", err)
			}
			rawSTxns[i] = *rawSTxn
		}
		blockReply.RawSTx = rawSTxns
	}

	return blockReply, nil
}

func createTxRawResult(chainParams *chaincfg.Params, mtx *wire.MsgTx, blkIdx uint32, blkHeader *wire.BlockHeader,
	confirmations int64, isTreasuryEnabled bool) (*dcrdtypes.TxRawResult, error) {

	b := new(strings.Builder)
	b.Grow(2 * mtx.SerializeSize())
	err := mtx.Serialize(hex.NewEncoder(b))
	if err != nil {
		return nil, err
	}

	txReply := &dcrdtypes.TxRawResult{
		Hex:           b.String(),
		Txid:          mtx.CachedTxHash().String(),
		Version:       int32(mtx.Version),
		LockTime:      mtx.LockTime,
		Expiry:        mtx.Expiry,
		Vin:           createVinList(mtx, isTreasuryEnabled),
		Vout:          createVoutList(mtx, chainParams, nil),
		BlockHash:     blkHeader.BlockHash().String(),
		BlockHeight:   int64(blkHeader.Height),
		BlockIndex:    blkIdx,
		Confirmations: confirmations,
		Time:          blkHeader.Timestamp.Unix(),
		Blocktime:     blkHeader.Timestamp.Unix(), // identical to Time in bitcoind too
	}

	return txReply, nil
}

// createVinList returns a slice of JSON objects for the inputs of the passed
// transaction.
func createVinList(mtx *wire.MsgTx, isTreasuryEnabled bool) []dcrdtypes.Vin {
	// Treasurybase transactions only have a single txin by definition.
	//
	// NOTE: This check MUST come before the coinbase check because a
	// treasurybase will be identified as a coinbase as well.
	vinList := make([]dcrdtypes.Vin, len(mtx.TxIn))
	if isTreasuryEnabled && blockchain.IsTreasuryBase(mtx) {
		txIn := mtx.TxIn[0]
		vinEntry := &vinList[0]
		vinEntry.Treasurybase = true
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		return vinList
	}

	// Coinbase transactions only have a single txin by definition.
	if blockchain.IsCoinBaseTx(mtx, isTreasuryEnabled) {
		txIn := mtx.TxIn[0]
		vinEntry := &vinList[0]
		vinEntry.Coinbase = hex.EncodeToString(txIn.SignatureScript)
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		return vinList
	}

	// Treasury spend transactions only have a single txin by definition.
	if isTreasuryEnabled && stake.IsTSpend(mtx) {
		txIn := mtx.TxIn[0]
		vinEntry := &vinList[0]
		vinEntry.TreasurySpend = hex.EncodeToString(txIn.SignatureScript)
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		return vinList
	}

	// Stakebase transactions (votes) have two inputs: a null stake base
	// followed by an input consuming a ticket's stakesubmission.
	isSSGen := stake.IsSSGen(mtx)

	for i, txIn := range mtx.TxIn {
		// Handle only the null input of a stakebase differently.
		if isSSGen && i == 0 {
			vinEntry := &vinList[0]
			vinEntry.Stakebase = hex.EncodeToString(txIn.SignatureScript)
			vinEntry.Sequence = txIn.Sequence
			vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
			vinEntry.BlockHeight = txIn.BlockHeight
			vinEntry.BlockIndex = txIn.BlockIndex
			continue
		}

		// The disassembled string will contain [error] inline
		// if the script doesn't fully parse, so ignore the
		// error here.
		disbuf, _ := txscript.DisasmString(txIn.SignatureScript)

		vinEntry := &vinList[i]
		vinEntry.Txid = txIn.PreviousOutPoint.Hash.String()
		vinEntry.Vout = txIn.PreviousOutPoint.Index
		vinEntry.Tree = txIn.PreviousOutPoint.Tree
		vinEntry.Sequence = txIn.Sequence
		vinEntry.AmountIn = dcrutil.Amount(txIn.ValueIn).ToCoin()
		vinEntry.BlockHeight = txIn.BlockHeight
		vinEntry.BlockIndex = txIn.BlockIndex
		vinEntry.ScriptSig = &dcrdtypes.ScriptSig{
			Asm: disbuf,
			Hex: hex.EncodeToString(txIn.SignatureScript),
		}
	}

	return vinList
}

// createVoutList returns a slice of JSON objects for the outputs of the passed
// transaction.
func createVoutList(mtx *wire.MsgTx, chainParams *chaincfg.Params, filterAddrMap map[string]struct{}) []dcrdtypes.Vout {
	txType := stake.DetermineTxType(mtx)
	voutList := make([]dcrdtypes.Vout, 0, len(mtx.TxOut))
	for i, v := range mtx.TxOut {
		// The disassembled string will contain [error] inline if the
		// script doesn't fully parse, so ignore the error here.
		disbuf, _ := txscript.DisasmString(v.PkScript)

		// Attempt to extract addresses from the public key script.  In
		// the case of stake submission transactions, the odd outputs
		// contain a commitment address, so detect that case
		// accordingly.
		var addrs []stdaddr.Address
		var scriptClass string
		var reqSigs uint16
		var commitAmt *dcrutil.Amount
		if txType == stake.TxTypeSStx && (i%2 != 0) {
			scriptClass = sstxCommitmentString
			addr, err := stake.AddrFromSStxPkScrCommitment(v.PkScript,
				chainParams)
			if err != nil {
				log.Warnf("failed to decode ticket "+
					"commitment addr output for tx hash "+
					"%v, output idx %v", mtx.TxHash(), i)
			} else {
				addrs = []stdaddr.Address{addr}
			}
			amt, err := stake.AmountFromSStxPkScrCommitment(v.PkScript)
			if err != nil {
				log.Warnf("failed to decode ticket "+
					"commitment amt output for tx hash %v"+
					", output idx %v", mtx.TxHash(), i)
			} else {
				commitAmt = &amt
			}
		} else {
			// Ignore the error here since an error means the script
			// couldn't parse and there is no additional information
			// about it anyways.
			var sc stdscript.ScriptType
			sc, addrs = stdscript.ExtractAddrs(v.Version, v.PkScript, chainParams)
			reqSigs = stdscript.DetermineRequiredSigs(v.Version, v.PkScript)
			scriptClass = sc.String()
		}

		// Encode the addresses while checking if the address passes the
		// filter when needed.
		passesFilter := len(filterAddrMap) == 0
		encodedAddrs := make([]string, len(addrs))
		for j, addr := range addrs {
			encodedAddr := addr.String()
			encodedAddrs[j] = encodedAddr

			// No need to check the map again if the filter already
			// passes.
			if passesFilter {
				continue
			}
			if _, exists := filterAddrMap[encodedAddr]; exists {
				passesFilter = true
			}
		}

		if !passesFilter {
			continue
		}

		var vout dcrdtypes.Vout
		voutSPK := &vout.ScriptPubKey
		vout.N = uint32(i)
		vout.Value = dcrutil.Amount(v.Value).ToCoin()
		vout.Version = v.Version
		voutSPK.Addresses = encodedAddrs
		voutSPK.Asm = disbuf
		voutSPK.Hex = hex.EncodeToString(v.PkScript)
		voutSPK.Type = scriptClass
		voutSPK.ReqSigs = int32(reqSigs)
		if commitAmt != nil {
			voutSPK.CommitAmt = dcrjson.Float64(commitAmt.ToCoin())
		}

		voutList = append(voutList, vout)
	}

	return voutList
}

// difficultyRatio returns the proof-of-work difficulty as a multiple of the
// minimum difficulty using the passed bits field from the header of a block.
func difficultyRatio(bits uint32, params *chaincfg.Params) float64 {
	// The minimum difficulty is the max possible proof-of-work limit bits
	// converted back to a number.  Note this is not the same as the proof
	// of work limit directly because the block difficulty is encoded in a
	// block with the compact form which loses precision.
	max := blockchain.CompactToBig(params.PowLimitBits)
	target := blockchain.CompactToBig(bits)

	difficulty := new(big.Rat).SetFrac(max, target)
	outString := difficulty.FloatString(8)
	diff, err := strconv.ParseFloat(outString, 64)
	if err != nil {
		log.Errorf("Cannot get difficulty: %v", err)
		return 0
	}
	return diff
}

// syncStatus handles a syncstatus request.
func (s *Server) syncStatus(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	walletBestHash, walletBestHeight := w.MainChainTip(ctx)
	bestBlock, err := w.BlockInfo(ctx, wallet.NewBlockIdentifierFromHash(&walletBestHash))
	if err != nil {
		return nil, err
	}
	_24HoursAgo := time.Now().UTC().Add(-24 * time.Hour).Unix()
	walletBestBlockTooOld := bestBlock.Timestamp < _24HoursAgo

	synced, targetHeight := n.Synced(ctx)

	var headersFetchProgress float32
	blocksToFetch := targetHeight - walletBestHeight
	if blocksToFetch <= 0 {
		headersFetchProgress = 1
	} else {
		totalHeadersToFetch := targetHeight - w.InitialHeight()
		headersFetchProgress = 1 - (float32(blocksToFetch) / float32(totalHeadersToFetch))
	}

	return &types.SyncStatusResult{
		Synced:               synced,
		InitialBlockDownload: walletBestBlockTooOld,
		HeadersFetchProgress: headersFetchProgress,
	}, nil
}

// getCurrentNet handles a getcurrentnet request.
func (s *Server) getCurrentNet(ctx context.Context, icmd any) (any, error) {
	return s.activeNet.Net, nil
}

// getInfo handles a getinfo request by returning a structure containing
// information about the current state of the wallet.
func (s *Server) getInfo(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	tipHash, tipHeight := w.MainChainTip(ctx)
	tipHeader, err := w.BlockHeader(ctx, &tipHash)
	if err != nil {
		return nil, err
	}

	balances, err := w.AccountBalances(ctx, 1)
	if err != nil {
		return nil, err
	}
	var spendableBalance dcrutil.Amount
	for _, balance := range balances {
		spendableBalance += balance.Spendable
	}

	info := &types.InfoResult{
		Version:         version.Integer,
		ProtocolVersion: int32(p2p.Pver),
		WalletVersion:   version.Integer,
		Balance:         spendableBalance.ToCoin(),
		Blocks:          tipHeight,
		TimeOffset:      0,
		Connections:     0,
		Proxy:           "",
		Difficulty:      difficultyRatio(tipHeader.Bits, w.ChainParams()),
		TestNet:         w.ChainParams().Net == wire.TestNet3,
		KeypoolOldest:   0,
		KeypoolSize:     0,
		UnlockedUntil:   0,
		PaytxFee:        w.RelayFee().ToCoin(),
		RelayFee:        0,
		Errors:          "",
	}

	n, _ := s.walletLoader.NetworkBackend()
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		var consensusInfo dcrdtypes.InfoChainResult
		err := chainSyncer.RPC().Call(ctx, "getinfo", &consensusInfo)
		if err != nil {
			return nil, err
		}
		info.Version = consensusInfo.Version
		info.ProtocolVersion = consensusInfo.ProtocolVersion
		info.TimeOffset = consensusInfo.TimeOffset
		info.Connections = consensusInfo.Connections
		info.Proxy = consensusInfo.Proxy
		info.RelayFee = consensusInfo.RelayFee
		info.Errors = consensusInfo.Errors
	}

	return info, nil
}

func decodeAddress(s string, params *chaincfg.Params) (stdaddr.Address, error) {
	// Secp256k1 pubkey as a string, handle differently.
	if len(s) == 66 || len(s) == 130 {
		pubKeyBytes, err := hex.DecodeString(s)
		if err != nil {
			return nil, err
		}
		pubKeyAddr, err := stdaddr.NewAddressPubKeyEcdsaSecp256k1V0Raw(
			pubKeyBytes, params)
		if err != nil {
			return nil, err
		}

		return pubKeyAddr, nil
	}

	addr, err := stdaddr.DecodeAddress(s, params)
	if err != nil {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey,
			"invalid address %q: decode failed: %#q", s, err)
	}
	return addr, nil
}

func decodeStakeAddress(s string, params *chaincfg.Params) (stdaddr.StakeAddress, error) {
	a, err := decodeAddress(s, params)
	if err != nil {
		return nil, err
	}
	if sa, ok := a.(stdaddr.StakeAddress); ok {
		return sa, nil
	}
	return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey,
		"invalid stake address %q", s)
}

// getAccount handles a getaccount request by returning the account name
// associated with a single address.
func (s *Server) getAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		return nil, err
	}

	a, err := w.KnownAddress(ctx, addr)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAddressNotInWallet
		}
		return nil, err
	}

	return a.AccountName(), nil
}

// getAccountAddress handles a getaccountaddress by returning the most
// recently-created chained address that has not yet been used (does not yet
// appear in the blockchain, or any tx that has arrived in the dcrd mempool).
// If the most recently-requested address has been used, a new address (the
// next chained address in the keypool) is used.  This can fail if the keypool
// runs out (and will return dcrjson.ErrRPCWalletKeypoolRanOut if that happens).
func (s *Server) getAccountAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetAccountAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	addr, err := w.CurrentAddress(account)
	if err != nil {
		// Expect account lookup to succeed
		if errors.Is(err, errors.NotExist) {
			return nil, rpcError(dcrjson.ErrRPCInternal.Code, err)
		}
		return nil, err
	}

	return addr.String(), nil
}

// getUnconfirmedBalance handles a getunconfirmedbalance extension request
// by returning the current unconfirmed balance of an account.
func (s *Server) getUnconfirmedBalance(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetUnconfirmedBalanceCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	acctName := "default"
	if cmd.Account != nil {
		acctName = *cmd.Account
	}
	account, err := w.AccountNumber(ctx, acctName)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	bals, err := w.AccountBalance(ctx, account, 1)
	if err != nil {
		// Expect account lookup to succeed
		if errors.Is(err, errors.NotExist) {
			return nil, rpcError(dcrjson.ErrRPCInternal.Code, err)
		}
		return nil, err
	}

	return (bals.Total - bals.Spendable).ToCoin(), nil
}

// getCFilterV2 implements the getcfilterv2 command.
func (s *Server) getCFilterV2(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetCFilterV2Cmd)
	blockHash, err := chainhash.NewHashFromStr(cmd.BlockHash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	key, filter, err := w.CFilterV2(ctx, blockHash)
	if err != nil {
		return nil, err
	}

	return &types.GetCFilterV2Result{
		BlockHash: cmd.BlockHash,
		Filter:    hex.EncodeToString(filter.Bytes()),
		Key:       hex.EncodeToString(key[:]),
	}, nil
}

// importCFiltersV2 handles an importcfiltersv2 request by parsing the provided
// hex-encoded filters into bytes and importing them into the wallet.
func (s *Server) importCFiltersV2(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ImportCFiltersV2Cmd)

	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	filterData := make([][]byte, len(cmd.Filters))
	for i, fdhex := range cmd.Filters {
		var err error
		filterData[i], err = hex.DecodeString(fdhex)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParams.Code, "filter %d is not a valid hex string", i)
		}
	}

	err := w.ImportCFiltersV2(ctx, cmd.StartHeight, filterData)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidRequest.Code, err)
	}

	return nil, nil
}

// importPrivKey handles an importprivkey request by parsing
// a WIF-encoded private key and adding it to an account.
func (s *Server) importPrivKey(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ImportPrivKeyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	rescan := true
	if cmd.Rescan != nil {
		rescan = *cmd.Rescan
	}
	scanFrom := int32(0)
	if cmd.ScanFrom != nil {
		scanFrom = int32(*cmd.ScanFrom)
	}
	n, ok := s.walletLoader.NetworkBackend()
	if rescan && !ok {
		return nil, errNoNetwork
	}

	// Ensure that private keys are only imported to the correct account.
	//
	// Yes, Label is the account name.
	if cmd.Label != nil && *cmd.Label != udb.ImportedAddrAccountName {
		return nil, errNotImportedAccount
	}

	wif, err := dcrutil.DecodeWIF(cmd.PrivKey, w.ChainParams().PrivateKeyID)
	if err != nil {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey, "WIF decode failed: %v", err)
	}

	// Import the private key, handling any errors.
	_, err = w.ImportPrivateKey(ctx, wif)
	if err != nil {
		switch {
		case errors.Is(err, errors.Exist):
			// Do not return duplicate key errors to the client.
			return nil, nil
		case errors.Is(err, errors.Locked):
			return nil, errWalletUnlockNeeded
		default:
			return nil, err
		}
	}

	if rescan {
		// Rescan in the background rather than blocking the rpc request. Use
		// the server waitgroup to ensure the rescan can return cleanly rather
		// than being killed mid database transaction.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			serverCtx := s.httpServer.BaseContext(nil)
			_ = w.RescanFromHeight(serverCtx, n, scanFrom)
		}()
	}

	return nil, nil
}

// importPubKey handles an importpubkey request by importing a hex-encoded
// compressed 33-byte secp256k1 public key with sign byte, as well as its
// derived P2PKH address.  This method may only be used by watching-only
// wallets and with the special "imported" account.
func (s *Server) importPubKey(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ImportPubKeyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	rescan := true
	if cmd.Rescan != nil {
		rescan = *cmd.Rescan
	}
	scanFrom := int32(0)
	if cmd.ScanFrom != nil {
		scanFrom = int32(*cmd.ScanFrom)
	}
	n, ok := s.walletLoader.NetworkBackend()
	if rescan && !ok {
		return nil, errNoNetwork
	}

	if cmd.Label != nil && *cmd.Label != udb.ImportedAddrAccountName {
		return nil, errNotImportedAccount
	}

	pk, err := hex.DecodeString(cmd.PubKey)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	_, err = w.ImportPublicKey(ctx, pk)
	if errors.Is(err, errors.Exist) {
		// Do not return duplicate address errors, and skip any
		// rescans.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if rescan {
		// Rescan in the background rather than blocking the rpc request. Use
		// the server waitgroup to ensure the rescan can return cleanly rather
		// than being killed mid database transaction.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			serverCtx := s.httpServer.BaseContext(nil)
			_ = w.RescanFromHeight(serverCtx, n, scanFrom)
		}()
	}

	return nil, nil
}

// importScript imports a redeem script for a P2SH output.
func (s *Server) importScript(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ImportScriptCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	rescan := true
	if cmd.Rescan != nil {
		rescan = *cmd.Rescan
	}
	scanFrom := int32(0)
	if cmd.ScanFrom != nil {
		scanFrom = int32(*cmd.ScanFrom)
	}
	n, ok := s.walletLoader.NetworkBackend()
	if rescan && !ok {
		return nil, errNoNetwork
	}

	rs, err := hex.DecodeString(cmd.Hex)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}
	if len(rs) == 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "empty script")
	}

	err = w.ImportScript(ctx, rs)
	if errors.Is(err, errors.Exist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if rescan {
		// Rescan in the background rather than blocking the rpc request. Use
		// the server waitgroup to ensure the rescan can return cleanly rather
		// than being killed mid database transaction.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			serverCtx := s.httpServer.BaseContext(nil)
			_ = w.RescanFromHeight(serverCtx, n, scanFrom)
		}()
	}

	return nil, nil
}

func (s *Server) importXpub(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ImportXpubCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	xpub, err := hdkeychain.NewKeyFromString(cmd.Xpub, w.ChainParams())
	if err != nil {
		return nil, err
	}

	return nil, w.ImportXpubAccount(ctx, cmd.Name, xpub)
}

// createNewAccount handles a createnewaccount request by creating and
// returning a new account. If the last account has no transaction history
// as per BIP 0044 a new account cannot be created so an error will be returned.
func (s *Server) createNewAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.CreateNewAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// The wildcard * is reserved by the rpc server with the special meaning
	// of "all accounts", so disallow naming accounts to this string.
	if cmd.Account == "*" {
		return nil, errReservedAccountName
	}

	_, err := w.NextAccount(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return nil, rpcErrorf(dcrjson.ErrRPCWalletUnlockNeeded, "creating new accounts requires an unlocked wallet")
		}
		return nil, err
	}
	return nil, nil
}

// renameAccount handles a renameaccount request by renaming an account.
// If the account does not exist an appropriate error will be returned.
func (s *Server) renameAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.RenameAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// The wildcard * is reserved by the rpc server with the special meaning
	// of "all accounts", so disallow naming accounts to this string.
	if cmd.NewAccount == "*" {
		return nil, errReservedAccountName
	}

	// Check that given account exists
	account, err := w.AccountNumber(ctx, cmd.OldAccount)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	err = w.RenameAccount(ctx, account, cmd.NewAccount)
	return nil, err
}

// getMultisigOutInfo displays information about a given multisignature
// output.
func (s *Server) getMultisigOutInfo(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetMultisigOutInfoCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	hash, err := chainhash.NewHashFromStr(cmd.Hash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	// Multisig outs are always in TxTreeRegular.
	op := &wire.OutPoint{
		Hash:  *hash,
		Index: cmd.Index,
		Tree:  wire.TxTreeRegular,
	}

	p2shOutput, err := w.FetchP2SHMultiSigOutput(ctx, op)
	if err != nil {
		return nil, err
	}

	// Get the list of pubkeys required to sign.
	_, pubkeyAddrs := stdscript.ExtractAddrs(scriptVersionAssumed, p2shOutput.RedeemScript, w.ChainParams())
	pubkeys := make([]string, 0, len(pubkeyAddrs))
	for _, pka := range pubkeyAddrs {
		switch pka := pka.(type) {
		case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
			pubkeys = append(pubkeys, hex.EncodeToString(pka.SerializedPubKey()))
		}
	}

	result := &types.GetMultisigOutInfoResult{
		Address:      p2shOutput.P2SHAddress.String(),
		RedeemScript: hex.EncodeToString(p2shOutput.RedeemScript),
		M:            p2shOutput.M,
		N:            p2shOutput.N,
		Pubkeys:      pubkeys,
		TxHash:       p2shOutput.OutPoint.Hash.String(),
		Amount:       p2shOutput.OutputAmount.ToCoin(),
	}
	if !p2shOutput.ContainingBlock.None() {
		result.BlockHeight = uint32(p2shOutput.ContainingBlock.Height)
		result.BlockHash = p2shOutput.ContainingBlock.Hash.String()
	}
	if p2shOutput.Redeemer != nil {
		result.Spent = true
		result.SpentBy = p2shOutput.Redeemer.TxHash.String()
		result.SpentByIndex = p2shOutput.Redeemer.InputIndex
	}
	return result, nil
}

// getNewAddress handles a getnewaddress request by returning a new
// address for an account.  If the account does not exist an appropriate
// error is returned.
func (s *Server) getNewAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetNewAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var callOpts []wallet.NextAddressCallOption
	if cmd.GapPolicy != nil {
		switch *cmd.GapPolicy {
		case "":
		case "error":
			callOpts = append(callOpts, wallet.WithGapPolicyError())
		case "ignore":
			callOpts = append(callOpts, wallet.WithGapPolicyIgnore())
		case "wrap":
			callOpts = append(callOpts, wallet.WithGapPolicyWrap())
		default:
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "unknown gap policy %q", *cmd.GapPolicy)
		}
	}

	acctName := "default"
	if cmd.Account != nil {
		acctName = *cmd.Account
	}
	account, err := w.AccountNumber(ctx, acctName)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	addr, err := w.NewExternalAddress(ctx, account, callOpts...)
	if err != nil {
		return nil, err
	}
	return addr.String(), nil
}

// getRawChangeAddress handles a getrawchangeaddress request by creating
// and returning a new change address for an account.
//
// Note: bitcoind allows specifying the account as an optional parameter,
// but ignores the parameter.
func (s *Server) getRawChangeAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetRawChangeAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	acctName := "default"
	if cmd.Account != nil {
		acctName = *cmd.Account
	}
	account, err := w.AccountNumber(ctx, acctName)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	addr, err := w.NewChangeAddress(ctx, account)
	if err != nil {
		return nil, err
	}

	// Return the new payment address string.
	return addr.String(), nil
}

// getReceivedByAccount handles a getreceivedbyaccount request by returning
// the total amount received by addresses of an account.
func (s *Server) getReceivedByAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetReceivedByAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	// Transactions are not tracked for imported xpub accounts.
	if account > udb.ImportedAddrAccount {
		return 0.0, nil
	}

	// TODO: This is more inefficient that it could be, but the entire
	// algorithm is already dominated by reading every transaction in the
	// wallet's history.
	results, err := w.TotalReceivedForAccounts(ctx, int32(*cmd.MinConf))
	if err != nil {
		return nil, err
	}
	acctIndex := int(account)
	if account == udb.ImportedAddrAccount {
		acctIndex = len(results) - 1
	}
	return results[acctIndex].TotalReceived.ToCoin(), nil
}

// getReceivedByAddress handles a getreceivedbyaddress request by returning
// the total amount received by a single address.
func (s *Server) getReceivedByAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetReceivedByAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		return nil, err
	}
	total, err := w.TotalReceivedForAddr(ctx, addr, int32(*cmd.MinConf))
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAddressNotInWallet
		}
		return nil, err
	}

	return total.ToCoin(), nil
}

// getMasterPubkey handles a getmasterpubkey request by returning the wallet
// master pubkey encoded as a string.
func (s *Server) getMasterPubkey(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetMasterPubkeyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// If no account is passed, we provide the extended public key
	// for the default account number.
	account := uint32(udb.DefaultAccountNum)
	if cmd.Account != nil {
		var err error
		account, err = w.AccountNumber(ctx, *cmd.Account)
		if err != nil {
			if errors.Is(err, errors.NotExist) {
				return nil, errAccountNotFound
			}
			return nil, err
		}
	}

	xpub, err := w.AccountXpub(ctx, account)
	if err != nil {
		return nil, err
	}

	log.Warnf("Attention: Extended public keys must not be shared with or " +
		"leaked to external parties, such as VSPs, in combination with " +
		"any account private key; this reveals all private keys of " +
		"this account")

	return xpub.String(), nil
}

// getPeerInfo responds to the getpeerinfo request.
// It gets the network backend and views the data on remote peers when in spv mode
func (s *Server) getPeerInfo(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	syncer, ok := n.(*spv.Syncer)
	if !ok {
		var resp []*types.GetPeerInfoResult
		if chainSyncer, ok := n.(*chain.Syncer); ok {
			err := chainSyncer.RPC().Call(ctx, "getpeerinfo", &resp)
			if err != nil {
				return nil, err
			}
		}
		return resp, nil
	}

	rps := syncer.GetRemotePeers()
	infos := make([]*types.GetPeerInfoResult, 0, len(rps))

	for _, rp := range rps {
		info := &types.GetPeerInfoResult{
			ID:             int32(rp.ID()),
			Addr:           rp.RemoteAddr().String(),
			AddrLocal:      rp.LocalAddr().String(),
			Services:       fmt.Sprintf("%08d", uint64(rp.Services())),
			Version:        rp.Pver(),
			SubVer:         rp.UA(),
			StartingHeight: int64(rp.InitialHeight()),
			BanScore:       int32(rp.BanScore()),
		}
		infos = append(infos, info)
	}
	slices.SortFunc(infos, func(a, b *types.GetPeerInfoResult) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	return infos, nil
}

// getStakeInfo gets a large amounts of information about the stake environment
// and a number of statistics about local staking in the wallet.
func (s *Server) getStakeInfo(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, _ := s.walletLoader.NetworkBackend()
	var sinfo *wallet.StakeInfoData
	var err error
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		sinfo, err = w.StakeInfoPrecise(ctx, chainSyncer.RPC())
	} else {
		sinfo, err = w.StakeInfo(ctx)
	}
	if err != nil {
		return nil, err
	}

	var proportionLive, proportionMissed float64
	if sinfo.PoolSize > 0 {
		proportionLive = float64(sinfo.Live) / float64(sinfo.PoolSize)
	}
	if sinfo.Missed > 0 {
		proportionMissed = float64(sinfo.Missed) / (float64(sinfo.Voted + sinfo.Missed))
	}

	resp := &types.GetStakeInfoResult{
		BlockHeight:  sinfo.BlockHeight,
		Difficulty:   sinfo.Sdiff.ToCoin(),
		TotalSubsidy: sinfo.TotalSubsidy.ToCoin(),

		OwnMempoolTix:  sinfo.OwnMempoolTix,
		Immature:       sinfo.Immature,
		Unspent:        sinfo.Unspent,
		Voted:          sinfo.Voted,
		Revoked:        sinfo.Revoked,
		UnspentExpired: sinfo.UnspentExpired,

		PoolSize:         sinfo.PoolSize,
		AllMempoolTix:    sinfo.AllMempoolTix,
		Live:             sinfo.Live,
		ProportionLive:   proportionLive,
		Missed:           sinfo.Missed,
		ProportionMissed: proportionMissed,
		Expired:          sinfo.Expired,
	}

	return resp, nil
}

// getTickets handles a gettickets request by returning the hashes of the tickets
// currently owned by wallet, encoded as strings.
func (s *Server) getTickets(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetTicketsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, _ := s.walletLoader.NetworkBackend()
	rpc, _ := n.(wallet.LiveTicketQuerier) // nil rpc indicates SPV to LiveTicketHashes

	ticketHashes, err := w.LiveTicketHashes(ctx, rpc, cmd.IncludeImmature)
	if err != nil {
		return nil, err
	}

	// Compose a slice of strings to return.
	ticketHashStrs := make([]string, 0, len(ticketHashes))
	for i := range ticketHashes {
		ticketHashStrs = append(ticketHashStrs, ticketHashes[i].String())
	}

	return &types.GetTicketsResult{Hashes: ticketHashStrs}, nil
}

// getTransaction handles a gettransaction request by returning details about
// a single transaction saved by wallet.
func (s *Server) getTransaction(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetTransactionCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	txHash, err := chainhash.NewHashFromStr(cmd.Txid)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	// returns nil details when not found
	txd, err := wallet.UnstableAPI(w).TxDetails(ctx, txHash)
	if errors.Is(err, errors.NotExist) {
		return nil, rpcErrorf(dcrjson.ErrRPCNoTxInfo, "no information for transaction")
	} else if err != nil {
		return nil, err
	}

	_, tipHeight := w.MainChainTip(ctx)

	var b strings.Builder
	b.Grow(2 * txd.MsgTx.SerializeSize())
	err = txd.MsgTx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}

	// TODO: Add a "generated" field to this result type.  "generated":true
	// is only added if the transaction is a coinbase.
	ret := types.GetTransactionResult{
		TxID:            cmd.Txid,
		Hex:             b.String(),
		Time:            txd.Received.Unix(),
		TimeReceived:    txd.Received.Unix(),
		WalletConflicts: []string{}, // Not saved
		//Generated:     compat.IsEitherCoinBaseTx(&details.MsgTx),
	}

	if txd.Block.Height != -1 {
		ret.BlockHash = txd.Block.Hash.String()
		ret.BlockTime = txd.Block.Time.Unix()
		ret.Confirmations = int64(confirms(txd.Block.Height,
			tipHeight))
	}

	var (
		debitTotal  dcrutil.Amount
		creditTotal dcrutil.Amount
		fee         dcrutil.Amount
		negFeeF64   float64
	)
	for _, deb := range txd.Debits {
		debitTotal += deb.Amount
	}
	for _, cred := range txd.Credits {
		creditTotal += cred.Amount
	}
	// Fee can only be determined if every input is a debit.
	if len(txd.Debits) == len(txd.MsgTx.TxIn) {
		var outputTotal dcrutil.Amount
		for _, output := range txd.MsgTx.TxOut {
			outputTotal += dcrutil.Amount(output.Value)
		}
		fee = debitTotal - outputTotal
		negFeeF64 = (-fee).ToCoin()
	}
	ret.Amount = (creditTotal - debitTotal).ToCoin()
	ret.Fee = negFeeF64

	details, err := w.ListTransactionDetails(ctx, txHash)
	if err != nil {
		return nil, err
	}
	ret.Details = make([]types.GetTransactionDetailsResult, len(details))
	for i, d := range details {
		ret.Details[i] = types.GetTransactionDetailsResult{
			Account:           d.Account,
			Address:           d.Address,
			Amount:            d.Amount,
			Category:          d.Category,
			InvolvesWatchOnly: d.InvolvesWatchOnly,
			Fee:               d.Fee,
			Vout:              d.Vout,
		}
	}

	return ret, nil
}

// getTxOut handles a gettxout request by returning details about an unspent
// output. In SPV mode, details are only returned for transaction outputs that
// are relevant to the wallet.
// To match the behavior in RPC mode, (nil, nil) is returned if the transaction
// output could not be found (never existed or was pruned) or is spent by another
// transaction already in the main chain.  Mined transactions that are spent by
// a mempool transaction are not affected by this.
func (s *Server) getTxOut(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetTxOutCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Attempt RPC passthrough if connected to DCRD.
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		var resp json.RawMessage
		err := chainSyncer.RPC().Call(ctx, "gettxout", &resp, cmd.Txid, cmd.Vout, cmd.Tree, cmd.IncludeMempool)
		return resp, err
	}

	txHash, err := chainhash.NewHashFromStr(cmd.Txid)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	if cmd.Tree != wire.TxTreeRegular && cmd.Tree != wire.TxTreeStake {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "Tx tree must be regular or stake")
	}

	// Attempt to read the unspent txout info from wallet.
	outpoint := wire.OutPoint{Hash: *txHash, Index: cmd.Vout, Tree: cmd.Tree}
	utxo, err := w.UnspentOutput(ctx, outpoint, *cmd.IncludeMempool)
	if err != nil && !errors.Is(err, errors.NotExist) {
		return nil, err
	}
	if utxo == nil {
		return nil, nil // output is spent or does not exist.
	}

	// Disassemble script into single line printable format.  The
	// disassembled string will contain [error] inline if the script
	// doesn't fully parse, so ignore the error here.
	disbuf, _ := txscript.DisasmString(utxo.PkScript)

	// Get further info about the script.  Ignore the error here since an
	// error means the script couldn't parse and there is no additional
	// information about it anyways.
	scriptClass, addrs := stdscript.ExtractAddrs(scriptVersionAssumed, utxo.PkScript, s.activeNet)
	reqSigs := stdscript.DetermineRequiredSigs(scriptVersionAssumed, utxo.PkScript)
	addresses := make([]string, len(addrs))
	for i, addr := range addrs {
		addresses[i] = addr.String()
	}

	bestHash, bestHeight := w.MainChainTip(ctx)
	var confirmations int64
	if utxo.Block.Height != -1 {
		confirmations = int64(confirms(utxo.Block.Height, bestHeight))
	}

	return &dcrdtypes.GetTxOutResult{
		BestBlock:     bestHash.String(),
		Confirmations: confirmations,
		Value:         utxo.Amount.ToCoin(),
		ScriptPubKey: dcrdtypes.ScriptPubKeyResult{
			Asm:       disbuf,
			Hex:       hex.EncodeToString(utxo.PkScript),
			ReqSigs:   int32(reqSigs),
			Type:      scriptClass.String(),
			Addresses: addresses,
		},
		Coinbase: utxo.FromCoinBase,
	}, nil
}

// getVoteChoices handles a getvotechoices request by returning configured vote
// preferences for each agenda of the latest supported stake version.
func (s *Server) getVoteChoices(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.GetVoteChoicesCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var ticketHash *chainhash.Hash
	if cmd.TicketHash != nil {
		hash, err := chainhash.NewHashFromStr(*cmd.TicketHash)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
		ticketHash = hash
	}

	version, agendas := wallet.CurrentAgendas(w.ChainParams())
	resp := &types.GetVoteChoicesResult{
		Version: version,
		Choices: make([]types.VoteChoice, 0, len(agendas)),
	}

	choices, _, err := w.AgendaChoices(ctx, ticketHash)
	if err != nil {
		return nil, err
	}

	for _, agenda := range agendas {
		agendaID := agenda.Vote.Id
		voteChoice := types.VoteChoice{
			AgendaID:          agendaID,
			AgendaDescription: agenda.Vote.Description,
			ChoiceID:          choices[agendaID],
			ChoiceDescription: "", // Set below
		}

		for _, choice := range agenda.Vote.Choices {
			if choices[agendaID] == choice.Id {
				voteChoice.ChoiceDescription = choice.Description
				break
			}
		}

		resp.Choices = append(resp.Choices, voteChoice)
	}

	return resp, nil
}

// getWalletFee returns the currently set tx fee for the requested wallet
func (s *Server) getWalletFee(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	return w.RelayFee().ToCoin(), nil
}

// These generators create the following global variables in this package:
//
//   var localeHelpDescs map[string]func() map[string]string
//   var requestUsages string
//
// localeHelpDescs maps from locale strings (e.g. "en_US") to a function that
// builds a map of help texts for each RPC server method.  This prevents help
// text maps for every locale map from being rooted and created during init.
// Instead, the appropriate function is looked up when help text is first needed
// using the current locale and saved to the global below for further reuse.
//
// requestUsages contains single line usages for every supported request,
// separated by newlines.  It is set during init.  These usages are used for all
// locales.
//
//go:generate go run ../../rpchelp/genrpcserverhelp.go jsonrpc
//go:generate gofmt -w rpcserverhelp.go

var helpDescs map[string]string
var helpDescsMu sync.Mutex // Help may execute concurrently, so synchronize access.

// help handles the help request by returning one line usage of all available
// methods, or full help for a specific method.  The chainClient is optional,
// and this is simply a helper function for the HelpNoChainRPC and
// HelpWithChainRPC handlers.
func (s *Server) help(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.HelpCmd)
	// TODO: The "help" RPC should use a HTTP POST client when calling down to
	// dcrd for additional help methods.  This avoids including websocket-only
	// requests in the help, which are not callable by wallet JSON-RPC clients.
	var rpc *dcrd.RPC
	n, _ := s.walletLoader.NetworkBackend()
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		rpc = chainSyncer.RPC()
	}
	if cmd.Command == nil || *cmd.Command == "" {
		// Prepend chain server usage if it is available.
		usages := requestUsages
		if rpc != nil {
			var usage string
			err := rpc.Call(ctx, "help", &usage)
			if err != nil {
				return nil, err
			}
			if usage != "" {
				usages = "Chain server usage:\n\n" + usage + "\n\n" +
					"Wallet server usage (overrides chain requests):\n\n" +
					requestUsages
			}
		}
		return usages, nil
	}

	defer helpDescsMu.Unlock()
	helpDescsMu.Lock()

	if helpDescs == nil {
		// TODO: Allow other locales to be set via config or detemine
		// this from environment variables.  For now, hardcode US
		// English.
		helpDescs = localeHelpDescs["en_US"]()
	}

	helpText, ok := helpDescs[*cmd.Command]
	if ok {
		return helpText, nil
	}

	// Return the chain server's detailed help if possible.
	var chainHelp string
	if rpc != nil {
		err := rpc.Call(ctx, "help", &chainHelp, *cmd.Command)
		if err != nil {
			return nil, err
		}
	}
	if chainHelp != "" {
		return chainHelp, nil
	}
	return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "no help for method %q", *cmd.Command)
}

// listAccounts handles a listaccounts request by returning a map of account
// names to their balances.
func (s *Server) listAccounts(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListAccountsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	accountBalances := map[string]float64{}
	results, err := w.AccountBalances(ctx, int32(*cmd.MinConf))
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		accountName, err := w.AccountName(ctx, result.Account)
		if err != nil {
			// Expect name lookup to succeed
			if errors.Is(err, errors.NotExist) {
				return nil, rpcError(dcrjson.ErrRPCInternal.Code, err)
			}
			return nil, err
		}
		accountBalances[accountName] = result.Spendable.ToCoin()
	}
	// Return the map.  This will be marshaled into a JSON object.
	return accountBalances, nil
}

// listLockUnspent handles a listlockunspent request by returning an slice of
// all locked outpoints.
func (s *Server) listLockUnspent(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var account string
	cmd := icmd.(*types.ListLockUnspentCmd)
	if cmd.Account != nil {
		account = *cmd.Account
	}
	return w.LockedOutpoints(ctx, account)
}

// listReceivedByAccount handles a listreceivedbyaccount request by returning
// a slice of objects, each one containing:
//
//	"account": the receiving account;
//	"amount": total amount received by the account;
//	"confirmations": number of confirmations of the most recent transaction.
//
// It takes two parameters:
//
//	"minconf": minimum number of confirmations to consider a transaction -
//	           default: one;
//	"includeempty": whether or not to include addresses that have no transactions -
//	                default: false.
func (s *Server) listReceivedByAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListReceivedByAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	results, err := w.TotalReceivedForAccounts(ctx, int32(*cmd.MinConf))
	if err != nil {
		return nil, err
	}

	jsonResults := make([]types.ListReceivedByAccountResult, 0, len(results))
	for _, result := range results {
		jsonResults = append(jsonResults, types.ListReceivedByAccountResult{
			Account:       result.AccountName,
			Amount:        result.TotalReceived.ToCoin(),
			Confirmations: uint64(result.LastConfirmation),
		})
	}
	return jsonResults, nil
}

// listReceivedByAddress handles a listreceivedbyaddress request by returning
// a slice of objects, each one containing:
//
//	"account": the account of the receiving address;
//	"address": the receiving address;
//	"amount": total amount received by the address;
//	"confirmations": number of confirmations of the most recent transaction.
//
// It takes two parameters:
//
//	"minconf": minimum number of confirmations to consider a transaction -
//	           default: one;
//	"includeempty": whether or not to include addresses that have no transactions -
//	                default: false.
func (s *Server) listReceivedByAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListReceivedByAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Intermediate data for each address.
	type AddrData struct {
		// Total amount received.
		amount dcrutil.Amount
		// Number of confirmations of the last transaction.
		confirmations int32
		// Hashes of transactions which include an output paying to the address
		tx []string
	}

	_, tipHeight := w.MainChainTip(ctx)

	// Intermediate data for all addresses.
	allAddrData := make(map[string]AddrData)
	// Create an AddrData entry for each active address in the account.
	// Otherwise we'll just get addresses from transactions later.
	sortedAddrs, err := w.SortedActivePaymentAddresses(ctx)
	if err != nil {
		return nil, err
	}
	for _, address := range sortedAddrs {
		// There might be duplicates, just overwrite them.
		allAddrData[address] = AddrData{}
	}

	minConf := *cmd.MinConf
	var endHeight int32
	if minConf == 0 {
		endHeight = -1
	} else {
		endHeight = tipHeight - int32(minConf) + 1
	}
	err = wallet.UnstableAPI(w).RangeTransactions(ctx, 0, endHeight, func(details []udb.TxDetails) (bool, error) {
		confirmations := confirms(details[0].Block.Height, tipHeight)
		for _, tx := range details {
			for _, cred := range tx.Credits {
				pkVersion := tx.MsgTx.TxOut[cred.Index].Version
				pkScript := tx.MsgTx.TxOut[cred.Index].PkScript
				_, addrs := stdscript.ExtractAddrs(pkVersion, pkScript, w.ChainParams())
				for _, addr := range addrs {
					addrStr := addr.String()
					addrData, ok := allAddrData[addrStr]
					if ok {
						addrData.amount += cred.Amount
						// Always overwrite confirmations with newer ones.
						addrData.confirmations = confirmations
					} else {
						addrData = AddrData{
							amount:        cred.Amount,
							confirmations: confirmations,
						}
					}
					addrData.tx = append(addrData.tx, tx.Hash.String())
					allAddrData[addrStr] = addrData
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Massage address data into output format.
	numAddresses := len(allAddrData)
	ret := make([]types.ListReceivedByAddressResult, numAddresses)
	idx := 0
	for address, addrData := range allAddrData {
		ret[idx] = types.ListReceivedByAddressResult{
			Address:       address,
			Amount:        addrData.amount.ToCoin(),
			Confirmations: uint64(addrData.confirmations),
			TxIDs:         addrData.tx,
		}
		idx++
	}
	return ret, nil
}

// listSinceBlock handles a listsinceblock request by returning an array of maps
// with details of sent and received wallet transactions since the given block.
func (s *Server) listSinceBlock(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListSinceBlockCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	targetConf := int32(*cmd.TargetConfirmations)
	if targetConf < 1 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "target_confirmations must be positive")
	}

	tipHash, tipHeight := w.MainChainTip(ctx)
	lastBlock := &tipHash
	if targetConf > 0 {
		id := wallet.NewBlockIdentifierFromHeight((tipHeight + 1) - targetConf)
		info, err := w.BlockInfo(ctx, id)
		if err != nil {
			return nil, err
		}

		lastBlock = &info.Hash
	}

	// TODO: This must begin at the fork point in the main chain, not the height
	// of this block.
	var end int32
	if cmd.BlockHash != nil {
		hash, err := chainhash.NewHashFromStr(*cmd.BlockHash)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
		header, err := w.BlockHeader(ctx, hash)
		if err != nil {
			return nil, err
		}
		end = int32(header.Height)
	}

	txInfoList, err := w.ListSinceBlock(ctx, -1, end, tipHeight)
	if err != nil {
		return nil, err
	}

	res := &types.ListSinceBlockResult{
		Transactions: txInfoList,
		LastBlock:    lastBlock.String(),
	}
	return res, nil
}

// listTransactions handles a listtransactions request by returning an
// array of maps with details of sent and recevied wallet transactions.
func (s *Server) listTransactions(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListTransactionsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// TODO: ListTransactions does not currently understand the difference
	// between transactions pertaining to one account from another.  This
	// will be resolved when wtxmgr is combined with the waddrmgr namespace.

	if cmd.Account != nil && *cmd.Account != "*" {
		// For now, don't bother trying to continue if the user
		// specified an account, since this can't be (easily or
		// efficiently) calculated.
		return nil,
			errors.E(`Transactions can not be searched by account. ` +
				`Use "*" to reference all accounts.`)
	}

	return w.ListTransactions(ctx, *cmd.From, *cmd.Count)
}

// listAddressTransactions handles a listaddresstransactions request by
// returning an array of maps with details of spent and received wallet
// transactions.  The form of the reply is identical to listtransactions,
// but the array elements are limited to transaction details which are
// about the addresess included in the request.
func (s *Server) listAddressTransactions(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListAddressTransactionsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	if cmd.Account != nil && *cmd.Account != "*" {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
			"listing transactions for addresses may only be done for all accounts")
	}

	// Decode addresses.
	hash160Map := make(map[string]struct{})
	for _, addrStr := range cmd.Addresses {
		addr, err := decodeAddress(addrStr, w.ChainParams())
		if err != nil {
			return nil, err
		}
		hash160er, ok := addr.(stdaddr.Hash160er)
		if !ok {
			// Not tracked by the wallet so skip reporting history
			// of this address.
			continue
		}
		hash160Map[string(hash160er.Hash160()[:])] = struct{}{}
	}

	return w.ListAddressTransactions(ctx, hash160Map)
}

// listAllTransactions handles a listalltransactions request by returning
// a map with details of sent and recevied wallet transactions.  This is
// similar to ListTransactions, except it takes only a single optional
// argument for the account name and replies with all transactions.
func (s *Server) listAllTransactions(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListAllTransactionsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	if cmd.Account != nil && *cmd.Account != "*" {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
			"listing all transactions may only be done for all accounts")
	}

	return w.ListAllTransactions(ctx)
}

// listUnspent handles the listunspent command.
func (s *Server) listUnspent(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ListUnspentCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var addresses map[string]struct{}
	if cmd.Addresses != nil {
		addresses = make(map[string]struct{})
		// confirm that all of them are good:
		for _, as := range *cmd.Addresses {
			a, err := decodeAddress(as, w.ChainParams())
			if err != nil {
				return nil, err
			}
			addresses[a.String()] = struct{}{}
		}
	}

	var account string
	if cmd.Account != nil {
		account = *cmd.Account
	}
	result, err := w.ListUnspent(ctx, int32(*cmd.MinConf), int32(*cmd.MaxConf), addresses, account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAddressNotInWallet
		}
		return nil, err
	}
	return result, nil
}

// lockUnspent handles the lockunspent command.
func (s *Server) lockUnspent(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.LockUnspentCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	switch {
	case cmd.Unlock && len(cmd.Transactions) == 0:
		w.ResetLockedOutpoints()
	default:
		for _, input := range cmd.Transactions {
			txHash, err := chainhash.NewHashFromStr(input.Txid)
			if err != nil {
				return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
			}
			if cmd.Unlock {
				w.UnlockOutpoint(txHash, input.Vout)
			} else {
				w.LockOutpoint(txHash, input.Vout)
			}
		}
	}
	return true, nil
}

// purchaseTicket indicates to the wallet that a ticket should be purchased
// using all currently available funds. If the ticket could not be purchased
// because there are not enough eligible funds, an error will be returned.
func (s *Server) purchaseTicket(ctx context.Context, icmd any) (any, error) {
	// Enforce valid and positive spend limit.
	cmd := icmd.(*types.PurchaseTicketCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	spendLimit, err := dcrutil.NewAmount(cmd.SpendLimit)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}
	if spendLimit < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative spend limit")
	}

	account, err := w.AccountNumber(ctx, cmd.FromAccount)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	// Override the minimum number of required confirmations if specified
	// and enforce it is positive.
	minConf := int32(1)
	if cmd.MinConf != nil {
		minConf = int32(*cmd.MinConf)
		if minConf < 0 {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative minconf")
		}
	}

	numTickets := 1
	if cmd.NumTickets != nil {
		if *cmd.NumTickets > 1 {
			numTickets = *cmd.NumTickets
		}
	}

	// Set the expiry if specified.
	expiry := int32(0)
	if cmd.Expiry != nil {
		expiry = int32(*cmd.Expiry)
	}

	dontSignTx := false
	if cmd.DontSignTx != nil {
		dontSignTx = *cmd.DontSignTx
	}

	var mixedAccount uint32
	var mixedAccountBranch uint32
	var mixedSplitAccount uint32
	// Use purchasing account as change account by default (overridden below if
	// mixing is enabled).
	var changeAccount = account

	if s.cfg.MixingEnabled {
		mixedAccount, err = w.AccountNumber(ctx, s.cfg.MixAccount)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Mixing enabled, but error on mixed account: %v", err)
		}
		mixedAccountBranch = s.cfg.MixBranch
		if mixedAccountBranch != 0 && mixedAccountBranch != 1 {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"MixedAccountBranch should be 0 or 1.")
		}
		mixedSplitAccount, err = w.AccountNumber(ctx, s.cfg.TicketSplitAccount)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Mixing enabled, but error on mixedSplitAccount: %v", err)
		}
		changeAccount, err = w.AccountNumber(ctx, s.cfg.MixChangeAccount)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Mixing enabled, but error on changeAccount: %v", err)
		}
	}

	var vspClient *wallet.VSPClient
	if s.cfg.VSPHost != "" {
		cfg := wallet.VSPClientConfig{
			URL:    s.cfg.VSPHost,
			PubKey: s.cfg.VSPPubKey,
			Policy: &wallet.VSPPolicy{
				MaxFee:     w.VSPMaxFee(),
				FeeAcct:    account,
				ChangeAcct: changeAccount,
			},
		}
		vspClient, err = w.VSP(cfg)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCMisc,
				"VSP Server instance failed to start: %v", err)
		}
	}

	request := &wallet.PurchaseTicketsRequest{
		Count:         numTickets,
		SourceAccount: account,
		MinConf:       minConf,
		Expiry:        expiry,
		DontSignTx:    dontSignTx,

		// CSPP
		Mixing:             s.cfg.MixingEnabled,
		MixedAccount:       mixedAccount,
		MixedAccountBranch: mixedAccountBranch,
		MixedSplitAccount:  mixedSplitAccount,
		ChangeAccount:      changeAccount,

		VSPClient: vspClient,
	}
	// Use the mixed account as voting account if mixing is enabled,
	// otherwise use the source account.
	if s.cfg.MixingEnabled {
		request.VotingAccount = mixedAccount
	} else {
		request.VotingAccount = account
	}

	ticketsResponse, err := w.PurchaseTickets(ctx, n, request)
	if err != nil {
		return nil, err
	}
	ticketsTx := ticketsResponse.Tickets
	splitTx := ticketsResponse.SplitTx

	// If dontSignTx is false, we return the TicketHashes of the published txs.
	if !dontSignTx {
		hashes := ticketsResponse.TicketHashes
		hashStrs := make([]string, len(hashes))
		for i := range hashes {
			hashStrs[i] = hashes[i].String()
		}

		return hashStrs, err
	}

	// Otherwise we return its unsigned tickets bytes and the splittx, so a
	// cold wallet can handle it.
	var stringBuilder strings.Builder
	unsignedTickets := make([]string, len(ticketsTx))
	for i, mtx := range ticketsTx {
		err = mtx.Serialize(hex.NewEncoder(&stringBuilder))
		if err != nil {
			return nil, err
		}
		unsignedTickets[i] = stringBuilder.String()
		stringBuilder.Reset()
	}

	err = splitTx.Serialize(hex.NewEncoder(&stringBuilder))
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}

	splitTxString := stringBuilder.String()

	return types.CreateUnsignedTicketResult{
		UnsignedTickets: unsignedTickets,
		SplitTx:         splitTxString,
	}, nil
}

// processUnmanagedTicket takes a ticket hash as an argument and attempts to
// start managing it for the set vsp client from the config.
func (s *Server) processUnmanagedTicket(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ProcessUnmanagedTicketCmd)

	if cmd.TicketHash == "" {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "ticket hash must be provided")
	}

	hash, err := chainhash.NewHashFromStr(cmd.TicketHash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}

	vspHost := s.cfg.VSPHost
	if vspHost == "" {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "vsphost must be set in options")
	}

	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	vspClient, err := w.LookupVSP(vspHost)
	if err != nil {
		return nil, err
	}

	ticket, err := w.NewVSPTicket(ctx, hash)
	if err != nil {
		return nil, err
	}

	err = vspClient.Process(ctx, ticket, nil)
	if err != nil {
		return nil, err
	}

	return nil, nil

}

// makeOutputs creates a slice of transaction outputs from a pair of address
// strings to amounts.  This is used to create the outputs to include in newly
// created transactions from a JSON object describing the output destinations
// and amounts.
func makeOutputs(pairs map[string]dcrutil.Amount, chainParams *chaincfg.Params) ([]*wire.TxOut, error) {
	outputs := make([]*wire.TxOut, 0, len(pairs))
	for addrStr, amt := range pairs {
		if amt < 0 {
			return nil, errNeedPositiveAmount
		}
		addr, err := decodeAddress(addrStr, chainParams)
		if err != nil {
			return nil, err
		}

		vers, pkScript := addr.PaymentScript()

		outputs = append(outputs, &wire.TxOut{
			Value:    int64(amt),
			PkScript: pkScript,
			Version:  vers,
		})
	}
	return outputs, nil
}

// sendPairs creates and sends payment transactions.
// It returns the transaction hash in string format upon success
// All errors are returned in dcrjson.RPCError format
func (s *Server) sendPairs(ctx context.Context, w *wallet.Wallet, amounts map[string]dcrutil.Amount, account uint32, minconf int32) (string, error) {
	changeAccount := account
	if s.cfg.MixingEnabled && s.cfg.MixAccount != "" && s.cfg.MixChangeAccount != "" {
		mixAccount, err := w.AccountNumber(ctx, s.cfg.MixAccount)
		if err != nil {
			return "", err
		}
		if account == mixAccount {
			changeAccount, err = w.AccountNumber(ctx, s.cfg.MixChangeAccount)
			if err != nil {
				return "", err
			}
		}
	}

	outputs, err := makeOutputs(amounts, w.ChainParams())
	if err != nil {
		return "", err
	}
	txSha, err := w.SendOutputs(ctx, outputs, account, changeAccount, minconf)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return "", errWalletUnlockNeeded
		}
		if errors.Is(err, errors.InsufficientBalance) {
			return "", rpcError(dcrjson.ErrRPCWalletInsufficientFunds, err)
		}
		return "", err
	}

	return txSha.String(), nil
}

// sendAmountToTreasury creates and sends payment transactions to the treasury.
// It returns the transaction hash in string format upon success All errors are
// returned in dcrjson.RPCError format
func (s *Server) sendAmountToTreasury(ctx context.Context, w *wallet.Wallet, amount dcrutil.Amount, account uint32, minconf int32) (string, error) {
	changeAccount := account
	if s.cfg.MixingEnabled {
		mixAccount, err := w.AccountNumber(ctx, s.cfg.MixAccount)
		if err != nil {
			return "", err
		}
		if account == mixAccount {
			changeAccount, err = w.AccountNumber(ctx,
				s.cfg.MixChangeAccount)
			if err != nil {
				return "", err
			}
		}
	}

	outputs := []*wire.TxOut{
		{
			Value:    int64(amount),
			PkScript: []byte{txscript.OP_TADD},
			Version:  wire.DefaultPkScriptVersion,
		},
	}
	txSha, err := w.SendOutputsToTreasury(ctx, outputs, account,
		changeAccount, minconf)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return "", errWalletUnlockNeeded
		}
		if errors.Is(err, errors.InsufficientBalance) {
			return "", rpcError(dcrjson.ErrRPCWalletInsufficientFunds,
				err)
		}
		return "", err
	}

	return txSha.String(), nil
}

// sendOutputsFromTreasury creates and sends payment transactions from the treasury.
// It returns the transaction hash in string format upon success All errors are
// returned in dcrjson.RPCError format
func (s *Server) sendOutputsFromTreasury(ctx context.Context, w *wallet.Wallet, cmd types.SendFromTreasuryCmd) (string, error) {
	// Look to see if the we have the private key imported.
	publicKey, err := decodeAddress(cmd.Key, w.ChainParams())
	if err != nil {
		return "", err
	}
	privKey, zero, err := w.LoadPrivateKey(ctx, publicKey)
	if err != nil {
		return "", err
	}
	defer zero()

	_, tipHeight := w.MainChainTip(ctx)

	// OP_RETURN <8 Bytes ValueIn><24 byte random>. The encoded ValueIn is
	// added at the end of this function.
	var payload [32]byte
	rand.Read(payload[8:])
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_RETURN)
	builder.AddData(payload[:])
	opretScript, err := builder.Script()
	if err != nil {
		return "", rpcErrorf(dcrjson.ErrRPCInternal.Code,
			"sendOutputsFromTreasury NewScriptBuilder: %v", err)
	}
	msgTx := wire.NewMsgTx()
	msgTx.Version = wire.TxVersionTreasury
	msgTx.AddTxOut(wire.NewTxOut(0, opretScript))

	// Calculate expiry.
	msgTx.Expiry = blockchain.CalcTSpendExpiry(int64(tipHeight+1),
		w.ChainParams().TreasuryVoteInterval,
		w.ChainParams().TreasuryVoteIntervalMultiplier)

	// OP_TGEN and calculate totals.
	var totalPayout dcrutil.Amount
	for address, amount := range cmd.Amounts {
		amt, err := dcrutil.NewAmount(amount)
		if err != nil {
			return "", rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}

		// While looping calculate total amount
		totalPayout += amt

		// Decode address.
		addr, err := decodeStakeAddress(address, w.ChainParams())
		if err != nil {
			return "", err
		}

		// Create OP_TGEN prefixed script.
		vers, script := addr.PayFromTreasuryScript()

		// Make sure this is not dust.
		txOut := &wire.TxOut{
			Value:    int64(amt),
			Version:  vers,
			PkScript: script,
		}
		if txrules.IsDustOutput(txOut, w.RelayFee()) {
			return "", rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Amount is dust: %v %v", addr, amt)
		}

		// Add to transaction.
		msgTx.AddTxOut(txOut)
	}

	// Calculate fee. Inputs are <signature> <compressed key> OP_TSPEND.
	estimatedFee := txsizes.EstimateSerializeSize([]int{txsizes.TSPENDInputSize},
		msgTx.TxOut, 0)
	fee := txrules.FeeForSerializeSize(w.RelayFee(), estimatedFee)

	// Assemble TxIn.
	msgTx.AddTxIn(&wire.TxIn{
		// Stakebase transactions have no inputs, so previous outpoint
		// is zero hash and max index.
		PreviousOutPoint: *wire.NewOutPoint(&chainhash.Hash{},
			wire.MaxPrevOutIndex, wire.TxTreeRegular),
		Sequence:        wire.MaxTxInSequenceNum,
		ValueIn:         int64(fee) + int64(totalPayout),
		BlockHeight:     wire.NullBlockHeight,
		BlockIndex:      wire.NullBlockIndex,
		SignatureScript: []byte{}, // Empty for now
	})

	// Encode total amount in first 8 bytes of TxOut[0] OP_RETURN.
	binary.LittleEndian.PutUint64(msgTx.TxOut[0].PkScript[2:2+8],
		uint64(fee)+uint64(totalPayout))

	// Calculate TSpend signature without SigHashType.
	privKeyBytes := privKey.Serialize()
	sigscript, err := sign.TSpendSignatureScript(msgTx, privKeyBytes)
	if err != nil {
		return "", err
	}
	msgTx.TxIn[0].SignatureScript = sigscript

	_, _, err = stake.CheckTSpend(msgTx)
	if err != nil {
		return "", err
	}

	// Send to dcrd.
	n, ok := s.walletLoader.NetworkBackend()
	if !ok {
		return "", errNoNetwork
	}
	err = n.PublishTransactions(ctx, msgTx)
	if err != nil {
		return "", err
	}

	return msgTx.TxHash().String(), nil
}

// treasuryPolicy returns voting policies for treasury spends by a particular
// key.  If a key is specified, that policy is returned; otherwise the policies
// for all keys are returned in an array.  If both a key and ticket hash are
// provided, the per-ticket key policy is returned.
func (s *Server) treasuryPolicy(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.TreasuryPolicyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var ticketHash *chainhash.Hash
	if cmd.Ticket != nil && *cmd.Ticket != "" {
		var err error
		ticketHash, err = chainhash.NewHashFromStr(*cmd.Ticket)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
	}

	if cmd.Key != nil && *cmd.Key != "" {
		pikey, err := hex.DecodeString(*cmd.Key)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
		var policy string
		switch w.TreasuryKeyPolicy(pikey, ticketHash) {
		case stake.TreasuryVoteYes:
			policy = "yes"
		case stake.TreasuryVoteNo:
			policy = "no"
		default:
			policy = "abstain"
		}
		res := &types.TreasuryPolicyResult{
			Key:    *cmd.Key,
			Policy: policy,
		}
		if cmd.Ticket != nil {
			res.Ticket = *cmd.Ticket
		}
		return res, nil
	}

	policies := w.TreasuryKeyPolicies()
	res := make([]types.TreasuryPolicyResult, 0, len(policies))
	for i := range policies {
		var policy string
		switch policies[i].Policy {
		case stake.TreasuryVoteYes:
			policy = "yes"
		case stake.TreasuryVoteNo:
			policy = "no"
		}
		r := types.TreasuryPolicyResult{
			Key:    hex.EncodeToString(policies[i].PiKey),
			Policy: policy,
		}
		if policies[i].Ticket != nil {
			r.Ticket = policies[i].Ticket.String()
		}
		res = append(res, r)
	}
	return res, nil
}

// setDisapprovePercent sets the wallet's disapprove percentage.
func (s *Server) setDisapprovePercent(ctx context.Context, icmd any) (any, error) {
	if s.activeNet.Net == wire.MainNet {
		return nil, dcrjson.ErrInvalidRequest
	}
	cmd := icmd.(*types.SetDisapprovePercentCmd)
	if cmd.Percent > 100 {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter,
			errors.New("percent must be from 0 to 100"))
	}
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}
	w.SetDisapprovePercent(cmd.Percent)
	return nil, nil
}

// setTreasuryPolicy saves the voting policy for treasury spends by a particular
// key, and optionally, setting the key policy used by a specific ticket.
//
// If a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (s *Server) setTreasuryPolicy(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SetTreasuryPolicyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var ticketHash *chainhash.Hash
	if cmd.Ticket != nil && *cmd.Ticket != "" {
		if len(*cmd.Ticket) != chainhash.MaxHashStringSize {
			err := fmt.Errorf("invalid ticket hash length, expected %d got %d",
				chainhash.MaxHashStringSize, len(*cmd.Ticket))
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
		var err error
		ticketHash, err = chainhash.NewHashFromStr(*cmd.Ticket)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
	}

	pikey, err := hex.DecodeString(cmd.Key)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}
	if len(pikey) != secp256k1.PubKeyBytesLenCompressed {
		err := errors.New("treasury key must be 33 bytes")
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}
	var policy stake.TreasuryVoteT
	switch cmd.Policy {
	case "abstain", "invalid", "":
		policy = stake.TreasuryVoteInvalid
	case "yes":
		policy = stake.TreasuryVoteYes
	case "no":
		policy = stake.TreasuryVoteNo
	default:
		err := fmt.Errorf("unknown policy %q", cmd.Policy)
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}

	err = w.SetTreasuryKeyPolicy(ctx, pikey, policy, ticketHash)
	if err != nil {
		return nil, err
	}

	// Update voting preferences on VSPs if required.
	policyMap := map[string]string{
		cmd.Key: cmd.Policy,
	}
	err = s.updateVSPVoteChoices(ctx, w, ticketHash, nil, nil, policyMap)

	return nil, err
}

// tspendPolicy returns voting policies for particular treasury spends
// transactions.  If a tspend transaction hash is specified, that policy is
// returned; otherwise the policies for all known tspends are returned in an
// array.  If both a tspend transaction hash and a ticket hash are provided,
// the per-ticket tspend policy is returned.
func (s *Server) tspendPolicy(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.TSpendPolicyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var ticketHash *chainhash.Hash
	if cmd.Ticket != nil && *cmd.Ticket != "" {
		var err error
		ticketHash, err = chainhash.NewHashFromStr(*cmd.Ticket)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
	}

	if cmd.Hash != nil && *cmd.Hash != "" {
		hash, err := chainhash.NewHashFromStr(*cmd.Hash)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
		var policy string
		switch w.TSpendPolicy(hash, ticketHash) {
		case stake.TreasuryVoteYes:
			policy = "yes"
		case stake.TreasuryVoteNo:
			policy = "no"
		default:
			policy = "abstain"
		}
		res := &types.TSpendPolicyResult{
			Hash:   *cmd.Hash,
			Policy: policy,
		}
		if cmd.Ticket != nil {
			res.Ticket = *cmd.Ticket
		}
		return res, nil
	}

	tspends := w.GetAllTSpends(ctx)
	res := make([]types.TSpendPolicyResult, 0, len(tspends))
	for i := range tspends {
		tspendHash := tspends[i].TxHash()
		p := w.TSpendPolicy(&tspendHash, ticketHash)

		var policy string
		switch p {
		case stake.TreasuryVoteYes:
			policy = "yes"
		case stake.TreasuryVoteNo:
			policy = "no"
		}
		r := types.TSpendPolicyResult{
			Hash:   tspendHash.String(),
			Policy: policy,
		}
		if cmd.Ticket != nil {
			r.Ticket = *cmd.Ticket
		}
		res = append(res, r)
	}
	return res, nil
}

// setTSpendPolicy saves the voting policy for a particular tspend transaction
// hash, and optionally, setting the tspend policy used by a specific ticket.
//
// If a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (s *Server) setTSpendPolicy(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SetTSpendPolicyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	if len(cmd.Hash) != chainhash.MaxHashStringSize {
		err := fmt.Errorf("invalid tspend hash length, expected %d got %d",
			chainhash.MaxHashStringSize, len(cmd.Hash))
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	hash, err := chainhash.NewHashFromStr(cmd.Hash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	var ticketHash *chainhash.Hash
	if cmd.Ticket != nil && *cmd.Ticket != "" {
		if len(*cmd.Ticket) != chainhash.MaxHashStringSize {
			err := fmt.Errorf("invalid ticket hash length, expected %d got %d",
				chainhash.MaxHashStringSize, len(*cmd.Ticket))
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
		var err error
		ticketHash, err = chainhash.NewHashFromStr(*cmd.Ticket)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
	}

	var policy stake.TreasuryVoteT
	switch cmd.Policy {
	case "abstain", "invalid", "":
		policy = stake.TreasuryVoteInvalid
	case "yes":
		policy = stake.TreasuryVoteYes
	case "no":
		policy = stake.TreasuryVoteNo
	default:
		err := fmt.Errorf("unknown policy %q", cmd.Policy)
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}

	err = w.SetTSpendPolicy(ctx, hash, policy, ticketHash)
	if err != nil {
		return nil, err
	}

	// Update voting preferences on VSPs if required.
	policyMap := map[string]string{
		cmd.Hash: cmd.Policy,
	}
	err = s.updateVSPVoteChoices(ctx, w, ticketHash, nil, policyMap, nil)
	return nil, err
}

// redeemMultiSigOut receives a transaction hash/idx and fetches the first output
// index or indices with known script hashes from the transaction. It then
// construct a transaction with a single P2PKH paying to a specified address.
// It signs any inputs that it can, then provides the raw transaction to
// the user to export to others to sign.
func (s *Server) redeemMultiSigOut(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.RedeemMultiSigOutCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Convert the address to a useable format. If
	// we have no address, create a new address in
	// this wallet to send the output to.
	var addr stdaddr.Address
	var err error
	if cmd.Address != nil {
		addr, err = decodeAddress(*cmd.Address, w.ChainParams())
		if err != nil {
			return nil, err
		}
	} else {
		account := uint32(udb.DefaultAccountNum)
		addr, err = w.NewInternalAddress(ctx, account, wallet.WithGapPolicyWrap())
		if err != nil {
			return nil, err
		}
	}

	// Lookup the multisignature output and get the amount
	// along with the script for that transaction. Then,
	// begin crafting a MsgTx.
	hash, err := chainhash.NewHashFromStr(cmd.Hash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}
	op := wire.OutPoint{
		Hash:  *hash,
		Index: cmd.Index,
		Tree:  cmd.Tree,
	}
	p2shOutput, err := w.FetchP2SHMultiSigOutput(ctx, &op)
	if err != nil {
		return nil, err
	}
	sc := stdscript.DetermineScriptType(scriptVersionAssumed, p2shOutput.RedeemScript)
	if sc != stdscript.STMultiSig {
		return nil, errors.E("P2SH redeem script is not multisig")
	}
	msgTx := wire.NewMsgTx()
	txIn := wire.NewTxIn(&op, int64(p2shOutput.OutputAmount), nil)
	msgTx.AddTxIn(txIn)

	_, pkScript := addr.PaymentScript()

	err = w.PrepareRedeemMultiSigOutTxOutput(msgTx, p2shOutput, &pkScript)
	if err != nil {
		return nil, err
	}

	// Start creating the SignRawTransactionCmd.
	_, outpointScript := p2shOutput.P2SHAddress.PaymentScript()
	outpointScriptStr := hex.EncodeToString(outpointScript)

	rti := types.RawTxInput{
		Txid:         cmd.Hash,
		Vout:         cmd.Index,
		Tree:         cmd.Tree,
		ScriptPubKey: outpointScriptStr,
		RedeemScript: "",
	}
	rtis := []types.RawTxInput{rti}

	var b strings.Builder
	b.Grow(2 * msgTx.SerializeSize())
	err = msgTx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}
	sigHashAll := "ALL"

	srtc := &types.SignRawTransactionCmd{
		RawTx:    b.String(),
		Inputs:   &rtis,
		PrivKeys: &[]string{},
		Flags:    &sigHashAll,
	}

	// Sign it and give the results to the user.
	signedTxResult, err := s.signRawTransaction(ctx, srtc)
	if signedTxResult == nil || err != nil {
		return nil, err
	}
	srtTyped := signedTxResult.(types.SignRawTransactionResult)
	return types.RedeemMultiSigOutResult(srtTyped), nil
}

// redeemMultisigOuts receives a script hash (in the form of a
// script hash address), looks up all the unspent outpoints associated
// with that address, then generates a list of partially signed
// transactions spending to either an address specified or internal
// addresses in this wallet.
func (s *Server) redeemMultiSigOuts(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.RedeemMultiSigOutsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Get all the multisignature outpoints that are unspent for this
	// address.
	addr, err := decodeAddress(cmd.FromScrAddress, w.ChainParams())
	if err != nil {
		return nil, err
	}
	p2shAddr, ok := addr.(*stdaddr.AddressScriptHashV0)
	if !ok {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "address is not P2SH")
	}
	msos, err := wallet.UnstableAPI(w).UnspentMultisigCreditsForAddress(ctx, p2shAddr)
	if err != nil {
		return nil, err
	}
	max := uint32(0xffffffff)
	if cmd.Number != nil {
		max = uint32(*cmd.Number)
	}

	itr := uint32(0)
	rmsoResults := make([]types.RedeemMultiSigOutResult, len(msos))
	for i, mso := range msos {
		if itr > max {
			break
		}

		rmsoRequest := &types.RedeemMultiSigOutCmd{
			Hash:    mso.OutPoint.Hash.String(),
			Index:   mso.OutPoint.Index,
			Tree:    mso.OutPoint.Tree,
			Address: cmd.ToAddress,
		}
		redeemResult, err := s.redeemMultiSigOut(ctx, rmsoRequest)
		if err != nil {
			return nil, err
		}
		redeemResultTyped := redeemResult.(types.RedeemMultiSigOutResult)
		rmsoResults[i] = redeemResultTyped

		itr++
	}

	return types.RedeemMultiSigOutsResult{Results: rmsoResults}, nil
}

// rescanWallet initiates a rescan of the block chain for wallet data, blocking
// until the rescan completes or exits with an error.
func (s *Server) rescanWallet(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.RescanWalletCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, ok := s.walletLoader.NetworkBackend()
	if !ok {
		return nil, errNoNetwork
	}

	err := w.RescanFromHeight(ctx, n, int32(*cmd.BeginHeight))
	return nil, err
}

// spendOutputsInputSource creates an input source from a wallet and a list of
// outputs to be spent.  Only the provided outputs will be returned by the
// source, without any other input selection.
func spendOutputsInputSource(ctx context.Context, w *wallet.Wallet,
	account string, inputs []*wire.TxIn) (txauthor.InputSource, error) {

	params := w.ChainParams()

	detail := new(txauthor.InputDetail)
	detail.Inputs = inputs
	detail.Scripts = make([][]byte, len(inputs))
	detail.RedeemScriptSizes = make([]int, len(inputs))
	for i, in := range inputs {
		prevOut, err := w.FetchOutput(ctx, &in.PreviousOutPoint)
		if err != nil {
			return nil, err
		}
		detail.Amount += dcrutil.Amount(prevOut.Value)
		detail.Scripts[i] = prevOut.PkScript
		st, addrs := stdscript.ExtractAddrs(prevOut.Version,
			prevOut.PkScript, params)
		var addr stdaddr.Address
		var redeemScriptSize int
		switch st {
		case stdscript.STPubKeyHashEcdsaSecp256k1:
			addr = addrs[0]
			redeemScriptSize = txsizes.RedeemP2PKHInputSize
		default:
			// XXX: don't assume P2PKH, support other script types
			return nil, errors.E("unsupport address type")
		}
		ka, err := w.KnownAddress(ctx, addr)
		if err != nil {
			return nil, err
		}
		if ka.AccountName() != account {
			err := errors.Errorf("output address of %v does not "+
				"belong to account %q", &in.PreviousOutPoint,
				account)
			return nil, errors.E(errors.Invalid, err)
		}
		detail.RedeemScriptSizes[i] = redeemScriptSize
	}

	fn := func(target dcrutil.Amount) (*txauthor.InputDetail, error) {
		return detail, nil
	}
	return fn, nil
}

type accountChangeSource struct {
	ctx     context.Context
	wallet  *wallet.Wallet
	account uint32
	addr    stdaddr.Address
}

func (a *accountChangeSource) Script() (script []byte, version uint16, err error) {
	if a.addr == nil {
		addr, err := a.wallet.NewChangeAddress(a.ctx, a.account)
		if err != nil {
			return nil, 0, err
		}
		a.addr = addr
	}

	version, script = a.addr.PaymentScript()
	return
}

func (a *accountChangeSource) ScriptSize() int {
	// XXX: shouldn't assume P2PKH
	return txsizes.P2PKHOutputSize
}

// spendOutputs creates, signs and publishes a transaction that spends the
// specified outputs belonging to an account, pays a list of address/amount
// pairs, with any change returned to the specified account.
func (s *Server) spendOutputs(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SpendOutputsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	params := w.ChainParams()

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		return nil, err
	}

	inputs := make([]*wire.TxIn, 0, len(cmd.PreviousOutpoints))
	outputs := make([]*wire.TxOut, 0, len(cmd.Outputs))
	for _, outpointStr := range cmd.PreviousOutpoints {
		op, err := parseOutpoint(outpointStr)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
		inputs = append(inputs, wire.NewTxIn(op, wire.NullValueIn, nil))
	}
	for _, output := range cmd.Outputs {
		addr, err := stdaddr.DecodeAddress(output.Address, params)
		if err != nil {
			return nil, err
		}
		amount, err := dcrutil.NewAmount(output.Amount)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
		scriptVersion, script := addr.PaymentScript()
		txOut := wire.NewTxOut(int64(amount), script)
		txOut.Version = scriptVersion
		outputs = append(outputs, txOut)
	}
	rand.ShuffleSlice(inputs)
	rand.ShuffleSlice(outputs)

	inputSource, err := spendOutputsInputSource(ctx, w, cmd.Account,
		inputs)
	if err != nil {
		return nil, err
	}

	changeSource := &accountChangeSource{
		ctx:     ctx,
		wallet:  w,
		account: account,
	}

	secretsSource, err := w.SecretsSource()
	if err != nil {
		return nil, err
	}
	defer secretsSource.Close()

	atx, err := txauthor.NewUnsignedTransaction(outputs, w.RelayFee(),
		inputSource, changeSource, params.MaxTxSize)
	if err != nil {
		return nil, err
	}
	atx.RandomizeChangePosition()
	err = atx.AddAllInputScripts(secretsSource)
	if err != nil {
		return nil, err
	}

	hash, err := w.PublishTransaction(ctx, atx.Tx, n)
	if err != nil {
		return nil, err
	}
	return hash.String(), nil
}

func (s *Server) ticketInfo(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.TicketInfoCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	res := make([]types.TicketInfoResult, 0)

	start := wallet.NewBlockIdentifierFromHeight(*cmd.StartHeight)
	end := wallet.NewBlockIdentifierFromHeight(-1)
	tmptx := new(wire.MsgTx)
	err := w.GetTickets(ctx, func(ts []*wallet.TicketSummary, h *wire.BlockHeader) (bool, error) {
		for _, t := range ts {
			status := t.Status
			if status == wallet.TicketStatusUnmined {
				// Standardize on immature.  An unmined ticket
				// can be determined by the block height field
				// and the lack of a block hash.
				status = wallet.TicketStatusImmature
			}
			err := tmptx.Deserialize(bytes.NewReader(t.Ticket.Transaction))
			if err != nil {
				return false, err
			}
			out := tmptx.TxOut[0]
			info := types.TicketInfoResult{
				Hash:        t.Ticket.Hash.String(),
				Cost:        dcrutil.Amount(out.Value).ToCoin(),
				BlockHeight: -1,
				Status:      status.String(),
			}

			_, addrs := stdscript.ExtractAddrs(out.Version, out.PkScript, w.ChainParams())
			if len(addrs) == 0 {
				return false, errors.New("unable to decode ticket pkScript")
			}
			info.VotingAddress = addrs[0].String()
			if h != nil {
				info.BlockHash = h.BlockHash().String()
				info.BlockHeight = int32(h.Height)
			}
			if t.Spender != nil {
				hash := t.Spender.Hash.String()
				if t.Spender.Type == wallet.TransactionTypeRevocation {
					info.Revocation = hash
				} else {
					info.Vote = hash
				}
			}

			choices, _, err := w.AgendaChoices(ctx, t.Ticket.Hash)
			if err != nil {
				return false, err
			}
			info.Choices = make([]types.VoteChoice, 0, len(choices))
			for agendaID, choiceID := range choices {
				info.Choices = append(info.Choices, types.VoteChoice{
					AgendaID: agendaID,
					ChoiceID: choiceID,
				})
			}

			host, err := w.VSPHostForTicket(ctx, t.Ticket.Hash)
			if err != nil && !errors.Is(err, errors.NotExist) {
				return false, err
			}
			info.VSPHost = host

			res = append(res, info)
		}
		return false, nil
	}, start, end)

	return res, err
}

func isNilOrEmpty(s *string) bool {
	return s == nil || *s == ""
}

// sendFrom handles a sendfrom RPC request by creating a new transaction
// spending unspent transaction outputs for a wallet to another payment
// address.  Leftover inputs not sent to the payment address or a fee for
// the miner are sent back to a new address in the wallet.  Upon success,
// the TxID for the created transaction is returned.
func (s *Server) sendFrom(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SendFromCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Transaction comments are not yet supported.  Error instead of
	// pretending to save them.
	if !isNilOrEmpty(cmd.Comment) || !isNilOrEmpty(cmd.CommentTo) {
		return nil, rpcErrorf(dcrjson.ErrRPCUnimplemented, "transaction comments are unsupported")
	}

	account, err := w.AccountNumber(ctx, cmd.FromAccount)
	if err != nil {
		return nil, err
	}

	// Check that signed integer parameters are positive.
	if cmd.Amount < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative amount")
	}
	minConf := int32(*cmd.MinConf)
	if minConf < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative minconf")
	}
	// Create map of address and amount pairs.
	amt, err := dcrutil.NewAmount(cmd.Amount)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}
	pairs := map[string]dcrutil.Amount{
		cmd.ToAddress: amt,
	}

	return s.sendPairs(ctx, w, pairs, account, minConf)
}

// sendMany handles a sendmany RPC request by creating a new transaction
// spending unspent transaction outputs for a wallet to any number of
// payment addresses.  Leftover inputs not sent to the payment address
// or a fee for the miner are sent back to a new address in the wallet.
// Upon success, the TxID for the created transaction is returned.
func (s *Server) sendMany(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SendManyCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Transaction comments are not yet supported.  Error instead of
	// pretending to save them.
	if !isNilOrEmpty(cmd.Comment) {
		return nil, rpcErrorf(dcrjson.ErrRPCUnimplemented, "transaction comments are unsupported")
	}

	account, err := w.AccountNumber(ctx, cmd.FromAccount)
	if err != nil {
		return nil, err
	}

	// Check that minconf is positive.
	minConf := int32(*cmd.MinConf)
	if minConf < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative minconf")
	}

	// Recreate address/amount pairs, using dcrutil.Amount.
	pairs := make(map[string]dcrutil.Amount, len(cmd.Amounts))
	for k, v := range cmd.Amounts {
		amt, err := dcrutil.NewAmount(v)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
		pairs[k] = amt
	}

	return s.sendPairs(ctx, w, pairs, account, minConf)
}

// sendToAddress handles a sendtoaddress RPC request by creating a new
// transaction spending unspent transaction outputs for a wallet to another
// payment address.  Leftover inputs not sent to the payment address or a fee
// for the miner are sent back to a new address in the wallet.  Upon success,
// the TxID for the created transaction is returned.
func (s *Server) sendToAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SendToAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Transaction comments are not yet supported.  Error instead of
	// pretending to save them.
	if !isNilOrEmpty(cmd.Comment) || !isNilOrEmpty(cmd.CommentTo) {
		return nil, rpcErrorf(dcrjson.ErrRPCUnimplemented, "transaction comments are unsupported")
	}

	amt, err := dcrutil.NewAmount(cmd.Amount)
	if err != nil {
		return nil, err
	}

	// Check that signed integer parameters are positive.
	if amt < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative amount")
	}

	// Mock up map of address and amount pairs.
	pairs := map[string]dcrutil.Amount{
		cmd.Address: amt,
	}

	// sendtoaddress always spends from the default account, this matches bitcoind
	return s.sendPairs(ctx, w, pairs, udb.DefaultAccountNum, 1)
}

// sendToMultiSig handles a sendtomultisig RPC request by creating a new
// transaction spending amount many funds to an output containing a multi-
// signature script hash. The function will fail if there isn't at least one
// public key in the public key list that corresponds to one that is owned
// locally.
// Upon successfully sending the transaction to the daemon, the script hash
// is stored in the transaction manager and the corresponding address
// specified to be watched by the daemon.
// The function returns a tx hash, P2SH address, and a multisig script if
// successful.
// TODO Use with non-default accounts as well
func (s *Server) sendToMultiSig(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SendToMultiSigCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account := uint32(udb.DefaultAccountNum)
	amount, err := dcrutil.NewAmount(cmd.Amount)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}
	nrequired := int8(*cmd.NRequired)
	minconf := int32(*cmd.MinConf)

	pubKeys, err := walletPubKeys(ctx, w, cmd.Pubkeys)
	if err != nil {
		return nil, err
	}

	tx, addr, script, err :=
		w.CreateMultisigTx(ctx, account, amount, pubKeys, nrequired, minconf)
	if err != nil {
		return nil, err
	}

	result := &types.SendToMultiSigResult{
		TxHash:       tx.MsgTx.TxHash().String(),
		Address:      addr.String(),
		RedeemScript: hex.EncodeToString(script),
	}

	log.Infof("Successfully sent funds to multisignature output in "+
		"transaction %v", tx.MsgTx.TxHash().String())

	return result, nil
}

// sendRawTransaction handles a sendrawtransaction RPC request by decoding hex
// transaction and sending it to the network backend for propagation.
func (s *Server) sendRawTransaction(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SendRawTransactionCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	msgtx := wire.NewMsgTx()
	err = msgtx.Deserialize(hex.NewDecoder(strings.NewReader(cmd.HexTx)))
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDeserialization, err)
	}

	if !*cmd.AllowHighFees {
		highFees, err := txrules.TxPaysHighFees(msgtx)
		if err != nil {
			return nil, err
		}
		if highFees {
			return nil, errors.E(errors.Policy, "high fees")
		}
	}

	txHash, err := w.PublishTransaction(ctx, msgtx, n)
	if err != nil {
		return nil, err
	}

	return txHash.String(), nil
}

// sendToTreasury handles a sendtotreasury RPC request by creating a new
// transaction spending unspent transaction outputs for a wallet to the
// treasury.  Leftover inputs not sent to the payment address or a fee for the
// miner are sent back to a new address in the wallet.  Upon success, the TxID
// for the created transaction is returned.
func (s *Server) sendToTreasury(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SendToTreasuryCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	amt, err := dcrutil.NewAmount(cmd.Amount)
	if err != nil {
		return nil, err
	}

	// Check that signed integer parameters are positive.
	if amt <= 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative amount")
	}

	// sendtotreasury always spends from the default account.
	return s.sendAmountToTreasury(ctx, w, amt, udb.DefaultAccountNum, 1)
}

// transaction spending treasury balance.
// Upon success, the TxID for the created transaction is returned.
func (s *Server) sendFromTreasury(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SendFromTreasuryCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	return s.sendOutputsFromTreasury(ctx, w, *cmd)
}

// setTxFee sets the transaction fee per kilobyte added to transactions.
func (s *Server) setTxFee(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SetTxFeeCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Check that amount is not negative.
	if cmd.Amount < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative amount")
	}

	relayFee, err := dcrutil.NewAmount(cmd.Amount)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}
	w.SetRelayFee(relayFee)

	// A boolean true result is returned upon success.
	return true, nil
}

// setVoteChoice handles a setvotechoice request by modifying the preferred
// choice for a voting agenda.
//
// If a VSP host is configured in the application settings, the voting
// preferences will also be set with the VSP.
func (s *Server) setVoteChoice(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SetVoteChoiceCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var ticketHash *chainhash.Hash
	if cmd.TicketHash != nil {
		hash, err := chainhash.NewHashFromStr(*cmd.TicketHash)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
		ticketHash = hash
	}

	choice := map[string]string{
		cmd.AgendaID: cmd.ChoiceID,
	}

	_, err := w.SetAgendaChoices(ctx, ticketHash, choice)
	if err != nil {
		return nil, err
	}

	// Update voting preferences on VSPs if required.
	err = s.updateVSPVoteChoices(ctx, w, ticketHash, choice, nil, nil)
	return nil, err
}

func (s *Server) updateVSPVoteChoices(ctx context.Context, w *wallet.Wallet, ticketHash *chainhash.Hash,
	choices map[string]string, tspendPolicy map[string]string, treasuryPolicy map[string]string) error {

	if ticketHash != nil {
		vspHost, err := w.VSPHostForTicket(ctx, ticketHash)
		if err != nil {
			if errors.Is(err, errors.NotExist) {
				// Ticket is not registered with a VSP, nothing more to do here.
				return nil
			}
			return err
		}
		vspClient, err := w.LookupVSP(vspHost)
		if err != nil {
			return err
		}

		ticket, err := w.NewVSPTicket(ctx, ticketHash)
		if err != nil {
			return err
		}

		err = vspClient.SetVoteChoice(ctx, ticket, choices, tspendPolicy, treasuryPolicy)
		return err
	}

	err := w.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		vspHost, err := w.VSPHostForTicket(ctx, hash)
		if err != nil {
			if errors.Is(err, errors.NotExist) {
				// Ticket is not registered with a VSP, nothing more to do here.
				return nil
			}
			return err
		}
		vspClient, err := w.LookupVSP(vspHost)
		if err != nil {
			return err
		}

		ticket, err := w.NewVSPTicket(ctx, hash)
		if err != nil {
			return err
		}

		// Never return errors here, so all tickets are tried.
		// The first error will be returned to the user.
		err = vspClient.SetVoteChoice(ctx, ticket, choices, tspendPolicy, treasuryPolicy)
		if err != nil {
			return err
		}
		return nil
	})
	return err
}

// signMessage signs the given message with the private key for the given
// address
func (s *Server) signMessage(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SignMessageCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		return nil, err
	}
	sig, err := w.SignMessage(ctx, cmd.Message, addr)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAddressNotInWallet
		}
		if errors.Is(err, errors.Locked) {
			return nil, errWalletUnlockNeeded
		}
		return nil, err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// signRawTransaction handles the signrawtransaction command.
//
// chainClient may be nil, in which case it was called by the NoChainRPC
// variant.  It must be checked before all usage.
func (s *Server) signRawTransaction(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SignRawTransactionCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	tx := wire.NewMsgTx()
	err := tx.Deserialize(hex.NewDecoder(strings.NewReader(cmd.RawTx)))
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDeserialization, err)
	}
	if len(tx.TxIn) == 0 {
		err := errors.New("transaction with no inputs cannot be signed")
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}

	var hashType txscript.SigHashType
	switch *cmd.Flags {
	case "ALL":
		hashType = txscript.SigHashAll
	case "NONE":
		hashType = txscript.SigHashNone
	case "SINGLE":
		hashType = txscript.SigHashSingle
	case "ALL|ANYONECANPAY":
		hashType = txscript.SigHashAll | txscript.SigHashAnyOneCanPay
	case "NONE|ANYONECANPAY":
		hashType = txscript.SigHashNone | txscript.SigHashAnyOneCanPay
	case "SINGLE|ANYONECANPAY":
		hashType = txscript.SigHashSingle | txscript.SigHashAnyOneCanPay
	case "ssgen": // Special case of SigHashAll
		hashType = txscript.SigHashAll
	case "ssrtx": // Special case of SigHashAll
		hashType = txscript.SigHashAll
	default:
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "invalid sighash flag")
	}

	// TODO: really we probably should look these up with dcrd anyway to
	// make sure that they match the blockchain if present.
	inputs := make(map[wire.OutPoint][]byte)
	scripts := make(map[string][]byte)
	var cmdInputs []types.RawTxInput
	if cmd.Inputs != nil {
		cmdInputs = *cmd.Inputs
	}
	for _, rti := range cmdInputs {
		inputSha, err := chainhash.NewHashFromStr(rti.Txid)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}

		script, err := decodeHexStr(rti.ScriptPubKey)
		if err != nil {
			return nil, err
		}

		// redeemScript is only actually used iff the user provided
		// private keys. In which case, it is used to get the scripts
		// for signing. If the user did not provide keys then we always
		// get scripts from the wallet.
		// Empty strings are ok for this one and hex.DecodeString will
		// DTRT.
		// Note that redeemScript is NOT only the redeemscript
		// required to be appended to the end of a P2SH output
		// spend, but the entire signature script for spending
		// *any* outpoint with dummy values inserted into it
		// that can later be replacing by txscript's sign.
		if cmd.PrivKeys != nil && len(*cmd.PrivKeys) != 0 {
			redeemScript, err := decodeHexStr(rti.RedeemScript)
			if err != nil {
				return nil, err
			}

			addr, err := stdaddr.NewAddressScriptHashV0(redeemScript,
				w.ChainParams())
			if err != nil {
				return nil, err
			}
			scripts[addr.String()] = redeemScript
		}
		inputs[wire.OutPoint{
			Hash:  *inputSha,
			Tree:  rti.Tree,
			Index: rti.Vout,
		}] = script
	}

	// Now we go and look for any inputs that we were not provided by
	// querying dcrd with getrawtransaction. We queue up a bunch of async
	// requests and will wait for replies after we have checked the rest of
	// the arguments.
	requested := make(map[wire.OutPoint]*dcrdtypes.GetTxOutResult)
	var requestedMu sync.Mutex
	requestedGroup, gctx := errgroup.WithContext(ctx)
	n, _ := s.walletLoader.NetworkBackend()
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		for i, txIn := range tx.TxIn {
			// We don't need the first input of a stakebase tx, as it's garbage
			// anyway.
			if i == 0 && *cmd.Flags == "ssgen" {
				continue
			}

			// Did we get this outpoint from the arguments?
			if _, ok := inputs[txIn.PreviousOutPoint]; ok {
				continue
			}

			// Asynchronously request the output script.
			txIn := txIn
			requestedGroup.Go(func() error {
				hash := &txIn.PreviousOutPoint.Hash
				index := txIn.PreviousOutPoint.Index
				tree := txIn.PreviousOutPoint.Tree
				res, err := chainSyncer.GetTxOut(gctx, hash, index, tree, true)
				if err != nil {
					return err
				}
				requestedMu.Lock()
				requested[txIn.PreviousOutPoint] = res
				requestedMu.Unlock()
				return nil
			})
		}
	}

	// Parse list of private keys, if present. If there are any keys here
	// they are the keys that we may use for signing. If empty we will
	// use any keys known to us already.
	var keys map[string]*dcrutil.WIF
	if cmd.PrivKeys != nil {
		keys = make(map[string]*dcrutil.WIF)

		for _, key := range *cmd.PrivKeys {
			wif, err := dcrutil.DecodeWIF(key, w.ChainParams().PrivateKeyID)
			if err != nil {
				return nil, rpcError(dcrjson.ErrRPCDeserialization, err)
			}

			var addr stdaddr.Address
			switch wif.DSA() {
			case dcrec.STEcdsaSecp256k1:
				addr, err = stdaddr.NewAddressPubKeyEcdsaSecp256k1V0Raw(
					wif.PubKey(), w.ChainParams())
				if err != nil {
					return nil, err
				}
			case dcrec.STEd25519:
				addr, err = stdaddr.NewAddressPubKeyEd25519V0Raw(
					wif.PubKey(), w.ChainParams())
				if err != nil {
					return nil, err
				}
			case dcrec.STSchnorrSecp256k1:
				addr, err = stdaddr.NewAddressPubKeySchnorrSecp256k1V0Raw(
					wif.PubKey(), w.ChainParams())
				if err != nil {
					return nil, err
				}
			}
			keys[addr.String()] = wif

			// Add the pubkey hash variant for supported addresses as well.
			if pkH, ok := addr.(stdaddr.AddressPubKeyHasher); ok {
				keys[pkH.AddressPubKeyHash().String()] = wif
			}
		}
	}

	// We have checked the rest of the args. now we can collect the async
	// txs.
	err = requestedGroup.Wait()
	if err != nil {
		return nil, err
	}
	for outPoint, result := range requested {
		// gettxout returns JSON null if the output is found, but is spent by
		// another transaction in the main chain.
		if result == nil {
			continue
		}
		script, err := hex.DecodeString(result.ScriptPubKey.Hex)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
		}
		inputs[outPoint] = script
	}

	// All args collected. Now we can sign all the inputs that we can.
	// `complete' denotes that we successfully signed all outputs and that
	// all scripts will run to completion. This is returned as part of the
	// reply.
	signErrs, signErr := w.SignTransaction(ctx, tx, hashType, inputs, keys, scripts)

	var b strings.Builder
	b.Grow(2 * tx.SerializeSize())
	err = tx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}

	signErrors := make([]types.SignRawTransactionError, 0, len(signErrs))
	for _, e := range signErrs {
		input := tx.TxIn[e.InputIndex]
		signErrors = append(signErrors, types.SignRawTransactionError{
			TxID:      input.PreviousOutPoint.Hash.String(),
			Vout:      input.PreviousOutPoint.Index,
			ScriptSig: hex.EncodeToString(input.SignatureScript),
			Sequence:  input.Sequence,
			Error:     e.Error.Error(),
		})
	}

	return types.SignRawTransactionResult{
		Hex:      b.String(),
		Complete: len(signErrors) == 0 && signErr == nil,
		Errors:   signErrors,
	}, nil
}

// signRawTransactions handles the signrawtransactions command.
func (s *Server) signRawTransactions(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SignRawTransactionsCmd)

	// Sign each transaction sequentially and record the results.
	// Error out if we meet some unexpected failure.
	results := make([]types.SignRawTransactionResult, len(cmd.RawTxs))
	for i, etx := range cmd.RawTxs {
		flagAll := "ALL"
		srtc := &types.SignRawTransactionCmd{
			RawTx: etx,
			Flags: &flagAll,
		}
		result, err := s.signRawTransaction(ctx, srtc)
		if err != nil {
			return nil, err
		}

		tResult := result.(types.SignRawTransactionResult)
		results[i] = tResult
	}

	// If the user wants completed transactions to be automatically send,
	// do that now. Otherwise, construct the slice and return it.
	toReturn := make([]types.SignedTransaction, len(cmd.RawTxs))

	if *cmd.Send {
		n, ok := s.walletLoader.NetworkBackend()
		if !ok {
			return nil, errNoNetwork
		}

		for i, result := range results {
			if result.Complete {
				// Slow/mem hungry because of the deserializing.
				msgTx := wire.NewMsgTx()
				err := msgTx.Deserialize(hex.NewDecoder(strings.NewReader(result.Hex)))
				if err != nil {
					return nil, rpcError(dcrjson.ErrRPCDeserialization, err)
				}
				sent := false
				hashStr := ""
				err = n.PublishTransactions(ctx, msgTx)
				// If sendrawtransaction errors out (blockchain rule
				// issue, etc), continue onto the next transaction.
				if err == nil {
					sent = true
					hashStr = msgTx.TxHash().String()
				}

				st := types.SignedTransaction{
					SigningResult: result,
					Sent:          sent,
					TxHash:        &hashStr,
				}
				toReturn[i] = st
			} else {
				st := types.SignedTransaction{
					SigningResult: result,
					Sent:          false,
					TxHash:        nil,
				}
				toReturn[i] = st
			}
		}
	} else { // Just return the results.
		for i, result := range results {
			st := types.SignedTransaction{
				SigningResult: result,
				Sent:          false,
				TxHash:        nil,
			}
			toReturn[i] = st
		}
	}

	return &types.SignRawTransactionsResult{Results: toReturn}, nil
}

// scriptChangeSource is a ChangeSource which is used to
// receive all correlated previous input value.
type scriptChangeSource struct {
	version uint16
	script  []byte
}

func (src *scriptChangeSource) Script() ([]byte, uint16, error) {
	return src.script, src.version, nil
}

func (src *scriptChangeSource) ScriptSize() int {
	return len(src.script)
}

func makeScriptChangeSource(address string, params *chaincfg.Params) (*scriptChangeSource, error) {
	destinationAddress, err := stdaddr.DecodeAddress(address, params)
	if err != nil {
		return nil, err
	}
	version, script := destinationAddress.PaymentScript()
	source := &scriptChangeSource{
		version: version,
		script:  script,
	}
	return source, nil
}

func sumOutputValues(outputs []*wire.TxOut) (totalOutput dcrutil.Amount) {
	for _, txOut := range outputs {
		totalOutput += dcrutil.Amount(txOut.Value)
	}
	return totalOutput
}

// sweepAccount handles the sweepaccount command.
func (s *Server) sweepAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SweepAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// use provided fee per Kb if specified
	feePerKb := w.RelayFee()
	if cmd.FeePerKb != nil {
		var err error
		feePerKb, err = dcrutil.NewAmount(*cmd.FeePerKb)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
	}

	// use provided required confirmations if specified
	requiredConfs := int32(1)
	if cmd.RequiredConfirmations != nil {
		requiredConfs = int32(*cmd.RequiredConfirmations)
		if requiredConfs < 0 {
			return nil, errNeedPositiveAmount
		}
	}

	account, err := w.AccountNumber(ctx, cmd.SourceAccount)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	changeSource, err := makeScriptChangeSource(cmd.DestinationAddress, w.ChainParams())
	if err != nil {
		return nil, err
	}
	tx, err := w.NewUnsignedTransaction(ctx, nil, feePerKb, account,
		requiredConfs, wallet.OutputSelectionAlgorithmAll, changeSource, nil)
	if err != nil {
		if errors.Is(err, errors.InsufficientBalance) {
			return nil, rpcError(dcrjson.ErrRPCWalletInsufficientFunds, err)
		}
		return nil, err
	}

	var b strings.Builder
	b.Grow(2 * tx.Tx.SerializeSize())
	err = tx.Tx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}

	res := &types.SweepAccountResult{
		UnsignedTransaction:       b.String(),
		TotalPreviousOutputAmount: tx.TotalInput.ToCoin(),
		TotalOutputAmount:         sumOutputValues(tx.Tx.TxOut).ToCoin(),
		EstimatedSignedSize:       uint32(tx.EstimatedSignedSerializeSize),
	}

	return res, nil
}

// validateAddress handles the validateaddress command.
func (s *Server) validateAddress(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.ValidateAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	result := types.ValidateAddressResult{}
	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		result.Script = stdscript.STNonStandard.String()
		// Use result zero value (IsValid=false).
		return result, nil
	}

	result.Address = addr.String()
	result.IsValid = true
	ver, scr := addr.PaymentScript()
	class, _ := stdscript.ExtractAddrs(ver, scr, w.ChainParams())
	result.Script = class.String()
	if pker, ok := addr.(stdaddr.SerializedPubKeyer); ok {
		result.PubKey = hex.EncodeToString(pker.SerializedPubKey())
		result.PubKeyAddr = addr.String()
	}
	if class == stdscript.STScriptHash {
		result.IsScript = true
	}
	if _, ok := addr.(stdaddr.Hash160er); ok {
		result.IsCompressed = true
	}

	ka, err := w.KnownAddress(ctx, addr)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			// No additional information available about the address.
			return result, nil
		}
		return nil, err
	}

	// The address lookup was successful which means there is further
	// information about it available and it is "mine".
	result.IsMine = true
	result.Account = ka.AccountName()

	switch ka := ka.(type) {
	case wallet.PubKeyHashAddress:
		pubKey := ka.PubKey()
		result.PubKey = hex.EncodeToString(pubKey)
		pubKeyAddr, err := stdaddr.NewAddressPubKeyEcdsaSecp256k1V0Raw(pubKey, w.ChainParams())
		if err != nil {
			return nil, err
		}
		result.PubKeyAddr = pubKeyAddr.String()
	case wallet.P2SHAddress:
		version, script := ka.RedeemScript()
		result.Hex = hex.EncodeToString(script)

		class, addrs := stdscript.ExtractAddrs(version, script, w.ChainParams())
		addrStrings := make([]string, len(addrs))
		for i, a := range addrs {
			addrStrings[i] = a.String()
		}
		result.Addresses = addrStrings
		result.Script = class.String()

		// Multi-signature scripts also provide the number of required
		// signatures.
		if class == stdscript.STMultiSig {
			result.SigsRequired = int32(stdscript.DetermineRequiredSigs(version, script))
		}
	}

	if ka, ok := ka.(wallet.BIP0044Address); ok {
		acct, branch, child := ka.Path()
		if ka.AccountKind() != wallet.AccountKindImportedXpub {
			result.AccountN = &acct
		}
		result.Branch = &branch
		result.Index = &child
	}

	return result, nil
}

// validatePreDCP0005CF handles the validatepredcp0005cf command.
func (s *Server) validatePreDCP0005CF(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	err := w.ValidatePreDCP0005CFilters(ctx)
	return err == nil, err
}

// verifyMessage handles the verifymessage command by verifying the provided
// compact signature for the given address and message.
func (s *Server) verifyMessage(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.VerifyMessageCmd)

	var valid bool

	// Decode address and base64 signature from the request.
	addr, err := stdaddr.DecodeAddress(cmd.Address, s.activeNet)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(cmd.Signature)
	if err != nil {
		return nil, err
	}

	// Addresses must have an associated secp256k1 private key and must be P2PKH
	// (P2PK and P2SH is not allowed).
	switch addr.(type) {
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
	default:
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
			"address must be secp256k1 pay-to-pubkey-hash")
	}

	valid, err = wallet.VerifyMessage(cmd.Message, addr, sig, s.activeNet)
	// Mirror Bitcoin Core behavior, which treats all erorrs as an invalid
	// signature.
	return err == nil && valid, nil
}

// version handles the version command by returning the RPC API versions of the
// wallet and, optionally, the consensus RPC server as well if it is associated
// with the server.  The chainClient is optional, and this is simply a helper
// function for the versionWithChainRPC and versionNoChainRPC handlers.
func (s *Server) version(ctx context.Context, icmd any) (any, error) {
	resp := make(map[string]dcrdtypes.VersionResult)
	n, _ := s.walletLoader.NetworkBackend()
	if chainSyncer, ok := n.(*chain.Syncer); ok {
		err := chainSyncer.RPC().Call(ctx, "version", &resp)
		if err != nil {
			return nil, err
		}
	}

	resp["dcrwallet"] = dcrdtypes.VersionResult{
		VersionString: version.String(),
		Major:         version.Major,
		Minor:         version.Minor,
		Patch:         version.Patch,
		Prerelease:    version.PreRelease,
		BuildMetadata: version.BuildMetadata,
	}
	resp["dcrwalletjsonrpcapi"] = dcrdtypes.VersionResult{
		VersionString: jsonrpcSemverString,
		Major:         jsonrpcSemverMajor,
		Minor:         jsonrpcSemverMinor,
		Patch:         jsonrpcSemverPatch,
	}
	return resp, nil
}

// walletInfo gets the current information about the wallet. If the daemon
// is connected and fails to ping, the function will still return that the
// daemon is disconnected.
func (s *Server) walletInfo(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var connected, spvMode bool
	switch n, _ := w.NetworkBackend(); syncer := n.(type) {
	case *spv.Syncer:
		spvMode = true
		connected = len(syncer.GetRemotePeers()) > 0
	case *chain.Syncer:
		err := syncer.RPC().Call(ctx, "ping", nil)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			log.Warnf("Ping failed on connected daemon client: %v", err)
		} else {
			connected = true
		}
	case nil:
		log.Warnf("walletInfo - no network backend")
	default:
		log.Errorf("walletInfo - invalid network backend (%T).", n)
		return nil, &dcrjson.RPCError{
			Code:    dcrjson.ErrRPCMisc,
			Message: "invalid network backend",
		}
	}

	coinType, err := w.CoinType(ctx)
	if errors.Is(err, errors.WatchingOnly) {
		// This is a watching-only wallet, which does not store the active coin
		// type. Return CoinTypes default value (0), which will be omitted from
		// the JSON response, and log a debug message.
		log.Debug("Watching only wallets do not store the coin type keys.")
	} else if err != nil {
		log.Errorf("Failed to retrieve the active coin type: %v", err)
		coinType = 0
	}

	unlocked := !(w.Locked())
	fi := w.RelayFee()
	voteBits := w.VoteBits()
	var voteVersion uint32
	_ = binary.Read(bytes.NewBuffer(voteBits.ExtendedBits[0:4]), binary.LittleEndian, &voteVersion)
	voting := w.VotingEnabled()

	wi := &types.WalletInfoResult{
		DaemonConnected:  connected,
		SPV:              spvMode,
		Unlocked:         unlocked,
		CoinType:         coinType,
		TxFee:            fi.ToCoin(),
		VoteBits:         voteBits.Bits,
		VoteBitsExtended: hex.EncodeToString(voteBits.ExtendedBits),
		VoteVersion:      voteVersion,
		Voting:           voting,
		VSP:              s.cfg.VSPHost,
		ManualTickets:    w.ManualTickets(),
	}

	birthState, err := w.BirthState(ctx)
	if err != nil {
		log.Errorf("Failed to get birth state: %v", err)
	} else if birthState != nil &&
		!(birthState.SetFromTime || birthState.SetFromHeight) {
		wi.BirthHash = birthState.Hash.String()
		wi.BirthHeight = birthState.Height
	}

	return wi, nil
}

// walletIsLocked handles the walletislocked extension request by
// returning the current lock state (false for unlocked, true for locked)
// of an account.
func (s *Server) walletIsLocked(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	return w.Locked(), nil
}

// walletLock handles a walletlock request by locking the all account
// wallets, returning an error if any wallet is not encrypted (for example,
// a watching-only wallet).
func (s *Server) walletLock(ctx context.Context, icmd any) (any, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	w.Lock()
	return nil, nil
}

// walletPassphrase responds to the walletpassphrase request by unlocking the
// wallet. The decryption key is saved in the wallet until timeout seconds
// expires, after which the wallet is locked. A timeout of 0 leaves the wallet
// unlocked indefinitely.
func (s *Server) walletPassphrase(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.WalletPassphraseCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	timeout := time.Second * time.Duration(cmd.Timeout)
	var unlockAfter <-chan time.Time
	if timeout != 0 {
		unlockAfter = time.After(timeout)
	}
	err := w.Unlock(ctx, []byte(cmd.Passphrase), unlockAfter)
	return nil, err
}

// walletPassphraseChange responds to the walletpassphrasechange request
// by unlocking all accounts with the provided old passphrase, and
// re-encrypting each private key with an AES key derived from the new
// passphrase.
//
// If the old passphrase is correct and the passphrase is changed, all
// wallets will be immediately locked.
func (s *Server) walletPassphraseChange(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.WalletPassphraseChangeCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	err := w.ChangePrivatePassphrase(ctx, []byte(cmd.OldPassphrase),
		[]byte(cmd.NewPassphrase))
	if err != nil {
		if errors.Is(err, errors.Passphrase) {
			return nil, rpcErrorf(dcrjson.ErrRPCWalletPassphraseIncorrect, "incorrect passphrase")
		}
		return nil, err
	}
	return nil, nil
}

func (s *Server) mixOutput(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.MixOutputCmd)
	if !s.cfg.MixingEnabled {
		return nil, errors.E("Mixing is not configured")
	}
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	outpoint, err := parseOutpoint(cmd.Outpoint)
	if err != nil {
		return nil, err
	}

	mixAccount, err := w.AccountNumber(ctx, s.cfg.MixAccount)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	changeAccount, err := w.AccountNumber(ctx, s.cfg.MixChangeAccount)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	mixBranch := s.cfg.MixBranch

	err = w.MixOutput(ctx, outpoint, changeAccount, mixAccount, mixBranch)
	return nil, err
}

func (s *Server) mixAccount(ctx context.Context, icmd any) (any, error) {
	if !s.cfg.MixingEnabled {
		return nil, errors.E("Mixing is not configured")
	}
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	mixAccount, err := w.AccountNumber(ctx, s.cfg.MixAccount)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	changeAccount, err := w.AccountNumber(ctx, s.cfg.MixChangeAccount)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	mixBranch := s.cfg.MixBranch

	err = w.MixAccount(ctx, changeAccount, mixAccount, mixBranch)
	return nil, err
}

func parseOutpoint(s string) (*wire.OutPoint, error) {
	const op errors.Op = "parseOutpoint"
	if len(s) < 66 {
		return nil, errors.E(op, "bad len")
	}
	if s[64] != ':' { // sep follows 32 bytes of hex
		return nil, errors.E(op, "bad separator")
	}
	hash, err := chainhash.NewHashFromStr(s[:64])
	if err != nil {
		return nil, errors.E(op, err)
	}
	index, err := strconv.ParseUint(s[65:], 10, 32)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return &wire.OutPoint{Hash: *hash, Index: uint32(index)}, nil
}

// walletPubPassphraseChange responds to the walletpubpassphrasechange request
// by modifying the public passphrase of the wallet.
func (s *Server) walletPubPassphraseChange(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.WalletPubPassphraseChangeCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	err := w.ChangePublicPassphrase(ctx, []byte(cmd.OldPassphrase),
		[]byte(cmd.NewPassphrase))
	if errors.Is(errors.Passphrase, err) {
		return nil, rpcErrorf(dcrjson.ErrRPCWalletPassphraseIncorrect, "incorrect passphrase")
	}
	return nil, err
}

func (s *Server) setAccountPassphrase(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.SetAccountPassphraseCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	err = w.SetAccountPassphrase(ctx, account, []byte(cmd.Passphrase))
	return nil, err
}

func (s *Server) accountUnlocked(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.AccountUnlockedCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}

	encrypted, err := w.AccountHasPassphrase(ctx, account)
	if err != nil {
		return nil, err
	}
	if !encrypted {
		return &types.AccountUnlockedResult{}, nil
	}

	unlocked, err := w.AccountUnlocked(ctx, account)
	if err != nil {
		return nil, err
	}

	return &types.AccountUnlockedResult{
		Encrypted: true,
		Unlocked:  &unlocked,
	}, nil
}

func (s *Server) unlockAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.UnlockAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	err = w.UnlockAccount(ctx, account, []byte(cmd.Passphrase))
	return nil, err
}

func (s *Server) lockAccount(ctx context.Context, icmd any) (any, error) {
	cmd := icmd.(*types.LockAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	if cmd.Account == "*" {
		a, err := w.Accounts(ctx)
		if err != nil {
			return nil, err
		}
		for _, acct := range a.Accounts {
			if acct.AccountEncrypted && acct.AccountUnlocked {
				err = w.LockAccount(ctx, acct.AccountNumber)
				if err != nil {
					return nil, err
				}
			}
		}
		return nil, nil
	}

	account, err := w.AccountNumber(ctx, cmd.Account)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAccountNotFound
		}
		return nil, err
	}
	err = w.LockAccount(ctx, account)
	return nil, err
}

// decodeHexStr decodes the hex encoding of a string, possibly prepending a
// leading '0' character if there is an odd number of bytes in the hex string.
// This is to prevent an error for an invalid hex string when using an odd
// number of bytes when calling hex.Decode.
func decodeHexStr(hexStr string) ([]byte, error) {
	if len(hexStr)%2 != 0 {
		hexStr = "0" + hexStr
	}
	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, rpcErrorf(dcrjson.ErrRPCDecodeHexString, "hex string decode failed: %v", err)
	}
	return decoded, nil
}

func (s *Server) getcoinjoinsbyacct(ctx context.Context, icmd any) (any, error) {
	_ = icmd.(*types.GetCoinjoinsByAcctCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	acctCoinjoinsSum, err := w.GetCoinjoinTxsSumbByAcct(ctx)
	if err != nil {
		if errors.Is(err, errors.Passphrase) {
			return nil, rpcErrorf(dcrjson.ErrRPCWalletPassphraseIncorrect, "incorrect passphrase")
		}
		return nil, err
	}

	acctNameCoinjoinSum := map[string]int{}
	for acctIdx, coinjoinSum := range acctCoinjoinsSum {
		accountName, err := w.AccountName(ctx, acctIdx)
		if err != nil {
			// Expect account lookup to succeed
			if errors.Is(err, errors.NotExist) {
				return nil, rpcError(dcrjson.ErrRPCInternal.Code, err)
			}
			return nil, err
		}
		acctNameCoinjoinSum[accountName] = coinjoinSum
	}

	return acctNameCoinjoinSum, nil
}
