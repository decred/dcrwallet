// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package rpcserver implements the RPC API and is used by the main package to
// start gRPC services.
//
// Full documentation of the API implemented by this package is maintained in a
// language-agnostic document:
//
//   https://github.com/decred/dcrwallet/blob/master/rpc/documentation/api.md
//
// Any API changes must be performed according to the steps listed here:
//
//   https://github.com/decred/dcrwallet/blob/master/rpc/documentation/serverchanges.md
package rpcserver

import (
	"bytes"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/internal/cfgutil"
	"github.com/decred/dcrwallet/internal/zero"
	"github.com/decred/dcrwallet/netparams"
	pb "github.com/decred/dcrwallet/rpc/walletrpc"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/walletdb"
	"github.com/decred/dcrwallet/wtxmgr"
)

// Public API version constants
const (
	semverString = "4.0.1"
	semverMajor  = 4
	semverMinor  = 0
	semverPatch  = 1
)

// translateError creates a new gRPC error with an appropiate error code for
// recognized errors.
//
// This function is by no means complete and should be expanded based on other
// known errors.  Any RPC handler not returning a gRPC error (with grpc.Errorf)
// should return this result instead.
func translateError(err error) error {
	code := errorCode(err)
	return grpc.Errorf(code, "%s", err.Error())
}

func errorCode(err error) codes.Code {
	// waddrmgr.IsError is convenient, but not granular enough when the
	// underlying error has to be checked.  Unwrap the underlying error
	// if it exists.
	if e, ok := err.(waddrmgr.ManagerError); ok {
		// For these waddrmgr error codes, the underlying error isn't
		// needed to determine the grpc error code.
		switch e.ErrorCode {
		case waddrmgr.ErrWrongPassphrase: // public and private
			return codes.InvalidArgument
		case waddrmgr.ErrAccountNotFound:
			return codes.NotFound
		case waddrmgr.ErrInvalidAccount: // reserved account
			return codes.InvalidArgument
		case waddrmgr.ErrDuplicateAccount:
			return codes.AlreadyExists
		}

		err = e.Err
	}

	if e, ok := err.(wtxmgr.Error); ok {
		switch e.Code {
		case wtxmgr.ErrValueNoExists:
			return codes.NotFound
		}

		err = e.Err
	}

	switch err {
	case wallet.ErrLoaded:
		return codes.FailedPrecondition
	case walletdb.ErrDbNotOpen:
		return codes.Aborted
	case walletdb.ErrDbExists:
		return codes.AlreadyExists
	case walletdb.ErrDbDoesNotExist:
		return codes.NotFound
	case hdkeychain.ErrInvalidSeedLen:
		return codes.InvalidArgument
	default:
		return codes.Unknown
	}
}

// versionServer provides RPC clients with the ability to query the RPC server
// version.
type versionServer struct {
}

// walletServer provides wallet services for RPC clients.
type walletServer struct {
	wallet *wallet.Wallet
}

// loaderServer provides RPC clients with the ability to load and close wallets,
// as well as establishing a RPC connection to a dcrd consensus server.
type loaderServer struct {
	loader    *wallet.Loader
	activeNet *netparams.Params
	rpcClient *chain.RPCClient
	mu        sync.Mutex
}

// StartVersionService creates an implementation of the VersionService and
// registers it with the gRPC server.
func StartVersionService(server *grpc.Server) {
	pb.RegisterVersionServiceServer(server, &versionServer{})
}

func (*versionServer) Version(ctx context.Context, req *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{
		VersionString: semverString,
		Major:         semverMajor,
		Minor:         semverMinor,
		Patch:         semverPatch,
	}, nil
}

// StartWalletService creates an implementation of the WalletService and
// registers it with the gRPC server.
func StartWalletService(server *grpc.Server, wallet *wallet.Wallet) {
	service := &walletServer{wallet}
	pb.RegisterWalletServiceServer(server, service)
}

// requireChainClient checks whether the wallet has been associated with the
// consensus server RPC client, returning a gRPC error when it is not.
func (s *walletServer) requireChainClient() (*chain.RPCClient, error) {
	chainClient := s.wallet.ChainClient()
	if chainClient == nil {
		return nil, grpc.Errorf(codes.FailedPrecondition,
			"wallet is not associated with a consensus server RPC client")
	}
	return chainClient, nil
}

func (s *walletServer) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	return &pb.PingResponse{}, nil
}

func (s *walletServer) Network(ctx context.Context, req *pb.NetworkRequest) (
	*pb.NetworkResponse, error) {

	return &pb.NetworkResponse{ActiveNetwork: uint32(s.wallet.ChainParams().Net)}, nil
}

func (s *walletServer) AccountNumber(ctx context.Context, req *pb.AccountNumberRequest) (
	*pb.AccountNumberResponse, error) {

	accountNum, err := s.wallet.AccountNumber(req.AccountName)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.AccountNumberResponse{AccountNumber: accountNum}, nil
}

func (s *walletServer) Accounts(ctx context.Context, req *pb.AccountsRequest) (
	*pb.AccountsResponse, error) {

	resp, err := s.wallet.Accounts()
	if err != nil {
		return nil, translateError(err)
	}
	accounts := make([]*pb.AccountsResponse_Account, len(resp.Accounts))
	for i := range resp.Accounts {
		a := &resp.Accounts[i]
		accounts[i] = &pb.AccountsResponse_Account{
			AccountNumber:    a.AccountNumber,
			AccountName:      a.AccountName,
			TotalBalance:     int64(a.TotalBalance),
			ExternalKeyCount: a.ExternalKeyCount,
			InternalKeyCount: a.InternalKeyCount,
			ImportedKeyCount: a.ImportedKeyCount,
		}
	}
	return &pb.AccountsResponse{
		Accounts:           accounts,
		CurrentBlockHash:   resp.CurrentBlockHash[:],
		CurrentBlockHeight: resp.CurrentBlockHeight,
	}, nil
}

func (s *walletServer) RenameAccount(ctx context.Context, req *pb.RenameAccountRequest) (
	*pb.RenameAccountResponse, error) {

	err := s.wallet.RenameAccount(req.AccountNumber, req.NewName)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.RenameAccountResponse{}, nil
}

func (s *walletServer) Rescan(req *pb.RescanRequest, svr pb.WalletService_RescanServer) error {
	chainClient, err := s.requireChainClient()
	if err != nil {
		return err
	}

	if req.BeginHeight < 0 {
		return grpc.Errorf(codes.InvalidArgument, "begin height must be non-negative")
	}

	progress := make(chan wallet.RescanProgress, 1)
	cancel := make(chan struct{})
	go s.wallet.RescanProgressFromHeight(chainClient, req.BeginHeight, progress, cancel)

	ctxDone := svr.Context().Done()
	for {
		select {
		case p, ok := <-progress:
			if !ok {
				// finished or cancelled rescan without error
				select {
				case <-cancel:
					return grpc.Errorf(codes.Canceled, "rescan canceled")
				default:
					return nil
				}
			}
			if p.Err != nil {
				return translateError(p.Err)
			}
			resp := &pb.RescanResponse{RescannedThrough: p.ScannedThrough}
			err := svr.Send(resp)
			if err != nil {
				return translateError(err)
			}
		case <-ctxDone:
			close(cancel)
			ctxDone = nil
		}
	}
}

func (s *walletServer) NextAccount(ctx context.Context, req *pb.NextAccountRequest) (
	*pb.NextAccountResponse, error) {

	defer zero.Bytes(req.Passphrase)

	if req.AccountName == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, "account name may not be empty")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	account, err := s.wallet.NextAccount(req.AccountName)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.NextAccountResponse{AccountNumber: account}, nil
}

func (s *walletServer) NextAddress(ctx context.Context, req *pb.NextAddressRequest) (
	*pb.NextAddressResponse, error) {

	var (
		addr dcrutil.Address
		err  error
	)
	switch req.Kind {
	case pb.NextAddressRequest_BIP0044_EXTERNAL:
		addr, err = s.wallet.NewAddress(req.Account, waddrmgr.ExternalBranch)
		if err != nil {
			return nil, translateError(err)
		}
	case pb.NextAddressRequest_BIP0044_INTERNAL:
		addr, err = s.wallet.NewAddress(req.Account, waddrmgr.InternalBranch)
		if err != nil {
			return nil, translateError(err)
		}
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "kind=%v", req.Kind)
	}
	if err != nil {
		return nil, translateError(err)
	}

	pubKey, err := s.wallet.PubKeyForAddress(addr)
	if err != nil {
		return nil, translateError(err)
	}
	pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(pubKey.Serialize(), s.wallet.ChainParams())
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.NextAddressResponse{
		Address:   addr.EncodeAddress(),
		PublicKey: pubKeyAddr.String(),
	}, nil
}

func (s *walletServer) ImportPrivateKey(ctx context.Context, req *pb.ImportPrivateKeyRequest) (
	*pb.ImportPrivateKeyResponse, error) {

	defer zero.Bytes(req.Passphrase)

	wif, err := dcrutil.DecodeWIF(req.PrivateKeyWif)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Invalid WIF-encoded private key: %v", err)
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	// At the moment, only the special-cased import account can be used to
	// import keys.
	if req.Account != waddrmgr.ImportedAddrAccount {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Only the imported account accepts private key imports")
	}

	if req.ScanFrom < 0 {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Attempted to scan from a negative block height")
	}

	if req.ScanFrom > 0 && req.Rescan == true {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Passed a rescan height without rescan set")
	}

	chainClient, err := s.requireChainClient()
	if err != nil {
		return nil, err
	}

	_, err = s.wallet.ImportPrivateKey(wif)
	if err != nil {
		return nil, translateError(err)
	}

	if req.Rescan {
		s.wallet.RescanFromHeight(chainClient, req.ScanFrom)
	}

	return &pb.ImportPrivateKeyResponse{}, nil
}

func (s *walletServer) ImportScript(ctx context.Context,
	req *pb.ImportScriptRequest) (*pb.ImportScriptResponse, error) {

	defer zero.Bytes(req.Passphrase)

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	if req.ScanFrom < 0 {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Attempted to scan from a negative block height")
	}

	if req.ScanFrom > 0 && req.Rescan == true {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Passed a rescan height without rescan set")
	}

	chainClient, err := s.requireChainClient()
	if err != nil {
		return nil, err
	}

	err = s.wallet.ImportScript(req.Script)
	if err != nil {
		return nil, translateError(err)
	}

	if req.Rescan {
		s.wallet.RescanFromHeight(chainClient, req.ScanFrom)
	}

	return &pb.ImportScriptResponse{}, nil
}

func (s *walletServer) Balance(ctx context.Context, req *pb.BalanceRequest) (
	*pb.BalanceResponse, error) {

	account := req.AccountNumber
	reqConfs := req.RequiredConfirmations
	bals, err := s.wallet.CalculateAccountBalances(account, reqConfs)
	if err != nil {
		return nil, translateError(err)
	}

	// TODO: Spendable currently includes multisig outputs that may not
	// actually be spendable without additional keys.
	resp := &pb.BalanceResponse{
		Total:          int64(bals.Total),
		Spendable:      int64(bals.Spendable),
		ImmatureReward: int64(bals.ImmatureReward),
	}
	return resp, nil
}

func (s *walletServer) TicketPrice(ctx context.Context,
	req *pb.TicketPriceRequest) (*pb.TicketPriceResponse, error) {

	chainClient, err := s.requireChainClient()
	if err != nil {
		return nil, err
	}

	tp, err := s.wallet.StakeDifficulty()
	if err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition,
			"Failed to query stake difficulty: %s", err.Error())
	}

	_, blockHeight, err := chainClient.GetBestBlock()
	if err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition,
			"Failed to query block height: %s", err.Error())
	}

	return &pb.TicketPriceResponse{
		TicketPrice: int64(tp),
		Height:      int32(blockHeight),
	}, nil
}

func (s *walletServer) StakeInfo(ctx context.Context,
	req *pb.StakeInfoRequest) (*pb.StakeInfoResponse, error) {
	si, err := s.wallet.StakeInfo()
	if err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition,
			"Failed to query stake info: %s", err.Error())
	}

	return &pb.StakeInfoResponse{
		PoolSize:      si.PoolSize,
		AllMempoolTix: si.AllMempoolTix,
		OwnMempoolTix: si.OwnMempoolTix,
		Immature:      si.Immature,
		Live:          si.Live,
		Voted:         si.Voted,
		Missed:        si.Missed,
		Revoked:       si.Revoked,
		Expired:       0, // Placeholder
		TotalSubsidy:  int64(si.TotalSubsidy),
	}, nil
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

func (s *walletServer) FundTransaction(ctx context.Context, req *pb.FundTransactionRequest) (
	*pb.FundTransactionResponse, error) {

	policy := wallet.OutputSelectionPolicy{
		Account:               req.Account,
		RequiredConfirmations: req.RequiredConfirmations,
	}
	unspentOutputs, err := s.wallet.UnspentOutputs(policy)
	if err != nil {
		return nil, translateError(err)
	}

	selectedOutputs := make([]*pb.FundTransactionResponse_PreviousOutput, 0, len(unspentOutputs))
	var totalAmount dcrutil.Amount
	for _, output := range unspentOutputs {
		selectedOutputs = append(selectedOutputs, &pb.FundTransactionResponse_PreviousOutput{
			TransactionHash: output.OutPoint.Hash[:],
			OutputIndex:     output.OutPoint.Index,
			Amount:          output.Output.Value,
			PkScript:        output.Output.PkScript,
			ReceiveTime:     output.ReceiveTime.Unix(),
			FromCoinbase:    output.OutputKind == wallet.OutputKindCoinbase,
			Tree:            int32(output.OutPoint.Tree),
		})
		totalAmount += dcrutil.Amount(output.Output.Value)

		if req.TargetAmount != 0 && totalAmount > dcrutil.Amount(req.TargetAmount) {
			break
		}
	}

	var changeScript []byte
	if req.IncludeChangeScript && totalAmount > dcrutil.Amount(req.TargetAmount) {
		changeAddr, err := s.wallet.NewAddress(req.Account,
			waddrmgr.InternalBranch)
		if err != nil {
			return nil, translateError(err)
		}
		changeScript, err = txscript.PayToAddrScript(changeAddr)
		if err != nil {
			return nil, translateError(err)
		}
	}

	return &pb.FundTransactionResponse{
		SelectedOutputs: selectedOutputs,
		TotalAmount:     int64(totalAmount),
		ChangePkScript:  changeScript,
	}, nil
}

// BUGS:
// - MinimumRecentTransactions is ignored.
// - Wrong error codes when a block height or hash is not recognized
func (s *walletServer) GetTransactions(req *pb.GetTransactionsRequest,
	server pb.WalletService_GetTransactionsServer) error {

	var startBlock, endBlock *wallet.BlockIdentifier
	if req.StartingBlockHash != nil && req.StartingBlockHeight != 0 {
		return grpc.Errorf(codes.InvalidArgument,
			"starting block hash and height may not be specified simultaneously")
	} else if req.StartingBlockHash != nil {
		startBlockHash, err := chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return grpc.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		startBlock = wallet.NewBlockIdentifierFromHash(startBlockHash)
	} else if req.StartingBlockHeight != 0 {
		startBlock = wallet.NewBlockIdentifierFromHeight(req.StartingBlockHeight)
	}

	if req.EndingBlockHash != nil && req.EndingBlockHeight != 0 {
		return grpc.Errorf(codes.InvalidArgument,
			"ending block hash and height may not be specified simultaneously")
	} else if req.EndingBlockHash != nil {
		endBlockHash, err := chainhash.NewHash(req.EndingBlockHash)
		if err != nil {
			return grpc.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		endBlock = wallet.NewBlockIdentifierFromHash(endBlockHash)
	} else if req.EndingBlockHeight != 0 {
		endBlock = wallet.NewBlockIdentifierFromHeight(req.EndingBlockHeight)
	}

	var minRecentTxs int
	if req.MinimumRecentTransactions != 0 {
		if endBlock != nil {
			return grpc.Errorf(codes.InvalidArgument,
				"ending block and minimum number of recent transactions "+
					"may not be specified simultaneously")
		}
		minRecentTxs = int(req.MinimumRecentTransactions)
		if minRecentTxs < 0 {
			return grpc.Errorf(codes.InvalidArgument,
				"minimum number of recent transactions may not be negative")
		}
	}

	_ = minRecentTxs

	gtr, err := s.wallet.GetTransactions(startBlock, endBlock, server.Context().Done())
	if err != nil {
		return translateError(err)
	}
	for i := range gtr.MinedTransactions {
		resp := &pb.GetTransactionsResponse{
			MinedTransactions: marshalBlock(&gtr.MinedTransactions[i]),
		}
		err = server.Send(resp)
		if err != nil {
			return err
		}
	}
	if len(gtr.UnminedTransactions) > 0 {
		resp := &pb.GetTransactionsResponse{
			UnminedTransactions: marshalTransactionDetails(gtr.UnminedTransactions),
		}
		err = server.Send(resp)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *walletServer) ChangePassphrase(ctx context.Context, req *pb.ChangePassphraseRequest) (
	*pb.ChangePassphraseResponse, error) {

	defer func() {
		zero.Bytes(req.OldPassphrase)
		zero.Bytes(req.NewPassphrase)
	}()

	var err error
	switch req.Key {
	case pb.ChangePassphraseRequest_PRIVATE:
		err = s.wallet.ChangePrivatePassphrase(req.OldPassphrase, req.NewPassphrase)
	case pb.ChangePassphraseRequest_PUBLIC:
		err = s.wallet.ChangePublicPassphrase(req.OldPassphrase, req.NewPassphrase)
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "Unknown key type (%d)", req.Key)
	}
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.ChangePassphraseResponse{}, nil
}

// BUGS:
// - InputIndexes request field is ignored.
func (s *walletServer) SignTransaction(ctx context.Context, req *pb.SignTransactionRequest) (
	*pb.SignTransactionResponse, error) {

	defer zero.Bytes(req.Passphrase)

	var tx wire.MsgTx
	err := tx.Deserialize(bytes.NewReader(req.SerializedTransaction))
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Bytes do not represent a valid raw transaction: %v", err)
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	invalidSigs, err := s.wallet.SignTransaction(&tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		return nil, translateError(err)
	}

	invalidInputIndexes := make([]uint32, len(invalidSigs))
	for i, e := range invalidSigs {
		invalidInputIndexes[i] = e.InputIndex
	}

	var serializedTransaction bytes.Buffer
	serializedTransaction.Grow(tx.SerializeSize())
	err = tx.Serialize(&serializedTransaction)
	if err != nil {
		return nil, translateError(err)
	}

	resp := &pb.SignTransactionResponse{
		Transaction:          serializedTransaction.Bytes(),
		UnsignedInputIndexes: invalidInputIndexes,
	}
	return resp, nil
}

// BUGS:
// - The transaction is not inspected to be relevant before publishing using
//   sendrawtransaction, so connection errors to dcrd could result in the tx
//   never being added to the wallet database.
// - Once the above bug is fixed, wallet will require a way to purge invalid
//   transactions from the database when they are rejected by the network, other
//   than double spending them.
func (s *walletServer) PublishTransaction(ctx context.Context, req *pb.PublishTransactionRequest) (
	*pb.PublishTransactionResponse, error) {

	var msgTx wire.MsgTx
	err := msgTx.Deserialize(bytes.NewReader(req.SignedTransaction))
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Bytes do not represent a valid raw transaction: %v", err)
	}

	txHash, err := s.wallet.PublishTransaction(&msgTx)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.PublishTransactionResponse{TransactionHash: txHash[:]}, nil
}

// PurchaseTickets purchases tickets from the wallet.
func (s *walletServer) PurchaseTickets(ctx context.Context,
	req *pb.PurchaseTicketsRequest) (*pb.PurchaseTicketsResponse, error) {
	// Unmarshall the received data and prepare it as input for the ticket
	// purchase request.
	spendLimit := dcrutil.Amount(req.SpendLimit)
	if spendLimit < 0 {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Negative spend limit given")
	}

	minConf := int32(req.RequiredConfirmations)

	var ticketAddr dcrutil.Address
	var err error
	if req.TicketAddress != "" {
		ticketAddr, err = dcrutil.DecodeAddress(req.TicketAddress,
			s.wallet.ChainParams())
		if err != nil {
			return nil, grpc.Errorf(codes.InvalidArgument,
				"Ticket address invalid: %v", err)
		}
	}

	var poolAddr dcrutil.Address
	if req.PoolAddress != "" {
		poolAddr, err = dcrutil.DecodeAddress(req.PoolAddress,
			s.wallet.ChainParams())
		if err != nil {
			return nil, grpc.Errorf(codes.InvalidArgument,
				"Pool address invalid: %v", err)
		}
	}

	if req.PoolFees > 0 {
		err = txrules.IsValidPoolFeeRate(req.PoolFees)
		if err != nil {
			return nil, grpc.Errorf(codes.InvalidArgument,
				"Pool fees amount invalid: %v", err)
		}
	}

	if req.PoolFees > 0 && poolAddr == nil {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Pool fees set but no pool address given")
	}

	if req.PoolFees <= 0 && poolAddr != nil {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Pool fees negative or unset but pool address given")
	}

	numTickets := int(req.NumTickets)
	if numTickets < 1 {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Zero or negative number of tickets given")
	}

	expiry := int32(req.Expiry)
	txFee := dcrutil.Amount(req.TxFee)
	ticketFee := dcrutil.Amount(req.TicketFee)

	if txFee < 0 || ticketFee < 0 {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Negative fees per KB given")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	resp, err := s.wallet.PurchaseTickets(0, spendLimit, minConf,
		ticketAddr, req.Account, numTickets, poolAddr, req.PoolFees,
		expiry, txFee, ticketFee)
	if err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition,
			"Unable to purchase tickets: %v", err)
	}

	respTyped, ok := resp.([]*chainhash.Hash)
	if !ok {
		return nil, grpc.Errorf(codes.Internal,
			"Unable to cast response as a slice of hash strings")
	}
	hashes := marshalHashes(respTyped)

	return &pb.PurchaseTicketsResponse{TicketHashes: hashes}, nil
}

func (s *walletServer) LoadActiveDataFilters(ctx context.Context, req *pb.LoadActiveDataFiltersRequest) (
	*pb.LoadActiveDataFiltersResponse, error) {

	chainClient, err := s.requireChainClient()
	if err != nil {
		return nil, err
	}

	err = s.wallet.LoadActiveDataFilters(chainClient)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.LoadActiveDataFiltersResponse{}, nil
}

func marshalTransactionInputs(v []wallet.TransactionSummaryInput) []*pb.TransactionDetails_Input {
	inputs := make([]*pb.TransactionDetails_Input, len(v))
	for i := range v {
		input := &v[i]
		inputs[i] = &pb.TransactionDetails_Input{
			Index:           input.Index,
			PreviousAccount: input.PreviousAccount,
			PreviousAmount:  int64(input.PreviousAmount),
		}
	}
	return inputs
}

func marshalTransactionOutputs(v []wallet.TransactionSummaryOutput) []*pb.TransactionDetails_Output {
	outputs := make([]*pb.TransactionDetails_Output, len(v))
	for i := range v {
		output := &v[i]
		outputs[i] = &pb.TransactionDetails_Output{
			Index:    output.Index,
			Account:  output.Account,
			Internal: output.Internal,
		}
	}
	return outputs
}

func marshalTransactionDetails(v []wallet.TransactionSummary) []*pb.TransactionDetails {
	txs := make([]*pb.TransactionDetails, len(v))
	for i := range v {
		tx := &v[i]
		txs[i] = &pb.TransactionDetails{
			Hash:        tx.Hash[:],
			Transaction: tx.Transaction,
			Debits:      marshalTransactionInputs(tx.MyInputs),
			Credits:     marshalTransactionOutputs(tx.MyOutputs),
			Fee:         int64(tx.Fee),
			Timestamp:   tx.Timestamp,
		}
	}
	return txs
}

func marshalBlock(v *wallet.Block) *pb.BlockDetails {
	return &pb.BlockDetails{
		Hash:         v.Hash[:],
		Height:       v.Height,
		Timestamp:    v.Timestamp,
		Transactions: marshalTransactionDetails(v.Transactions),
	}
}

func marshalBlocks(v []wallet.Block) []*pb.BlockDetails {
	blocks := make([]*pb.BlockDetails, len(v))
	for i := range v {
		blocks[i] = marshalBlock(&v[i])
	}
	return blocks
}

func marshalHashes(v []*chainhash.Hash) [][]byte {
	hashes := make([][]byte, len(v))
	for i, hash := range v {
		hashes[i] = hash[:]
	}
	return hashes
}

func marshalAccountBalances(v []wallet.AccountBalance) []*pb.AccountBalance {
	balances := make([]*pb.AccountBalance, len(v))
	for i := range v {
		balance := &v[i]
		balances[i] = &pb.AccountBalance{
			Account:      balance.Account,
			TotalBalance: int64(balance.TotalBalance),
		}
	}
	return balances
}

func (s *walletServer) TransactionNotifications(req *pb.TransactionNotificationsRequest,
	svr pb.WalletService_TransactionNotificationsServer) error {

	n := s.wallet.NtfnServer.TransactionNotifications()
	defer n.Done()

	ctxDone := svr.Context().Done()
	for {
		select {
		case v := <-n.C:
			resp := pb.TransactionNotificationsResponse{
				AttachedBlocks:           marshalBlocks(v.AttachedBlocks),
				DetachedBlocks:           marshalHashes(v.DetachedBlocks),
				UnminedTransactions:      marshalTransactionDetails(v.UnminedTransactions),
				UnminedTransactionHashes: marshalHashes(v.UnminedTransactionHashes),
			}
			err := svr.Send(&resp)
			if err != nil {
				return translateError(err)
			}

		case <-ctxDone:
			return nil
		}
	}
}

func (s *walletServer) SpentnessNotifications(req *pb.SpentnessNotificationsRequest,
	svr pb.WalletService_SpentnessNotificationsServer) error {

	if req.NoNotifyUnspent && req.NoNotifySpent {
		return grpc.Errorf(codes.InvalidArgument,
			"no_notify_unspent and no_notify_spent may not both be true")
	}

	n := s.wallet.NtfnServer.AccountSpentnessNotifications(req.Account)
	defer n.Done()

	ctxDone := svr.Context().Done()
	for {
		select {
		case v := <-n.C:
			spenderHash, spenderIndex, spent := v.Spender()
			if (spent && req.NoNotifySpent) || (!spent && req.NoNotifyUnspent) {
				continue
			}
			index := v.Index()
			resp := pb.SpentnessNotificationsResponse{
				TransactionHash: v.Hash()[:],
				OutputIndex:     index,
			}
			if spent {
				resp.Spender = &pb.SpentnessNotificationsResponse_Spender{
					TransactionHash: spenderHash[:],
					InputIndex:      spenderIndex,
				}
			}
			err := svr.Send(&resp)
			if err != nil {
				return translateError(err)
			}

		case <-ctxDone:
			return nil
		}
	}
}

func (s *walletServer) AccountNotifications(req *pb.AccountNotificationsRequest,
	svr pb.WalletService_AccountNotificationsServer) error {

	n := s.wallet.NtfnServer.AccountNotifications()
	defer n.Done()

	ctxDone := svr.Context().Done()
	for {
		select {
		case v := <-n.C:
			resp := pb.AccountNotificationsResponse{
				AccountNumber:    v.AccountNumber,
				AccountName:      v.AccountName,
				ExternalKeyCount: v.ExternalKeyCount,
				InternalKeyCount: v.InternalKeyCount,
				ImportedKeyCount: v.ImportedKeyCount,
			}
			err := svr.Send(&resp)
			if err != nil {
				return translateError(err)
			}

		case <-ctxDone:
			return nil
		}
	}
}

// StartWalletLoaderService creates an implementation of the WalletLoaderService
// and registers it with the gRPC server.
func StartWalletLoaderService(server *grpc.Server, loader *wallet.Loader,
	activeNet *netparams.Params) {

	service := &loaderServer{loader: loader, activeNet: activeNet}
	pb.RegisterWalletLoaderServiceServer(server, service)
}

func (s *loaderServer) CreateWallet(ctx context.Context, req *pb.CreateWalletRequest) (
	*pb.CreateWalletResponse, error) {

	defer func() {
		zero.Bytes(req.PrivatePassphrase)
		zero.Bytes(req.Seed)
	}()

	// Use an insecure public passphrase when the request's is empty.
	pubPassphrase := req.PublicPassphrase
	if len(pubPassphrase) == 0 {
		pubPassphrase = []byte(wallet.InsecurePubPassphrase)
	}

	// Seed is required.
	if len(req.Seed) == 0 {
		return nil, grpc.Errorf(codes.InvalidArgument, "seed is a required parameter")
	}

	_, err := s.loader.CreateNewWallet(pubPassphrase, req.PrivatePassphrase, req.Seed)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.CreateWalletResponse{}, nil
}

func (s *loaderServer) OpenWallet(ctx context.Context, req *pb.OpenWalletRequest) (
	*pb.OpenWalletResponse, error) {

	// Use an insecure public passphrase when the request's is empty.
	pubPassphrase := req.PublicPassphrase
	if len(pubPassphrase) == 0 {
		pubPassphrase = []byte(wallet.InsecurePubPassphrase)
	}

	_, err := s.loader.OpenExistingWallet(pubPassphrase, false)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.OpenWalletResponse{}, nil
}

func (s *loaderServer) WalletExists(ctx context.Context, req *pb.WalletExistsRequest) (
	*pb.WalletExistsResponse, error) {

	exists, err := s.loader.WalletExists()
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.WalletExistsResponse{Exists: exists}, nil
}

func (s *loaderServer) CloseWallet(ctx context.Context, req *pb.CloseWalletRequest) (
	*pb.CloseWalletResponse, error) {

	err := s.loader.UnloadWallet()
	if err == wallet.ErrNotLoaded {
		return nil, grpc.Errorf(codes.FailedPrecondition, "wallet is not loaded")
	}
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.CloseWalletResponse{}, nil
}

func (s *loaderServer) StartConsensusRpc(ctx context.Context, req *pb.StartConsensusRpcRequest) (
	*pb.StartConsensusRpcResponse, error) {

	defer zero.Bytes(req.Password)

	defer s.mu.Unlock()
	s.mu.Lock()

	if s.rpcClient != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "RPC client already created")
	}

	networkAddress, err := cfgutil.NormalizeAddress(req.NetworkAddress,
		s.activeNet.RPCClientPort)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument,
			"Network address is ill-formed: %v", err)
	}

	// Error if the wallet is already syncing with the network.
	wallet, walletLoaded := s.loader.LoadedWallet()
	if walletLoaded && wallet.SynchronizingToNetwork() {
		return nil, grpc.Errorf(codes.FailedPrecondition,
			"wallet is loaded and already synchronizing")
	}

	rpcClient, err := chain.NewRPCClient(s.activeNet.Params, networkAddress, req.Username,
		string(req.Password), req.Certificate, len(req.Certificate) == 0, 1)
	if err != nil {
		return nil, translateError(err)
	}

	err = rpcClient.Start()
	if err != nil {
		if err == dcrrpcclient.ErrInvalidAuth {
			return nil, grpc.Errorf(codes.InvalidArgument,
				"Invalid RPC credentials: %v", err)
		}
		return nil, grpc.Errorf(codes.NotFound,
			"Connection to RPC server failed: %v", err)
	}

	s.rpcClient = rpcClient

	return &pb.StartConsensusRpcResponse{}, nil
}

func (s *loaderServer) DiscoverAddresses(ctx context.Context, req *pb.DiscoverAddressesRequest) (
	*pb.DiscoverAddressesResponse, error) {

	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, grpc.Errorf(codes.FailedPrecondition, "wallet has not been loaded")
	}

	s.mu.Lock()
	chainClient := s.rpcClient
	s.mu.Unlock()
	if chainClient == nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "consensus server RPC client has not been loaded")
	}

	if req.DiscoverAccounts && len(req.PrivatePassphrase) == 0 {
		return nil, grpc.Errorf(codes.InvalidArgument, "private passphrase is required for discovering accounts")
	}

	if req.DiscoverAccounts {
		lock := make(chan time.Time, 1)
		defer func() {
			lock <- time.Time{}
			zero.Bytes(req.PrivatePassphrase)
		}()
		err := wallet.Unlock(req.PrivatePassphrase, lock)
		if err != nil {
			return nil, translateError(err)
		}
	}

	err := wallet.DiscoverActiveAddresses(chainClient, req.DiscoverAccounts)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.DiscoverAddressesResponse{}, nil
}

func (s *loaderServer) SubscribeToBlockNotifications(ctx context.Context, req *pb.SubscribeToBlockNotificationsRequest) (
	*pb.SubscribeToBlockNotificationsResponse, error) {

	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, grpc.Errorf(codes.FailedPrecondition, "wallet has not been loaded")
	}

	s.mu.Lock()
	chainClient := s.rpcClient
	s.mu.Unlock()
	if chainClient == nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "consensus server RPC client has not been loaded")
	}

	err := chainClient.NotifyBlocks()
	if err != nil {
		return nil, translateError(err)
	}

	wallet.AssociateConsensusRPC(chainClient)

	return &pb.SubscribeToBlockNotificationsResponse{}, nil
}

func (s *loaderServer) FetchHeaders(ctx context.Context, req *pb.FetchHeadersRequest) (
	*pb.FetchHeadersResponse, error) {

	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, grpc.Errorf(codes.FailedPrecondition, "wallet has not been loaded")
	}

	s.mu.Lock()
	chainClient := s.rpcClient
	s.mu.Unlock()
	if chainClient == nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "consensus server RPC client has not been loaded")
	}

	fetchedHeaderCount, rescanFrom, rescanFromHeight, err := wallet.FetchHeaders(chainClient)
	if err != nil {
		return nil, translateError(err)
	}

	res := &pb.FetchHeadersResponse{
		FetchedHeadersCount: uint32(fetchedHeaderCount),
	}
	if fetchedHeaderCount > 0 {
		res.FirstNewBlockHash = rescanFrom[:]
		res.FirstNewBlockHeight = rescanFromHeight
	}
	return res, nil
}
