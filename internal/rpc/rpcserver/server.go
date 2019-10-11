// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016-2019 The Decred developers
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
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"decred.org/dcrwallet/internal/cfgutil"
	"decred.org/dcrwallet/internal/loader"
	"decred.org/dcrwallet/internal/netparams"
	"github.com/decred/dcrd/addrmgr"
	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/chain/v3"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/p2p/v2"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	pb "github.com/decred/dcrwallet/rpc/walletrpc"
	"github.com/decred/dcrwallet/spv/v3"
	"github.com/decred/dcrwallet/ticketbuyer/v4"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/txauthor"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/walletseed"
)

// Public API version constants
const (
	semverString = "7.2.0"
	semverMajor  = 7
	semverMinor  = 2
	semverPatch  = 0
)

// translateError creates a new gRPC error with an appropriate error code for
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
	var kind errors.Kind
	if errors.As(err, &kind) {
		switch kind {
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
		}
	}
	if errors.Is(err, hdkeychain.ErrInvalidSeedLen) {
		return codes.InvalidArgument
	}
	return codes.Unknown
}

// decodeAddress decodes an address and verifies it is intended for the active
// network.  This should be used preferred to direct usage of
// dcrutil.DecodeAddress, which does not perform the network check.
func decodeAddress(a string, params *chaincfg.Params) (dcrutil.Address, error) {
	addr, err := dcrutil.DecodeAddress(a, params)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid address %v: %v", a, err)
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
}

// seedServer provides RPC clients with the ability to generate secure random
// seeds encoded in both binary and human-readable formats, and decode any
// human-readable input back to binary.
type seedServer struct{}

// ticketbuyerServer provides RPC clients with the ability to start/stop the
// automatic ticket buyer service.
type ticketbuyerV2Server struct {
	ready  uint32 // atomic
	loader *loader.Loader
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
type messageVerificationServer struct {
	chainParams *chaincfg.Params
}

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
	ticketBuyerV2Service       ticketbuyerV2Server
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
	pb.RegisterTicketBuyerV2ServiceServer(server, &ticketBuyerV2Service)
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
	"walletrpc.TicketBuyerV2Service":       &ticketBuyerV2Service,
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

func (s *walletServer) CoinType(ctx context.Context, req *pb.CoinTypeRequest) (*pb.CoinTypeResponse, error) {
	coinType, err := s.wallet.CoinType(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	return &pb.CoinTypeResponse{CoinType: coinType}, nil
}

func (s *walletServer) AccountNumber(ctx context.Context, req *pb.AccountNumberRequest) (
	*pb.AccountNumberResponse, error) {

	accountNum, err := s.wallet.AccountNumber(ctx, req.AccountName)
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.AccountNumberResponse{AccountNumber: accountNum}, nil
}

func (s *walletServer) Accounts(ctx context.Context, req *pb.AccountsRequest) (*pb.AccountsResponse, error) {
	resp, err := s.wallet.Accounts(ctx)
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

	err := s.wallet.RenameAccount(ctx, req.AccountNumber, req.NewName)
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

	b, err := s.wallet.BlockInfo(svr.Context(), blockID)
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

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (s *walletServer) NextAccount(ctx context.Context, req *pb.NextAccountRequest) (
	*pb.NextAccountResponse, error) {

	defer zero(req.Passphrase)

	if req.AccountName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "account name may not be empty")
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(ctx, req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	account, err := s.wallet.NextAccount(ctx, req.AccountName)
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
		addr, err = s.wallet.NewExternalAddress(ctx, req.Account, callOpts...)
		if err != nil {
			return nil, translateError(err)
		}
	case pb.NextAddressRequest_BIP0044_INTERNAL:
		addr, err = s.wallet.NewInternalAddress(ctx, req.Account, callOpts...)
		if err != nil {
			return nil, translateError(err)
		}
	default:
		return nil, status.Errorf(codes.InvalidArgument, "kind=%v", req.Kind)
	}
	if err != nil {
		return nil, translateError(err)
	}

	var pubKeyAddrString string
	if secp, ok := addr.(wallet.SecpPubKeyer); ok {
		pubKey := secp.SecpPubKey()
		pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(pubKey, s.wallet.ChainParams())
		if err != nil {
			return nil, translateError(err)
		}
		pubKeyAddrString = pubKeyAddr.String()
	}

	return &pb.NextAddressResponse{
		Address:   addr.Address(),
		PublicKey: pubKeyAddrString,
	}, nil
}

func (s *walletServer) ImportPrivateKey(ctx context.Context, req *pb.ImportPrivateKeyRequest) (
	*pb.ImportPrivateKeyResponse, error) {

	defer zero(req.Passphrase)

	wif, err := dcrutil.DecodeWIF(req.PrivateKeyWif, s.wallet.ChainParams().PrivateKeyID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"Invalid WIF-encoded private key: %v", err)
	}

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err = s.wallet.Unlock(ctx, req.Passphrase, lock)
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

	_, err = s.wallet.ImportPrivateKey(ctx, wif)
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

	defer zero(req.Passphrase)

	// TODO: Rather than assuming the "default" version, it must be a parameter
	// to the request.
	sc, addrs, requiredSigs, err := txscript.ExtractPkScriptAddrs(
		0, req.Script, s.wallet.ChainParams())
	if err != nil && req.RequireRedeemable {
		return nil, status.Errorf(codes.FailedPrecondition,
			"The script is not redeemable by the wallet")
	}
	ownAddrs := 0
	for _, a := range addrs {
		haveAddr, err := s.wallet.HaveAddress(ctx, a)
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

	if !s.wallet.Manager.WatchingOnly() {
		lock := make(chan time.Time, 1)
		defer func() {
			lock <- time.Time{} // send matters, not the value
		}()
		err = s.wallet.Unlock(ctx, req.Passphrase, lock)
		if err != nil {
			return nil, translateError(err)
		}
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

	err = s.wallet.ImportScript(ctx, req.Script)
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
	bals, err := s.wallet.CalculateAccountBalance(ctx, account, reqConfs)
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
	sdiff, err := s.wallet.NextStakeDifficulty(ctx)
	if err != nil {
		return nil, translateError(err)
	}
	_, tipHeight := s.wallet.MainChainTip(ctx)
	resp := &pb.TicketPriceResponse{
		TicketPrice: int64(sdiff),
		Height:      tipHeight,
	}
	return resp, nil
}

func (s *walletServer) StakeInfo(ctx context.Context, req *pb.StakeInfoRequest) (*pb.StakeInfoResponse, error) {
	var rpc *dcrd.RPC
	n, _ := s.wallet.NetworkBackend()
	if client, ok := n.(*dcrd.RPC); ok {
		rpc = client
	}
	var si *wallet.StakeInfoData
	var err error
	if rpc != nil {
		si, err = s.wallet.StakeInfoPrecise(ctx, rpc)
	} else {
		si, err = s.wallet.StakeInfo(ctx)
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
		Unspent:       si.Unspent,
	}, nil
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

func (s *walletServer) SweepAccount(ctx context.Context, req *pb.SweepAccountRequest) (*pb.SweepAccountResponse, error) {
	feePerKb := s.wallet.RelayFee()

	// Use provided fee per Kb if specified.
	if req.FeePerKb < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "%s",
			"fee per kb argument cannot be negative")
	}

	if req.FeePerKb > 0 {
		var err error
		feePerKb, err = dcrutil.NewAmount(req.FeePerKb)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
	}

	account, err := s.wallet.AccountNumber(ctx, req.SourceAccount)
	if err != nil {
		return nil, translateError(err)
	}

	changeSource, err := makeScriptChangeSource(req.DestinationAddress, 0, s.wallet.ChainParams())
	if err != nil {
		return nil, translateError(err)
	}

	tx, err := s.wallet.NewUnsignedTransaction(ctx, nil, feePerKb, account,
		int32(req.RequiredConfirmations), wallet.OutputSelectionAlgorithmAll,
		changeSource)
	if err != nil {
		return nil, translateError(err)
	}

	var txBuf bytes.Buffer
	txBuf.Grow(tx.Tx.SerializeSize())
	err = tx.Tx.Serialize(&txBuf)
	if err != nil {
		return nil, translateError(err)
	}

	res := &pb.SweepAccountResponse{
		UnsignedTransaction:       txBuf.Bytes(),
		TotalPreviousOutputAmount: int64(tx.TotalInput),
		TotalOutputAmount:         int64(sumOutputValues(tx.Tx.TxOut)),
		EstimatedSignedSize:       uint32(tx.EstimatedSignedSerializeSize),
	}

	return res, nil
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

	b, err := s.wallet.BlockInfo(ctx, blockID)
	if err != nil {
		return nil, translateError(err)
	}

	header := new(wire.BlockHeader)
	err = header.Deserialize(bytes.NewReader(b.Header))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to deserialize saved block header: %v", err)
	}

	return &pb.BlockInfoResponse{
		BlockHash:        b.Hash[:],
		BlockHeight:      b.Height,
		Confirmations:    b.Confirmations,
		Timestamp:        b.Timestamp,
		BlockHeader:      b.Header,
		StakeInvalidated: b.StakeInvalidated,
		ApprovesParent:   header.VoteBits&dcrutil.BlockValid != 0,
	}, nil
}

func (s *walletServer) UnspentOutputs(req *pb.UnspentOutputsRequest, svr pb.WalletService_UnspentOutputsServer) error {
	policy := wallet.OutputSelectionPolicy{
		Account:               req.Account,
		RequiredConfirmations: req.RequiredConfirmations,
	}
	inputDetail, err := s.wallet.SelectInputs(svr.Context(), dcrutil.Amount(req.TargetAmount), policy)
	// Do not return errors to caller when there was insufficient spendable
	// outputs available for the target amount.
	if err != nil && !errors.Is(err, errors.InsufficientBalance) {
		return translateError(err)
	}

	var sum int64
	for i, input := range inputDetail.Inputs {
		select {
		case <-svr.Context().Done():
			return status.Errorf(codes.Canceled, "unspentoutputs cancelled")
		default:
			outputInfo, err := s.wallet.OutputInfo(svr.Context(), &input.PreviousOutPoint)
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
	inputDetail, err := s.wallet.SelectInputs(ctx, dcrutil.Amount(req.TargetAmount), policy)
	// Do not return errors to caller when there was insufficient spendable
	// outputs available for the target amount.
	if err != nil && !errors.Is(err, errors.InsufficientBalance) {
		return nil, translateError(err)
	}

	selectedOutputs := make([]*pb.FundTransactionResponse_PreviousOutput, len(inputDetail.Inputs))
	for i, input := range inputDetail.Inputs {
		outputInfo, err := s.wallet.OutputInfo(ctx, &input.PreviousOutPoint)
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
		changeAddr, err := s.wallet.NewChangeAddress(ctx, req.Account)
		if err != nil {
			return nil, translateError(err)
		}
		changeScript, _, err = addressScript(changeAddr)
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
		pkScript, version, err = addressScript(addr)
		if err != nil {
			return nil, 0, translateError(err)
		}
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

	tx, err := s.wallet.NewUnsignedTransaction(ctx, outputs, feePerKb, req.SourceAccount,
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
		TotalOutputAmount:         int64(sumOutputValues(tx.Tx.TxOut)),
		EstimatedSignedSize:       uint32(tx.EstimatedSignedSerializeSize),
		ChangeIndex:               int32(tx.ChangeIndex),
	}
	return res, nil
}

func (s *walletServer) GetAccountExtendedPubKey(ctx context.Context, req *pb.GetAccountExtendedPubKeyRequest) (*pb.GetAccountExtendedPubKeyResponse, error) {
	accExtendedPubKey, err := s.wallet.MasterPubKey(ctx, req.AccountNumber)
	if err != nil {
		return nil, err
	}
	res := &pb.GetAccountExtendedPubKeyResponse{
		AccExtendedPubKey: accExtendedPubKey.String(),
	}
	return res, nil
}

func (s *walletServer) GetAccountExtendedPrivKey(ctx context.Context, req *pb.GetAccountExtendedPrivKeyRequest) (*pb.GetAccountExtendedPrivKeyResponse, error) {
	lock := make(chan time.Time, 1)
	lockWallet := func() {
		lock <- time.Time{}
		zero(req.Passphrase)
	}

	err := s.wallet.Unlock(ctx, req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}
	defer lockWallet()

	accExtendedPrivKey, err := s.wallet.MasterPrivKey(ctx, req.AccountNumber)
	if err != nil {
		return nil, translateError(err)
	}
	res := &pb.GetAccountExtendedPrivKeyResponse{
		AccExtendedPrivKey: accExtendedPrivKey.String(),
	}
	return res, nil
}

func (s *walletServer) GetTransaction(ctx context.Context, req *pb.GetTransactionRequest) (*pb.GetTransactionResponse, error) {
	txHash, err := chainhash.NewHash(req.TransactionHash)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "transaction_hash has invalid length")
	}

	txSummary, confs, blockHash, err := s.wallet.TransactionSummary(ctx, txHash)
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

	err := s.wallet.GetTransactions(ctx, rangeFn, startBlock, endBlock)
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (s *walletServer) GetTicket(ctx context.Context, req *pb.GetTicketRequest) (*pb.GetTicketsResponse, error) {
	ticketHash, err := chainhash.NewHash(req.TicketHash)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	// The dcrd client could be nil here if the network backend is not
	// the consensus rpc client.  This is fine since the chain client is
	// optional.
	n, _ := s.wallet.NetworkBackend()
	rpc, _ := n.(*dcrd.RPC)

	var ticketSummary *wallet.TicketSummary
	var blockHeader *wire.BlockHeader
	if rpc == nil {
		ticketSummary, blockHeader, err = s.wallet.GetTicketInfo(ctx, ticketHash)
	} else {
		ticketSummary, blockHeader, err =
			s.wallet.GetTicketInfoPrecise(ctx, rpc, ticketHash)
	}
	if err != nil {
		return nil, translateError(err)
	}

	resp := &pb.GetTicketsResponse{
		Ticket: marshalTicketDetails(ticketSummary),
		Block:  marshalGetTicketBlockDetails(blockHeader),
	}
	return resp, nil
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
	n, _ := s.wallet.NetworkBackend()
	var err error
	if rpc, ok := n.(*dcrd.RPC); ok {
		err = s.wallet.GetTicketsPrecise(ctx, rpc, rangeFn, startBlock, endBlock)
	} else {
		err = s.wallet.GetTickets(ctx, rangeFn, startBlock, endBlock)
	}
	if err != nil {
		return translateError(err)
	}

	return nil
}

func (s *walletServer) ChangePassphrase(ctx context.Context, req *pb.ChangePassphraseRequest) (
	*pb.ChangePassphraseResponse, error) {

	defer func() {
		zero(req.OldPassphrase)
		zero(req.NewPassphrase)
	}()

	var (
		oldPass = req.OldPassphrase
		newPass = req.NewPassphrase
	)

	var err error
	switch req.Key {
	case pb.ChangePassphraseRequest_PRIVATE:
		err = s.wallet.ChangePrivatePassphrase(ctx, oldPass, newPass)
	case pb.ChangePassphraseRequest_PUBLIC:
		if len(oldPass) == 0 {
			oldPass = []byte(wallet.InsecurePubPassphrase)
		}
		if len(newPass) == 0 {
			newPass = []byte(wallet.InsecurePubPassphrase)
		}
		err = s.wallet.ChangePublicPassphrase(ctx, oldPass, newPass)
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

	defer zero(req.Passphrase)

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
	err = s.wallet.Unlock(ctx, req.Passphrase, lock)
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

	invalidSigs, err := s.wallet.SignTransaction(ctx, &tx, txscript.SigHashAll, additionalPkScripts, nil, nil)
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
	defer zero(req.Passphrase)

	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(ctx, req.Passphrase, lock)
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
	resp.Transactions = make([]*pb.SignTransactionsResponse_SignedTransaction, 0, len(req.Transactions))
	for _, unsignedTx := range req.Transactions {
		var tx wire.MsgTx
		err := tx.Deserialize(bytes.NewReader(unsignedTx.SerializedTransaction))
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"Bytes do not represent a valid raw transaction: %v", err)
		}

		invalidSigs, err := s.wallet.SignTransaction(ctx, &tx, txscript.SigHashAll, additionalPkScripts, nil, nil)
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

	defer zero(req.Passphrase)

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
	err = s.wallet.Unlock(ctx, req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	addr, err := decodeAddress(req.Address, s.wallet.ChainParams())
	if err != nil {
		return nil, err
	}

	hashType := txscript.SigHashType(req.HashType)
	sig, pubkey, err := s.wallet.CreateSignature(ctx, &tx, req.InputIndex, addr, hashType, req.PreviousPkScript)
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

	txHash, err := s.wallet.PublishTransaction(ctx, &msgTx, req.SignedTransaction, n)
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
	err = s.wallet.Unlock(ctx, req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	resp, err := s.wallet.PurchaseTickets(ctx, 0, spendLimit, minConf,
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
	err = s.wallet.Unlock(ctx, req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	// The wallet is not locally aware of when tickets are selected to vote and
	// when they are missed.  RevokeTickets uses trusted RPCs to determine which
	// tickets were missed.  RevokeExpiredTickets is only able to create
	// revocations for tickets which have reached their expiry time even if they
	// were missed prior to expiry, but is able to be used with other backends.
	if rpc, ok := n.(*dcrd.RPC); ok {
		err := s.wallet.RevokeTickets(ctx, rpc)
		if err != nil {
			return nil, translateError(err)
		}
	} else {
		err := s.wallet.RevokeExpiredTickets(ctx, n)
		if err != nil {
			return nil, translateError(err)
		}
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
	out, outAddr, err := s.wallet.CommittedTickets(ctx, in)
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

func (s *walletServer) signMessage(ctx context.Context, address, message string) ([]byte, error) {
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
		if a.DSA() != dcrec.STEcdsaSecp256k1 {
			goto WrongAddrKind
		}
	default:
		goto WrongAddrKind
	}

	sig, err = s.wallet.SignMessage(ctx, message, addr)
	if err != nil {
		return nil, translateError(err)
	}
	return sig, nil

WrongAddrKind:
	return nil, status.Error(codes.InvalidArgument,
		"address must be secp256k1 P2PK or P2PKH")
}

func (s *walletServer) SignMessage(ctx context.Context, req *pb.SignMessageRequest) (*pb.SignMessageResponse, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(ctx, req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	sig, err := s.signMessage(ctx, req.Address, req.Message)
	if err != nil {
		return nil, err
	}

	return &pb.SignMessageResponse{Signature: sig}, nil
}

func (s *walletServer) SignMessages(ctx context.Context, req *pb.SignMessagesRequest) (*pb.SignMessagesResponse, error) {
	lock := make(chan time.Time, 1)
	defer func() {
		lock <- time.Time{} // send matters, not the value
	}()
	err := s.wallet.Unlock(ctx, req.Passphrase, lock)
	if err != nil {
		return nil, translateError(err)
	}

	smr := pb.SignMessagesResponse{
		Replies: make([]*pb.SignMessagesResponse_SignReply, 0,
			len(req.Messages)),
	}
	for _, v := range req.Messages {
		e := ""
		sig, err := s.signMessage(ctx, v.Address, v.Message)
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
	addrInfo, err := s.wallet.AddressInfo(ctx, addr)
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
	acctName, err := s.wallet.AccountName(ctx, addrInfo.Account())
	if err != nil {
		return nil, translateError(err)
	}

	acctNumber, err := s.wallet.AccountNumber(ctx, acctName)
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
		result.PubKeyAddr = pubKeyAddr.String()
		result.IsInternal = ma.Internal()
		result.Index = ma.Index()

	case udb.ManagedScriptAddress:
		result.IsScript = true

		// The script is only available if the manager is unlocked, so
		// just break out now if there is an error.
		script, err := s.wallet.RedeemScriptCopy(ctx, addr)
		if err != nil {
			break
		}
		result.PayToAddrScript = script

		// This typically shouldn't fail unless an invalid script was
		// imported.  However, if it fails for any reason, there is no
		// further information available, so just set the script type
		// a non-standard and break out now.
		class, addrs, reqSigs, err := txscript.ExtractPkScriptAddrs(
			0, script, s.wallet.ChainParams())
		if err != nil {
			result.ScriptType = pb.ValidateAddressResponse_NonStandardTy
			break
		}

		addrStrings := make([]string, len(addrs))
		for i, a := range addrs {
			addrStrings[i] = a.Address()
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
	if v == nil {
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
		if errors.Is(err, context.Canceled) {
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

// StartTicketBuyerV2Service starts the TicketBuyerV2Service.
func StartTicketBuyerV2Service(server *grpc.Server, loader *loader.Loader) {
	ticketBuyerV2Service.loader = loader
	if atomic.SwapUint32(&ticketBuyerV2Service.ready, 1) != 0 {
		panic("service already started")
	}
}

// StartTicketBuyer starts the automatic ticket buyer for the v2 service.
func (t *ticketbuyerV2Server) RunTicketBuyer(req *pb.RunTicketBuyerRequest, svr pb.TicketBuyerV2Service_RunTicketBuyerServer) error {
	wallet, ok := t.loader.LoadedWallet()
	if !ok {
		return status.Errorf(codes.FailedPrecondition, "Wallet has not been loaded")
	}
	params := wallet.ChainParams()

	// Confirm validity of provided voting addresses and pool addresses.
	var votingAddress dcrutil.Address
	var err error
	if req.VotingAddress != "" {
		votingAddress, err = decodeAddress(req.VotingAddress, params)
		if err != nil {
			return err
		}
	}
	var poolAddress dcrutil.Address
	if req.PoolAddress != "" {
		poolAddress, err = decodeAddress(req.PoolAddress, params)
		if err != nil {
			return err
		}
	}

	if req.BalanceToMaintain < 0 {
		return status.Errorf(codes.InvalidArgument, "Negative balance to maintain given")
	}

	tb := ticketbuyer.New(wallet)

	// Set ticketbuyerV2 config
	tb.AccessConfig(func(c *ticketbuyer.Config) {
		c.Account = req.Account
		c.VotingAccount = req.VotingAccount
		c.Maintain = dcrutil.Amount(req.BalanceToMaintain)
		c.VotingAddr = votingAddress
		c.PoolFeeAddr = poolAddress
		c.PoolFees = req.PoolFees
	})

	lock := make(chan time.Time, 1)

	lockWallet := func() {
		lock <- time.Time{}
		zero(req.Passphrase)
	}

	err = wallet.Unlock(svr.Context(), req.Passphrase, lock)
	if err != nil {
		return translateError(err)
	}
	defer lockWallet()

	err = tb.Run(svr.Context(), req.Passphrase)
	if err != nil {
		if svr.Context().Err() != nil {
			return status.Errorf(codes.Canceled, "TicketBuyerV2 instance canceled, account number: %v", req.Account)
		}
		return status.Errorf(codes.Unknown, "TicketBuyerV2 instance errored: %v", err)
	}

	return nil
}

func (t *ticketbuyerV2Server) checkReady() bool {
	return atomic.LoadUint32(&t.ready) != 0
}

func (s *loaderServer) CreateWallet(ctx context.Context, req *pb.CreateWalletRequest) (
	*pb.CreateWalletResponse, error) {

	defer func() {
		zero(req.PrivatePassphrase)
		zero(req.Seed)
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

	_, err := s.loader.CreateNewWallet(ctx, pubPassphrase, req.PrivatePassphrase, req.Seed)
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

	_, err := s.loader.CreateWatchingOnlyWallet(ctx, req.ExtendedPubKey, pubPassphrase)
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

	w, err := s.loader.OpenExistingWallet(ctx, pubPassphrase)
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
	if errors.Is(err, errors.Invalid) {
		return nil, status.Errorf(codes.FailedPrecondition, "Wallet is not loaded")
	}
	if err != nil {
		return nil, translateError(err)
	}

	return &pb.CloseWalletResponse{}, nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		addr = host
	}
	if addr == "localhost" {
		return true
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func (s *loaderServer) RpcSync(req *pb.RpcSyncRequest, svr pb.WalletLoaderService_RpcSyncServer) error {
	defer zero(req.Password)

	// Error if the wallet is already syncing with the network.
	wallet, walletLoaded := s.loader.LoadedWallet()
	if walletLoaded {
		_, err := wallet.NetworkBackend()
		if err == nil {
			return status.Errorf(codes.FailedPrecondition, "wallet is loaded and already synchronizing")
		}
	}

	if req.DiscoverAccounts && len(req.PrivatePassphrase) == 0 {
		return status.Errorf(codes.InvalidArgument, "private passphrase is required for discovering accounts")
	}
	var lockWallet func()
	if req.DiscoverAccounts {
		lock := make(chan time.Time, 1)
		lockWallet = func() {
			lock <- time.Time{}
			zero(req.PrivatePassphrase)
		}
		defer lockWallet()
		err := wallet.Unlock(svr.Context(), req.PrivatePassphrase, lock)
		if err != nil {
			return translateError(err)
		}
	}

	syncer := chain.NewSyncer(wallet, &chain.RPCOptions{
		Address:     req.NetworkAddress,
		DefaultPort: s.activeNet.JSONRPCClientPort,
		User:        req.Username,
		Pass:        string(req.Password),
		CA:          req.Certificate,
		Insecure:    isLoopback(req.NetworkAddress) && len(req.Certificate) == 0,
	})

	cbs := &chain.Callbacks{
		Synced: func(sync bool) {
			resp := &pb.RpcSyncResponse{}
			resp.Synced = sync
			if sync {
				resp.NotificationType = pb.SyncNotificationType_SYNCED
			} else {
				resp.NotificationType = pb.SyncNotificationType_UNSYNCED
			}
			_ = svr.Send(resp)
		},
		FetchMissingCFiltersStarted: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_MISSING_CFILTERS_STARTED,
			}
			_ = svr.Send(resp)
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_MISSING_CFILTERS_PROGRESS,
				FetchMissingCfilters: &pb.FetchMissingCFiltersNotification{
					FetchedCfiltersStartHeight: missingCFitlersStart,
					FetchedCfiltersEndHeight:   missingCFitlersEnd,
				},
			}
			_ = svr.Send(resp)
		},
		FetchMissingCFiltersFinished: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_MISSING_CFILTERS_FINISHED,
			}
			_ = svr.Send(resp)
		},
		FetchHeadersStarted: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_HEADERS_STARTED,
			}
			_ = svr.Send(resp)
		},
		FetchHeadersProgress: func(fetchedHeadersCount int32, lastHeaderTime int64) {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_HEADERS_PROGRESS,
				FetchHeaders: &pb.FetchHeadersNotification{
					FetchedHeadersCount: fetchedHeadersCount,
					LastHeaderTime:      lastHeaderTime,
				},
			}
			_ = svr.Send(resp)
		},
		FetchHeadersFinished: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_HEADERS_FINISHED,
			}
			_ = svr.Send(resp)
		},
		DiscoverAddressesStarted: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_DISCOVER_ADDRESSES_STARTED,
			}
			_ = svr.Send(resp)
		},
		DiscoverAddressesFinished: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_DISCOVER_ADDRESSES_FINISHED,
			}

			// Lock the wallet after the first time discovered while also
			// discovering accounts.
			if lockWallet != nil {
				lockWallet()
				lockWallet = nil
			}
			_ = svr.Send(resp)
		},
		RescanStarted: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_RESCAN_STARTED,
			}
			_ = svr.Send(resp)
		},
		RescanProgress: func(rescannedThrough int32) {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_RESCAN_PROGRESS,
				RescanProgress: &pb.RescanProgressNotification{
					RescannedThrough: rescannedThrough,
				},
			}
			_ = svr.Send(resp)
		},
		RescanFinished: func() {
			resp := &pb.RpcSyncResponse{
				NotificationType: pb.SyncNotificationType_RESCAN_FINISHED,
			}
			_ = svr.Send(resp)
		},
	}
	syncer.SetCallbacks(cbs)

	// Synchronize until error or RPC cancelation.
	err := syncer.Run(svr.Context())
	if err != nil {
		if svr.Context().Err() != nil {
			return status.Errorf(codes.Canceled, "Wallet synchronization canceled: %v", err)
		}
		return status.Errorf(codes.Unknown, "Wallet synchronization stopped: %v", err)
	}

	return nil
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
			zero(req.PrivatePassphrase)
		}
		defer lockWallet()
		err := wallet.Unlock(svr.Context(), req.PrivatePassphrase, lock)
		if err != nil {
			return translateError(err)
		}
	}
	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	amgr := addrmgr.New(s.loader.DbDirPath(), net.LookupIP) // TODO: be mindful of tor
	lp := p2p.NewLocalPeer(wallet.ChainParams(), addr, amgr)

	ntfns := &spv.Notifications{
		Synced: func(sync bool) {
			resp := &pb.SpvSyncResponse{}
			resp.Synced = sync
			if sync {
				resp.NotificationType = pb.SyncNotificationType_SYNCED
			} else {
				resp.NotificationType = pb.SyncNotificationType_UNSYNCED
			}
			_ = svr.Send(resp)
		},
		PeerConnected: func(peerCount int32, addr string) {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_PEER_CONNECTED,
				PeerInformation: &pb.PeerNotification{
					PeerCount: peerCount,
					Address:   addr,
				},
			}
			_ = svr.Send(resp)
		},
		PeerDisconnected: func(peerCount int32, addr string) {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_PEER_DISCONNECTED,
				PeerInformation: &pb.PeerNotification{
					PeerCount: peerCount,
					Address:   addr,
				},
			}
			_ = svr.Send(resp)
		},
		FetchMissingCFiltersStarted: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_MISSING_CFILTERS_STARTED,
			}
			_ = svr.Send(resp)
		},
		FetchMissingCFiltersProgress: func(missingCFitlersStart, missingCFitlersEnd int32) {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_MISSING_CFILTERS_PROGRESS,
				FetchMissingCfilters: &pb.FetchMissingCFiltersNotification{
					FetchedCfiltersStartHeight: missingCFitlersStart,
					FetchedCfiltersEndHeight:   missingCFitlersEnd,
				},
			}
			_ = svr.Send(resp)
		},
		FetchMissingCFiltersFinished: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_MISSING_CFILTERS_FINISHED,
			}
			_ = svr.Send(resp)
		},
		FetchHeadersStarted: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_HEADERS_STARTED,
			}
			_ = svr.Send(resp)
		},
		FetchHeadersProgress: func(fetchedHeadersCount int32, lastHeaderTime int64) {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_HEADERS_PROGRESS,
				FetchHeaders: &pb.FetchHeadersNotification{
					FetchedHeadersCount: fetchedHeadersCount,
					LastHeaderTime:      lastHeaderTime,
				},
			}
			_ = svr.Send(resp)
		},
		FetchHeadersFinished: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_FETCHED_HEADERS_FINISHED,
			}
			_ = svr.Send(resp)
		},
		DiscoverAddressesStarted: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_DISCOVER_ADDRESSES_STARTED,
			}
			_ = svr.Send(resp)
		},
		DiscoverAddressesFinished: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_DISCOVER_ADDRESSES_FINISHED,
			}

			// Lock the wallet after the first time discovered while also
			// discovering accounts.
			if lockWallet != nil {
				lockWallet()
				lockWallet = nil
			}
			_ = svr.Send(resp)
		},
		RescanStarted: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_RESCAN_STARTED,
			}
			_ = svr.Send(resp)
		},
		RescanProgress: func(rescannedThrough int32) {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_RESCAN_PROGRESS,
				RescanProgress: &pb.RescanProgressNotification{
					RescannedThrough: rescannedThrough,
				},
			}
			_ = svr.Send(resp)
		},
		RescanFinished: func() {
			resp := &pb.SpvSyncResponse{
				NotificationType: pb.SyncNotificationType_RESCAN_FINISHED,
			}
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
		syncer.SetPersistentPeers(spvConnects)
	}

	err := syncer.Run(svr.Context())
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return status.Errorf(codes.Canceled, "SPV synchronization canceled: %v", err)
		} else if errors.Is(err, context.DeadlineExceeded) {
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
	rescanPoint, err := wallet.RescanPoint(ctx)
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
	choices, voteBits, err := s.wallet.AgendaChoices(ctx)
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
	voteBits, err := s.wallet.SetAgendaChoices(ctx, choices...)
	if err != nil {
		return nil, translateError(err)
	}
	resp := &pb.SetVoteChoicesResponse{
		Votebits: uint32(voteBits),
	}
	return resp, nil
}

// StartMessageVerificationService starts the MessageVerification service
func StartMessageVerificationService(server *grpc.Server, chainParams *chaincfg.Params) {
	messageVerificationService.chainParams = chainParams
}

func (s *messageVerificationServer) VerifyMessage(ctx context.Context, req *pb.VerifyMessageRequest) (
	*pb.VerifyMessageResponse, error) {

	var valid bool

	addr, err := dcrutil.DecodeAddress(req.Address, s.chainParams)
	if err != nil {
		return nil, translateError(err)
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

	valid, err = wallet.VerifyMessage(req.Message, addr, req.Signature, s.chainParams)
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
			Tree:                     pb.DecodedTransaction_Input_TreeType(txIn.PreviousOutPoint.Tree),
			Sequence:                 txIn.Sequence,
			AmountIn:                 txIn.ValueIn,
			BlockHeight:              txIn.BlockHeight,
			BlockIndex:               txIn.BlockIndex,
			SignatureScript:          txIn.SignatureScript,
			SignatureScriptAsm:       disbuf,
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
				encodedAddrs = []string{addr.Address()}
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
				encodedAddrs[j] = addr.Address()
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
	hash, height := s.wallet.MainChainTip(ctx)
	resp := &pb.BestBlockResponse{
		Hash:   hash[:],
		Height: uint32(height),
	}
	return resp, nil
}
