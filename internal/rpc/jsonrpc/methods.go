// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
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
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v2"
	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/p2p/v2"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	"github.com/decred/dcrwallet/rpc/jsonrpc/types"
	"github.com/decred/dcrwallet/version"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"golang.org/x/sync/errgroup"
)

// API version constants
const (
	jsonrpcSemverString = "6.2.0"
	jsonrpcSemverMajor  = 6
	jsonrpcSemverMinor  = 2
	jsonrpcSemverPatch  = 0
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
	// Reference implementation wallet methods (implemented)
	"abandontransaction":      {fn: (*Server).abandonTransaction},
	"accountaddressindex":     {fn: (*Server).accountAddressIndex},
	"accountsyncaddressindex": {fn: (*Server).accountSyncAddressIndex},
	"addmultisigaddress":      {fn: (*Server).addMultiSigAddress},
	"addticket":               {fn: (*Server).addTicket},
	"auditreuse":              {fn: (*Server).auditReuse},
	"consolidate":             {fn: (*Server).consolidate},
	"createmultisig":          {fn: (*Server).createMultiSig},
	"createrawtransaction":    {fn: (*Server).createRawTransaction},
	"dumpprivkey":             {fn: (*Server).dumpPrivKey},
	"generatevote":            {fn: (*Server).generateVote},
	"getaccount":              {fn: (*Server).getAccount},
	"getaccountaddress":       {fn: (*Server).getAccountAddress},
	"getaddressesbyaccount":   {fn: (*Server).getAddressesByAccount},
	"getbalance":              {fn: (*Server).getBalance},
	"getbestblockhash":        {fn: (*Server).getBestBlockHash},
	"getblockcount":           {fn: (*Server).getBlockCount},
	"getblockhash":            {fn: (*Server).getBlockHash},
	"getinfo":                 {fn: (*Server).getInfo},
	"getmasterpubkey":         {fn: (*Server).getMasterPubkey},
	"getmultisigoutinfo":      {fn: (*Server).getMultisigOutInfo},
	"getnewaddress":           {fn: (*Server).getNewAddress},
	"getrawchangeaddress":     {fn: (*Server).getRawChangeAddress},
	"getreceivedbyaccount":    {fn: (*Server).getReceivedByAccount},
	"getreceivedbyaddress":    {fn: (*Server).getReceivedByAddress},
	"getstakeinfo":            {fn: (*Server).getStakeInfo},
	"getticketfee":            {fn: (*Server).getTicketFee},
	"gettickets":              {fn: (*Server).getTickets},
	"gettransaction":          {fn: (*Server).getTransaction},
	"getvotechoices":          {fn: (*Server).getVoteChoices},
	"getwalletfee":            {fn: (*Server).getWalletFee},
	"help":                    {fn: (*Server).help},
	"importprivkey":           {fn: (*Server).importPrivKey},
	"importscript":            {fn: (*Server).importScript},
	"importxpub":              {fn: (*Server).importXpub},
	"listaccounts":            {fn: (*Server).listAccounts},
	"listlockunspent":         {fn: (*Server).listLockUnspent},
	"listreceivedbyaccount":   {fn: (*Server).listReceivedByAccount},
	"listreceivedbyaddress":   {fn: (*Server).listReceivedByAddress},
	"listsinceblock":          {fn: (*Server).listSinceBlock},
	"listscripts":             {fn: (*Server).listScripts},
	"listtransactions":        {fn: (*Server).listTransactions},
	"listunspent":             {fn: (*Server).listUnspent},
	"lockunspent":             {fn: (*Server).lockUnspent},
	"mixaccount":              {fn: (*Server).mixAccount},
	"mixoutput":               {fn: (*Server).mixOutput},
	"purchaseticket":          {fn: (*Server).purchaseTicket},
	"rescanwallet":            {fn: (*Server).rescanWallet},
	"revoketickets":           {fn: (*Server).revokeTickets},
	"sendfrom":                {fn: (*Server).sendFrom},
	"sendmany":                {fn: (*Server).sendMany},
	"sendtoaddress":           {fn: (*Server).sendToAddress},
	"sendtomultisig":          {fn: (*Server).sendToMultiSig},
	"setticketfee":            {fn: (*Server).setTicketFee},
	"settxfee":                {fn: (*Server).setTxFee},
	"setvotechoice":           {fn: (*Server).setVoteChoice},
	"signmessage":             {fn: (*Server).signMessage},
	"signrawtransaction":      {fn: (*Server).signRawTransaction},
	"signrawtransactions":     {fn: (*Server).signRawTransactions},
	"sweepaccount":            {fn: (*Server).sweepAccount},
	"redeemmultisigout":       {fn: (*Server).redeemMultiSigOut},
	"redeemmultisigouts":      {fn: (*Server).redeemMultiSigOuts},
	"stakepooluserinfo":       {fn: (*Server).stakePoolUserInfo},
	"ticketsforaddress":       {fn: (*Server).ticketsForAddress},
	"validateaddress":         {fn: (*Server).validateAddress},
	"verifymessage":           {fn: (*Server).verifyMessage},
	"version":                 {fn: (*Server).version},
	"walletinfo":              {fn: (*Server).walletInfo},
	"walletlock":              {fn: (*Server).walletLock},
	"walletpassphrase":        {fn: (*Server).walletPassphrase},
	"walletpassphrasechange":  {fn: (*Server).walletPassphraseChange},

	// Extensions to the reference client JSON-RPC API
	"getbestblock":     {fn: (*Server).getBestBlock},
	"createnewaccount": {fn: (*Server).createNewAccount},
	// This was an extension but the reference implementation added it as
	// well, but with a different API (no account parameter).  It's listed
	// here because it hasn't been update to use the reference
	// implemenation's API.
	"getunconfirmedbalance":   {fn: (*Server).getUnconfirmedBalance},
	"listaddresstransactions": {fn: (*Server).listAddressTransactions},
	"listalltransactions":     {fn: (*Server).listAllTransactions},
	"renameaccount":           {fn: (*Server).renameAccount},
	"walletislocked":          {fn: (*Server).walletIsLocked},

	// Reference implementation methods (still unimplemented)
	"backupwallet":         {fn: unimplemented, noHelp: true},
	"getwalletinfo":        {fn: unimplemented, noHelp: true},
	"importwallet":         {fn: unimplemented, noHelp: true},
	"listaddressgroupings": {fn: unimplemented, noHelp: true},

	// Reference methods which can't be implemented by dcrwallet due to
	// design decision differences
	"dumpwallet":    {fn: unsupported, noHelp: true},
	"encryptwallet": {fn: unsupported, noHelp: true},
	"move":          {fn: unsupported, noHelp: true},
	"setaccount":    {fn: unsupported, noHelp: true},
}

// unimplemented handles an unimplemented RPC request with the
// appropriate error.
func unimplemented(*Server, context.Context, interface{}) (interface{}, error) {
	return nil, &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCUnimplemented,
		Message: "Method unimplemented",
	}
}

// unsupported handles a standard bitcoind RPC request which is
// unsupported by dcrwallet due to design differences.
func unsupported(*Server, context.Context, interface{}) (interface{}, error) {
	return nil, &dcrjson.RPCError{
		Code:    -1,
		Message: "Request unsupported by dcrwallet",
	}
}

// lazyHandler is a closure over a requestHandler or passthrough request with
// the RPC server's wallet and chain server variables as part of the closure
// context.
type lazyHandler func() (interface{}, *dcrjson.RPCError)

// lazyApplyHandler looks up the best request handler func for the method,
// returning a closure that will execute it with the (required) wallet and
// (optional) consensus RPC server.  If no handlers are found and the
// chainClient is not nil, the returned handler performs RPC passthrough.
func lazyApplyHandler(s *Server, ctx context.Context, request *dcrjson.Request) lazyHandler {
	handlerData, ok := handlers[request.Method]
	if !ok {
		return func() (interface{}, *dcrjson.RPCError) {
			// Attempt RPC passthrough if possible
			n, ok := s.walletLoader.NetworkBackend()
			if !ok {
				return nil, errRPCClientNotConnected
			}
			rpc, ok := n.(*dcrd.RPC)
			if !ok {
				return nil, rpcErrorf(dcrjson.ErrRPCClientNotConnected, "RPC passthrough requires dcrd RPC synchronization")
			}
			var resp json.RawMessage
			var params = make([]interface{}, len(request.Params))
			for i := range request.Params {
				params[i] = request.Params[i]
			}
			err := rpc.Call(ctx, request.Method, &resp, params...)
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

	return func() (interface{}, *dcrjson.RPCError) {
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
func makeResponse(id, result interface{}, err error) dcrjson.Response {
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
func (s *Server) abandonTransaction(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) accountAddressIndex(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) accountSyncAddressIndex(ctx context.Context, icmd interface{}) (interface{}, error) {
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

func makeMultiSigScript(ctx context.Context, w *wallet.Wallet, keys []string, nRequired int) ([]byte, error) {
	keysesPrecious := make([]*dcrutil.AddressSecpPubKey, len(keys))

	// The address list will made up either of addreseses (pubkey hash), for
	// which we need to look up the keys in wallet, straight pubkeys, or a
	// mixture of the two.
	for i, a := range keys {
		// try to parse as pubkey address
		a, err := decodeAddress(a, w.ChainParams())
		if err != nil {
			return nil, err
		}

		switch addr := a.(type) {
		case *dcrutil.AddressSecpPubKey:
			keysesPrecious[i] = addr
		default:
			pubKey, err := w.PubKeyForAddress(ctx, addr)
			if err != nil {
				if errors.Is(err, errors.NotExist) {
					return nil, errAddressNotInWallet
				}
				return nil, err
			}
			if dcrec.SignatureType(pubKey.GetType()) != dcrec.STEcdsaSecp256k1 {
				return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
					"only secp256k1 pubkeys are currently supported")
			}
			pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(
				pubKey.Serialize(), w.ChainParams())
			if err != nil {
				return nil, rpcError(dcrjson.ErrRPCInvalidAddressOrKey, err)
			}
			keysesPrecious[i] = pubKeyAddr
		}
	}

	return txscript.MultiSigScript(keysesPrecious, nRequired)
}

// addMultiSigAddress handles an addmultisigaddress request by adding a
// multisig address to the given wallet.
func (s *Server) addMultiSigAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.AddMultisigAddressCmd)
	// If an account is specified, ensure that is the imported account.
	if cmd.Account != nil && *cmd.Account != udb.ImportedAddrAccountName {
		return nil, errNotImportedAccount
	}

	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	secp256k1Addrs := make([]dcrutil.Address, len(cmd.Keys))
	for i, k := range cmd.Keys {
		addr, err := decodeAddress(k, w.ChainParams())
		if err != nil {
			return nil, err
		}
		secp256k1Addrs[i] = addr
	}

	script, err := w.MakeSecp256k1MultiSigScript(ctx, secp256k1Addrs, cmd.NRequired)
	if err != nil {
		return nil, err
	}

	p2shAddr, err := w.ImportP2SHRedeemScript(ctx, script)
	if err != nil {
		return nil, err
	}

	n, ok := s.walletLoader.NetworkBackend()
	if !ok {
		return nil, errNoNetwork
	}
	err = n.LoadTxFilter(ctx, false, []dcrutil.Address{p2shAddr}, nil)
	if err != nil {
		return nil, err
	}

	return p2shAddr.Address(), nil
}

// addTicket adds a ticket to the stake manager manually.
func (s *Server) addTicket(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.AddTicketCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	mtx := new(wire.MsgTx)
	err := mtx.Deserialize(hex.NewDecoder(strings.NewReader(cmd.TicketHex)))
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDeserialization, err)
	}

	err = w.AddTicket(ctx, mtx)
	return nil, err
}

// auditReuse returns an object keying reused addresses to two or more outputs
// referencing them.
func (s *Server) auditReuse(ctx context.Context, icmd interface{}) (interface{}, error) {
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
				_, err = w.AddressInfo(ctx, addr)
				if errors.Is(err, errors.NotExist) {
					continue
				}
				if err != nil {
					return false, err
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
func (s *Server) consolidate(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	var changeAddr dcrutil.Address
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
func (s *Server) createMultiSig(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.CreateMultisigCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	script, err := makeMultiSigScript(ctx, w, cmd.Keys, cmd.NRequired)
	if err != nil {
		return nil, err
	}

	address, err := dcrutil.NewAddressScriptHash(script, w.ChainParams())
	if err != nil {
		return nil, err
	}

	return types.CreateMultiSigResult{
		Address:      address.Address(),
		RedeemScript: hex.EncodeToString(script),
	}, nil
}

// createRawTransaction handles createrawtransaction commands.
func (s *Server) createRawTransaction(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*dcrdtypes.CreateRawTransactionCmd)

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
		// Ensure amount is in the valid range for monetary amounts.
		if amount <= 0 || amount > dcrutil.MaxAmount {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter,
				"Invalid amount: 0 >= %v > %v", amount, dcrutil.MaxAmount)
		}

		// Decode the provided address.  This also ensures the network encoded
		// with the address matches the network the server is currently on.
		addr, err := dcrutil.DecodeAddress(encodedAddr, s.activeNet)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey,
				"Address %q: %v", encodedAddr, err)
		}

		// Ensure the address is one of the supported types.
		switch addr.(type) {
		case *dcrutil.AddressPubKeyHash:
		case *dcrutil.AddressScriptHash:
		default:
			return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey,
				"Invalid type: %T", addr)
		}

		// Create a new script which pays to the provided address.
		pkScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code,
				"Pay to address script: %v", err)
		}

		atomic, err := dcrutil.NewAmount(amount)
		if err != nil {
			return nil, rpcErrorf(dcrjson.ErrRPCInternal.Code,
				"New amount: %v", err)
		}

		txOut := wire.NewTxOut(int64(atomic), pkScript)
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

// dumpPrivKey handles a dumpprivkey request with the private key
// for a single address, or an appropriate error if the wallet
// is locked.
func (s *Server) dumpPrivKey(ctx context.Context, icmd interface{}) (interface{}, error) {
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

// generateVote handles a generatevote request by constructing a signed
// vote and returning it.
func (s *Server) generateVote(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.GenerateVoteCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	blockHash, err := chainhash.NewHashFromStr(cmd.BlockHash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	ticketHash, err := chainhash.NewHashFromStr(cmd.TicketHash)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}

	var voteBitsExt []byte
	voteBitsExt, err = hex.DecodeString(cmd.VoteBitsExt)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
	}
	voteBits := stake.VoteBits{
		Bits:         cmd.VoteBits,
		ExtendedBits: voteBitsExt,
	}

	ssgentx, err := w.GenerateVoteTx(ctx, blockHash, int32(cmd.Height), ticketHash,
		voteBits)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.Grow(2 * ssgentx.SerializeSize())
	err = ssgentx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}
	resp := &types.GenerateVoteResult{
		Hex: b.String(),
	}
	return resp, nil
}

// getAddressesByAccount handles a getaddressesbyaccount request by returning
// all addresses for an account, or an error if the requested account does
// not exist.
func (s *Server) getAddressesByAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.GetAddressesByAccountCmd)
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

	// Find the next child address indexes for the account.
	endExt, endInt, err := w.BIP0044BranchNextIndexes(ctx, account)
	if err != nil {
		return nil, err
	}

	// Nothing to do if we have no addresses.
	if endExt+endInt == 0 {
		return nil, nil
	}

	// Derive the addresses.
	addrsStr := make([]string, endInt+endExt)
	addrsExt, err := w.AccountBranchAddressRange(account, udb.ExternalBranch, 0, endExt)
	if err != nil {
		return nil, err
	}
	for i := range addrsExt {
		addrsStr[i] = addrsExt[i].Address()
	}
	addrsInt, err := w.AccountBranchAddressRange(account, udb.InternalBranch, 0, endInt)
	if err != nil {
		return nil, err
	}
	for i := range addrsInt {
		addrsStr[i+int(endExt)] = addrsInt[i].Address()
	}

	return addrsStr, nil
}

// getBalance handles a getbalance request by returning the balance for an
// account (wallet), or an error if the requested account does not
// exist.
func (s *Server) getBalance(ctx context.Context, icmd interface{}) (interface{}, error) {
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
		balanceMap, err := w.CalculateAccountBalances(ctx, int32(*cmd.MinConf))
		if err != nil {
			return nil, err
		}
		balances := make([]*udb.Balances, 0, len(balanceMap))
		for _, bal := range balanceMap {
			balances = append(balances, bal)
		}
		sort.Slice(balances, func(i, j int) bool {
			return balances[i].Account < balances[j].Account
		})

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

		bal, err := w.CalculateAccountBalance(ctx, account, int32(*cmd.MinConf))
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
func (s *Server) getBestBlock(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) getBestBlockHash(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	hash, _ := w.MainChainTip(ctx)
	return hash.String(), nil
}

// getBlockCount handles a getblockcount request by returning the chain height
// of the most recently processed block.
func (s *Server) getBlockCount(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	_, height := w.MainChainTip(ctx)
	return height, nil
}

// getBlockHash handles a getblockhash request by returning the main chain hash
// for a block at some height.
func (s *Server) getBlockHash(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*dcrdtypes.GetBlockHashCmd)
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

// difficultyRatio returns the proof-of-work difficulty as a multiple of the
// minimum difficulty using the passed bits field from the header of a block.
func difficultyRatio(bits uint32, params *chaincfg.Params) float64 {
	max := blockchain.CompactToBig(params.PowLimitBits)
	target := blockchain.CompactToBig(bits)
	ratio, _ := new(big.Rat).SetFrac(max, target).Float64()
	return ratio
}

// getInfo handles a getinfo request by returning a structure containing
// information about the current state of the wallet.
func (s *Server) getInfo(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	tipHash, tipHeight := w.MainChainTip(ctx)
	tipHeader, err := w.BlockHeader(ctx, &tipHash)
	if err != nil {
		return nil, err
	}

	balances, err := w.CalculateAccountBalances(ctx, 1)
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
	if rpc, ok := n.(*dcrd.RPC); ok {
		var consensusInfo dcrdtypes.InfoChainResult
		err := rpc.Call(ctx, "getinfo", &consensusInfo)
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

func decodeAddress(s string, params *chaincfg.Params) (dcrutil.Address, error) {
	// Secp256k1 pubkey as a string, handle differently.
	if len(s) == 66 || len(s) == 130 {
		pubKeyBytes, err := hex.DecodeString(s)
		if err != nil {
			return nil, err
		}
		pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(pubKeyBytes,
			params)
		if err != nil {
			return nil, err
		}

		return pubKeyAddr, nil
	}

	addr, err := dcrutil.DecodeAddress(s, params)
	if err != nil {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidAddressOrKey,
			"invalid address %q: decode failed: %#q", s, err)
	}
	return addr, nil
}

// getAccount handles a getaccount request by returning the account name
// associated with a single address.
func (s *Server) getAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.GetAccountCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		return nil, err
	}

	// Fetch the associated account
	account, err := w.AccountOfAddress(ctx, addr)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAddressNotInWallet
		}
		return nil, err
	}

	acctName, err := w.AccountName(ctx, account)
	if err != nil {
		return nil, err
	}
	return acctName, nil
}

// getAccountAddress handles a getaccountaddress by returning the most
// recently-created chained address that has not yet been used (does not yet
// appear in the blockchain, or any tx that has arrived in the dcrd mempool).
// If the most recently-requested address has been used, a new address (the
// next chained address in the keypool) is used.  This can fail if the keypool
// runs out (and will return dcrjson.ErrRPCWalletKeypoolRanOut if that happens).
func (s *Server) getAccountAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
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

	return addr.Address(), nil
}

// getUnconfirmedBalance handles a getunconfirmedbalance extension request
// by returning the current unconfirmed balance of an account.
func (s *Server) getUnconfirmedBalance(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	bals, err := w.CalculateAccountBalance(ctx, account, 1)
	if err != nil {
		// Expect account lookup to succeed
		if errors.Is(err, errors.NotExist) {
			return nil, rpcError(dcrjson.ErrRPCInternal.Code, err)
		}
		return nil, err
	}

	return (bals.Total - bals.Spendable).ToCoin(), nil
}

// importPrivKey handles an importprivkey request by parsing
// a WIF-encoded private key and adding it to an account.
func (s *Server) importPrivKey(ctx context.Context, icmd interface{}) (interface{}, error) {
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
		// TODO: This is not synchronized with process shutdown and
		// will cause panics when the DB is closed mid-transaction.
		go w.RescanFromHeight(context.Background(), n, scanFrom)
	}

	return nil, nil
}

// importScript imports a redeem script for a P2SH output.
func (s *Server) importScript(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	if err != nil {
		switch {
		case errors.Is(err, errors.Exist):
			// Do not return duplicate script errors to the client.
			return nil, nil
		case errors.Is(err, errors.Locked):
			return nil, errWalletUnlockNeeded
		default:
			return nil, err
		}
	}

	if rescan {
		// TODO: This is not synchronized with process shutdown and
		// will cause panics when the DB is closed mid-transaction.
		go w.RescanFromHeight(context.Background(), n, scanFrom)
	}

	return nil, nil
}

func (s *Server) importXpub(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) createNewAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) renameAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) getMultisigOutInfo(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	_, pubkeyAddrs, _, err := txscript.ExtractPkScriptAddrs(
		0, p2shOutput.RedeemScript,
		w.ChainParams())
	if err != nil {
		return nil, err
	}
	pubkeys := make([]string, 0, len(pubkeyAddrs))
	for _, pka := range pubkeyAddrs {
		pubkeys = append(pubkeys, hex.EncodeToString(pka.ScriptAddress()))
	}

	result := &types.GetMultisigOutInfoResult{
		Address:      p2shOutput.P2SHAddress.Address(),
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
func (s *Server) getNewAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	return addr.Address(), nil
}

// getRawChangeAddress handles a getrawchangeaddress request by creating
// and returning a new change address for an account.
//
// Note: bitcoind allows specifying the account as an optional parameter,
// but ignores the parameter.
func (s *Server) getRawChangeAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	return addr.Address(), nil
}

// getReceivedByAccount handles a getreceivedbyaccount request by returning
// the total amount received by addresses of an account.
func (s *Server) getReceivedByAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) getReceivedByAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) getMasterPubkey(ctx context.Context, icmd interface{}) (interface{}, error) {
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

	masterPubKey, err := w.MasterPubKey(ctx, account)
	if err != nil {
		return nil, err
	}
	return masterPubKey.String(), nil
}

// getStakeInfo gets a large amounts of information about the stake environment
// and a number of statistics about local staking in the wallet.
func (s *Server) getStakeInfo(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	var rpc *dcrd.RPC
	n, _ := s.walletLoader.NetworkBackend()
	if client, ok := n.(*dcrd.RPC); ok {
		rpc = client
	}
	var sinfo *wallet.StakeInfoData
	var err error
	if rpc != nil {
		sinfo, err = w.StakeInfoPrecise(ctx, rpc)
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

// getTicketFee gets the currently set price per kb for tickets
func (s *Server) getTicketFee(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	return w.TicketFeeIncrement().ToCoin(), nil
}

// getTickets handles a gettickets request by returning the hashes of the tickets
// currently owned by wallet, encoded as strings.
func (s *Server) getTickets(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.GetTicketsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, _ := s.walletLoader.NetworkBackend()
	rpc, ok := n.(*dcrd.RPC)
	if !ok {
		return nil, errRPCClientNotConnected
	}

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
func (s *Server) getTransaction(ctx context.Context, icmd interface{}) (interface{}, error) {
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
		//Generated:     blockchain.IsCoinBaseTx(&details.MsgTx),
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

// getVoteChoices handles a getvotechoices request by returning configured vote
// preferences for each agenda of the latest supported stake version.
func (s *Server) getVoteChoices(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	version, agendas := wallet.CurrentAgendas(w.ChainParams())
	resp := &types.GetVoteChoicesResult{
		Version: version,
		Choices: make([]types.VoteChoice, len(agendas)),
	}

	choices, _, err := w.AgendaChoices(ctx)
	if err != nil {
		return nil, err
	}

	for i := range choices {
		resp.Choices[i] = types.VoteChoice{
			AgendaID:          choices[i].AgendaID,
			AgendaDescription: agendas[i].Vote.Description,
			ChoiceID:          choices[i].ChoiceID,
			ChoiceDescription: "", // Set below
		}
		for j := range agendas[i].Vote.Choices {
			if choices[i].ChoiceID == agendas[i].Vote.Choices[j].Id {
				resp.Choices[i].ChoiceDescription = agendas[i].Vote.Choices[j].Description
				break
			}
		}
	}

	return resp, nil
}

// getWalletFee returns the currently set tx fee for the requested wallet
func (s *Server) getWalletFee(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) help(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*dcrdtypes.HelpCmd)
	// TODO: The "help" RPC should use a HTTP POST client when calling down to
	// dcrd for additional help methods.  This avoids including websocket-only
	// requests in the help, which are not callable by wallet JSON-RPC clients.
	var rpc *dcrd.RPC
	n, _ := s.walletLoader.NetworkBackend()
	if client, ok := n.(*dcrd.RPC); ok {
		rpc = client
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
func (s *Server) listAccounts(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.ListAccountsCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	accountBalances := map[string]float64{}
	results, err := w.CalculateAccountBalances(ctx, int32(*cmd.MinConf))
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
func (s *Server) listLockUnspent(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	return w.LockedOutpoints(), nil
}

// listReceivedByAccount handles a listreceivedbyaccount request by returning
// a slice of objects, each one containing:
//  "account": the receiving account;
//  "amount": total amount received by the account;
//  "confirmations": number of confirmations of the most recent transaction.
// It takes two parameters:
//  "minconf": minimum number of confirmations to consider a transaction -
//             default: one;
//  "includeempty": whether or not to include addresses that have no transactions -
//                  default: false.
func (s *Server) listReceivedByAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
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
//  "account": the account of the receiving address;
//  "address": the receiving address;
//  "amount": total amount received by the address;
//  "confirmations": number of confirmations of the most recent transaction.
// It takes two parameters:
//  "minconf": minimum number of confirmations to consider a transaction -
//             default: one;
//  "includeempty": whether or not to include addresses that have no transactions -
//                  default: false.
func (s *Server) listReceivedByAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
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
				_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkVersion,
					pkScript, w.ChainParams())
				if err != nil {
					// Non standard script, skip.
					continue
				}
				for _, addr := range addrs {
					addrStr := addr.Address()
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
func (s *Server) listSinceBlock(ctx context.Context, icmd interface{}) (interface{}, error) {
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

// listScripts handles a listscripts request by returning an
// array of script details for all scripts in the wallet.
func (s *Server) listScripts(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	redeemScripts, err := w.FetchAllRedeemScripts(ctx)
	if err != nil {
		return nil, err
	}
	listScriptsResultSIs := make([]types.ScriptInfo, len(redeemScripts))
	for i, redeemScript := range redeemScripts {
		p2shAddr, err := dcrutil.NewAddressScriptHash(redeemScript,
			w.ChainParams())
		if err != nil {
			return nil, err
		}
		listScriptsResultSIs[i] = types.ScriptInfo{
			Hash160:      hex.EncodeToString(p2shAddr.Hash160()[:]),
			Address:      p2shAddr.Address(),
			RedeemScript: hex.EncodeToString(redeemScript),
		}
	}
	return &types.ListScriptsResult{Scripts: listScriptsResultSIs}, nil
}

// listTransactions handles a listtransactions request by returning an
// array of maps with details of sent and recevied wallet transactions.
func (s *Server) listTransactions(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) listAddressTransactions(ctx context.Context, icmd interface{}) (interface{}, error) {
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
		hash160Map[string(addr.ScriptAddress())] = struct{}{}
	}

	return w.ListAddressTransactions(ctx, hash160Map)
}

// listAllTransactions handles a listalltransactions request by returning
// a map with details of sent and recevied wallet transactions.  This is
// similar to ListTransactions, except it takes only a single optional
// argument for the account name and replies with all transactions.
func (s *Server) listAllTransactions(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) listUnspent(ctx context.Context, icmd interface{}) (interface{}, error) {
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
			addresses[a.Address()] = struct{}{}
		}
	}

	result, err := w.ListUnspent(ctx, int32(*cmd.MinConf), int32(*cmd.MaxConf), addresses)
	if err != nil {
		if errors.Is(err, errors.NotExist) {
			return nil, errAddressNotInWallet
		}
		return nil, err
	}
	return result, nil
}

// lockUnspent handles the lockunspent command.
func (s *Server) lockUnspent(ctx context.Context, icmd interface{}) (interface{}, error) {
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
			txSha, err := chainhash.NewHashFromStr(input.Txid)
			if err != nil {
				return nil, rpcError(dcrjson.ErrRPCDecodeHexString, err)
			}
			op := wire.OutPoint{Hash: *txSha, Index: input.Vout}
			if cmd.Unlock {
				w.UnlockOutpoint(op)
			} else {
				w.LockOutpoint(op)
			}
		}
	}
	return true, nil
}

// purchaseTicket indicates to the wallet that a ticket should be purchased
// using all currently available funds. If the ticket could not be purchased
// because there are not enough eligible funds, an error will be returned.
func (s *Server) purchaseTicket(ctx context.Context, icmd interface{}) (interface{}, error) {
	// Enforce valid and positive spend limit.
	cmd := icmd.(*types.PurchaseTicketCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
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

	// Set ticket address if specified.
	var ticketAddr dcrutil.Address
	if cmd.TicketAddress != nil {
		if *cmd.TicketAddress != "" {
			addr, err := decodeAddress(*cmd.TicketAddress, w.ChainParams())
			if err != nil {
				return nil, err
			}
			ticketAddr = addr
		}
	}

	numTickets := 1
	if cmd.NumTickets != nil {
		if *cmd.NumTickets > 1 {
			numTickets = *cmd.NumTickets
		}
	}

	// Set pool address if specified.
	var poolAddr dcrutil.Address
	var poolFee float64
	if cmd.PoolAddress != nil {
		if *cmd.PoolAddress != "" {
			addr, err := decodeAddress(*cmd.PoolAddress, w.ChainParams())
			if err != nil {
				return nil, err
			}
			poolAddr = addr

			// Attempt to get the amount to send to
			// the pool after.
			if cmd.PoolFees == nil {
				return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "pool address set without pool fee")
			}
			poolFee = *cmd.PoolFees
			if !txrules.ValidPoolFeeRate(poolFee) {
				return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "pool fee percentage %v", poolFee)
			}
		}
	}

	// Set the expiry if specified.
	expiry := int32(0)
	if cmd.Expiry != nil {
		expiry = int32(*cmd.Expiry)
	}

	ticketFee := w.TicketFeeIncrement()

	// Set the ticket fee if specified.
	if cmd.TicketFee != nil {
		ticketFee, err = dcrutil.NewAmount(*cmd.TicketFee)
		if err != nil {
			return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
		}
	}

	hashes, err := w.PurchaseTickets(ctx, 0, spendLimit, minConf, ticketAddr,
		account, numTickets, poolAddr, poolFee, expiry, w.RelayFee(),
		ticketFee)
	if err != nil {
		return nil, err
	}

	hashStrs := make([]string, len(hashes))
	for i := range hashes {
		hashStrs[i] = hashes[i].String()
	}

	return hashStrs, err
}

func addressScript(addr dcrutil.Address) (pkScript []byte, version uint16, err error) {
	switch addr := addr.(type) {
	case wallet.V0Scripter:
		return addr.ScriptV0(), 0, nil
	default:
		pkScript, err = txscript.PayToAddrScript(addr)
		return pkScript, 0, err
	}
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

		pkScript, vers, err := addressScript(addr)
		if err != nil {
			return nil, err
		}

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
	if s.cfg.CSPPServer != "" {
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

// redeemMultiSigOut receives a transaction hash/idx and fetches the first output
// index or indices with known script hashes from the transaction. It then
// construct a transaction with a single P2PKH paying to a specified address.
// It signs any inputs that it can, then provides the raw transaction to
// the user to export to others to sign.
func (s *Server) redeemMultiSigOut(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.RedeemMultiSigOutCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Convert the address to a useable format. If
	// we have no address, create a new address in
	// this wallet to send the output to.
	var addr dcrutil.Address
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
	sc := txscript.GetScriptClass(0,
		p2shOutput.RedeemScript)
	if sc != txscript.MultiSigTy {
		return nil, errors.E("P2SH redeem script is not multisig")
	}
	var msgTx wire.MsgTx
	txIn := wire.NewTxIn(&op, int64(p2shOutput.OutputAmount), nil)
	msgTx.AddTxIn(txIn)

	pkScript, _, err := addressScript(addr)
	if err != nil {
		return nil, err
	}

	err = w.PrepareRedeemMultiSigOutTxOutput(&msgTx, p2shOutput, &pkScript)
	if err != nil {
		return nil, err
	}

	// Start creating the SignRawTransactionCmd.
	outpointScript, err := txscript.PayToScriptHashScript(p2shOutput.P2SHAddress.Hash160()[:])
	if err != nil {
		return nil, err
	}
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
func (s *Server) redeemMultiSigOuts(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	p2shAddr, ok := addr.(*dcrutil.AddressScriptHash)
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
func (s *Server) rescanWallet(ctx context.Context, icmd interface{}) (interface{}, error) {
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

// revokeTickets initiates the wallet to issue revocations for any missing
// tickets that not yet been revoked.
func (s *Server) revokeTickets(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// The wallet is not locally aware of when tickets are selected to vote and
	// when they are missed.  RevokeTickets uses trusted RPCs to determine which
	// tickets were missed.  RevokeExpiredTickets is only able to create
	// revocations for tickets which have reached their expiry time even if they
	// were missed prior to expiry, but is able to be used with other backends.
	n, ok := s.walletLoader.NetworkBackend()
	if !ok {
		return nil, errNoNetwork
	}
	if rpc, ok := n.(*dcrd.RPC); ok {
		err := w.RevokeTickets(ctx, rpc)
		return nil, err
	}
	err := w.RevokeExpiredTickets(ctx, n)
	return nil, err
}

// stakePoolUserInfo returns the ticket information for a given user from the
// stake pool.
func (s *Server) stakePoolUserInfo(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.StakePoolUserInfoCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	userAddr, err := dcrutil.DecodeAddress(cmd.User, w.ChainParams())
	if err != nil {
		return nil, err
	}
	spui, err := w.StakePoolUserInfo(ctx, userAddr)
	if err != nil {
		return nil, err
	}

	resp := new(types.StakePoolUserInfoResult)
	resp.Tickets = make([]types.PoolUserTicket, 0, len(spui.Tickets))
	resp.InvalidTickets = make([]string, 0, len(spui.InvalidTickets))
	_, height := w.MainChainTip(ctx)
	for _, ticket := range spui.Tickets {
		var ticketRes types.PoolUserTicket

		status := ""
		switch ticket.Status {
		case udb.TSImmatureOrLive:
			maturedHeight := int32(ticket.HeightTicket + uint32(w.ChainParams().TicketMaturity) + 1)

			if height >= maturedHeight {
				status = "live"
			} else {
				status = "immature"
			}
		case udb.TSVoted:
			status = "voted"
		case udb.TSMissed:
			status = "missed"
			if ticket.HeightSpent-ticket.HeightTicket >= w.ChainParams().TicketExpiry {
				status = "expired"
			}
		}
		ticketRes.Status = status

		ticketRes.Ticket = ticket.Ticket.String()
		ticketRes.TicketHeight = ticket.HeightTicket
		ticketRes.SpentBy = ticket.SpentBy.String()
		ticketRes.SpentByHeight = ticket.HeightSpent

		resp.Tickets = append(resp.Tickets, ticketRes)
	}
	for _, invalid := range spui.InvalidTickets {
		invalidTicket := invalid.String()

		resp.InvalidTickets = append(resp.InvalidTickets, invalidTicket)
	}

	return resp, nil
}

// ticketsForAddress retrieves all ticket hashes that have the passed voting
// address. It will only return tickets that are in the mempool or blockchain,
// and should not return pruned tickets.
func (s *Server) ticketsForAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*dcrdtypes.TicketsForAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	addr, err := dcrutil.DecodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		return nil, err
	}

	ticketHashes, err := w.TicketHashesForVotingAddress(ctx, addr)
	if err != nil {
		return nil, err
	}

	ticketHashStrs := make([]string, 0, len(ticketHashes))
	for _, hash := range ticketHashes {
		ticketHashStrs = append(ticketHashStrs, hash.String())
	}

	return dcrdtypes.TicketsForAddressResult{Tickets: ticketHashStrs}, nil
}

func isNilOrEmpty(s *string) bool {
	return s == nil || *s == ""
}

// sendFrom handles a sendfrom RPC request by creating a new transaction
// spending unspent transaction outputs for a wallet to another payment
// address.  Leftover inputs not sent to the payment address or a fee for
// the miner are sent back to a new address in the wallet.  Upon success,
// the TxID for the created transaction is returned.
func (s *Server) sendFrom(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) sendMany(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) sendToAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) sendToMultiSig(ctx context.Context, icmd interface{}) (interface{}, error) {
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
	pubkeys := make([]*dcrutil.AddressSecpPubKey, len(cmd.Pubkeys))

	// The address list will made up either of addreseses (pubkey hash), for
	// which we need to look up the keys in wallet, straight pubkeys, or a
	// mixture of the two.
	for i, a := range cmd.Pubkeys {
		// Try to parse as pubkey address.
		a, err := decodeAddress(a, w.ChainParams())
		if err != nil {
			return nil, err
		}

		switch addr := a.(type) {
		case *dcrutil.AddressSecpPubKey:
			pubkeys[i] = addr
		default:
			pubKey, err := w.PubKeyForAddress(ctx, addr)
			if err != nil {
				if errors.Is(err, errors.NotExist) {
					return nil, errAddressNotInWallet
				}
				return nil, err
			}
			if dcrec.SignatureType(pubKey.GetType()) != dcrec.STEcdsaSecp256k1 {
				return nil, errors.New("only secp256k1 " +
					"pubkeys are currently supported")
			}
			pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(
				pubKey.Serialize(), w.ChainParams())
			if err != nil {
				return nil, err
			}
			pubkeys[i] = pubKeyAddr
		}
	}

	tx, addr, script, err :=
		w.CreateMultisigTx(ctx, account, amount, pubkeys, nrequired, minconf)
	if err != nil {
		return nil, err
	}

	result := &types.SendToMultiSigResult{
		TxHash:       tx.MsgTx.TxHash().String(),
		Address:      addr.Address(),
		RedeemScript: hex.EncodeToString(script),
	}

	log.Infof("Successfully sent funds to multisignature output in "+
		"transaction %v", tx.MsgTx.TxHash().String())

	return result, nil
}

// setTicketFee sets the transaction fee per kilobyte added to tickets.
func (s *Server) setTicketFee(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.SetTicketFeeCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	// Check that amount is not negative.
	if cmd.Fee < 0 {
		return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "negative fee")
	}

	incr, err := dcrutil.NewAmount(cmd.Fee)
	if err != nil {
		return nil, rpcError(dcrjson.ErrRPCInvalidParameter, err)
	}
	w.SetTicketFeeIncrement(incr)

	// A boolean true result is returned upon success.
	return true, nil
}

// setTxFee sets the transaction fee per kilobyte added to transactions.
func (s *Server) setTxFee(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) setVoteChoice(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.SetVoteChoiceCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	_, err := w.SetAgendaChoices(ctx, wallet.AgendaChoice{
		AgendaID: cmd.AgendaID,
		ChoiceID: cmd.ChoiceID,
	})
	return nil, err
}

// signMessage signs the given message with the private key for the given
// address
func (s *Server) signMessage(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) signRawTransaction(ctx context.Context, icmd interface{}) (interface{}, error) {
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

			addr, err := dcrutil.NewAddressScriptHash(redeemScript,
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
	if rpc, ok := n.(*dcrd.RPC); ok {
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
				hash := txIn.PreviousOutPoint.Hash.String()
				index := txIn.PreviousOutPoint.Index
				// gettxout returns null without error if the output exists
				// but is spent.  A double pointer is used to handle this case.
				var res *dcrdtypes.GetTxOutResult
				err := rpc.Call(gctx, "gettxout", &res, hash, index, true)
				if err != nil {
					return errors.E(errors.Op("dcrd.jsonrpc.gettxout"), err)
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

			var addr dcrutil.Address
			switch wif.DSA() {
			case dcrec.STEcdsaSecp256k1:
				addr, err = dcrutil.NewAddressSecpPubKey(wif.SerializePubKey(),
					w.ChainParams())
				if err != nil {
					return nil, err
				}
			case dcrec.STEd25519:
				addr, err = dcrutil.NewAddressEdwardsPubKey(
					wif.SerializePubKey(),
					w.ChainParams())
				if err != nil {
					return nil, err
				}
			case dcrec.STSchnorrSecp256k1:
				addr, err = dcrutil.NewAddressSecSchnorrPubKey(
					wif.SerializePubKey(),
					w.ChainParams())
				if err != nil {
					return nil, err
				}
			}
			keys[addr.Address()] = wif
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
	signErrs, err := w.SignTransaction(ctx, tx, hashType, inputs, keys, scripts)
	if err != nil {
		return nil, err
	}

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
		Complete: len(signErrors) == 0,
		Errors:   signErrors,
	}, nil
}

// signRawTransactions handles the signrawtransactions command.
func (s *Server) signRawTransactions(ctx context.Context, icmd interface{}) (interface{}, error) {
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

func makeScriptChangeSource(address string, version uint16, params *chaincfg.Params) (*scriptChangeSource, error) {
	destinationAddress, err := dcrutil.DecodeAddress(address, params)
	if err != nil {
		return nil, err
	}

	var script []byte
	if addr, ok := destinationAddress.(wallet.V0Scripter); ok && version == 0 {
		script = addr.ScriptV0()
	} else {
		script, err = txscript.PayToAddrScript(destinationAddress)
		if err != nil {
			return nil, err
		}
	}

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
func (s *Server) sweepAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
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

	changeSource, err := makeScriptChangeSource(cmd.DestinationAddress, 0, w.ChainParams())
	if err != nil {
		return nil, err
	}
	tx, err := w.NewUnsignedTransaction(ctx, nil, feePerKb, account,
		requiredConfs, wallet.OutputSelectionAlgorithmAll, changeSource)
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
func (s *Server) validateAddress(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*dcrdtypes.ValidateAddressCmd)
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	result := types.ValidateAddressResult{}
	addr, err := decodeAddress(cmd.Address, w.ChainParams())
	if err != nil {
		// Use result zero value (IsValid=false).
		return result, nil
	}

	// We could put whether or not the address is a script here,
	// by checking the type of "addr", however, the reference
	// implementation only puts that information if the script is
	// "ismine", and we follow that behaviour.
	result.Address = addr.Address()
	result.IsValid = true

	ainfo, err := w.AddressInfo(ctx, addr)
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
	acctName, err := w.AccountName(ctx, ainfo.Account())
	if err != nil {
		return nil, err
	}
	result.Account = acctName

	switch ma := ainfo.(type) {
	case udb.ManagedPubKeyAddress:
		result.IsCompressed = ma.Compressed()
		result.PubKey = ma.ExportPubKey()
		pubKeyBytes, err := hex.DecodeString(result.PubKey)
		if err != nil {
			return nil, err
		}
		pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(pubKeyBytes,
			w.ChainParams())
		if err != nil {
			return nil, err
		}
		result.PubKeyAddr = pubKeyAddr.String()

	case udb.ManagedScriptAddress:
		result.IsScript = true

		// The script is only available if the manager is unlocked, so
		// just break out now if there is an error.
		script, err := w.RedeemScriptCopy(ctx, addr)
		if err != nil {
			if errors.Is(err, errors.Locked) {
				break
			}
			return nil, err
		}
		result.Hex = hex.EncodeToString(script)

		// This typically shouldn't fail unless an invalid script was
		// imported.  However, if it fails for any reason, there is no
		// further information available, so just set the script type
		// a non-standard and break out now.
		class, addrs, reqSigs, err := txscript.ExtractPkScriptAddrs(
			0, script, w.ChainParams())
		if err != nil {
			result.Script = txscript.NonStandardTy.String()
			break
		}

		addrStrings := make([]string, len(addrs))
		for i, a := range addrs {
			addrStrings[i] = a.Address()
		}
		result.Addresses = addrStrings

		// Multi-signature scripts also provide the number of required
		// signatures.
		result.Script = class.String()
		if class == txscript.MultiSigTy {
			result.SigsRequired = int32(reqSigs)
		}
	}

	return result, nil
}

// verifyMessage handles the verifymessage command by verifying the provided
// compact signature for the given address and message.
func (s *Server) verifyMessage(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*dcrdtypes.VerifyMessageCmd)

	var valid bool

	// Decode address and base64 signature from the request.
	addr, err := dcrutil.DecodeAddress(cmd.Address, s.activeNet)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(cmd.Signature)
	if err != nil {
		return nil, err
	}

	// Addresses must have an associated secp256k1 private key and therefore
	// must be P2PK or P2PKH (P2SH is not allowed).
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA() != dcrec.STEcdsaSecp256k1 {
			goto WrongAddrKind
		}
	default:
		goto WrongAddrKind
	}

	valid, err = wallet.VerifyMessage(cmd.Message, addr, sig, s.activeNet)
	// Mirror Bitcoin Core behavior, which treats all erorrs as an invalid
	// signature.
	return err == nil && valid, nil

WrongAddrKind:
	return nil, rpcErrorf(dcrjson.ErrRPCInvalidParameter, "address must be secp256k1 P2PK or P2PKH")
}

// version handles the version command by returning the RPC API versions of the
// wallet and, optionally, the consensus RPC server as well if it is associated
// with the server.  The chainClient is optional, and this is simply a helper
// function for the versionWithChainRPC and versionNoChainRPC handlers.
func (s *Server) version(ctx context.Context, icmd interface{}) (interface{}, error) {
	resp := make(map[string]dcrdtypes.VersionResult)
	n, _ := s.walletLoader.NetworkBackend()
	if rpc, ok := n.(*dcrd.RPC); ok {
		err := rpc.Call(ctx, "version", &resp)
		if err != nil {
			return nil, err
		}
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
func (s *Server) walletInfo(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	n, err := w.NetworkBackend()
	connected := err == nil
	if connected {
		if rpc, ok := n.(*dcrd.RPC); ok {
			err := rpc.Call(ctx, "ping", nil)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != nil {
				log.Warnf("Ping failed on connected daemon client: %v", err)
				connected = false
			}
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
	tfi := w.TicketFeeIncrement()
	voteBits := w.VoteBits()
	var voteVersion uint32
	_ = binary.Read(bytes.NewBuffer(voteBits.ExtendedBits[0:4]), binary.LittleEndian, &voteVersion)
	voting := w.VotingEnabled()

	return &types.WalletInfoResult{
		DaemonConnected:  connected,
		Unlocked:         unlocked,
		CoinType:         coinType,
		TxFee:            fi.ToCoin(),
		TicketFee:        tfi.ToCoin(),
		VoteBits:         voteBits.Bits,
		VoteBitsExtended: hex.EncodeToString(voteBits.ExtendedBits),
		VoteVersion:      voteVersion,
		Voting:           voting,
	}, nil
}

// walletIsLocked handles the walletislocked extension request by
// returning the current lock state (false for unlocked, true for locked)
// of an account.
func (s *Server) walletIsLocked(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	return w.Locked(), nil
}

// walletLock handles a walletlock request by locking the all account
// wallets, returning an error if any wallet is not encrypted (for example,
// a watching-only wallet).
func (s *Server) walletLock(ctx context.Context, icmd interface{}) (interface{}, error) {
	w, ok := s.walletLoader.LoadedWallet()
	if !ok {
		return nil, errUnloadedWallet
	}

	w.Lock()
	return nil, nil
}

// walletPassphrase responds to the walletpassphrase request by unlocking
// the wallet.  The decryption key is saved in the wallet until timeout
// seconds expires, after which the wallet is locked.
func (s *Server) walletPassphrase(ctx context.Context, icmd interface{}) (interface{}, error) {
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
func (s *Server) walletPassphraseChange(ctx context.Context, icmd interface{}) (interface{}, error) {
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

func (s *Server) mixOutput(ctx context.Context, icmd interface{}) (interface{}, error) {
	cmd := icmd.(*types.MixOutputCmd)
	if s.cfg.CSPPServer == "" {
		return nil, errors.E("CoinShuffle++ server is not configured")
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

	dial := s.cfg.DialCSPPServer
	server := s.cfg.CSPPServer
	mixBranch := s.cfg.MixBranch

	err = w.MixOutput(ctx, dial, server, outpoint, changeAccount, mixAccount, mixBranch)
	return nil, err
}

func (s *Server) mixAccount(ctx context.Context, icmd interface{}) (interface{}, error) {
	if s.cfg.CSPPServer == "" {
		return nil, errors.E("CoinShuffle++ server is not configured")
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

	dial := s.cfg.DialCSPPServer
	server := s.cfg.CSPPServer
	mixBranch := s.cfg.MixBranch

	err = w.MixAccount(ctx, dial, server, changeAccount, mixAccount, mixBranch)
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
