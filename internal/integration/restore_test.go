package integration

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"decred.org/dcrwallet/v4/wallet/txrules"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrtest/dcrdtest"
	"github.com/decred/dcrtest/dcrwtest"
	"matheusd.com/testctx"
)

type testHarness struct {
	testing.TB
	vw     *dcrdtest.VotingWallet
	h      *dcrdtest.Harness
	params *chaincfg.Params
}

func (t *testHarness) mine(nbBlocks int) []*chainhash.Hash {
	t.Helper()
	res, err := t.vw.GenerateBlocks(testctx.New(t), uint32(nbBlocks))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (t *testHarness) assertBlockIncludesTx(block, tx *chainhash.Hash) {
	t.Helper()
	b, err := t.h.Node.GetBlock(testctx.New(t), block)
	if err != nil {
		t.Fatal(err)
	}
	for _, txh := range b.Transactions {
		if txh.TxHash() == *tx {
			return
		}
	}
	t.Fatalf("block %s does not include tx %s", block, tx)
}

func (t *testHarness) assertCumulativeBalance(w *dcrwtest.Wallet, wantBalance dcrutil.Amount) {
	t.Helper()
	wbal, err := jrpcClient(t, w).GetBalance(testctx.New(t), "*")
	if err != nil {
		t.Fatal(err)
	}

	gotBalance, err := dcrutil.NewAmount(wbal.CumulativeTotal)
	if err != nil {
		t.Fatal(err)
	}

	if gotBalance != wantBalance {
		t.Fatalf("Unexpected balance: got %v, want %v", gotBalance, wantBalance)
	}
}

func (t *testHarness) waitWalletHeight(ctx context.Context, w *dcrwtest.Wallet, targetHeight int32) {
	for {
		info, err := jrpcClient(t, w).GetInfo(ctx)
		if err == nil && info.Blocks >= targetHeight {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatal("ctx.Done while waiting target height")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (t *testHarness) sendTo(addr string, amount dcrutil.Amount) *chainhash.Hash {
	t.Helper()
	saddr, err := stdaddr.DecodeAddress(addr, t.params)
	if err != nil {
		t.Fatal(err)
	}
	version, pkScript := saddr.PaymentScript()
	out := wire.NewTxOut(int64(amount), pkScript)
	out.Version = version
	hash, err := t.h.SendOutputs(testctx.New(t), []*wire.TxOut{out}, txrules.DefaultRelayFeePerKb)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func (t *testHarness) addrFromSeed(seed []byte, path string) stdaddr.Address {
	t.Helper()
	key, err := hdkeychain.NewMaster(seed, t.params)
	if err != nil {
		t.Fatal(err)
	}

	split := strings.Split(path, "/")
	ipath := make([]uint32, len(split))
	for i, s := range split {
		if s[len(s)-1] == '\'' {
			ipath[i] = hdkeychain.HardenedKeyStart
			s = s[:len(s)-1]
		}
		v, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		ipath[i] += uint32(v)
	}
	for i := range ipath {
		var child *hdkeychain.ExtendedKey
		for child == nil {
			child, _ = key.Child(ipath[i])
			ipath[i] += 1
		}
		key = child
	}

	addrPK, err := stdaddr.NewAddressPubKeyEcdsaSecp256k1V0Raw(key.SerializedPubKey(), t.params)
	if err != nil {
		t.Fatal(err)
	}
	addrPKH := addrPK.AddressPubKeyHash()
	return addrPKH
}

type restoreScenario struct {
	name           string
	walletAddrPath string
	syncOpt        string
	createOpt      string
	wantBalance    bool
}

func testRestore(t *testHarness, rs *restoreScenario) {
	var syncOpt, syncOptWithProxy dcrwtest.Opt
	var proxy syncerProxy
	switch rs.syncOpt {
	case "rpc":
		syncOpt = dcrwtest.WithRPCSync(t.h.RPCConfig())
		rpcProxy := newRPCSyncerProxy(t, t.h.RPCConfig())
		proxy = rpcProxy
		syncOptWithProxy = dcrwtest.WithRPCSync(rpcProxy.connConfig())
	case "spv":
		syncOpt = dcrwtest.WithSPVSync(t.h.P2PAddress())
		spvProxy := newSPVSyncerProxy(t, t.h.RPCConfig())
		proxy = spvProxy
		syncOptWithProxy = dcrwtest.WithSPVSync(spvProxy.addr())
	default:
		panic("unknown syncOpt")
	}

	var createOpt, syncMethod dcrwtest.Opt
	switch rs.createOpt {
	case "stdio":
		createOpt = dcrwtest.WithCreateUsingStdio()
		syncMethod = dcrwtest.WithSyncUsingJsonRPC()
	case "grpc":
		createOpt = dcrwtest.WithCreateUsingGRPC()
		syncMethod = dcrwtest.WithSyncUsingGRPC()
	default:
		panic("unknown createOpt")
	}

	const sendAmount dcrutil.Amount = 1e8

	// Generate a seed.
	var seed [32]byte
	rand.Read(seed[:])

	// Send funds to a wallet address that should be found after restore.
	addr := t.addrFromSeed(seed[:], rs.walletAddrPath)
	tx := t.sendTo(addr.String(), sendAmount)
	bl := t.mine(1)[0]
	t.assertBlockIncludesTx(bl, tx)

	// First test: restore with tx in tip block.
	opts := []dcrwtest.Opt{
		dcrwtest.WithDebugLevel("trace"),
		dcrwtest.WithHDSeed(seed[:]),
		syncOpt,
		createOpt,
		syncMethod,
		// dcrwtest.WithExposeCmdOutput(),
	}
	w := dcrwtest.RunForTest(testctx.New(t), t, t.params, opts...)
	time.Sleep(2 * time.Second) // FIXME: remove after :2317 is fixed.
	t.assertCumulativeBalance(w, sendAmount)

	// Second test: restore with tx deeper in history.
	t.mine(2)
	w = dcrwtest.RunForTest(testctx.New(t), t, t.params, opts...)
	time.Sleep(2 * time.Second) // FIXME: remove after :2317 is fixed.
	t.assertCumulativeBalance(w, sendAmount)

	// Third test: Stop wallet halfway through syncing (before block with
	// tx) then restart.  Create the wallet and run it using a proxy syncer
	// that does not return blocks after block 10.  Then stop and restart
	// the wallet without the proxy blocker.
	proxy.blockCFiltersAfter(101)
	ctx, cancel := testctx.WithCancel(t)
	go proxy.run(ctx)
	optsWithProxy := append(opts, syncOptWithProxy, dcrwtest.WithNoWaitInitialSync())
	w = dcrwtest.RunForTest(ctx, t, t.params, optsWithProxy...)
	select {
	case <-proxy.blocked():
	case <-ctx.Done():
		t.Fatal("never blocked any CFs")
	}
	t.waitWalletHeight(ctx, w, 100)
	cancel()
	select {
	case <-w.RunDone():
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for wallet to finish running")
	}
	restartOpts := append(opts, dcrwtest.WithRestartWallet(w))
	w = dcrwtest.RunForTest(testctx.New(t), t, t.params, restartOpts...)
	time.Sleep(2 * time.Second) // FIXME: remove after :2317 is fixed.
	t.assertCumulativeBalance(w, sendAmount)
}

// TestRestore tests several wallet restore scenarios.
func TestRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping due to -short")
	}

	// Setup simnet chain.
	params := chaincfg.SimNetParams()
	h, err := dcrdtest.New(t, params, nil, nil)
	if err != nil {
		t.Fatalf("unable to create main harness: %v", err)
	}
	err = h.SetUp(testctx.New(t), false, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h.TearDownInTest(t)
	if _, err := dcrdtest.AdjustedSimnetMiner(testctx.New(t), h.Node, 110); err != nil {
		t.Fatal(err)
	}

	// Setup voting wallet.
	vw, err := dcrdtest.NewVotingWallet(testctx.WithBackground(t), h)
	if err != nil {
		t.Fatalf("unable to create voting wallet: %v", err)
	}
	vw.SetMiner(dcrdtest.AdjustedSimnetMinerForClient(h.Node))
	vw.Start(testctx.WithBackground(t))

	tests := []restoreScenario{
		{
			name:           "legacy account",
			walletAddrPath: "44'/115'/1'/0/5",
			wantBalance:    true,
		}, {
			name:           "SLIP0044 account",
			syncOpt:        "spv",
			walletAddrPath: "44'/1'/1'/0/5",
			wantBalance:    true,
		}}

	for _, rs := range tests {
		for _, syncOpt := range []string{"spv", "rpc"} {
			for _, createOpt := range []string{"stdio", "grpc"} {
				tc := rs
				tc.syncOpt = syncOpt
				tc.createOpt = createOpt
				name := fmt.Sprintf("%s/%s/%s",
					syncOpt, createOpt, tc.name)
				t.Run(name, func(t *testing.T) {
					// setTestLogger(t)
					th := &testHarness{TB: t, h: h, vw: vw, params: params}
					testRestore(th, &tc)
				})
			}
		}
	}
}
