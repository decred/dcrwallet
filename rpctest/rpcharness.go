// Copyright (c) 2016 The decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package rpctest

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/chain"

	rpc "github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

var (
	// current number of active test nodes.
	numTestInstances = 0

	// defaultP2pPort is the initial p2p port which will be used by the
	// first created rpc harnesses to listen on for incoming p2p connections.
	// Subsequent allocated ports for future rpc harness instances will be
	// monotonically increasing odd numbers calculated as such:
	// defaultP2pPort + (2 * harness.nodeNum).
	defaultP2pPort = 18555

	// defaultRPCPort is the initial rpc port which will be used by the first created
	// rpc harnesses to listen on for incoming rpc connections. Subsequent
	// allocated ports for future rpc harness instances will be monotonically
	// increasing even numbers calculated as such:
	// defaultP2pPort + (2 * harness.nodeNum).
	defaultRPCPort = 19556

	defaultWalletRPCPort = 19557

	// testInstances is a private package-level slice used to keep track of
	// allvactive test harnesses. This global can be used to perform various
	// "joins", shutdown several active harnesses after a test, etc.
	testInstances map[string]*Harness

	// Used to protest concurrent access to above declared variables.
	harnessStateMtx sync.RWMutex
)

// Harness fully encapsulates an active dcrd process, along with an embdedded
// dcrwallet in order to provide a unified platform for creating rpc driven
// integration tests involving dcrd. The active dcrd node will typically be
// run in simnet mode in order to allow for easy generation of test blockchains.
// Additionally, a special method is provided which allows on to easily generate
// coinbase spends. The active dcrd process if fully managed by Harness, which
// handles the necessary initialization, and teardown of the process along with
// any temporary directories created as a result. Multiple Harness instances may
// be run concurrently, in order to allow for testing complex scenarios involving
// multuple nodes.
type Harness struct {
	ActiveNet *chaincfg.Params

	Node      *rpc.Client
	WalletRPC *rpc.Client
	node      *node
	handlers  *rpc.NotificationHandlers

	wallet *walletTest

	chainClient *chain.RPCClient

	testNodeDir    string
	testWalletDir  string
	maxConnRetries int
	nodeNum        int
}

// NewHarness creates and initializes new instance of the rpc test harness.
// Optionally, websocket handlers and a specified configuration may be passed.
// In the case that a nil config is passed, a default configuration will be used.
//
// NOTE: This function is safe for concurrent access.
func NewHarness(activeNet *chaincfg.Params, handlers *rpc.NotificationHandlers,
	extraArgs []string) (*Harness, error) {

	harnessStateMtx.Lock()
	defer harnessStateMtx.Unlock()

	harnessID := strconv.Itoa(int(numTestInstances))
	testDataPath := "rpctest-" + harnessID

	testData, err := ioutil.TempDir("", testDataPath)
	if err != nil {
		return nil, err
	}

	nodeTestData, err := ioutil.TempDir(testData, "node")
	if err != nil {
		return nil, err
	}

	walletTestData, err := ioutil.TempDir(testData, "wallet")
	if err != nil {
		return nil, err
	}

	certFile := filepath.Join(nodeTestData, "rpc.cert")
	keyFile := filepath.Join(nodeTestData, "rpc.key")
	certFileWallet := filepath.Join(walletTestData, "rpc.cert")
	keyFileWallet := filepath.Join(walletTestData, "rpc.key")

	// Generate the default config if needed.
	if err := genCertPair(certFile, keyFile, certFileWallet, keyFileWallet); err != nil {
		return nil, err
	}

	// Generate p2p+rpc listening addresses.
	p2p, rpcPort, walletRPC := generateListeningAddresses()

	config, err := newConfig(nodeTestData, certFile, keyFile, extraArgs)
	if err != nil {
		return nil, err
	}
	config.listen = p2p
	config.rpcListen = rpcPort

	// Create the testing node bounded to the simnet.
	node, err := newNode(config, nodeTestData)
	if err != nil {
		return nil, err
	}

	walletConfig, err := newWalletConfig(walletTestData, certFile, certFileWallet, keyFileWallet, nil)
	if err != nil {
		return nil, err
	}

	// Generate p2p+rpc listening addresses.
	walletConfig.rpcConnect = rpcPort
	walletConfig.rpcListen = walletRPC

	walletTest, err := newWallet(walletConfig, walletTestData)
	if err != nil {
		return nil, err
	}

	nodeNum := numTestInstances
	numTestInstances++

	h := &Harness{
		handlers:       handlers,
		node:           node,
		wallet:         walletTest,
		maxConnRetries: 20,
		testNodeDir:    nodeTestData,
		testWalletDir:  walletTestData,
		ActiveNet:      activeNet,
		nodeNum:        nodeNum,
	}

	// Track this newly created test instance within the package level
	// global map of all active test instances.
	testInstances[h.testNodeDir] = h

	return h, nil
}

// SetUp initializes the rpc test state. Initialization includes: starting up a
// simnet node, creating a websocket client and connecting to the started node,
// and finally: optionally generating and submitting a testchain with a configurable
// number of mature coinbase outputs coinbase outputs.
func (h *Harness) SetUp(createTestChain bool, numMatureOutputs uint32) error {
	var err error

	// Start the dcrd node itself. This spawns a new process which will be
	// managed
	if err = h.node.Start(); err != nil {
		return err
	}
	if err := h.connectRPCClient(); err != nil {
		return err
	}

	// Start the dcrd node itself. This spawns a new process which will be
	// managed
	if err = h.wallet.Start(); err != nil {
		return err
	}

	// Connect walletClient so we can get the miningaddress
	var walletClient *rpc.Client
	walletRPCConf := h.wallet.config.rpcConnConfig()
	for i := 0; i < 200; i++ {
		if walletClient, err = rpc.New(&walletRPCConf, nil); err != nil {
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			continue
		}
		break
	}
	if walletClient == nil {
		return fmt.Errorf("walletclient connection timedout")

	}
	h.WalletRPC = walletClient

	// Attempt to a new address from the wallet to be set as the miningaddress
	var miningAddr dcrutil.Address
	for i := 0; i < 100; i++ {
		if miningAddr, err = walletClient.GetNewAddress("default"); err != nil {
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			continue
		}
		break
	}
	if miningAddr == nil {
		return fmt.Errorf("RPC not up for mining addr %v %v", h.testNodeDir, h.testWalletDir)
	}

	var extraArgs []string
	miningArg := fmt.Sprintf("--miningaddr=%s", miningAddr)
	extraArgs = append(extraArgs, miningArg)

	// Shutdown
	if err := h.node.Shutdown(); err != nil {
		return err
	}

	config, err := newConfig(h.node.config.prefix, h.node.config.certFile, h.node.config.keyFile, extraArgs)
	if err != nil {
		return err
	}
	config.listen = h.node.config.listen
	config.rpcListen = h.node.config.rpcListen

	// Create the testing node bounded to the simnet.
	node, err := newNode(config, h.testNodeDir)
	if err != nil {
		return err
	}

	h.node = node
	// Restart node with mining address set
	if err = h.node.Start(); err != nil {
		return err
	}
	if err := h.connectRPCClient(); err != nil {
		return err
	}

	// Create a test chain with the desired number of mature coinbase
	// outputs.
	if createTestChain {
		numToGenerate := uint32(h.ActiveNet.CoinbaseMaturity) + numMatureOutputs
		_, err := h.Node.Generate(numToGenerate)
		if err != nil {
			return err
		}
	}

	rpcConf := h.node.config.rpcConnConfig()
	rpcc, err := chain.NewRPCClient(h.ActiveNet, rpcConf.Host, rpcConf.User,
		rpcConf.Pass, rpcConf.Certificates, false, 20)
	if err != nil {
		return err
	}

	// Start the goroutines in the underlying wallet.
	h.chainClient = rpcc
	if err := h.chainClient.Start(); err != nil {
		return err
	}

	// Wait for the wallet to sync up to the current height.
	ticker := time.NewTicker(time.Millisecond * 100)
	desiredHeight := int64(numMatureOutputs + uint32(h.ActiveNet.CoinbaseMaturity))
out:
	for {
		select {
		case <-ticker.C:
			count, err := h.WalletRPC.GetBlockCount()
			if err != nil {
				return err
			}
			if count == desiredHeight {
				break out
			}
		}
	}
	ticker.Stop()

	return nil
}

// TearDown stops the running rpc test instance. All created p.
// killed, and temporary directories removed.
func (h *Harness) TearDown() error {
	if h.Node != nil {
		h.Node.Shutdown()
	}

	if h.chainClient != nil {
		h.chainClient.Shutdown()
	}

	if err := h.node.Shutdown(); err != nil {
		return err
	}
	if err := h.wallet.Shutdown(); err != nil {
		return err
	}

	if err := os.RemoveAll(h.testNodeDir); err != nil {
		return err
	}

	if err := os.RemoveAll(h.testWalletDir); err != nil {
		return err
	}

	delete(testInstances, h.testNodeDir)

	return nil
}

// connectRPCClient attempts to establish an RPC connection to the created
// dcrd process belonging to this Harness instance. If the initial connection
// attempt fails, this function will retry h.maxConnRetries times, backing off
// the time between subsequent attempts. If after h.maxConnRetries attempts,
// we're not able to establish a connection, this function returns with an error.
func (h *Harness) connectRPCClient() error {
	var client *rpc.Client
	var err error

	rpcConf := h.node.config.rpcConnConfig()
	for i := 0; i < h.maxConnRetries; i++ {
		if client, err = rpc.New(&rpcConf, h.handlers); err != nil {
			time.Sleep(time.Duration(i) * 50 * time.Millisecond)
			continue
		}
		break
	}

	if client == nil {
		return fmt.Errorf("connection timedout")
	}

	h.Node = client
	return nil
}

// RPCConfig returns the harnesses current rpc configuration. This allows other
// potential RPC clients created within tests to connect to a given test harness
// instance.
func (h *Harness) RPCConfig() rpc.ConnConfig {
	return h.node.config.rpcConnConfig()
}

// generateListeningAddresses returns two strings representing listening
// addresses designated for the current rpc test. If there haven't been any
// test instances created, the default ports are used. Otherwise, in order to
// support multiple test nodes running at once, the p2p and rpc port are
// incremented after each initialization.
func generateListeningAddresses() (string, string, string) {
	var p2p, rpc, walletRPC string
	localhost := "127.0.0.1"

	if numTestInstances == 0 {
		p2p = net.JoinHostPort(localhost, strconv.Itoa(defaultP2pPort))
		rpc = net.JoinHostPort(localhost, strconv.Itoa(defaultRPCPort))
		walletRPC = net.JoinHostPort(localhost, strconv.Itoa(defaultWalletRPCPort))
	} else {
		p2p = net.JoinHostPort(localhost,
			strconv.Itoa(defaultP2pPort+(2*numTestInstances)))
		rpc = net.JoinHostPort(localhost,
			strconv.Itoa(defaultRPCPort+(2*numTestInstances)))
		walletRPC = net.JoinHostPort(localhost,
			strconv.Itoa(defaultRPCPort+(2*numTestInstances)))
	}

	return p2p, rpc, walletRPC
}

// GenerateBlock is a helper function to ensure that the chain has actually
// incremented due to FORK blocks after stake voting height that may occur.
func (h *Harness) GenerateBlock(startHeight uint32) ([]*chainhash.Hash, error) {
	blockHashes, err := h.Node.Generate(1)
	if err != nil {
		return nil, fmt.Errorf("unable to generate single block: %v", err)
	}
	block, err := h.Node.GetBlock(blockHashes[0])
	if err != nil {
		return nil, fmt.Errorf("unable to get block: %v", err)
	}
	newHeight := block.MsgBlock().Header.Height
	for newHeight == startHeight {
		blockHashes, err := h.Node.Generate(1)
		if err != nil {
			return nil, fmt.Errorf("unable to generate single block: %v", err)
		}
		block, err := h.Node.GetBlock(blockHashes[0])
		if err != nil {
			return nil, fmt.Errorf("unable to get block: %v", err)
		}
		newHeight = block.MsgBlock().Header.Height
	}
	return blockHashes, nil
}

func init() {
	// Create the testInstances map once the package has been imported.
	testInstances = make(map[string]*Harness)
}
