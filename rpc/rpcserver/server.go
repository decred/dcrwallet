// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016-2017 The Decred developers
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
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/decred/dcrd/addrmgr"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/internal/cfgutil"
	h "github.com/decred/dcrwallet/internal/helpers"
	"github.com/decred/dcrwallet/internal/zero"
	"github.com/decred/dcrwallet/loader"
	"github.com/decred/dcrwallet/netparams"
	"github.com/decred/dcrwallet/p2p"
	pb "github.com/decred/dcrwallet/rpc/walletrpc"
	"github.com/decred/dcrwallet/spv"
	"github.com/decred/dcrwallet/ticketbuyer"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletseed"
)

// Public API version constants
const (
	semverString = "5.1.0"
	semverMajor  = 5
	semverMinor  = 1
	semverPatch  = 0
)

// translateError creates a new gRPC error with an appropiate error code for
// recognized errors.
//
// This function is by no means complete and should be expanded based on other
// known errors.  Any RPC handler not returning a gRPC error (with status.Errorf)
// should return this result instead.
func translateError(err error) error {
	code := errorCode(err)
	return status.Errorf(code, "%s", err.Error())
}

func errorCode(err error) codes.Code {
	var inner error
	if err, ok := err.(*errors.Error); ok {
		switch err.Kind {
		case errors.Bug:
		case errors.Invalid:
			return codes.InvalidArgument
		case errors.Permission:
			return codes.PermissionDenied
		case errors.IO:
		case errors.Exist:
			return codes.AlreadyExists
		case errors.NotExist:
			return codes.NotFound
		case errors.Encoding:
		case errors.Crypto:
			return codes.DataLoss
		case errors.Locked:
			return codes.FailedPrecondition
		case errors.Passphrase:
			return codes.InvalidArgument
		case errors.Seed:
			return codes.InvalidArgument
		case errors.WatchingOnly:
			return codes.Unimplemented
		case errors.InsufficientBalance:
			return codes.ResourceExhausted
		case errors.ScriptFailure:
		case errors.Policy:
		case errors.DoubleSpend:
		case errors.Protocol:
		case errors.NoPeers:
			return codes.Unavailable
		default:
			inner = err.Err
			for {
				err, ok := inner.(*errors.Error)
				if !ok {
					break
				}
				inner = err.Err
			}
		}
	}
	switch inner {
	case hdkeychain.ErrInvalidSeedLen:
		return codes.InvalidArgument
	}
	return codes.Unknown
}

// decodeAddress decodes an address and verifies it is intended for the active
// network.  This should be used preferred to direct usage of
// dcrutil.DecodeAddress, which does not perform the network check.
func decodeAddress(a string, params *chaincfg.Params) (dcrutil.Address, error) {
	addr, err := dcrutil.DecodeAddress(a)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid address %v: %v", a, err)
	}
	if !addr.IsForNet(params) {
		return nil, status.Errorf(codes.InvalidArgument,
			"address %v is not intended for use on %v", a, params.Name)
	}
	return addr, nil
}

func decodeHashes(in [][]byte) ([]*chainhash.Hash, error) {
	out := make([]*chainhash.Hash, len(in))
	var err error
	for i, h := range in {
		out[i], err = chainhash.NewHash(h)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "hash (hex %x): %v", h, err)
		}
	}
	return out, nil
}

// versionServer provides RPC clients with the ability to query the RPC server
// version.
type versionServer struct{}

// walletServer provides wallet services for RPC clients.
type walletServer struct {
	ready  uint32 // atomic
	wallet *wallet.Wallet
}

// loaderServer provides RPC clients with the ability to load and close wallets,
// as well as establishing a RPC connection to a dcrd consensus server.
type loaderServer struct {
	ready     uint32 // atomic
	loader    *loader.Loader
	activeNet *netparams.Params
	rpcClient *chain.RPCClient
	mu        sync.Mutex
}

// seedServer provides RPC clients with the ability to generate secure random
// seeds encoded in both binary and human-readable formats, and decode any
// human-readable input back to binary.
type seedServer struct{}

// ticketbuyerServer provides RPC clients with the ability to start/stop the
// automatic ticket buyer service.
type ticketbuyerServer struct {
	ready          uint32 // atomic
	loader         *loader.Loader
	ticketbuyerCfg *ticketbuyer.Config
}

type agendaServer struct {
	ready     uint32 // atomic
	activeNet *chaincfg.Params
}

type votingServer struct {
	ready  uint32 // atomic
	wallet *wallet.Wallet
}

// messageVerificationServer provides RPC clients with the ability to verify
// that a message was signed using the private key of a particular address.
type messageVerificationServer struct{}

type decodeMessageServer struct {
	chainParams *chaincfg.Params
}

// Singleton implementations of each service.  Not all services are immediately
// usable.
var (
	versionService             versionServer
	walletService              walletServer
	loaderService              loaderServer
	seedService                seedServer
	ticketBuyerService         ticketbuyerServer
	agendaService              agendaServer
	votingService              votingServer
	messageVerificationService messageVerificationServer
	decodeMessageService       decodeMessageServer
)

// RegisterServices registers implementations of each gRPC service and registers
// it with the server.  Not all service are ready to be used after registration.
func RegisterServices(server *grpc.Server) {
	pb.RegisterVersionServiceServer(server, &versionService)
	pb.RegisterWalletServiceServer(server, &walletService)
	pb.RegisterWalletLoaderServiceServer(server, &loaderService)
	pb.RegisterSeedServiceServer(server, &seedService)
	pb.RegisterTicketBuyerServiceServer(server, &ticketBuyerService)
	pb.RegisterAgendaServiceServer(server, &agendaService)
	pb.RegisterVotingServiceServer(server, &votingService)
	pb.RegisterMessageVerificationServiceServer(server, &messageVerificationService)
	pb.RegisterDecodeMessageServiceServer(server, &decodeMessageService)
}

var serviceMap = map[string]interface{}{
	"walletrpc.VersionService":             &versionService,
	"walletrpc.WalletService":              &walletService,
	"walletrpc.WalletLoaderService":        &loaderService,
	"walletrpc.SeedService":                &seedService,
	"walletrpc.TicketBuyerService":         &ticketBuyerService,
	"walletrpc.AgendaService":              &agendaService,
	"walletrpc.VotingService":              &votingService,
	"walletrpc.MessageVerificationService": &messageVerificationService,
	"walletrpc.DecodeMessageService":       &decodeMessageService,
}

// ServiceReady returns nil when the service is ready and a gRPC error when not.
func ServiceReady(service string) error {
	s, ok := serviceMap[service]
	if !ok {
		return status.Errorf(codes.Unimplemented, "service %s not found", service)
	}
	type readyChecker interface {
		checkReady() bool
	}
	ready := true
	r, ok := s.(readyChecker)
	if ok {
		ready = r.checkReady()
	}
	if !ready {
		return status.Errorf(codes.FailedPrecondition, "service %v is not ready", service)
	}
	return nil
}

func (*versionServer) Version(ctx context.Context, req *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{
		VersionString: semverString,
		Major:         semverMajor,
		Minor:         semverMinor,
		Patch:         semverPatch,
	}, nil
}

// StartWalletService starts the WalletService.
func StartWalletService(server *grpc.Server, wallet *wallet.Wallet) {
	walletService.wallet = wallet
	if atomic.SwapUint32(&walletService.ready, 1) != 0 {
		panic("service already started")
	}
}

func (s *walletServer) checkReady() bool {
	return atomic.LoadUint32(&s.ready) != 0
}

// requireNetworkBackend checks whether the wallet has been associated with the
// consensus server RPC client, returning a gRPC error when it is not.
func (s *walletServer) requireNetworkBackend() (wallet.NetworkBackend, error) {
	n, err := s.wallet.NetworkBackend()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"wallet is not associated with a consensus server RPC client")
	}
	return n, nil
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

func (s *walletServer) Accounts(ctx context.Context, req *pb.AccountsRequest) (*pb.AccountsResponse, error) {
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
			ExternalKeyCount: a.LastUsedExternalIndex + 20, // Add gap limit
			InternalKeyCount: a.LastUsedInternalIndex + 20,
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

func (s *walletServer) PublishUnminedTransactions(ctx context.Context, req *pb.PublishUnminedTransactionsRequest) (
	*pb.PublishUnminedTransactionsResponse, error) {
	n, err := s.requireNetworkBackend()
	if err != nil {
		return nil, err
	}
	err = s.wallet.PublishUnminedTransactions(ctx, n)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.PublishUnminedTransactionsResponse{}, nil
}

func (s *walletServer) Rescan(req *pb.RescanRequest, svr pb.WalletService_RescanServer) error {
	n, err := s.requireNetworkBackend()
	if err != nil {
		return err
	}

	var blockID *wallet.BlockIdentifier
	switch {
	case req.BeginHash != nil && req.BeginHeight != 0:
		return status.Errorf(codes.InvalidArgument, "begin hash and height must not be set together")
	case req.BeginHeight < 0:
		return status.Errorf(codes.InvalidArgument, "begin height must be non-negative")
	case req.BeginHash != nil:
		blockHash, err := chainhash.NewHash(req.BeginHash)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "block hash has invalid length")
		}
		blockID = wallet.NewBlockIdentifierFromHash(blockHash)
	default:
		blockID = wallet.NewBlockIdentifierFromHeight(req.BeginHeight)
	}

	b, err := s.wallet.BlockInfo(blockID)
	if err != nil {
		return translateError(err)
	}

	progress := make(chan wallet.RescanProgress, 1)
	go s.wallet.RescanProgressFromHeight(svr.Context(), n, b.Height, progress)

	for p := range progress {
		if p.Err != nil {
			return translateError(p.Err)
		}
		resp := &pb.RescanResponse{RescannedThrough: p.ScannedThrough}
		err := svr.Send(resp)
		if err != nil {
			return translateError(err)
		}
	}
	// finished or cancelled rescan without error
	select {
	case <-svr.Context().Done():
		return status.Errorf(codes.Canceled, "rescan canceled")
	default:
		return nil
	}
}

func (s *walletServer) NextAccount(ctx context.Context, req *pb.NextAccountRequest) (
	*pb.NextAccountResponse, error) {

	defer zero.Bytes(req.Passphrase)

	if req.AccountName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "account name may not be empty")
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

	var callOpts []wallet.NextAddressCallOption
	switch req.GapPolicy {
	case pb.NextAddressRequest_GAP_POLICY_UNSPECIFIED:
	case pb.NextAddressRequest_GAP_POLICY_ERROR:
		callOpts = append(callOpts, wallet.WithGapPolicyError())
	case pb.NextAddressRequest_GAP_POLICY_IGNORE:
		callOpts = append(callOpts, wallet.WithGapPolicyIgnore())
	case pb.NextAddressRequest_GAP_POLICY_WRAP:
		callOpts = append(callOpts, wallet.WithGapPolicyWrap())
	default:
		return nil, status.Errorf(codes.InvalidArgument, "gap_policy=%v", req.GapPolicy)
	}

	var (
		addr dcrutil.Address
		err  error
	)
	switch req.Kind {
	case pb.NextAddressRequest_BIP0044_EXTERNAL:
		addr, err = s.wallet.NewExternalAddress(req.Account, callOpts...)
		if err != nil {
			return nil, translateError(err)
		}
	case pb.NextAddressRequest_BIP0044_INTERNAL:
		addr, err = s.wallet.NewInternalAddress(req.Account, callOpts...)
		if err != nil {
			return nil, translateError(err)
		}
	default:
		return nil, status.Errorf(codes.InvalidArgument, "kind=%v", req.Kind)
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
		return nil, status.Errorf(codes.InvalidArgument,
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
	if req.Account != udb.ImportedAddrAccount {
		return nil, status.Errorf(codes.InvalidArgument,
			"Only the imported account accepts private key imports")
	}

	if req.ScanFrom < 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"Attempted to scan from a negative block height")
	}

	if req.ScanFrom > 0 && req.Rescan {
		return nil, status.Errorf(codes.InvalidArgument,
			"Passed a rescan height without rescan set")
	}

	n, err := s.requireNetworkBackend()
	if err != nil {
		return nil, err
	}

	_, err = s.wallet.ImportPrivateKey(wif)
	if err != nil {
		return nil, translateError(err)
	}

	if req.Rescan {
		go s.wallet.RescanFromHeight(context.Background(), n, req.ScanFrom)
	}

	return &pb.ImportPrivateKeyResponse{}, nil
}

func (s *walletServer) ImportScript(ctx context.Context,
	req *pb.ImportScriptRequest) (*pb.ImportScriptResponse, error) {

	defer zero.Bytes(req.Passphrase)

	// TODO: Rather than assuming the "default" version, it must be a parameter
	// to the request.
	sc, addrs, requiredSigs, err := txscript.ExtractPkScriptAddrs(
		txscript.DefaultScriptVersion, req.Script, s.wallet.ChainParams())
	if err != nil && req.RequireRedeemable {
		return nil, status.Errorf(codes.FailedPrecondition,
			"The script is not redeemable by the wallet")
	}
	ownAddrs := 0
	for _, a := range addrs {
		haveAddr, err := s.wallet.HaveAddress(a)
		if err != nil {
			return nil, translateError(err)
		}
		if haveAddr {
			ownAddrs++
		}
	}
	redeemable := sc == txscript.MultiSigTy && ownAddrs >= requiredSigs
	if !redeemable && req.RequireRedeemable {
		return nil, status.Errorf(codes.FailedPrecondition,
			"The script is not redeemable by the wallet")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	if req.ScanFrom < 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"Attempted to scan from a negative block height")
	}

	if req.ScanFrom > 0 && req.Rescan {
		return nil, status.Errorf(codes.InvalidArgument,
			"Passed a rescan height without rescan set")
	}

	n, err := s.requireNetworkBackend()
	if err != nil {
		return nil, err
	}

	err = s.wallet.ImportScript(req.Script)
	if err != nil {
		return nil, translateError(err)
	}

	if req.Rescan {
		go s.wallet.RescanFromHeight(context.Background(), n, req.ScanFrom)
	}

	p2sh, err := dcrutil.NewAddressScriptHash(req.Script, s.wallet.ChainParams())
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.ImportScriptResponse{P2ShAddress: p2sh.String(), Redeemable: redeemable}, nil
}

func (s *walletServer) Balance(ctx context.Context, req *pb.BalanceRequest) (
	*pb.BalanceResponse, error) {

	account := req.AccountNumber
	reqConfs := req.RequiredConfirmations
	bals, err := s.wallet.CalculateAccountBalance(account, reqConfs)
	if err != nil {
		return nil, translateError(err)
	}

	// TODO: Spendable currently includes multisig outputs that may not
	// actually be spendable without additional keys.
	resp := &pb.BalanceResponse{
		Total:                   int64(bals.Total),
		Spendable:               int64(bals.Spendable),
		ImmatureReward:          int64(bals.ImmatureCoinbaseRewards),
		ImmatureStakeGeneration: int64(bals.ImmatureStakeGeneration),
		LockedByTickets:         int64(bals.LockedByTickets),
		VotingAuthority:         int64(bals.VotingAuthority),
		Unconfirmed:             int64(bals.Unconfirmed),
	}
	return resp, nil
}

func (s *walletServer) TicketPrice(ctx context.Context, req *pb.TicketPriceRequest) (*pb.TicketPriceResponse, error) {
	sdiff, err := s.wallet.NextStakeDifficulty()
	if err == nil {
		_, tipHeight := s.wallet.MainChainTip()
		resp := &pb.TicketPriceResponse{
			TicketPrice: int64(sdiff),
			Height:      tipHeight,
		}
		return resp, nil
	}

	n, err := s.requireNetworkBackend()
	if err != nil {
		return nil, err
	}
	chainClient, err := chain.RPCClientFromBackend(n)
	if err != nil {
		return nil, translateError(err)
	}

	ticketPrice, err := n.StakeDifficulty(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	_, blockHeight, err := chainClient.GetBestBlock()
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.TicketPriceResponse{
		TicketPrice: int64(ticketPrice),
		Height:      int32(blockHeight),
	}, nil
}

func (s *walletServer) StakeInfo(ctx context.Context, req *pb.StakeInfoRequest) (*pb.StakeInfoResponse, error) {
	var chainClient *rpcclient.Client
	if n, err := s.wallet.NetworkBackend(); err == nil {
		client, err := chain.RPCClientFromBackend(n)
		if err == nil {
			chainClient = client
		}
	}
	var si *wallet.StakeInfoData
	var err error
	if chainClient != nil {
		si, err = s.wallet.StakeInfoPrecise(chainClient)
	} else {
		si, err = s.wallet.StakeInfo()
	}
	if err != nil {
		return nil, translateError(err)
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
		Expired:       si.Expired,
		TotalSubsidy:  int64(si.TotalSubsidy),
	}, nil
}

func (s *walletServer) BlockInfo(ctx context.Context, req *pb.BlockInfoRequest) (*pb.BlockInfoResponse, error) {
	var blockID *wallet.BlockIdentifier
	switch {
	case req.BlockHash != nil && req.BlockHeight != 0:
		return nil, status.Errorf(codes.InvalidArgument, "block hash and height must not be set together")
	case req.BlockHash != nil:
		blockHash, err := chainhash.NewHash(req.BlockHash)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "block hash has invalid length")
		}
		blockID = wallet.NewBlockIdentifierFromHash(blockHash)
	default:
		blockID = wallet.NewBlockIdentifierFromHeight(req.BlockHeight)
	}

	b, err := s.wallet.BlockInfo(blockID)
	if err != nil {
		return nil, translateError(err)
	}

	header := new(wire.BlockHeader)
	err = header.Deserialize(bytes.NewReader(b.Header[:]))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to deserialize saved block header: %v", err)
	}

	return &pb.BlockInfoResponse{
		BlockHash:        b.Hash[:],
		BlockHeight:      b.Height,
		Confirmations:    b.Confirmations,
		Timestamp:        b.Timestamp,
		BlockHeader:      b.Header[:],
		StakeInvalidated: b.StakeInvalidated,
		ApprovesParent:   header.VoteBits&dcrutil.BlockValid != 0,
	}, nil
}

func (s *walletServer) UnspentOutputs(req *pb.UnspentOutputsRequest, svr pb.WalletService_UnspentOutputsServer) error {
	policy := wallet.OutputSelectionPolicy{
		Account:               req.Account,
		RequiredConfirmations: req.RequiredConfirmations,
	}
	inputDetail, err := s.wallet.SelectInputs(dcrutil.Amount(req.TargetAmount), policy)
	// Do not return errors to caller when there was insufficient spendable
	// outputs available for the target amount.
	if err != nil && !errors.Is(errors.InsufficientBalance, err) {
		return translateError(err)
	}

	var sum int64
	for i, input := range inputDetail.Inputs {
		select {
		case <-svr.Context().Done():
			return status.Errorf(codes.Canceled, "unspentoutputs cancelled")
		default:
			outputInfo, err := s.wallet.OutputInfo(&input.PreviousOutPoint)
			if err != nil {
				return translateError(err)
			}
			unspentOutput := &pb.UnspentOutputResponse{
				TransactionHash: input.PreviousOutPoint.Hash[:],
				OutputIndex:     input.PreviousOutPoint.Index,
				Tree:            int32(input.PreviousOutPoint.Tree),
				Amount:          int64(outputInfo.Amount),
				PkScript:        inputDetail.Scripts[i],
				ReceiveTime:     outputInfo.Received.Unix(),
				FromCoinbase:    outputInfo.FromCoinbase,
			}

			sum += unspentOutput.Amount
			unspentOutput.AmountSum = sum

			err = svr.Send(unspentOutput)
			if err != nil {
				return translateError(err)
			}
		}
	}
	return nil
}

func (s *walletServer) FundTransaction(ctx context.Context, req *pb.FundTransactionRequest) (
	*pb.FundTransactionResponse, error) {

	policy := wallet.OutputSelectionPolicy{
		Account:               req.Account,
		RequiredConfirmations: req.RequiredConfirmations,
	}
	inputDetail, err := s.wallet.SelectInputs(dcrutil.Amount(req.TargetAmount), policy)
	// Do not return errors to caller when there was insufficient spendable
	// outputs available for the target amount.
	if err != nil && !errors.Is(errors.InsufficientBalance, err) {
		return nil, translateError(err)
	}

	selectedOutputs := make([]*pb.FundTransactionResponse_PreviousOutput, len(inputDetail.Inputs))
	for i, input := range inputDetail.Inputs {
		outputInfo, err := s.wallet.OutputInfo(&input.PreviousOutPoint)
		if err != nil {
			return nil, translateError(err)
		}
		selectedOutputs[i] = &pb.FundTransactionResponse_PreviousOutput{
			TransactionHash: input.PreviousOutPoint.Hash[:],
			OutputIndex:     input.PreviousOutPoint.Index,
			Tree:            int32(input.PreviousOutPoint.Tree),
			Amount:          int64(outputInfo.Amount),
			PkScript:        inputDetail.Scripts[i],
			ReceiveTime:     outputInfo.Received.Unix(),
			FromCoinbase:    outputInfo.FromCoinbase,
		}
	}

	var changeScript []byte
	if req.IncludeChangeScript && inputDetail.Amount > dcrutil.Amount(req.TargetAmount) {
		changeAddr, err := s.wallet.NewChangeAddress(req.Account)
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
		TotalAmount:     int64(inputDetail.Amount),
		ChangePkScript:  changeScript,
	}, nil
}

func decodeDestination(dest *pb.ConstructTransactionRequest_OutputDestination,
	chainParams *chaincfg.Params) (pkScript []byte, version uint16, err error) {

	switch {
	case dest == nil:
		fallthrough
	default:
		return nil, 0, status.Errorf(codes.InvalidArgument, "unknown or missing output destination")

	case dest.Address != "":
		addr, err := decodeAddress(dest.Address, chainParams)
		if err != nil {
			return nil, 0, err
		}
		pkScript, err = txscript.PayToAddrScript(addr)
		if err != nil {
			return nil, 0, translateError(err)
		}
		version = txscript.DefaultScriptVersion
		return pkScript, version, nil
	case dest.Script != nil:
		if dest.ScriptVersion > uint32(^uint16(0)) {
			return nil, 0, status.Errorf(codes.InvalidArgument, "script_version overflows uint16")
		}
		return dest.Script, uint16(dest.ScriptVersion), nil
	}
}

type txChangeSource struct {
	version uint16
	script  []byte
}

func (src *txChangeSource) Script() ([]byte, uint16, error) {
	return src.script, src.version, nil
}

func (src *txChangeSource) ScriptSize() int {
	return len(src.script)
}

func makeTxChangeSource(destination *pb.ConstructTransactionRequest_OutputDestination,
	chainParams *chaincfg.Params) (*txChangeSource, error) {
	script, version, err := decodeDestination(destination, chainParams)
	if err != nil {
		return nil, err
	}
	changeSource := &txChangeSource{
		script:  script,
		version: version,
	}
	return changeSource, nil
}

func (s *walletServer) ConstructTransaction(ctx context.Context, req *pb.ConstructTransactionRequest) (
	*pb.ConstructTransactionResponse, error) {

	chainParams := s.wallet.ChainParams()

	if len(req.NonChangeOutputs) == 0 && req.ChangeDestination == nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"non_change_outputs and change_destination may not both be empty or null")
	}

	outputs := make([]*wire.TxOut, 0, len(req.NonChangeOutputs))
	for _, o := range req.NonChangeOutputs {
		script, version, err := decodeDestination(o.Destination, chainParams)
		if err != nil {
			return nil, err
		}
		output := &wire.TxOut{
			Value:    o.Amount,
			Version:  version,
			PkScript: script,
		}
		outputs = append(outputs, output)
	}

	var algo wallet.OutputSelectionAlgorithm
	switch req.OutputSelectionAlgorithm {
	case pb.ConstructTransactionRequest_UNSPECIFIED:
		algo = wallet.OutputSelectionAlgorithmDefault
	case pb.ConstructTransactionRequest_ALL:
		algo = wallet.OutputSelectionAlgorithmAll
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown output selection algorithm")
	}

	feePerKb := txrules.DefaultRelayFeePerKb
	if req.FeePerKb != 0 {
		feePerKb = dcrutil.Amount(req.FeePerKb)
	}

	var changeSource txauthor.ChangeSource
	var err error
	if req.ChangeDestination != nil {
		changeSource, err = makeTxChangeSource(req.ChangeDestination, chainParams)
		if err != nil {
			return nil, translateError(err)
		}
	}

	tx, err := s.wallet.NewUnsignedTransaction(outputs, feePerKb, req.SourceAccount,
		req.RequiredConfirmations, algo, changeSource)
	if err != nil {
		return nil, translateError(err)
	}

	if tx.ChangeIndex >= 0 {
		tx.RandomizeChangePosition()
	}

	var txBuf bytes.Buffer
	txBuf.Grow(tx.Tx.SerializeSize())
	err = tx.Tx.Serialize(&txBuf)
	if err != nil {
		return nil, translateError(err)
	}

	res := &pb.ConstructTransactionResponse{
		UnsignedTransaction:       txBuf.Bytes(),
		TotalPreviousOutputAmount: int64(tx.TotalInput),
		TotalOutputAmount:         int64(h.SumOutputValues(tx.Tx.TxOut)),
		EstimatedSignedSize:       uint32(tx.EstimatedSignedSerializeSize),
		ChangeIndex:               int32(tx.ChangeIndex),
	}
	return res, nil
}

func (s *walletServer) GetAccountExtendedPubKey(ctx context.Context, req *pb.GetAccountExtendedPubKeyRequest) (*pb.GetAccountExtendedPubKeyResponse, error) {
	accExtendedPubKey, err := s.wallet.MasterPubKey(req.AccountNumber)
	if err != nil {
		return nil, err
	}
	res := &pb.GetAccountExtendedPubKeyResponse{
		AccExtendedPubKey: accExtendedPubKey.String(),
	}
	return res, nil
}

func (s *walletServer) GetTransaction(ctx context.Context, req *pb.GetTransactionRequest) (*pb.GetTransactionResponse, error) {
	txHash, err := chainhash.NewHash(req.TransactionHash)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "transaction_hash has invalid length")
	}

	txSummary, confs, blockHash, err := s.wallet.TransactionSummary(txHash)
	if err != nil {
		return nil, translateError(err)
	}
	resp := &pb.GetTransactionResponse{
		Transaction:   marshalTransactionDetails(txSummary),
		Confirmations: confs,
	}
	if blockHash != nil {
		resp.BlockHash = blockHash[:]
	}
	return resp, nil
}

// BUGS:
// - MinimumRecentTransactions is ignored.
// - Wrong error codes when a block height or hash is not recognized
func (s *walletServer) GetTransactions(req *pb.GetTransactionsRequest,
	server pb.WalletService_GetTransactionsServer) error {

	var startBlock, endBlock *wallet.BlockIdentifier
	if req.StartingBlockHash != nil && req.StartingBlockHeight != 0 {
		return status.Errorf(codes.InvalidArgument,
			"starting block hash and height may not be specified simultaneously")
	} else if req.StartingBlockHash != nil {
		startBlockHash, err := chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		startBlock = wallet.NewBlockIdentifierFromHash(startBlockHash)
	} else if req.StartingBlockHeight != 0 {
		startBlock = wallet.NewBlockIdentifierFromHeight(req.StartingBlockHeight)
	}

	if req.EndingBlockHash != nil && req.EndingBlockHeight != 0 {
		return status.Errorf(codes.InvalidArgument,
			"ending block hash and height may not be specified simultaneously")
	} else if req.EndingBlockHash != nil {
		endBlockHash, err := chainhash.NewHash(req.EndingBlockHash)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		endBlock = wallet.NewBlockIdentifierFromHash(endBlockHash)
	} else if req.EndingBlockHeight != 0 {
		endBlock = wallet.NewBlockIdentifierFromHeight(req.EndingBlockHeight)
	}

	var minRecentTxs int
	if req.MinimumRecentTransactions != 0 {
		if endBlock != nil {
			return status.Errorf(codes.InvalidArgument,
				"ending block and minimum number of recent transactions "+
					"may not be specified simultaneously")
		}
		minRecentTxs = int(req.MinimumRecentTransactions)
		if minRecentTxs < 0 {
			return status.Errorf(codes.InvalidArgument,
				"minimum number of recent transactions may not be negative")
		}
	}
	_ = minRecentTxs

	targetTxCount := int(req.TargetTransactionCount)
	if targetTxCount < 0 {
		return status.Errorf(codes.InvalidArgument,
			"maximum transaction count may not be negative")
	}

	ctx := server.Context()
	txCount := 0

	rangeFn := func(block *wallet.Block) (bool, error) {
		var resp *pb.GetTransactionsResponse
		if block.Header != nil {
			resp = &pb.GetTransactionsResponse{
				MinedTransactions: marshalBlock(block),
			}
		} else {
			resp = &pb.GetTransactionsResponse{
				UnminedTransactions: marshalTransactionDetailsSlice(block.Transactions),
			}
		}
		txCount += len(block.Transactions)

		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
			err := server.Send(resp)
			return (err != nil) || ((targetTxCount > 0) && (txCount >= targetTxCount)), err
		}
	}

	err := s.wallet.GetTransactions(rangeFn, startBlock, endBlock)
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (s *walletServer) GetTickets(req *pb.GetTicketsRequest,
	server pb.WalletService_GetTicketsServer) error {

	var startBlock, endBlock *wallet.BlockIdentifier
	if req.StartingBlockHash != nil && req.StartingBlockHeight != 0 {
		return status.Errorf(codes.InvalidArgument,
			"starting block hash and height may not be specified simultaneously")
	} else if req.StartingBlockHash != nil {
		startBlockHash, err := chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		startBlock = wallet.NewBlockIdentifierFromHash(startBlockHash)
	} else if req.StartingBlockHeight != 0 {
		startBlock = wallet.NewBlockIdentifierFromHeight(req.StartingBlockHeight)
	}

	if req.EndingBlockHash != nil && req.EndingBlockHeight != 0 {
		return status.Errorf(codes.InvalidArgument,
			"ending block hash and height may not be specified simultaneously")
	} else if req.EndingBlockHash != nil {
		endBlockHash, err := chainhash.NewHash(req.EndingBlockHash)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "%s", err.Error())
		}
		endBlock = wallet.NewBlockIdentifierFromHash(endBlockHash)
	} else if req.EndingBlockHeight != 0 {
		endBlock = wallet.NewBlockIdentifierFromHeight(req.EndingBlockHeight)
	}

	targetTicketCount := int(req.TargetTicketCount)
	if targetTicketCount < 0 {
		return status.Errorf(codes.InvalidArgument,
			"target ticket count may not be negative")
	}

	ticketCount := 0
	ctx := server.Context()

	rangeFn := func(tickets []*wallet.TicketSummary, block *wire.BlockHeader) (bool, error) {
		resp := &pb.GetTicketsResponse{
			Block: marshalGetTicketBlockDetails(block),
		}

		// current contract for grpc GetTickets is for one ticket per response.
		// To make sure we don't miss any while paginating, we only check for
		// the targetTicketCount after sending all from this block.
		for _, t := range tickets {
			resp.Ticket = marshalTicketDetails(t)
			err := server.Send(resp)
			if err != nil {
				return true, err
			}
		}
		ticketCount += len(tickets)

		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
			return ((targetTicketCount > 0) && (ticketCount >= targetTicketCount)), nil
		}
	}
	var chainClient *rpcclient.Client
	if n, err := s.wallet.NetworkBackend(); err == nil {
		client, err := chain.RPCClientFromBackend(n)
		if err == nil {
			chainClient = client
		}
	}
	var err error
	if chainClient != nil {
		err = s.wallet.GetTicketsPrecise(rangeFn, chainClient, startBlock, endBlock)
	} else {
		err = s.wallet.GetTickets(rangeFn, startBlock, endBlock)
	}
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (s *walletServer) ChangePassphrase(ctx context.Context, req *pb.ChangePassphraseRequest) (
	*pb.ChangePassphraseResponse, error) {

	defer func() {
		zero.Bytes(req.OldPassphrase)
		zero.Bytes(req.NewPassphrase)
	}()

	var (
		oldPass = req.OldPassphrase
		newPass = req.NewPassphrase
	)

	var err error
	switch req.Key {
	case pb.ChangePassphraseRequest_PRIVATE:
		err = s.wallet.ChangePrivatePassphrase(oldPass, newPass)
	case pb.ChangePassphraseRequest_PUBLIC:
		if len(oldPass) == 0 {
			oldPass = []byte(wallet.InsecurePubPassphrase)
		}
		if len(newPass) == 0 {
			newPass = []byte(wallet.InsecurePubPassphrase)
		}
		err = s.wallet.ChangePublicPassphrase(oldPass, newPass)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "Unknown key type (%d)", req.Key)
	}
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.ChangePassphraseResponse{}, nil
}

func (s *walletServer) SignTransaction(ctx context.Context, req *pb.SignTransactionRequest) (
	*pb.SignTransactionResponse, error) {

	defer zero.Bytes(req.Passphrase)

	var tx wire.MsgTx
	err := tx.Deserialize(bytes.NewReader(req.SerializedTransaction))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
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

	var additionalPkScripts map[wire.OutPoint][]byte
	if len(req.AdditionalScripts) > 0 {
		additionalPkScripts = make(map[wire.OutPoint][]byte, len(req.AdditionalScripts))
		for _, script := range req.AdditionalScripts {
			op := wire.OutPoint{Index: script.OutputIndex, Tree: int8(script.Tree)}
			if len(script.TransactionHash) != chainhash.HashSize {
				return nil, status.Errorf(codes.InvalidArgument,
					"Invalid transaction hash length for script %v, expected %v got %v",
					script, chainhash.HashSize, len(script.TransactionHash))
			}

			copy(op.Hash[:], script.TransactionHash)
			additionalPkScripts[op] = script.PkScript
		}
	}

	invalidSigs, err := s.wallet.SignTransaction(&tx, txscript.SigHashAll, additionalPkScripts, nil, nil)
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

func (s *walletServer) SignTransactions(ctx context.Context, req *pb.SignTransactionsRequest) (
	*pb.SignTransactionsResponse, error) {
	defer zero.Bytes(req.Passphrase)

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	var additionalPkScripts map[wire.OutPoint][]byte
	if len(req.AdditionalScripts) > 0 {
		additionalPkScripts = make(map[wire.OutPoint][]byte, len(req.AdditionalScripts))
		for _, script := range req.AdditionalScripts {
			op := wire.OutPoint{Index: script.OutputIndex, Tree: int8(script.Tree)}
			if len(script.TransactionHash) != chainhash.HashSize {
				return nil, status.Errorf(codes.InvalidArgument,
					"Invalid transaction hash length for script %v, expected %v got %v",
					script, chainhash.HashSize, len(script.TransactionHash))
			}

			copy(op.Hash[:], script.TransactionHash)
			additionalPkScripts[op] = script.PkScript
		}
	}

	resp := pb.SignTransactionsResponse{}
	for _, unsignedTx := range req.Transactions {
		var tx wire.MsgTx
		err := tx.Deserialize(bytes.NewReader(unsignedTx.SerializedTransaction))
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"Bytes do not represent a valid raw transaction: %v", err)
		}

		invalidSigs, err := s.wallet.SignTransaction(&tx, txscript.SigHashAll, additionalPkScripts, nil, nil)
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

		resp.Transactions = append(resp.Transactions, &pb.SignTransactionsResponse_SignedTransaction{
			Transaction:          serializedTransaction.Bytes(),
			UnsignedInputIndexes: invalidInputIndexes,
		})
	}

	return &resp, nil
}

func (s *walletServer) CreateSignature(ctx context.Context, req *pb.CreateSignatureRequest) (
	*pb.CreateSignatureResponse, error) {

	defer zero.Bytes(req.Passphrase)

	var tx wire.MsgTx
	err := tx.Deserialize(bytes.NewReader(req.SerializedTransaction))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"Bytes do not represent a valid raw transaction: %v", err)
	}

	if req.InputIndex >= uint32(len(tx.TxIn)) {
		return nil, status.Errorf(codes.InvalidArgument,
			"transaction input %d does not exist", req.InputIndex)
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	addr, err := decodeAddress(req.Address, s.wallet.ChainParams())
	if err != nil {
		return nil, err
	}

	hashType := txscript.SigHashType(req.HashType)
	sig, pubkey, err := s.wallet.CreateSignature(&tx, req.InputIndex, addr, hashType, req.PreviousPkScript)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.CreateSignatureResponse{Signature: sig, PublicKey: pubkey}, nil
}

func (s *walletServer) PublishTransaction(ctx context.Context, req *pb.PublishTransactionRequest) (
	*pb.PublishTransactionResponse, error) {

	n, err := s.requireNetworkBackend()
	if err != nil {
		return nil, err
	}

	var msgTx wire.MsgTx
	err = msgTx.Deserialize(bytes.NewReader(req.SignedTransaction))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"Bytes do not represent a valid raw transaction: %v", err)
	}

	txHash, err := s.wallet.PublishTransaction(&msgTx, req.SignedTransaction, n)
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
		return nil, status.Errorf(codes.InvalidArgument,
			"Negative spend limit given")
	}

	minConf := int32(req.RequiredConfirmations)
	params := s.wallet.ChainParams()

	var ticketAddr dcrutil.Address
	var err error
	if req.TicketAddress != "" {
		ticketAddr, err = decodeAddress(req.TicketAddress, params)
		if err != nil {
			return nil, err
		}
	}

	var poolAddr dcrutil.Address
	if req.PoolAddress != "" {
		poolAddr, err = decodeAddress(req.PoolAddress, params)
		if err != nil {
			return nil, err
		}
	}

	if req.PoolFees > 0 {
		if !txrules.ValidPoolFeeRate(req.PoolFees) {
			return nil, status.Errorf(codes.InvalidArgument, "Invalid pool fees percentage")
		}
	}

	if req.PoolFees > 0 && poolAddr == nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"Pool fees set but no pool address given")
	}

	if req.PoolFees <= 0 && poolAddr != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"Pool fees negative or unset but pool address given")
	}

	numTickets := int(req.NumTickets)
	if numTickets < 1 {
		return nil, status.Errorf(codes.InvalidArgument,
			"Zero or negative number of tickets given")
	}

	expiry := int32(req.Expiry)
	txFee := dcrutil.Amount(req.TxFee)
	ticketFee := s.wallet.TicketFeeIncrement()

	// Set the ticket fee if specified
	if req.TicketFee > 0 {
		ticketFee = dcrutil.Amount(req.TicketFee)
	}

	if txFee < 0 || ticketFee < 0 {
		return nil, status.Errorf(codes.InvalidArgument,
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
		return nil, status.Errorf(codes.FailedPrecondition,
			"Unable to purchase tickets: %v", err)
	}

	hashes := marshalHashes(resp)

	return &pb.PurchaseTicketsResponse{TicketHashes: hashes}, nil
}

func (s *walletServer) RevokeTickets(ctx context.Context, req *pb.RevokeTicketsRequest) (*pb.RevokeTicketsResponse, error) {
	n, err := s.requireNetworkBackend()
	if err != nil {
		return nil, err
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	// The wallet is not locally aware of when tickets are selected to vote and
	// when they are missed.  RevokeTickets uses trusted RPCs to determine which
	// tickets were missed.  RevokeExpiredTickets is only able to create
	// revocations for tickets which have reached their expiry time even if they
	// were missed prior to expiry, but is able to be used with other backends.
	chainClient, err := chain.RPCClientFromBackend(n)
	if err != nil {
		err := s.wallet.RevokeExpiredTickets(ctx, n)
		if err != nil {
			return nil, translateError(err)
		}
		return &pb.RevokeTicketsResponse{}, nil
	}

	err = s.wallet.RevokeTickets(chainClient)
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.RevokeTicketsResponse{}, nil
}

func (s *walletServer) LoadActiveDataFilters(ctx context.Context, req *pb.LoadActiveDataFiltersRequest) (
	*pb.LoadActiveDataFiltersResponse, error) {

	n, err := s.requireNetworkBackend()
	if err != nil {
		return nil, err
	}

	err = s.wallet.LoadActiveDataFilters(ctx, n, false)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.LoadActiveDataFiltersResponse{}, nil
}

func (s *walletServer) CommittedTickets(ctx context.Context, req *pb.CommittedTicketsRequest) (
	*pb.CommittedTicketsResponse, error) {

	// Translate [][]byte to []*chainhash.Hash
	in := make([]*chainhash.Hash, 0, len(req.Tickets))
	for _, v := range req.Tickets {
		hash, err := chainhash.NewHash(v)
		if err != nil {
			return &pb.CommittedTicketsResponse{},
				status.Error(codes.InvalidArgument,
					"invalid hash "+hex.EncodeToString(v))
		}
		in = append(in, hash)
	}

	// Figure out which tickets we own
	out, outAddr, err := s.wallet.CommittedTickets(in)
	if err != nil {
		return nil, translateError(err)
	}
	if len(out) != len(outAddr) {
		// Sanity check
		return nil, status.Error(codes.Internal,
			"impossible condition: ticket and address count unequal")
	}

	// Translate []*chainhash.Hash to [][]byte
	ctr := &pb.CommittedTicketsResponse{
		TicketAddresses: make([]*pb.CommittedTicketsResponse_TicketAddress,
			0, len(out)),
	}
	for k, v := range out {
		ctr.TicketAddresses = append(ctr.TicketAddresses,
			&pb.CommittedTicketsResponse_TicketAddress{
				Ticket:  v[:],
				Address: outAddr[k].String(),
			})
	}

	return ctr, nil
}

func (s *walletServer) signMessage(address, message string) ([]byte, error) {
	addr, err := decodeAddress(address, s.wallet.ChainParams())
	if err != nil {
		return nil, err
	}

	// Addresses must have an associated secp256k1 private key and therefore
	// must be P2PK or P2PKH (P2SH is not allowed).
	var sig []byte
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA(a.Net()) != dcrec.STEcdsaSecp256k1 {
			goto WrongAddrKind
		}
	default:
		goto WrongAddrKind
	}

	sig, err = s.wallet.SignMessage(message, addr)
	if err != nil {
		return nil, translateError(err)
	}
	return sig, nil

WrongAddrKind:
	return nil, status.Error(codes.InvalidArgument,
		"address must be secp256k1 P2PK or P2PKH")
}

func (s *walletServer) SignMessage(cts context.Context, req *pb.SignMessageRequest) (*pb.SignMessageResponse, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	sig, err := s.signMessage(req.Address, req.Message)
	if err != nil {
		return nil, err
	}

	return &pb.SignMessageResponse{Signature: sig}, nil
}

func (s *walletServer) SignMessages(cts context.Context, req *pb.SignMessagesRequest) (*pb.SignMessagesResponse, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	smr := pb.SignMessagesResponse{
		Replies: make([]*pb.SignMessagesResponse_SignReply, 0,
			len(req.Messages)),
	}
	for _, v := range req.Messages {
		e := ""
		sig, err := s.signMessage(v.Address, v.Message)
		if err != nil {
			e = err.Error()
		}
		smr.Replies = append(smr.Replies,
			&pb.SignMessagesResponse_SignReply{
				Signature: sig,
				Error:     e,
			})
	}

	return &smr, nil
}

func (s *walletServer) ValidateAddress(ctx context.Context, req *pb.ValidateAddressRequest) (*pb.ValidateAddressResponse, error) {
	result := &pb.ValidateAddressResponse{}
	addr, err := decodeAddress(req.GetAddress(), s.wallet.ChainParams())
	if err != nil {
		return result, nil
	}

	result.IsValid = true
	addrInfo, err := s.wallet.AddressInfo(addr)
	if err != nil {
		if errors.Is(errors.NotExist, err) {
			// No additional information available about the address.
			return result, nil
		}
		return nil, err
	}

	// The address lookup was successful which means there is further
	// information about it available and it is "mine".
	result.IsMine = true
	acctName, err := s.wallet.AccountName(addrInfo.Account())
	if err != nil {
		return nil, translateError(err)
	}

	acctNumber, err := s.wallet.AccountNumber(acctName)
	if err != nil {
		return nil, err
	}
	result.AccountNumber = acctNumber

	switch ma := addrInfo.(type) {
	case udb.ManagedPubKeyAddress:
		result.PubKey = ma.PubKey().Serialize()
		pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(result.PubKey,
			s.wallet.ChainParams())
		if err != nil {
			return nil, err
		}
		result.PubKeyAddr = pubKeyAddr.EncodeAddress()

	case udb.ManagedScriptAddress:
		result.IsScript = true

		// The script is only available if the manager is unlocked, so
		// just break out now if there is an error.
		script, err := s.wallet.RedeemScriptCopy(addr)
		if err != nil {
			break
		}
		result.PayToAddrScript = script

		// This typically shouldn't fail unless an invalid script was
		// imported.  However, if it fails for any reason, there is no
		// further information available, so just set the script type
		// a non-standard and break out now.
		class, addrs, reqSigs, err := txscript.ExtractPkScriptAddrs(
			txscript.DefaultScriptVersion, script, s.wallet.ChainParams())
		if err != nil {
			result.ScriptType = pb.ValidateAddressResponse_NonStandardTy
			break
		}

		addrStrings := make([]string, len(addrs))
		for i, a := range addrs {
			addrStrings[i] = a.EncodeAddress()
		}
		result.PkScriptAddrs = addrStrings

		// Multi-signature scripts also provide the number of required
		// signatures.
		result.ScriptType = pb.ValidateAddressResponse_ScriptType(uint32(class))
		if class == txscript.MultiSigTy {
			result.SigsRequired = uint32(reqSigs)
		}
	}

	return result, nil
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
		address := ""
		if output.Address != nil {
			address = output.Address.String()
		}
		outputs[i] = &pb.TransactionDetails_Output{
			Index:        output.Index,
			Account:      output.Account,
			Internal:     output.Internal,
			Amount:       int64(output.Amount),
			Address:      address,
			OutputScript: output.OutputScript,
		}
	}
	return outputs
}

func marshalTxType(walletTxType wallet.TransactionType) pb.TransactionDetails_TransactionType {
	switch walletTxType {
	case wallet.TransactionTypeCoinbase:
		return pb.TransactionDetails_COINBASE
	case wallet.TransactionTypeTicketPurchase:
		return pb.TransactionDetails_TICKET_PURCHASE
	case wallet.TransactionTypeVote:
		return pb.TransactionDetails_VOTE
	case wallet.TransactionTypeRevocation:
		return pb.TransactionDetails_REVOCATION
	default:
		return pb.TransactionDetails_REGULAR
	}
}

func marshalTransactionDetails(tx *wallet.TransactionSummary) *pb.TransactionDetails {

	return &pb.TransactionDetails{
		Hash:            tx.Hash[:],
		Transaction:     tx.Transaction,
		Debits:          marshalTransactionInputs(tx.MyInputs),
		Credits:         marshalTransactionOutputs(tx.MyOutputs),
		Fee:             int64(tx.Fee),
		Timestamp:       tx.Timestamp,
		TransactionType: marshalTxType(tx.Type),
	}
}

func marshalTransactionDetailsSlice(v []wallet.TransactionSummary) []*pb.TransactionDetails {
	txs := make([]*pb.TransactionDetails, len(v))
	for i := range v {
		txs[i] = marshalTransactionDetails(&v[i])
	}
	return txs
}

func marshalTicketDetails(ticket *wallet.TicketSummary) *pb.GetTicketsResponse_TicketDetails {
	var ticketStatus = pb.GetTicketsResponse_TicketDetails_LIVE
	switch ticket.Status {
	case wallet.TicketStatusExpired:
		ticketStatus = pb.GetTicketsResponse_TicketDetails_EXPIRED
	case wallet.TicketStatusImmature:
		ticketStatus = pb.GetTicketsResponse_TicketDetails_IMMATURE
	case wallet.TicketStatusVoted:
		ticketStatus = pb.GetTicketsResponse_TicketDetails_VOTED
	case wallet.TicketStatusRevoked:
		ticketStatus = pb.GetTicketsResponse_TicketDetails_REVOKED
	case wallet.TicketStatusUnmined:
		ticketStatus = pb.GetTicketsResponse_TicketDetails_UNMINED
	case wallet.TicketStatusMissed:
		ticketStatus = pb.GetTicketsResponse_TicketDetails_MISSED
	case wallet.TicketStatusUnknown:
		ticketStatus = pb.GetTicketsResponse_TicketDetails_UNKNOWN
	}
	spender := &pb.TransactionDetails{}
	if ticket.Spender != nil {
		spender = marshalTransactionDetails(ticket.Spender)
	}
	return &pb.GetTicketsResponse_TicketDetails{
		Ticket:       marshalTransactionDetails(ticket.Ticket),
		Spender:      spender,
		TicketStatus: ticketStatus,
	}
}

func marshalGetTicketBlockDetails(v *wire.BlockHeader) *pb.GetTicketsResponse_BlockDetails {
	if v == nil || v.Height < 0 {
		return nil
	}

	blockHash := v.BlockHash()
	return &pb.GetTicketsResponse_BlockDetails{
		Hash:      blockHash[:],
		Height:    int32(v.Height),
		Timestamp: v.Timestamp.Unix(),
	}
}

func marshalBlock(v *wallet.Block) *pb.BlockDetails {
	txs := marshalTransactionDetailsSlice(v.Transactions)

	if v.Header == nil {
		return &pb.BlockDetails{
			Hash:         nil,
			Height:       -1,
			Transactions: txs,
		}
	}

	hash := v.Header.BlockHash()
	return &pb.BlockDetails{
		Hash:           hash[:],
		Height:         int32(v.Header.Height),
		Timestamp:      v.Header.Timestamp.Unix(),
		ApprovesParent: v.Header.VoteBits&dcrutil.BlockValid != 0,
		Transactions:   txs,
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
				UnminedTransactions:      marshalTransactionDetailsSlice(v.UnminedTransactions),
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

func (s *walletServer) ConfirmationNotifications(svr pb.WalletService_ConfirmationNotificationsServer) error {
	c := s.wallet.NtfnServer.ConfirmationNotifications(svr.Context())
	errOut := make(chan error, 2)
	go func() {
		for {
			req, err := svr.Recv()
			if err != nil {
				errOut <- err
				return
			}
			txHashes, err := decodeHashes(req.TxHashes)
			if err != nil {
				errOut <- err
				return
			}
			if req.StopAfter < 0 {
				errOut <- status.Errorf(codes.InvalidArgument, "stop_after must be non-negative")
				return
			}
			c.Watch(txHashes, req.StopAfter)
		}
	}()
	go func() {
		for {
			n, err := c.Recv()
			if err != nil {
				errOut <- err
				return
			}
			results := make([]*pb.ConfirmationNotificationsResponse_TransactionConfirmations, len(n))
			for i, r := range n {
				var blockHash []byte
				if r.BlockHash != nil {
					blockHash = r.BlockHash[:]
				}
				results[i] = &pb.ConfirmationNotificationsResponse_TransactionConfirmations{
					TxHash:        r.TxHash[:],
					Confirmations: r.Confirmations,
					BlockHash:     blockHash,
					BlockHeight:   r.BlockHeight,
				}
			}
			r := &pb.ConfirmationNotificationsResponse{
				Confirmations: results,
			}
			err = svr.Send(r)
			if err != nil {
				errOut <- err
				return
			}
		}
	}()

	select {
	case <-svr.Context().Done():
		return nil
	case err := <-errOut:
		if err == context.Canceled {
			return nil
		}
		if _, ok := status.FromError(err); ok {
			return err
		}
		return translateError(err)
	}
}

// StartWalletLoaderService starts the WalletLoaderService.
func StartWalletLoaderService(server *grpc.Server, loader *loader.Loader, activeNet *netparams.Params) {
	loaderService.loader = loader
	loaderService.activeNet = activeNet
	if atomic.SwapUint32(&loaderService.ready, 1) != 0 {
		panic("service already started")
	}
}

func (s *loaderServer) checkReady() bool {
	return atomic.LoadUint32(&s.ready) != 0
}

// StartTicketBuyerService starts the TicketBuyerService.
func StartTicketBuyerService(server *grpc.Server, loader *loader.Loader, tbCfg *ticketbuyer.Config) {
	ticketBuyerService.loader = loader
	ticketBuyerService.ticketbuyerCfg = tbCfg
	if atomic.SwapUint32(&ticketBuyerService.ready, 1) != 0 {
		panic("service already started")
	}
}

func (t *ticketbuyerServer) checkReady() bool {
	return atomic.LoadUint32(&t.ready) != 0
}

// StartAutoBuyer starts the automatic ticket buyer.
func (t *ticketbuyerServer) StartAutoBuyer(ctx context.Context, req *pb.StartAutoBuyerRequest) (
	*pb.StartAutoBuyerResponse, error) {

	wallet, ok := t.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}
	err := wallet.Unlock(req.Passphrase, nil)
	if err != nil {
		return nil, translateError(err)
	}

	accountName, err := wallet.AccountName(req.Account)
	if err != nil {
		return nil, translateError(err)
	}

	if req.BalanceToMaintain < 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"Negative balance to maintain given")
	}

	if req.MaxFeePerKb < 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"Negative max fee per KB given")
	}

	if req.MaxPriceAbsolute < 0 && req.MaxPriceRelative < 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"Negative max ticket price given")
	}
	params := wallet.ChainParams()

	var votingAddress dcrutil.Address
	if req.VotingAddress != "" {
		votingAddress, err = decodeAddress(req.VotingAddress, params)
		if err != nil {
			return nil, err
		}
	}

	var poolAddress dcrutil.Address
	if req.PoolAddress != "" {
		poolAddress, err = decodeAddress(req.PoolAddress, params)
		if err != nil {
			return nil, err
		}
	}

	poolFees := req.PoolFees
	switch {
	case poolFees == 0 && poolAddress != nil:
		return nil, status.Errorf(codes.InvalidArgument, "Pool address set but no pool fees given")
	case poolFees != 0 && poolAddress == nil:
		return nil, status.Errorf(codes.InvalidArgument, "Pool fees set but no pool address given")
	case poolFees != 0 && poolAddress != nil:
		if !txrules.ValidPoolFeeRate(req.PoolFees) {
			return nil, status.Errorf(codes.InvalidArgument, "Invalid pool fees percentage")
		}
	}

	config := &ticketbuyer.Config{
		AccountName:               accountName,
		AvgPriceMode:              t.ticketbuyerCfg.AvgPriceMode,
		AvgPriceVWAPDelta:         t.ticketbuyerCfg.AvgPriceVWAPDelta,
		BalanceToMaintainAbsolute: req.BalanceToMaintain,
		BlocksToAvg:               t.ticketbuyerCfg.BlocksToAvg,
		DontWaitForTickets:        t.ticketbuyerCfg.DontWaitForTickets,
		ExpiryDelta:               t.ticketbuyerCfg.ExpiryDelta,
		FeeSource:                 t.ticketbuyerCfg.FeeSource,
		FeeTargetScaling:          t.ticketbuyerCfg.FeeTargetScaling,
		MinFee:                    t.ticketbuyerCfg.MinFee,
		MaxFee:                    req.MaxFeePerKb,
		MaxPerBlock:               int(req.MaxPerBlock),
		MaxPriceAbsolute:          req.MaxPriceAbsolute,
		MaxPriceRelative:          req.MaxPriceRelative,
		MaxInMempool:              t.ticketbuyerCfg.MaxInMempool,
		PoolAddress:               poolAddress,
		PoolFees:                  poolFees,
		NoSpreadTicketPurchases:   t.ticketbuyerCfg.NoSpreadTicketPurchases,
		VotingAddress:             votingAddress,
		TxFee:                     t.ticketbuyerCfg.TxFee,
	}
	err = t.loader.StartTicketPurchase(req.Passphrase, config)
	if errors.Is(errors.Invalid, err) {
		return nil, status.Errorf(codes.FailedPrecondition, "%s", err)
	}
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.StartAutoBuyerResponse{}, err
}

// StopAutoBuyer stop the automatic ticket buyer.
func (t *ticketbuyerServer) StopAutoBuyer(ctx context.Context, req *pb.StopAutoBuyerRequest) (
	*pb.StopAutoBuyerResponse, error) {

	err := t.loader.StopTicketPurchase()
	if errors.Is(errors.Invalid, err) {
		return nil, status.Errorf(codes.FailedPrecondition, "Ticket buyer is not running")
	}
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.StopAutoBuyerResponse{}, nil
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
		return nil, status.Errorf(codes.InvalidArgument, "seed is a required parameter")
	}

	_, err := s.loader.CreateNewWallet(pubPassphrase, req.PrivatePassphrase, req.Seed)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.CreateWalletResponse{}, nil
}

func (s *loaderServer) CreateWatchingOnlyWallet(ctx context.Context, req *pb.CreateWatchingOnlyWalletRequest) (
	*pb.CreateWatchingOnlyWalletResponse, error) {

	// Use an insecure public passphrase when the request's is empty.
	pubPassphrase := req.PublicPassphrase
	if len(pubPassphrase) == 0 {
		pubPassphrase = []byte(wallet.InsecurePubPassphrase)
	}

	_, err := s.loader.CreateWatchingOnlyWallet(req.ExtendedPubKey, pubPassphrase)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.CreateWatchingOnlyWalletResponse{}, nil
}

func (s *loaderServer) OpenWallet(ctx context.Context, req *pb.OpenWalletRequest) (
	*pb.OpenWalletResponse, error) {

	// Use an insecure public passphrase when the request's is empty.
	pubPassphrase := req.PublicPassphrase
	if len(pubPassphrase) == 0 {
		pubPassphrase = []byte(wallet.InsecurePubPassphrase)
	}

	w, err := s.loader.OpenExistingWallet(pubPassphrase)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.OpenWalletResponse{
		WatchingOnly: w.Manager.WatchingOnly(),
	}, nil
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
	if errors.Is(errors.Invalid, err) {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet is not loaded")
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
		return nil, status.Errorf(codes.FailedPrecondition, "RPC client already created")
	}

	networkAddress, err := cfgutil.NormalizeAddress(req.NetworkAddress,
		s.activeNet.JSONRPCClientPort)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"Network address is ill-formed: %v", err)
	}

	// Error if the wallet is already syncing with the network.
	wallet, walletLoaded := s.loader.LoadedWallet()
	if walletLoaded {
		_, err := wallet.NetworkBackend()
		if err == nil {
			return nil, status.Errorf(codes.FailedPrecondition,
				"wallet is loaded and already synchronizing")
		}
	}

	rpcClient, err := chain.NewRPCClient(s.activeNet.Params, networkAddress, req.Username,
		string(req.Password), req.Certificate, len(req.Certificate) == 0)
	if err != nil {
		return nil, translateError(err)
	}

	err = rpcClient.Start(ctx, false)
	if err != nil {
		if err == rpcclient.ErrInvalidAuth {
			return nil, status.Errorf(codes.InvalidArgument,
				"Invalid RPC credentials: %v", err)
		}
		return nil, status.Errorf(codes.NotFound,
			"Connection to RPC server failed: %v", err)
	}

	s.rpcClient = rpcClient
	s.loader.SetNetworkBackend(chain.BackendFromRPCClient(rpcClient.Client))

	return &pb.StartConsensusRpcResponse{}, nil
}

func (s *loaderServer) DiscoverAddresses(ctx context.Context, req *pb.DiscoverAddressesRequest) (
	*pb.DiscoverAddressesResponse, error) {

	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}

	s.mu.Lock()
	chainClient := s.rpcClient
	s.mu.Unlock()
	if chainClient == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Consensus server RPC client has not been loaded")
	}

	if req.DiscoverAccounts && len(req.PrivatePassphrase) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "private passphrase is required for discovering accounts")
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
	n := chain.BackendFromRPCClient(chainClient.Client)
	startHash := wallet.ChainParams().GenesisHash
	var err error
	if req.StartingBlockHash != nil {
		startHash, err = chainhash.NewHash(req.StartingBlockHash)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Invalid starting block hash provided: %v", err)
		}
	}
	err = wallet.DiscoverActiveAddresses(ctx, n, startHash, req.DiscoverAccounts)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.DiscoverAddressesResponse{}, nil
}

func (s *loaderServer) FetchMissingCFilters(ctx context.Context, req *pb.FetchMissingCFiltersRequest) (
	*pb.FetchMissingCFiltersResponse, error) {

	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}

	n := chain.BackendFromRPCClient(s.rpcClient.Client)
	// Fetch any missing main chain compact filters.
	err := wallet.FetchMissingCFilters(ctx, n)
	if err != nil {
		return nil, err
	}

	return &pb.FetchMissingCFiltersResponse{}, nil
}

func (s *loaderServer) SpvSync(req *pb.SpvSyncRequest, svr pb.WalletLoaderService_SpvSyncServer) error {
	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}

	if req.DiscoverAccounts && len(req.PrivatePassphrase) == 0 {
		return status.Errorf(codes.InvalidArgument, "private passphrase is required for discovering accounts")
	}
	var lockWallet func()
	if req.DiscoverAccounts {
		lock := make(chan time.Time, 1)
		lockWallet = func() {
			lock <- time.Time{}
			zero.Bytes(req.PrivatePassphrase)
		}
		err := wallet.Unlock(req.PrivatePassphrase, lock)
		if err != nil {
			return translateError(err)
		}
	}
	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	amgr := addrmgr.New(s.loader.DbDirPath(), net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(wallet.ChainParams(), addr, amgr)

	ntfns := &spv.Notifications{
		Synced: func(sync bool) {
			resp := &pb.SpvSyncResponse{Synced: sync}

			// Lock the wallet after the first time synced while also
			// discovering accounts.
			if sync && lockWallet != nil {
				lockWallet()
				lockWallet = nil
			}

			// TODO: Add some kind of logging here.  Do nothing with the error
			// for now. Could be nice to see what happened, but not super
			// important.
			_ = svr.Send(resp)
		},
	}
	syncer := spv.NewSyncer(wallet, lp)
	syncer.SetNotifications(ntfns)
	if len(req.SpvConnect) > 0 {
		spvConnects := make([]string, len(req.SpvConnect))
		for i := 0; i < len(req.SpvConnect); i++ {
			spvConnect, err := cfgutil.NormalizeAddress(req.SpvConnect[i], s.activeNet.Params.DefaultPort)
			if err != nil {
				return status.Errorf(codes.FailedPrecondition, "SPV Connect address invalid: %v", err)
			}
			spvConnects[i] = spvConnect
		}
		syncer.SetPersistantPeers(spvConnects)
	}

	wallet.SetNetworkBackend(syncer)
	s.loader.SetNetworkBackend(syncer)

	err := syncer.Run(svr.Context())
	if err != nil {
		if err == context.Canceled {
			return status.Errorf(codes.Canceled, "SPV synchronization canceled: %v", err)
		} else if err == context.DeadlineExceeded {
			return status.Errorf(codes.DeadlineExceeded, "SPV synchronization deadline exceeded: %v", err)
		}
		return translateError(err)
	}
	return nil
}

func (s *loaderServer) RescanPoint(ctx context.Context, req *pb.RescanPointRequest) (*pb.RescanPointResponse, error) {
	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}
	rescanPoint, err := wallet.RescanPoint()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Rescan point failed to be requested %v", err)
	}
	if rescanPoint != nil {
		return &pb.RescanPointResponse{
			RescanPointHash: rescanPoint[:],
		}, nil
	}
	return &pb.RescanPointResponse{RescanPointHash: nil}, nil
}

func (s *loaderServer) SubscribeToBlockNotifications(ctx context.Context, req *pb.SubscribeToBlockNotificationsRequest) (
	*pb.SubscribeToBlockNotificationsResponse, error) {

	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}

	s.mu.Lock()
	chainClient := s.rpcClient
	s.mu.Unlock()
	if chainClient == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Consensus server RPC client has not been loaded")
	}

	err := chainClient.NotifyBlocks()
	if err != nil {
		return nil, translateError(err)
	}

	// TODO: instead of running the syncer in the background indefinitely,
	// deprecate this RPC and introduce two new RPCs, one to subscribe to the
	// notifications and one to perform the synchronization task.  This would be
	// a backwards-compatible way to improve error handling and provide more
	// control over how long the synchronization task runs.
	syncer := chain.NewRPCSyncer(wallet, chainClient)
	go syncer.Run(context.Background(), false)
	wallet.SetNetworkBackend(chain.BackendFromRPCClient(chainClient.Client))

	return &pb.SubscribeToBlockNotificationsResponse{}, nil
}

func (s *loaderServer) FetchHeaders(ctx context.Context, req *pb.FetchHeadersRequest) (
	*pb.FetchHeadersResponse, error) {

	wallet, ok := s.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}

	s.mu.Lock()
	chainClient := s.rpcClient
	s.mu.Unlock()
	if chainClient == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Consensus server RPC client has not been loaded")
	}
	n := chain.BackendFromRPCClient(chainClient.Client)

	fetchedHeaderCount, rescanFrom, rescanFromHeight,
		mainChainTipBlockHash, mainChainTipBlockHeight, err := wallet.FetchHeaders(ctx, n)
	if err != nil {
		return nil, translateError(err)
	}

	res := &pb.FetchHeadersResponse{
		FetchedHeadersCount:     uint32(fetchedHeaderCount),
		MainChainTipBlockHash:   mainChainTipBlockHash[:],
		MainChainTipBlockHeight: mainChainTipBlockHeight,
	}
	if fetchedHeaderCount > 0 {
		res.FirstNewBlockHash = rescanFrom[:]
		res.FirstNewBlockHeight = rescanFromHeight
	}
	return res, nil
}

func (s *seedServer) GenerateRandomSeed(ctx context.Context, req *pb.GenerateRandomSeedRequest) (
	*pb.GenerateRandomSeedResponse, error) {

	seedSize := req.SeedLength
	if seedSize == 0 {
		seedSize = hdkeychain.RecommendedSeedLen
	}
	if seedSize < hdkeychain.MinSeedBytes || seedSize > hdkeychain.MaxSeedBytes {
		return nil, status.Errorf(codes.InvalidArgument, "invalid seed length")
	}

	seed := make([]byte, seedSize)
	_, err := rand.Read(seed)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to read cryptographically-random data for seed: %v", err)
	}

	res := &pb.GenerateRandomSeedResponse{
		SeedBytes:    seed,
		SeedHex:      hex.EncodeToString(seed),
		SeedMnemonic: walletseed.EncodeMnemonic(seed),
	}
	return res, nil
}

func (s *seedServer) DecodeSeed(ctx context.Context, req *pb.DecodeSeedRequest) (*pb.DecodeSeedResponse, error) {
	seed, err := walletseed.DecodeUserInput(req.UserInput)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return &pb.DecodeSeedResponse{DecodedSeed: seed}, nil
}

// requirePurchaseManager checks whether the ticket buyer is running, returning
// a gRPC error when it is not.
func (t *ticketbuyerServer) requirePurchaseManager() (*ticketbuyer.PurchaseManager, error) {
	pm := t.loader.PurchaseManager()
	if pm == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "ticket buyer is not running")
	}
	return pm, nil
}

// TicketBuyerConfig returns the configuration of the ticket buyer.
func (t *ticketbuyerServer) TicketBuyerConfig(ctx context.Context, req *pb.TicketBuyerConfigRequest) (
	*pb.TicketBuyerConfigResponse, error) {
	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	config, err := pm.Purchaser().Config()
	if err != nil {
		return nil, err
	}
	w, ok := t.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "wallet has not been loaded")
	}
	account, err := w.AccountNumber(config.AccountName)
	if err != nil {
		return nil, translateError(err)
	}
	votingAddress := ""
	if config.VotingAddress != nil {
		votingAddress = config.VotingAddress.String()
	}
	poolAddress := ""
	if config.PoolAddress != nil {
		poolAddress = config.PoolAddress.String()
	}
	return &pb.TicketBuyerConfigResponse{
		Account:               account,
		AvgPriceMode:          config.AvgPriceMode,
		AvgPriceVWAPDelta:     int64(config.AvgPriceVWAPDelta),
		BalanceToMaintain:     config.BalanceToMaintainAbsolute,
		BlocksToAvg:           int64(config.BlocksToAvg),
		DontWaitForTickets:    config.DontWaitForTickets,
		ExpiryDelta:           int64(config.ExpiryDelta),
		FeeSource:             config.FeeSource,
		FeeTargetScaling:      config.FeeTargetScaling,
		MinFee:                config.MinFee,
		MaxFee:                config.MaxFee,
		MaxPerBlock:           int64(config.MaxPerBlock),
		MaxPriceAbsolute:      config.MaxPriceAbsolute,
		MaxPriceRelative:      config.MaxPriceRelative,
		MaxInMempool:          int64(config.MaxInMempool),
		PoolAddress:           poolAddress,
		PoolFees:              config.PoolFees,
		SpreadTicketPurchases: !config.NoSpreadTicketPurchases,
		VotingAddress:         votingAddress,
		TxFee:                 config.TxFee,
	}, nil
}

// SetAccount sets the account to use for purchasing tickets.
func (t *ticketbuyerServer) SetAccount(ctx context.Context, req *pb.SetAccountRequest) (
	*pb.SetAccountResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}

	wallet, ok := t.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}

	_, err = wallet.AccountName(req.Account)
	if err != nil {
		return nil, translateError(err)
	}

	pm.Purchaser().SetAccount(req.Account)
	return &pb.SetAccountResponse{}, nil
}

// SetBalanceToMaintain sets the balance to be maintained in the wallet.
func (t *ticketbuyerServer) SetBalanceToMaintain(ctx context.Context, req *pb.SetBalanceToMaintainRequest) (
	*pb.SetBalanceToMaintainResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	if req.BalanceToMaintain < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Negative balance to maintain given")
	}
	pm.Purchaser().SetBalanceToMaintain(req.BalanceToMaintain)
	return &pb.SetBalanceToMaintainResponse{}, nil
}

// SetMaxFee sets the max ticket fee per KB to use when purchasing tickets.
func (t *ticketbuyerServer) SetMaxFee(ctx context.Context, req *pb.SetMaxFeeRequest) (
	*pb.SetMaxFeeResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	if req.MaxFeePerKb < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Negative max fee per KB given")
	}
	pm.Purchaser().SetMaxFee(req.MaxFeePerKb)
	return &pb.SetMaxFeeResponse{}, nil
}

// SetMaxPriceRelative sets max price scaling factor.
func (t *ticketbuyerServer) SetMaxPriceRelative(ctx context.Context, req *pb.SetMaxPriceRelativeRequest) (
	*pb.SetMaxPriceRelativeResponse, error) {

	if req.MaxPriceRelative < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Negative max ticket price given")
	}

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	pm.Purchaser().SetMaxPriceRelative(req.MaxPriceRelative)
	return &pb.SetMaxPriceRelativeResponse{}, nil
}

// SetMaxPriceAbsolute sets the max absolute price to purchase a ticket.
func (t *ticketbuyerServer) SetMaxPriceAbsolute(ctx context.Context, req *pb.SetMaxPriceAbsoluteRequest) (
	*pb.SetMaxPriceAbsoluteResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	if req.MaxPriceAbsolute < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Negative max ticket price given")
	}
	pm.Purchaser().SetMaxPriceAbsolute(req.MaxPriceAbsolute)
	return &pb.SetMaxPriceAbsoluteResponse{}, nil
}

// SetVotingAddress sets the address to send ticket outputs to.
func (t *ticketbuyerServer) SetVotingAddress(ctx context.Context, req *pb.SetVotingAddressRequest) (
	*pb.SetVotingAddressResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	w, ok := t.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "wallet has not been loaded")
	}
	votingAddress, err := decodeAddress(req.VotingAddress, w.ChainParams())
	if err != nil {
		return nil, err
	}
	pm.Purchaser().SetVotingAddress(votingAddress)
	return &pb.SetVotingAddressResponse{}, nil
}

// SetPoolAddress sets the pool address where ticket fees are sent.
func (t *ticketbuyerServer) SetPoolAddress(ctx context.Context, req *pb.SetPoolAddressRequest) (
	*pb.SetPoolAddressResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	w, ok := t.loader.LoadedWallet()
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "wallet has not been loaded")
	}

	poolAddress := req.PoolAddress
	poolFees := pm.Purchaser().PoolFees()

	switch {
	case poolFees == 0 && poolAddress != "":
		return nil, status.Errorf(codes.InvalidArgument, "Pool address set but no pool fees given")
	case poolFees != 0 && poolAddress == "":
		return nil, status.Errorf(codes.InvalidArgument, "Pool fees set but no pool address given")
	}

	poolAddr, err := decodeAddress(poolAddress, w.ChainParams())
	if err != nil {
		return nil, err
	}
	pm.Purchaser().SetPoolAddress(poolAddr)
	return &pb.SetPoolAddressResponse{}, nil
}

// SetPoolFees sets the percent of ticket per ticket fee mandated by the pool.
func (t *ticketbuyerServer) SetPoolFees(ctx context.Context, req *pb.SetPoolFeesRequest) (
	*pb.SetPoolFeesResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}

	var poolAddress string
	poolAddr := pm.Purchaser().PoolAddress()
	if poolAddr != nil {
		poolAddress = poolAddr.String()
	}

	poolFees := req.PoolFees
	switch {
	case poolFees == 0 && poolAddress != "":
		return nil, status.Errorf(codes.InvalidArgument, "Pool address set but no pool fees given")
	case poolFees != 0 && poolAddress == "":
		return nil, status.Errorf(codes.InvalidArgument, "Pool fees set but no pool address given")
	case poolFees != 0 && poolAddress != "":
		if !txrules.ValidPoolFeeRate(req.PoolFees) {
			return nil, status.Errorf(codes.InvalidArgument, "Invalid pool fees percentage")
		}
	}

	pm.Purchaser().SetPoolFees(req.PoolFees)
	return &pb.SetPoolFeesResponse{}, nil
}

// SetMaxPerBlock sets the max tickets to purchase for a block.
func (t *ticketbuyerServer) SetMaxPerBlock(ctx context.Context, req *pb.SetMaxPerBlockRequest) (
	*pb.SetMaxPerBlockResponse, error) {

	pm, err := t.requirePurchaseManager()
	if err != nil {
		return nil, err
	}
	pm.Purchaser().SetMaxPerBlock(int(req.MaxPerBlock))
	return &pb.SetMaxPerBlockResponse{}, nil
}

// StartAgendaService starts the AgendaService.
func StartAgendaService(server *grpc.Server, activeNet *chaincfg.Params) {
	agendaService.activeNet = activeNet
	if atomic.SwapUint32(&agendaService.ready, 1) != 0 {
		panic("service already started")
	}
}

func (s *agendaServer) checkReady() bool {
	return atomic.LoadUint32(&s.ready) != 0
}

func (s *agendaServer) Agendas(ctx context.Context, req *pb.AgendasRequest) (*pb.AgendasResponse, error) {
	version, deployments := wallet.CurrentAgendas(s.activeNet)
	resp := &pb.AgendasResponse{
		Version: version,
		Agendas: make([]*pb.AgendasResponse_Agenda, len(deployments)),
	}
	for i := range deployments {
		d := &deployments[i]
		resp.Agendas[i] = &pb.AgendasResponse_Agenda{
			Id:          d.Vote.Id,
			Description: d.Vote.Description,
			Mask:        uint32(d.Vote.Mask),
			Choices:     make([]*pb.AgendasResponse_Choice, len(d.Vote.Choices)),
			StartTime:   int64(d.StartTime),
			ExpireTime:  int64(d.ExpireTime),
		}
		for j := range d.Vote.Choices {
			choice := &d.Vote.Choices[j]
			resp.Agendas[i].Choices[j] = &pb.AgendasResponse_Choice{
				Id:          choice.Id,
				Description: choice.Description,
				Bits:        uint32(choice.Bits),
				IsAbstain:   choice.IsAbstain,
				IsNo:        choice.IsNo,
			}
		}
	}
	return resp, nil
}

// StartVotingService starts the VotingService.
func StartVotingService(server *grpc.Server, wallet *wallet.Wallet) {
	votingService.wallet = wallet
	if atomic.SwapUint32(&votingService.ready, 1) != 0 {
		panic("service already started")
	}
}

func (s *votingServer) checkReady() bool {
	return atomic.LoadUint32(&s.ready) != 0
}

func (s *votingServer) VoteChoices(ctx context.Context, req *pb.VoteChoicesRequest) (*pb.VoteChoicesResponse, error) {
	version, agendas := wallet.CurrentAgendas(s.wallet.ChainParams())
	choices, voteBits, err := s.wallet.AgendaChoices()
	if err != nil {
		return nil, translateError(err)
	}
	resp := &pb.VoteChoicesResponse{
		Version:  version,
		Choices:  make([]*pb.VoteChoicesResponse_Choice, len(agendas)),
		Votebits: uint32(voteBits),
	}

	for i := range choices {
		resp.Choices[i] = &pb.VoteChoicesResponse_Choice{
			AgendaId:          choices[i].AgendaID,
			AgendaDescription: agendas[i].Vote.Description,
			ChoiceId:          choices[i].ChoiceID,
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

func (s *votingServer) SetVoteChoices(ctx context.Context, req *pb.SetVoteChoicesRequest) (*pb.SetVoteChoicesResponse, error) {
	choices := make([]wallet.AgendaChoice, len(req.Choices))
	for i, c := range req.Choices {
		choices[i] = wallet.AgendaChoice{
			AgendaID: c.AgendaId,
			ChoiceID: c.ChoiceId,
		}
	}
	voteBits, err := s.wallet.SetAgendaChoices(choices...)
	if err != nil {
		return nil, translateError(err)
	}
	resp := &pb.SetVoteChoicesResponse{
		Votebits: uint32(voteBits),
	}
	return resp, nil
}

func (s *messageVerificationServer) VerifyMessage(ctx context.Context, req *pb.VerifyMessageRequest) (
	*pb.VerifyMessageResponse, error) {

	var valid bool

	addr, err := dcrutil.DecodeAddress(req.Address)
	if err != nil {
		return nil, translateError(err)
	}

	// Addresses must have an associated secp256k1 private key and therefore
	// must be P2PK or P2PKH (P2SH is not allowed).
	switch a := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
	case *dcrutil.AddressPubKeyHash:
		if a.DSA(a.Net()) != dcrec.STEcdsaSecp256k1 {
			goto WrongAddrKind
		}
	default:
		goto WrongAddrKind
	}

	valid, err = wallet.VerifyMessage(req.Message, addr, req.Signature)
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.VerifyMessageResponse{Valid: valid}, nil

WrongAddrKind:
	return nil, status.Error(codes.InvalidArgument, "address must be secp256k1 P2PK or P2PKH")
}

// StartDecodeMessageService starts the MessageDecode service
func StartDecodeMessageService(server *grpc.Server, chainParams *chaincfg.Params) {
	decodeMessageService.chainParams = chainParams
}

func marshalDecodedTxInputs(mtx *wire.MsgTx) []*pb.DecodedTransaction_Input {
	inputs := make([]*pb.DecodedTransaction_Input, len(mtx.TxIn))

	for i, txIn := range mtx.TxIn {
		// The disassembled string will contain [error] inline
		// if the script doesn't fully parse, so ignore the
		// error here.
		disbuf, _ := txscript.DisasmString(txIn.SignatureScript)

		inputs[i] = &pb.DecodedTransaction_Input{
			PreviousTransactionHash:  txIn.PreviousOutPoint.Hash[:],
			PreviousTransactionIndex: txIn.PreviousOutPoint.Index,
			Tree:               pb.DecodedTransaction_Input_TreeType(txIn.PreviousOutPoint.Tree),
			Sequence:           txIn.Sequence,
			AmountIn:           txIn.ValueIn,
			BlockHeight:        txIn.BlockHeight,
			BlockIndex:         txIn.BlockIndex,
			SignatureScript:    txIn.SignatureScript,
			SignatureScriptAsm: disbuf,
		}
	}

	return inputs
}

func marshalDecodedTxOutputs(mtx *wire.MsgTx, chainParams *chaincfg.Params) []*pb.DecodedTransaction_Output {
	outputs := make([]*pb.DecodedTransaction_Output, len(mtx.TxOut))
	txType := stake.DetermineTxType(mtx)

	for i, v := range mtx.TxOut {
		// The disassembled string will contain [error] inline if the
		// script doesn't fully parse, so ignore the error here.
		disbuf, _ := txscript.DisasmString(v.PkScript)

		// Attempt to extract addresses from the public key script.  In
		// the case of stake submission transactions, the odd outputs
		// contain a commitment address, so detect that case
		// accordingly.
		var addrs []dcrutil.Address
		var encodedAddrs []string
		var scriptClass txscript.ScriptClass
		var reqSigs int
		var commitAmt *dcrutil.Amount
		if (txType == stake.TxTypeSStx) && (stake.IsStakeSubmissionTxOut(i)) {
			scriptClass = txscript.StakeSubmissionTy
			addr, err := stake.AddrFromSStxPkScrCommitment(v.PkScript,
				chainParams)
			if err != nil {
				encodedAddrs = []string{fmt.Sprintf(
					"[error] failed to decode ticket "+
						"commitment addr output for tx hash "+
						"%v, output idx %v", mtx.TxHash(), i)}
			} else {
				encodedAddrs = []string{addr.EncodeAddress()}
			}
			amt, err := stake.AmountFromSStxPkScrCommitment(v.PkScript)
			if err != nil {
				commitAmt = &amt
			}
		} else {
			// Ignore the error here since an error means the script
			// couldn't parse and there is no additional information
			// about it anyways.
			scriptClass, addrs, reqSigs, _ = txscript.ExtractPkScriptAddrs(
				v.Version, v.PkScript, chainParams)
			encodedAddrs = make([]string, len(addrs))
			for j, addr := range addrs {
				encodedAddrs[j] = addr.EncodeAddress()
			}
		}

		outputs[i] = &pb.DecodedTransaction_Output{
			Index:              uint32(i),
			Value:              v.Value,
			Version:            int32(v.Version),
			Addresses:          encodedAddrs,
			Script:             v.PkScript,
			ScriptAsm:          disbuf,
			ScriptClass:        pb.DecodedTransaction_Output_ScriptClass(scriptClass),
			RequiredSignatures: int32(reqSigs),
		}
		if commitAmt != nil {
			outputs[i].CommitmentAmount = int64(*commitAmt)
		}
	}

	return outputs
}

func (s *decodeMessageServer) DecodeRawTransaction(ctx context.Context, req *pb.DecodeRawTransactionRequest) (
	*pb.DecodeRawTransactionResponse, error) {

	serializedTx := req.SerializedTransaction

	var mtx wire.MsgTx
	err := mtx.Deserialize(bytes.NewReader(serializedTx))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Could not decode Tx: %v",
			err)
	}

	txHash := mtx.TxHash()
	resp := &pb.DecodeRawTransactionResponse{
		Transaction: &pb.DecodedTransaction{
			TransactionHash: txHash[:],
			TransactionType: marshalTxType(wallet.TxTransactionType(&mtx)),
			Version:         int32(mtx.Version),
			LockTime:        mtx.LockTime,
			Expiry:          mtx.Expiry,
			Inputs:          marshalDecodedTxInputs(&mtx),
			Outputs:         marshalDecodedTxOutputs(&mtx, s.chainParams),
		},
	}

	return resp, nil
}

func (s *walletServer) BestBlock(ctx context.Context, req *pb.BestBlockRequest) (*pb.BestBlockResponse, error) {
	hash, height := s.wallet.MainChainTip()
	resp := &pb.BestBlockResponse{
		Hash:   hash[:],
		Height: uint32(height),
	}
	return resp, nil
}
