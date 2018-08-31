package rpctest

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/errors"
)

// ChainWithMatureOutputsSpawner initializes the primary mining node
// with a test chain of desired height, providing numMatureOutputs coinbases
// to allow spending from for testing purposes.
type ChainWithMatureOutputsSpawner struct {
	// Each harness will be provided with a dedicated
	// folder inside the WorkingDir
	WorkingDir string

	// DebugDCRDOutput, set true to print out dcrd output to console
	DebugDCRDOutput bool

	// DebugWalletOutput, set true to print out wallet output to console
	DebugWalletOutput bool

	// newHarnessIndex for net port offset
	newHarnessIndex int

	// Harnesses will subsequently reserve
	// network ports starting from the BasePort value
	BasePort int

	// NumMatureOutputs sets requirement for the generated test chain
	NumMatureOutputs uint32
}

// NewInstance does the following:
//   1. Starts a new DcrdTestServer process with a fresh SimNet chain.
//   2. Creates a new temporary WalletTestServer connected to the running DcrdTestServer.
//   3. Gets a new address from the WalletTestServer for mining subsidy.
//   4. Restarts the DcrdTestServer with the new mining address.
//   5. Generates a number of blocks so that testing starts with a spendable
//      balance.
func (testSetup *ChainWithMatureOutputsSpawner) NewInstance(harnessName string) *Harness {
	harnessFolderName := "harness-" + harnessName
	harnessFolder := filepath.Join(testSetup.WorkingDir, harnessFolderName)

	p2p, dcrdRPC, walletRPC := generateListeningPorts(
		testSetup.newHarnessIndex, testSetup.BasePort)
	testSetup.newHarnessIndex++

	localhost := "127.0.0.1"

	cfg := &HarnessConfig{
		Name:       harnessName,
		WorkingDir: harnessFolder,

		ActiveNet: &chaincfg.SimNetParams,

		P2PHost: localhost,
		P2PPort: p2p,

		DcrdRPCHost: localhost,
		DcrdRPCPort: dcrdRPC,

		WalletRPCHost: localhost,
		WalletRPCPort: walletRPC,
	}
	harness := NewHarness(cfg)

	fmt.Println("Deploying Harness[" + cfg.Name + "]")
	//fmt.Println("dcrdConfig: " + spew.Sdump(harness.DcrdServer))
	//fmt.Println("walletConfig: " + spew.Sdump(harness.WalletServer))

	// launch a fresh harness (assumes harness working dir is empty)
	{
		args := newLaunchArguments(testSetup, harness)
		launchHarnessSequence(harness, args)
	}

	// Get a new address from the WalletTestServer
	// to be set with dcrd --miningaddr
	{
		harness.MiningAddress = getMiningAddr(harness.WalletRPCClient())

		assertNotNil("MiningAddress", harness.MiningAddress)
		assertNotEmpty("MiningAddress", harness.MiningAddress.String())

		fmt.Println("Mining address: " + harness.MiningAddress.String())
	}

	// restart the harness with the new argument
	{
		shutdownHarnessSequence(harness)

		args := newLaunchArguments(testSetup, harness)
		{
			// set create test chain with numMatureOutputs
			args.CreateTestChain = true
			args.NumMatureOutputs = testSetup.NumMatureOutputs

			// set mining address
			args.DcrdExtraArgs["miningaddr"] = harness.MiningAddress
		}
		launchHarnessSequence(harness, args)
	}

	// wait for the WalletTestServer to sync up to the current height
	{
		desiredHeight := int64(
			testSetup.NumMatureOutputs +
				uint32(cfg.ActiveNet.CoinbaseMaturity))
		syncWalletHarnessTo(harness, desiredHeight)
	}

	// sometimes wallet hangs on RPC calls made immediately after the launch,
	// wait 5 seconds before using it
	Sleep(5000)
	// ToDo: Figure out why Wallet RPC hangs. Race condition?

	fmt.Println("Harness[" + cfg.Name + "] is ready")

	return harness
}

// Dispose harness. This includes removing
// all temporary directories, and shutting down any created processes.
func (testSetup *ChainWithMatureOutputsSpawner) Dispose(h *Harness) error {
	if h == nil {
		return nil
	}
	if h.WalletClient.isConnected {
		h.WalletClient.Disconnect()
	}
	if h.WalletServer.IsRunning() {
		h.WalletServer.Stop()
	}
	if h.DcrdClient.isConnected {
		h.DcrdClient.Disconnect()
	}
	if h.DcrdServer.IsRunning() {
		h.DcrdServer.Stop()
	}
	return h.DeleteWorkingDir()
}

// NameForTag defines policy for mapping input tags to harness names
func (testSetup *ChainWithMatureOutputsSpawner) NameForTag(tag string) string {
	harnessName := tag
	return harnessName
}

// launchHarnessSequence
// 1. launch Dcrd node
// 2. connect to the node via RPC client
// 3. launch wallet and connects it to the Dcrd node
// 4. connect to the wallet via RPC client
func launchHarnessSequence(harness *Harness, args *launchArguments) {
	cfg := harness.Config
	DcrdServer := harness.DcrdServer
	WalletServer := harness.WalletServer

	DcrdServer.Start(args.DcrdExtraArgs, args.DebugDCRDOutput)
	// DCRD RPC instance will create a cert file when it is ready for incoming calls
	WaitForFile(DcrdServer.CertFile(), 7)

	fmt.Println("Connect to DCRD RPC...")
	{
		cfg := harness.DcrdConnectionConfig()
		harness.DcrdClient.Connect(cfg)
	}
	fmt.Println("DCRD RPC client connected.")

	if args.CreateTestChain {
		numToGenerate := uint32(cfg.ActiveNet.CoinbaseMaturity) + args.NumMatureOutputs
		err := generateTestChain(numToGenerate, harness.DcrdRPCClient())
		CheckTestSetupMalfunction(err)
	}

	WalletServer.Start(DcrdServer.CertFile(), args.WalletExtraArguments, args.DebugWalletOutput)
	// Wallet RPC instance will create a cert file when it is ready for incoming calls
	WaitForFile(WalletServer.CertFile(), 90)

	fmt.Println("Connect to Wallet RPC...")
	{
		cfg := harness.WalletConnectionConfig()
		harness.WalletClient.Connect(cfg)
	}
	fmt.Println("Wallet RPC client connected.")
}

// shutdownHarnessSequence reverses the launchHarnessSequence
// 4. disconnect from the wallet RPC
// 3. stop wallet
// 2. disconnect from the Dcrd RPC
// 1. stop Dcrd node
func shutdownHarnessSequence(harness *Harness) {
	DcrdServer := harness.DcrdServer
	WalletServer := harness.WalletServer

	fmt.Println("Disconnect from Wallet RPC...")
	harness.WalletClient.Disconnect()

	WalletServer.Stop()

	fmt.Println("Disconnect from DCRD RPC...")
	harness.DcrdClient.Disconnect()

	DcrdServer.Stop()

	// Delete files, RPC servers will recreate them on the next launch sequence
	DeleteFile(DcrdServer.CertFile())
	DeleteFile(DcrdServer.KeyFile())
	DeleteFile(WalletServer.CertFile())
	DeleteFile(WalletServer.KeyFile())
}

func generateListeningPorts(index, base int) (int, int, int) {
	x := base + index*3 + 0
	y := base + index*3 + 1
	z := base + index*3 + 2
	return x, y, z
}

// networkFor resolves network argument for dcrd and wallet console commands
func networkFor(net *chaincfg.Params) string {
	if net == &chaincfg.SimNetParams {
		return "simnet"
	}
	if net == &chaincfg.TestNet3Params {
		return "testnet"
	}
	if net == &chaincfg.MainNetParams {
		// no argument needed for the MainNet
		return NoArgument
	}

	// should never reach this line, report violation
	ReportTestSetupMalfunction(errors.Errorf("Unknown network: %v", net))
	return ""
}

func syncWalletHarnessTo(harness *Harness, desiredHeight int64) {
	fmt.Println("Waiting for WalletServer to sync to target height: " +
		strconv.FormatInt(desiredHeight, 10))

	rpcClient := harness.WalletRPCClient()
	count, err := syncWalletTo(rpcClient, desiredHeight)
	CheckTestSetupMalfunction(err)
	fmt.Println("Wallet sync complete, height: " + strconv.FormatInt(count, 10))
}

// local struct to bundle launchHarnessSequence function arguments
type launchArguments struct {
	DcrdExtraArgs        map[string]interface{}
	WalletExtraArguments map[string]interface{}
	DebugDCRDOutput      bool
	DebugWalletOutput    bool
	CreateTestChain      bool
	NumMatureOutputs     uint32
}

func newLaunchArguments(testSetup *ChainWithMatureOutputsSpawner, harness *Harness) *launchArguments {
	args := &launchArguments{
		DcrdExtraArgs:        make(map[string]interface{}),
		WalletExtraArguments: make(map[string]interface{}),
		DebugDCRDOutput:      testSetup.DebugDCRDOutput,
		DebugWalletOutput:    testSetup.DebugWalletOutput,
	}

	// set network
	network := networkFor(harness.ActiveNet())
	args.DcrdExtraArgs[network] = NoArgumentValue
	args.WalletExtraArguments[network] = NoArgumentValue

	// disable GRPC
	args.WalletExtraArguments["nogrpc"] = NoArgumentValue

	// enable ticket buyer
	args.WalletExtraArguments["enableticketbuyer"] = NoArgumentValue

	// set simnet flags
	if harness.ActiveNet() == &chaincfg.SimNetParams {
		args.WalletExtraArguments["createtemp"] = NoArgumentValue
	}

	return args
}
