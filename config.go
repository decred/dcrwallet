// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/internal/cfgutil"
	"github.com/decred/dcrwallet/netparams"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/txrules"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultCAFilename          = "dcrd.cert"
	defaultConfigFilename      = "dcrwallet.conf"
	defaultLogLevel            = "info"
	defaultLogDirname          = "logs"
	defaultLogFilename         = "dcrwallet.log"
	defaultRPCMaxClients       = 10
	defaultRPCMaxWebsockets    = 25
	defaultEnableStakeMining   = false
	defaultEnableTicketBuyer   = false
	defaultEnableVoting        = false
	defaultVoteBits            = 0x0001
	defaultVoteBitsExtended    = "02000000"
	defaultBalanceToMaintain   = 0.0
	defaultReuseAddresses      = false
	defaultRollbackTest        = false
	defaultPruneTickets        = false
	defaultPurchaseAccount     = "default"
	defaultTicketMaxPrice      = 100.0
	defaultTicketBuyFreq       = 1
	defaultAutomaticRepair     = false
	defaultUnsafeMainNet       = false
	defaultPromptPass          = false
	defaultAddrIdxScanLen      = 750
	defaultStakePoolColdExtKey = ""
	defaultAllowHighFees       = false

	walletDbName = "wallet.db"
)

var (
	dcrdDefaultCAFile  = filepath.Join(dcrutil.AppDataDir("dcrd", false), "rpc.cert")
	defaultAppDataDir  = dcrutil.AppDataDir("dcrwallet", false)
	defaultConfigFile  = filepath.Join(defaultAppDataDir, defaultConfigFilename)
	defaultRPCKeyFile  = filepath.Join(defaultAppDataDir, "rpc.key")
	defaultRPCCertFile = filepath.Join(defaultAppDataDir, "rpc.cert")
	defaultLogDir      = filepath.Join(defaultAppDataDir, defaultLogDirname)
)

type config struct {
	// General application behavior
	ConfigFile         string   `short:"C" long:"configfile" description:"Path to configuration file"`
	ShowVersion        bool     `short:"V" long:"version" description:"Display version information and exit"`
	Create             bool     `long:"create" description:"Create the wallet if it does not exist"`
	CreateTemp         bool     `long:"createtemp" description:"Create a temporary simulation wallet (pass=password) in the data directory indicated; must call with --datadir"`
	CreateWatchingOnly bool     `long:"createwatchingonly" description:"Create the wallet and instantiate it as watching only with an HD extended pubkey; must call with --create"`
	AppDataDir         string   `short:"A" long:"appdata" description:"Application data directory for wallet config, databases and logs"`
	TestNet            bool     `long:"testnet" description:"Use the test network (default mainnet)"`
	SimNet             bool     `long:"simnet" description:"Use the simulation test network (default mainnet)"`
	NoInitialLoad      bool     `long:"noinitialload" description:"Defer wallet creation/opening on startup and enable loading wallets over RPC"`
	DebugLevel         string   `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}"`
	LogDir             string   `long:"logdir" description:"Directory to log output."`
	Profile            []string `long:"profile" description:"Enable HTTP profiling this interface/port"`
	MemProfile         string   `long:"memprofile" description:"Write mem profile to the specified file"`
	RollbackTest       bool     `long:"rollbacktest" description:"Rollback testing is a simnet testing mode that eventually stops wallet and examines wtxmgr database integrity"`
	AutomaticRepair    bool     `long:"automaticrepair" description:"Attempt to repair the wallet automatically if a database inconsistency is found"`
	UnsafeMainNet      bool     `long:"unsafemainnet" description:"Enable storage of master seed in mainnet wallet when calling --create and enable unsafe private information RPC commands"`

	// Wallet options
	WalletPass          string  `long:"walletpass" default-mask:"-" description:"The public wallet password -- Only required if the wallet was created with one"`
	PromptPass          bool    `long:"promptpass" description:"The private wallet password is prompted for at start up, so the wallet starts unlocked without a time limit"`
	DisallowFree        bool    `long:"disallowfree" description:"Force transactions to always include a fee"`
	EnableTicketBuyer   bool    `long:"enableticketbuyer" description:"Enable the automatic ticket buyer"`
	EnableVoting        bool    `long:"enablevoting" description:"Enable creation of votes and revocations for owned tickets"`
	VoteBits            uint16  `long:"votebits" description:"Set your stake mining votebits to value (default: 0xFFFF)"`
	VoteBitsExtended    string  `long:"votebitsextended" description:"Set your stake mining extended votebits to the hexademical value indicated by the passed string"`
	BalanceToMaintain   float64 `long:"balancetomaintain" description:"Minimum amount of funds to leave in wallet when stake mining (default: 0.0)"`
	ReuseAddresses      bool    `long:"reuseaddresses" description:"Reuse addresses for ticket purchase to cut down on address overuse"`
	PruneTickets        bool    `long:"prunetickets" description:"Prune old tickets from the wallet and restore their inputs"`
	PurchaseAccount     string  `long:"purchaseaccount" description:"Name of the account to buy tickets from (default: default)"`
	TicketAddress       string  `long:"ticketaddress" description:"Send all ticket outputs to this address (P2PKH or P2SH only)"`
	TicketMaxPrice      float64 `long:"ticketmaxprice" description:"The maximum price the user is willing to spend on buying a ticket (default: 100.0)"`
	TicketBuyFreq       int     `long:"ticketbuyfreq" description:"The number of tickets to try to buy per block (default: 1), where negative numbers indicate one ticket for each 1-in-? blocks"`
	PoolAddress         string  `long:"pooladdress" description:"The ticket pool address where ticket fees will go to"`
	PoolFees            float64 `long:"poolfees" description:"The per-ticket fee mandated by the ticket pool as a percent (e.g. 1.00 for 1.00% fee)"`
	AddrIdxScanLen      int     `long:"addridxscanlen" description:"The width of the scan for last used addresses on wallet restore and start up (default: 750)"`
	StakePoolColdExtKey string  `long:"stakepoolcoldextkey" description:"Enables the wallet as a stake pool with an extended key in the format of \"xpub...:index\" to derive cold wallet addresses to send fees to"`
	AllowHighFees       bool    `long:"allowhighfees" description:"Force the RPC client to use the 'allowHighFees' flag when sending transactions"`
	RelayFee            float64 `long:"txfee" description:"Sets the wallet's tx fee per kb (default: 0.01)"`
	TicketFee           float64 `long:"ticketfee" description:"Sets the wallet's ticket fee per kb (default: 0.01)"`
	PipeRx              *uint   `long:"piperx" description:"File descriptor of read end pipe to enable parent -> child process communication"`

	// RPC client options
	RPCConnect       string `short:"c" long:"rpcconnect" description:"Hostname/IP and port of dcrd RPC server to connect to (default localhost:9109, testnet: localhost:19109, simnet: localhost:18556)"`
	CAFile           string `long:"cafile" description:"File containing root certificates to authenticate a TLS connections with dcrd"`
	DisableClientTLS bool   `long:"noclienttls" description:"Disable TLS for the RPC client -- NOTE: This is only allowed if the RPC client is connecting to localhost"`
	DcrdUsername     string `long:"dcrdusername" description:"Username for dcrd authentication"`
	DcrdPassword     string `long:"dcrdpassword" default-mask:"-" description:"Password for dcrd authentication"`
	Proxy            string `long:"proxy" description:"Connect via SOCKS5 proxy (eg. 127.0.0.1:9050)"`
	ProxyUser        string `long:"proxyuser" description:"Username for proxy server"`
	ProxyPass        string `long:"proxypass" default-mask:"-" description:"Password for proxy server"`

	// RPC server options
	//
	// The legacy server is still enabled by default (and eventually will be
	// replaced with the experimental server) so prepare for that change by
	// renaming the struct fields (but not the configuration options).
	//
	// Usernames can also be used for the consensus RPC client, so they
	// aren't considered legacy.
	RPCCert                string             `long:"rpccert" description:"File containing the certificate file"`
	RPCKey                 string             `long:"rpckey" description:"File containing the certificate key"`
	TLSCurve               *cfgutil.CurveFlag `long:"tlscurve" description:"Curve to use when generating TLS keypairs"`
	OneTimeTLSKey          bool               `long:"onetimetlskey" description:"Generate a new TLS certpair at startup, but only write the certificate to disk"`
	DisableServerTLS       bool               `long:"noservertls" description:"Disable TLS for the RPC server -- NOTE: This is only allowed if the RPC server is bound to localhost"`
	LegacyRPCListeners     []string           `long:"rpclisten" description:"Listen for legacy RPC connections on this interface/port (default port: 9110, testnet: 19110, simnet: 18557)"`
	LegacyRPCMaxClients    int64              `long:"rpcmaxclients" description:"Max number of legacy RPC clients for standard connections"`
	LegacyRPCMaxWebsockets int64              `long:"rpcmaxwebsockets" description:"Max number of legacy RPC websocket connections"`
	Username               string             `short:"u" long:"username" description:"Username for legacy RPC and dcrd authentication (if dcrdusername is unset)"`
	Password               string             `short:"P" long:"password" default-mask:"-" description:"Password for legacy RPC and dcrd authentication (if dcrdpassword is unset)"`

	// EXPERIMENTAL RPC server options
	//
	// These options will change (and require changes to config files, etc.)
	// when the new gRPC server is enabled.
	ExperimentalRPCListeners []string `long:"experimentalrpclisten" description:"Listen for RPC connections on this interface/port"`

	// Deprecated options
	DataDir           string `short:"b" long:"datadir" default-mask:"-" description:"DEPRECATED -- use appdata instead"`
	EnableStakeMining bool   `long:"enablestakemining" default-mask:"-" description:"DEPRECATED -- consider using enableticketbuyer and/or enablevoting instead"`
}

// cleanAndExpandPath expands environement variables and leading ~ in the
// passed path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but they variables can still be expanded via POSIX-style
	// $VARIABLE.
	path = os.ExpandEnv(path)

	if !strings.HasPrefix(path, "~") {
		return filepath.Clean(path)
	}

	// Expand initial ~ to the current user's home directory, or ~otheruser
	// to otheruser's home directory.  On Windows, both forward and backward
	// slashes can be used.
	path = path[1:]

	var pathSeparators string
	if runtime.GOOS == "windows" {
		pathSeparators = string(os.PathSeparator) + "/"
	} else {
		pathSeparators = string(os.PathSeparator)
	}

	userName := ""
	if i := strings.IndexAny(path, pathSeparators); i != -1 {
		userName = path[:i]
		path = path[i:]
	}

	homeDir := ""
	var u *user.User
	var err error
	if userName == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(userName)
	}
	if err == nil {
		homeDir = u.HomeDir
	}
	// Fallback to CWD if user lookup fails or user has no home directory.
	if homeDir == "" {
		homeDir = "."
	}

	return filepath.Join(homeDir, path)
}

// validLogLevel returns whether or not logLevel is a valid debug log level.
func validLogLevel(logLevel string) bool {
	switch logLevel {
	case "trace":
		fallthrough
	case "debug":
		fallthrough
	case "info":
		fallthrough
	case "warn":
		fallthrough
	case "error":
		fallthrough
	case "critical":
		return true
	}
	return false
}

// supportedSubsystems returns a sorted slice of the supported subsystems for
// logging purposes.
func supportedSubsystems() []string {
	// Convert the subsystemLoggers map keys to a slice.
	subsystems := make([]string, 0, len(subsystemLoggers))
	for subsysID := range subsystemLoggers {
		subsystems = append(subsystems, subsysID)
	}

	// Sort the subsytems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) error {
	// When the specified string doesn't have any delimters, treat it as
	// the log level for all subsystems.
	if !strings.Contains(debugLevel, ",") && !strings.Contains(debugLevel, "=") {
		// Validate debug log level.
		if !validLogLevel(debugLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, debugLevel)
		}

		// Change the logging level for all subsystems.
		setLogLevels(debugLevel)

		return nil
	}

	// Split the specified string into subsystem/level pairs while detecting
	// issues and update the log levels accordingly.
	for _, logLevelPair := range strings.Split(debugLevel, ",") {
		if !strings.Contains(logLevelPair, "=") {
			str := "The specified debug level contains an invalid " +
				"subsystem/level pair [%v]"
			return fmt.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate subsystem.
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "The specified subsystem [%v] is invalid -- " +
				"supported subsytems %v"
			return fmt.Errorf(str, subsysID, supportedSubsystems())
		}

		// Validate log level.
		if !validLogLevel(logLevel) {
			str := "The specified debug level [%v] is invalid"
			return fmt.Errorf(str, logLevel)
		}

		setLogLevel(subsysID, logLevel)
	}

	return nil
}

// loadConfig initializes and parses the config using a config file and command
// line options.
//
// The configuration proceeds as follows:
//      1) Start with a default config with sane settings
//      2) Pre-parse the command line to check for an alternative config file
//      3) Load configuration file overwriting defaults with any specified options
//      4) Parse CLI options and overwrite/add any specified options
//
// The above results in dcrwallet functioning properly without any config
// settings while still allowing the user to override settings with config files
// and command line options.  Command line options always take precedence.
// The bool returned indicates whether or not the wallet was recreated from a
// seed and needs to perform the initial resync. The []byte is the private
// passphrase required to do the sync for this special case.
func loadConfig() (*config, []string, error) {
	loadConfigError := func(err error) (*config, []string, error) {
		return nil, nil, err
	}

	// Default config.
	cfg := config{
		DebugLevel:             defaultLogLevel,
		ConfigFile:             defaultConfigFile,
		AppDataDir:             defaultAppDataDir,
		LogDir:                 defaultLogDir,
		WalletPass:             wallet.InsecurePubPassphrase,
		PromptPass:             defaultPromptPass,
		RPCKey:                 defaultRPCKeyFile,
		RPCCert:                defaultRPCCertFile,
		TLSCurve:               cfgutil.NewCurveFlag(cfgutil.CurveP521),
		LegacyRPCMaxClients:    defaultRPCMaxClients,
		LegacyRPCMaxWebsockets: defaultRPCMaxWebsockets,
		EnableTicketBuyer:      defaultEnableTicketBuyer,
		EnableVoting:           defaultEnableVoting,
		VoteBits:               defaultVoteBits,
		VoteBitsExtended:       defaultVoteBitsExtended,
		BalanceToMaintain:      defaultBalanceToMaintain,
		ReuseAddresses:         defaultReuseAddresses,
		RollbackTest:           defaultRollbackTest,
		PruneTickets:           defaultPruneTickets,
		PurchaseAccount:        defaultPurchaseAccount,
		TicketMaxPrice:         defaultTicketMaxPrice,
		TicketBuyFreq:          defaultTicketBuyFreq,
		AutomaticRepair:        defaultAutomaticRepair,
		UnsafeMainNet:          defaultUnsafeMainNet,
		AddrIdxScanLen:         defaultAddrIdxScanLen,
		StakePoolColdExtKey:    defaultStakePoolColdExtKey,
		AllowHighFees:          defaultAllowHighFees,
		DataDir:                defaultAppDataDir,
		EnableStakeMining:      defaultEnableStakeMining,
	}

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.
	preCfg := cfg
	preParser := flags.NewParser(&preCfg, flags.Default)
	_, err := preParser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			preParser.WriteHelp(os.Stderr)
		}
		return loadConfigError(err)
	}

	// Show the version and exit if the version flag was specified.
	funcName := "loadConfig"
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	usageMessage := fmt.Sprintf("Use %s -h to show usage", appName)
	if preCfg.ShowVersion {
		fmt.Println(appName, "version", version())
		os.Exit(0)
	}

	// Load additional config from file.
	var configFileError error
	parser := flags.NewParser(&cfg, flags.Default)
	configFilePath := preCfg.ConfigFile
	if configFilePath == defaultConfigFile {
		appDataDir := preCfg.AppDataDir
		if appDataDir == defaultAppDataDir && preCfg.DataDir != defaultAppDataDir {
			appDataDir = cleanAndExpandPath(preCfg.DataDir)
		}
		if appDataDir != defaultAppDataDir {
			configFilePath = filepath.Join(appDataDir, defaultConfigFilename)
		}
	} else {
		configFilePath = cleanAndExpandPath(configFilePath)
	}
	err = flags.NewIniParser(parser).ParseFile(configFilePath)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			fmt.Fprintln(os.Stderr, err)
			parser.WriteHelp(os.Stderr)
			return loadConfigError(err)
		}
		configFileError = err
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return loadConfigError(err)
	}

	// Warn about missing config file after the final command line parse
	// succeeds.  This prevents the warning on help messages and invalid
	// options.
	if configFileError != nil {
		log.Warnf("%v", configFileError)
	}

	// Check deprecated options.  The new options receive priority when both
	// are changed from the default.
	if cfg.DataDir != defaultAppDataDir {
		fmt.Fprintln(os.Stderr, "datadir option has been replaced by "+
			"appdata -- please update your config")
		if cfg.AppDataDir == defaultAppDataDir {
			cfg.AppDataDir = cfg.DataDir
		}
	}
	if cfg.EnableStakeMining {
		fmt.Fprintln(os.Stderr, "enablestakemining option is deprecated -- "+
			"consider updating your config to use enablevoting and enableticketbuyer")
		if cfg.EnableTicketBuyer {
			fmt.Fprintln(os.Stderr, "Because enableticketbuyer was set, "+
				"tickets will be purchased using the new buyer")
		} else {
			fmt.Fprintln(os.Stderr, "Because enableticketbuyer was not set, "+
				"tickets will be purchased using the old buyer")
		}
		// enablestakemining turns on voting/revocations, but never turns on the
		// new ticket buyer.
		cfg.EnableVoting = true
	}

	// If an alternate data directory was specified, and paths with defaults
	// relative to the data dir are unchanged, modify each path to be
	// relative to the new data dir.
	if cfg.AppDataDir != defaultAppDataDir {
		cfg.AppDataDir = cleanAndExpandPath(cfg.AppDataDir)
		if cfg.RPCKey == defaultRPCKeyFile {
			cfg.RPCKey = filepath.Join(cfg.AppDataDir, "rpc.key")
		}
		if cfg.RPCCert == defaultRPCCertFile {
			cfg.RPCCert = filepath.Join(cfg.AppDataDir, "rpc.cert")
		}
		if cfg.LogDir == defaultLogDir {
			cfg.LogDir = filepath.Join(cfg.AppDataDir, defaultLogDirname)
		}
	}

	// Choose the active network params based on the selected network.
	// Multiple networks can't be selected simultaneously.
	numNets := 0
	if cfg.TestNet {
		activeNet = &netparams.TestNetParams
		numNets++
	}
	if cfg.SimNet {
		activeNet = &netparams.SimNetParams
		numNets++
	}
	if numNets > 1 {
		str := "%s: The testnet and simnet params can't be used " +
			"together -- choose one"
		err := fmt.Errorf(str, "loadConfig")
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	// Append the network type to the log directory so it is "namespaced"
	// per network.
	cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	cfg.LogDir = filepath.Join(cfg.LogDir, activeNet.Params.Name)

	// Special show command to list supported subsystems and exit.
	if cfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// Initialize logging at the default logging level.
	initSeelogLogger(filepath.Join(cfg.LogDir, defaultLogFilename))
	setLogLevels(defaultLogLevel)

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err := fmt.Errorf("%s: %v", "loadConfig", err.Error())
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	// Exit if you try to use a simulation wallet with a standard
	// data directory.
	if cfg.AppDataDir == defaultAppDataDir && cfg.CreateTemp {
		fmt.Fprintln(os.Stderr, "Tried to create a temporary simulation "+
			"wallet, but failed to specify data directory!")
		os.Exit(0)
	}

	// Exit if you try to use a simulation wallet on anything other than
	// simnet or testnet.
	if !cfg.SimNet && cfg.CreateTemp {
		fmt.Fprintln(os.Stderr, "Tried to create a temporary simulation "+
			"wallet for network other than simnet!")
		os.Exit(0)
	}

	// Warn if rollback testing is enabled, as this feature was removed.
	if cfg.RollbackTest {
		fmt.Fprintln(os.Stderr, "WARN: Rollback testing no longer exists")
	}

	// Ensure the wallet exists or create it when the create flag is set.
	netDir := networkDir(cfg.AppDataDir, activeNet.Params)
	dbPath := filepath.Join(netDir, walletDbName)

	if cfg.CreateTemp && cfg.Create {
		err := fmt.Errorf("The flags --create and --createtemp can not " +
			"be specified together. Use --help for more information.")
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	dbFileExists, err := cfgutil.FileExists(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	if cfg.CreateTemp {
		tempWalletExists := false

		if dbFileExists {
			str := fmt.Sprintf("The wallet already exists. Loading this " +
				"wallet instead.")
			fmt.Fprintln(os.Stdout, str)
			tempWalletExists = true
		}

		// Ensure the data directory for the network exists.
		if err := checkCreateDir(netDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return loadConfigError(err)
		}

		if !tempWalletExists {
			// Perform the initial wallet creation wizard.
			if err := createSimulationWallet(&cfg); err != nil {
				fmt.Fprintln(os.Stderr, "Unable to create wallet:", err)
				return loadConfigError(err)
			}
		}
	} else if cfg.Create {
		// Error if the create flag is set and the wallet already
		// exists.
		if dbFileExists {
			err := fmt.Errorf("The wallet database file `%v` "+
				"already exists.", dbPath)
			fmt.Fprintln(os.Stderr, err)
			return loadConfigError(err)
		}

		// Ensure the data directory for the network exists.
		if err := checkCreateDir(netDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return loadConfigError(err)
		}

		// Perform the initial wallet creation wizard.
		if !cfg.CreateWatchingOnly {
			if err = createWallet(&cfg); err != nil {
				fmt.Fprintln(os.Stderr, "Unable to create wallet:", err)
				return loadConfigError(err)
			}
		} else if cfg.CreateWatchingOnly {
			if err = createWatchingOnlyWallet(&cfg); err != nil {
				fmt.Fprintln(os.Stderr, "Unable to create wallet:", err)
				return loadConfigError(err)
			}
		}

		// Created successfully, so exit now with success.
		os.Exit(0)
	} else if !dbFileExists && !cfg.NoInitialLoad {
		err := fmt.Errorf("The wallet does not exist.  Run with the " +
			"--create option to initialize and create it.")
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	if len(cfg.TicketAddress) != 0 {
		_, err := dcrutil.DecodeAddress(cfg.TicketAddress, activeNet.Params)
		if err != nil {
			err := fmt.Errorf("ticketaddress '%s' failed to decode: %v",
				cfg.TicketAddress, err)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return loadConfigError(err)
		}
	}

	if len(cfg.PoolAddress) != 0 {
		_, err := dcrutil.DecodeAddress(cfg.PoolAddress, activeNet.Params)
		if err != nil {
			err := fmt.Errorf("pooladdress '%s' failed to decode: %v",
				cfg.PoolAddress, err)
			fmt.Fprintln(os.Stderr, err.Error())
			fmt.Fprintln(os.Stderr, usageMessage)
			return loadConfigError(err)
		}
	}

	if cfg.PoolFees != 0.0 {
		err := txrules.IsValidPoolFeeRate(cfg.PoolFees)
		if err != nil {
			err := fmt.Errorf("poolfees '%v' failed to decode: %v",
				cfg.PoolFees, err)
			fmt.Fprintln(os.Stderr, err.Error())
			fmt.Fprintln(os.Stderr, usageMessage)
			return loadConfigError(err)
		}
	}

	if cfg.RPCConnect == "" {
		cfg.RPCConnect = net.JoinHostPort("localhost", activeNet.RPCClientPort)
	}

	// Set ticketfee and txfee to defaults if none are set.  Avoiding using default
	// confs because they are dcrutil.Amounts
	if cfg.TicketFee == 0.0 {
		cfg.TicketFee = wallet.DefaultTicketFeeIncrement.ToCoin()
	}
	if cfg.RelayFee == 0.0 {
		cfg.RelayFee = txrules.DefaultRelayFeePerKb.ToCoin()
	}

	// Add default port to connect flag if missing.
	cfg.RPCConnect, err = cfgutil.NormalizeAddress(cfg.RPCConnect,
		activeNet.RPCClientPort)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Invalid rpcconnect network address: %v\n", err)
		return loadConfigError(err)
	}

	localhostListeners := map[string]struct{}{
		"localhost": struct{}{},
		"127.0.0.1": struct{}{},
		"::1":       struct{}{},
	}
	RPCHost, _, err := net.SplitHostPort(cfg.RPCConnect)
	if err != nil {
		return loadConfigError(err)
	}
	if cfg.DisableClientTLS {
		if _, ok := localhostListeners[RPCHost]; !ok {
			str := "%s: the --noclienttls option may not be used " +
				"when connecting RPC to non localhost " +
				"addresses: %s"
			err := fmt.Errorf(str, funcName, cfg.RPCConnect)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return loadConfigError(err)
		}
	} else {
		// If CAFile is unset, choose either the copy or local dcrd cert.
		if cfg.CAFile == "" {
			cfg.CAFile = filepath.Join(cfg.AppDataDir, defaultCAFilename)

			// If the CA copy does not exist, check if we're connecting to
			// a local dcrd and switch to its RPC cert if it exists.
			certExists, err := cfgutil.FileExists(cfg.CAFile)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return loadConfigError(err)
			}
			if !certExists {
				if _, ok := localhostListeners[RPCHost]; ok {
					dcrdCertExists, err := cfgutil.FileExists(
						dcrdDefaultCAFile)
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
						return loadConfigError(err)
					}
					if dcrdCertExists {
						cfg.CAFile = dcrdDefaultCAFile
					}
				}
			}
		}
	}

	// Only set default RPC listeners when there are no listeners set for
	// the experimental RPC server.  This is required to prevent the old RPC
	// server from sharing listen addresses, since it is impossible to
	// remove defaults from go-flags slice options without assigning
	// specific behavior to a particular string.
	if len(cfg.ExperimentalRPCListeners) == 0 && len(cfg.LegacyRPCListeners) == 0 {
		addrs, err := net.LookupHost("localhost")
		if err != nil {
			return loadConfigError(err)
		}
		cfg.LegacyRPCListeners = make([]string, 0, len(addrs))
		for _, addr := range addrs {
			addr = net.JoinHostPort(addr, activeNet.RPCServerPort)
			cfg.LegacyRPCListeners = append(cfg.LegacyRPCListeners, addr)
		}
	}

	// Add default port to all rpc listener addresses if needed and remove
	// duplicate addresses.
	cfg.LegacyRPCListeners, err = cfgutil.NormalizeAddresses(
		cfg.LegacyRPCListeners, activeNet.RPCServerPort)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Invalid network address in legacy RPC listeners: %v\n", err)
		return loadConfigError(err)
	}
	cfg.ExperimentalRPCListeners, err = cfgutil.NormalizeAddresses(
		cfg.ExperimentalRPCListeners, activeNet.RPCServerPort)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Invalid network address in RPC listeners: %v\n", err)
		return loadConfigError(err)
	}

	// Both RPC servers may not listen on the same interface/port.
	if len(cfg.LegacyRPCListeners) > 0 && len(cfg.ExperimentalRPCListeners) > 0 {
		seenAddresses := make(map[string]struct{}, len(cfg.LegacyRPCListeners))
		for _, addr := range cfg.LegacyRPCListeners {
			seenAddresses[addr] = struct{}{}
		}
		for _, addr := range cfg.ExperimentalRPCListeners {
			_, seen := seenAddresses[addr]
			if seen {
				err := fmt.Errorf("Address `%s` may not be "+
					"used as a listener address for both "+
					"RPC servers", addr)
				fmt.Fprintln(os.Stderr, err)
				return loadConfigError(err)
			}
		}
	}

	// Only allow server TLS to be disabled if the RPC server is bound to
	// localhost addresses.
	if cfg.DisableServerTLS {
		allListeners := append(cfg.LegacyRPCListeners,
			cfg.ExperimentalRPCListeners...)
		for _, addr := range allListeners {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				str := "%s: RPC listen interface '%s' is " +
					"invalid: %v"
				err := fmt.Errorf(str, funcName, addr, err)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return loadConfigError(err)
			}
			if _, ok := localhostListeners[host]; !ok {
				str := "%s: the --noservertls option may not be used " +
					"when binding RPC to non localhost " +
					"addresses: %s"
				err := fmt.Errorf(str, funcName, addr)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return loadConfigError(err)
			}
		}
	}

	// Expand environment variable and leading ~ for filepaths.
	cfg.CAFile = cleanAndExpandPath(cfg.CAFile)
	cfg.RPCCert = cleanAndExpandPath(cfg.RPCCert)
	cfg.RPCKey = cleanAndExpandPath(cfg.RPCKey)

	// If the dcrd username or password are unset, use the same auth as for
	// the client.  The two settings were previously shared for dcrd and
	// client auth, so this avoids breaking backwards compatibility while
	// allowing users to use different auth settings for dcrd and wallet.
	if cfg.DcrdUsername == "" {
		cfg.DcrdUsername = cfg.Username
	}
	if cfg.DcrdPassword == "" {
		cfg.DcrdPassword = cfg.Password
	}

	return &cfg, remainingArgs, nil
}
