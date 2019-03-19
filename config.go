// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"decred.org/dcrwallet/internal/cfgutil"
	"decred.org/dcrwallet/internal/netparams"
	"github.com/decred/dcrd/connmgr"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/version"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/decred/go-socks/socks"
	"github.com/decred/slog"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultCAFilename              = "dcrd.cert"
	defaultConfigFilename          = "dcrwallet.conf"
	defaultLogLevel                = "info"
	defaultLogDirname              = "logs"
	defaultLogFilename             = "dcrwallet.log"
	defaultRPCMaxClients           = 10
	defaultRPCMaxWebsockets        = 25
	defaultEnableTicketBuyer       = false
	defaultEnableVoting            = false
	defaultReuseAddresses          = false
	defaultPurchaseAccount         = "default"
	defaultPromptPass              = false
	defaultPass                    = ""
	defaultPromptPublicPass        = false
	defaultGapLimit                = wallet.DefaultGapLimit
	defaultStakePoolColdExtKey     = ""
	defaultAllowHighFees           = false
	defaultAccountGapLimit         = wallet.DefaultAccountGapLimit
	defaultDisableCoinTypeUpgrades = false
	defaultCircuitLimit            = 32

	// ticket buyer options
	defaultBalanceToMaintainAbsolute = 0

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
	ConfigFile         *cfgutil.ExplicitString `short:"C" long:"configfile" description:"Path to configuration file"`
	ShowVersion        bool                    `short:"V" long:"version" description:"Display version information and exit"`
	Create             bool                    `long:"create" description:"Create new wallet"`
	CreateTemp         bool                    `long:"createtemp" description:"Create simulation wallet in nonstandard --appdata; private passphrase is 'password'"`
	CreateWatchingOnly bool                    `long:"createwatchingonly" description:"Create watching wallet from account extended pubkey"`
	AppDataDir         *cfgutil.ExplicitString `short:"A" long:"appdata" description:"Application data directory for wallet config, databases and logs"`
	TestNet            bool                    `long:"testnet" description:"Use the test network"`
	SimNet             bool                    `long:"simnet" description:"Use the simulation test network"`
	NoInitialLoad      bool                    `long:"noinitialload" description:"Defer wallet creation/opening on startup and enable loading wallets over RPC"`
	DebugLevel         string                  `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}"`
	LogDir             *cfgutil.ExplicitString `long:"logdir" description:"Directory to log output."`
	Profile            []string                `long:"profile" description:"Enable HTTP profiling this interface/port"`
	MemProfile         string                  `long:"memprofile" description:"Write mem profile to the specified file"`

	// Wallet options
	WalletPass              string               `long:"walletpass" default-mask:"-" description:"Public wallet password; required when created with one"`
	PromptPass              bool                 `long:"promptpass" description:"Prompt for private passphase from terminal and unlock without timeout"`
	Pass                    string               `long:"pass" description:"Unlock with private passphrase"`
	PromptPublicPass        bool                 `long:"promptpublicpass" description:"Prompt for public passphrase from terminal"`
	EnableTicketBuyer       bool                 `long:"enableticketbuyer" description:"Enable the automatic ticket buyer"`
	EnableVoting            bool                 `long:"enablevoting" description:"Automatically create votes and revocations"`
	PurchaseAccount         string               `long:"purchaseaccount" description:"Account to autobuy tickets from"`
	PoolAddress             *cfgutil.AddressFlag `long:"pooladdress" description:"VSP fee address"`
	poolAddress             dcrutil.Address
	PoolFees                float64             `long:"poolfees" description:"VSP fee percentage (1.00 equals 1.00% fee)"`
	GapLimit                int                 `long:"gaplimit" description:"Allowed unused address gap between used addresses of accounts"`
	StakePoolColdExtKey     string              `long:"stakepoolcoldextkey" description:"xpub:maxindex for fee addresses (VSP-only option)"`
	AllowHighFees           bool                `long:"allowhighfees" description:"Do not perform high fee checks"`
	RelayFee                *cfgutil.AmountFlag `long:"txfee" description:"Transaction fee per kilobyte"`
	AccountGapLimit         int                 `long:"accountgaplimit" description:"Allowed gap of unused accounts"`
	DisableCoinTypeUpgrades bool                `long:"disablecointypeupgrades" description:"Never upgrade from legacy to SLIP0044 coin type keys"`

	// RPC client options
	RPCConnect       string                  `short:"c" long:"rpcconnect" description:"Network address of dcrd RPC server"`
	CAFile           *cfgutil.ExplicitString `long:"cafile" description:"dcrd RPC Certificate Authority"`
	DisableClientTLS bool                    `long:"noclienttls" description:"Disable TLS for dcrd RPC; only allowed when connecting to localhost"`
	DcrdUsername     string                  `long:"dcrdusername" description:"dcrd RPC username; overrides --username"`
	DcrdPassword     string                  `long:"dcrdpassword" default-mask:"-" description:"dcrd RPC password; overrides --password"`

	// Proxy and Tor settings
	Proxy        string `long:"proxy" description:"Establish network connections and DNS lookups through a SOCKS5 proxy (e.g. 127.0.0.1:9050)"`
	ProxyUser    string `long:"proxyuser" description:"Proxy server username"`
	ProxyPass    string `long:"proxypass" default-mask:"-" description:"Proxy server password"`
	CircuitLimit int    `long:"circuitlimit" description:"Set maximum number of open Tor circuits; used only when --torisolation is enabled"`
	TorIsolation bool   `long:"torisolation" description:"Enable Tor stream isolation by randomizing user credentials for each connection"`
	dial         func(ctx context.Context, network, address string) (net.Conn, error)
	lookup       func(name string) ([]net.IP, error)

	// SPV options
	SPV        bool     `long:"spv" description:"Sync using simplified payment verification"`
	SPVConnect []string `long:"spvconnect" description:"SPV sync only with specified peers; disables DNS seeding"`

	// RPC server options
	RPCCert                *cfgutil.ExplicitString `long:"rpccert" description:"RPC server TLS certificate"`
	RPCKey                 *cfgutil.ExplicitString `long:"rpckey" description:"RPC server TLS key"`
	TLSCurve               *cfgutil.CurveFlag      `long:"tlscurve" description:"Curve to use when generating TLS keypairs"`
	OneTimeTLSKey          bool                    `long:"onetimetlskey" description:"Generate self-signed TLS keypairs each startup; only write certificate file"`
	DisableServerTLS       bool                    `long:"noservertls" description:"Disable TLS for the RPC servers; only allowed when binding to localhost"`
	GRPCListeners          []string                `long:"grpclisten" description:"Listen for gRPC connections on this interface"`
	LegacyRPCListeners     []string                `long:"rpclisten" description:"Listen for JSON-RPC connections on this interface"`
	NoGRPC                 bool                    `long:"nogrpc" description:"Disable gRPC server"`
	NoLegacyRPC            bool                    `long:"nolegacyrpc" description:"Disable JSON-RPC server"`
	LegacyRPCMaxClients    int64                   `long:"rpcmaxclients" description:"Max JSON-RPC HTTP POST clients"`
	LegacyRPCMaxWebsockets int64                   `long:"rpcmaxwebsockets" description:"Max JSON-RPC websocket clients"`
	Username               string                  `short:"u" long:"username" description:"JSON-RPC username and default dcrd RPC username"`
	Password               string                  `short:"P" long:"password" default-mask:"-" description:"JSON-RPC password and default dcrd RPC password"`

	// IPC options
	PipeTx            *uint `long:"pipetx" description:"File descriptor or handle of write end pipe to enable child -> parent process communication"`
	PipeRx            *uint `long:"piperx" description:"File descriptor or handle of read end pipe to enable parent -> child process communication"`
	RPCListenerEvents bool  `long:"rpclistenerevents" description:"Notify JSON-RPC and gRPC listener addresses over the TX pipe"`

	// CSPP
	CSPPServer         string `long:"csppserver" description:"Network address of CoinShuffle++ server"`
	CSPPServerCA       string `long:"csppserver.ca" description:"CoinShuffle++ Certificate Authority"`
	dialCSPPServer     func(ctx context.Context, network, addr string) (net.Conn, error)
	MixedAccount       string `long:"mixedaccount" description:"Account/branch used to derive CoinShuffle++ mixed outputs and voting rewards"`
	mixedAccount       string
	mixedBranch        uint32
	TicketSplitAccount string `long:"ticketsplitaccount" description:"Account to derive fresh addresses from for mixed ticket splits; uses mixedaccount if unset"`
	ChangeAccount      string `long:"changeaccount" description:"Account used to derive unmixed CoinJoin outputs in CoinShuffle++ protocol"`
	MixChange          bool   `long:"mixchange" description:"Use CoinShuffle++ to mix change account outputs into mix account"`

	TBOpts ticketBuyerOptions `group:"Ticket Buyer Options" namespace:"ticketbuyer"`

	// Deprecated options
	ReuseAddresses bool `long:"reuseaddresses" description:"DEPRECATED -- Reuse addresses for ticket purchase to cut down on address overuse"`
	DisallowFree   bool `long:"disallowfree" description:"DEPRECATED -- Force transactions to always include a fee"`
}

type ticketBuyerOptions struct {
	BalanceToMaintainAbsolute *cfgutil.AmountFlag  `long:"balancetomaintainabsolute" description:"Amount of funds to keep in wallet when purchasing tickets"`
	VotingAddress             *cfgutil.AddressFlag `long:"votingaddress" description:"Purchase tickets with voting rights assigned to this address"`
	votingAddress             dcrutil.Address
	Limit                     uint   `long:"limit" description:"Buy no more than specified number of tickets per block (0 disables limit)"`
	VotingAccount             string `long:"votingaccount" description:"Account used to derive addresses specifying voting rights"`
}

// cleanAndExpandPath expands environement variables and leading ~ in the
// passed path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Do not try to clean the empty string
	if path == "" {
		return ""
	}

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
	_, ok := slog.LevelFromString(logLevel)
	return ok
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
			return errors.Errorf(str, debugLevel)
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
			return errors.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate subsystem.
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "The specified subsystem [%v] is invalid -- " +
				"supported subsytems %v"
			return errors.Errorf(str, subsysID, supportedSubsystems())
		}

		// Validate log level.
		if !validLogLevel(logLevel) {
			str := "The specified debug level [%v] is invalid"
			return errors.Errorf(str, logLevel)
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
func loadConfig(ctx context.Context) (*config, []string, error) {
	loadConfigError := func(err error) (*config, []string, error) {
		return nil, nil, err
	}

	// Default config.
	cfg := config{
		DebugLevel:              defaultLogLevel,
		ConfigFile:              cfgutil.NewExplicitString(defaultConfigFile),
		AppDataDir:              cfgutil.NewExplicitString(defaultAppDataDir),
		LogDir:                  cfgutil.NewExplicitString(defaultLogDir),
		WalletPass:              wallet.InsecurePubPassphrase,
		CAFile:                  cfgutil.NewExplicitString(""),
		dial:                    new(net.Dialer).DialContext,
		lookup:                  net.LookupIP,
		PromptPass:              defaultPromptPass,
		Pass:                    defaultPass,
		PromptPublicPass:        defaultPromptPublicPass,
		RPCKey:                  cfgutil.NewExplicitString(defaultRPCKeyFile),
		RPCCert:                 cfgutil.NewExplicitString(defaultRPCCertFile),
		TLSCurve:                cfgutil.NewCurveFlag(cfgutil.PreferredCurve),
		LegacyRPCMaxClients:     defaultRPCMaxClients,
		LegacyRPCMaxWebsockets:  defaultRPCMaxWebsockets,
		EnableTicketBuyer:       defaultEnableTicketBuyer,
		EnableVoting:            defaultEnableVoting,
		ReuseAddresses:          defaultReuseAddresses,
		PurchaseAccount:         defaultPurchaseAccount,
		GapLimit:                defaultGapLimit,
		StakePoolColdExtKey:     defaultStakePoolColdExtKey,
		AllowHighFees:           defaultAllowHighFees,
		RelayFee:                cfgutil.NewAmountFlag(txrules.DefaultRelayFeePerKb),
		PoolAddress:             cfgutil.NewAddressFlag(),
		AccountGapLimit:         defaultAccountGapLimit,
		DisableCoinTypeUpgrades: defaultDisableCoinTypeUpgrades,
		CircuitLimit:            defaultCircuitLimit,

		// Ticket Buyer Options
		TBOpts: ticketBuyerOptions{
			BalanceToMaintainAbsolute: cfgutil.NewAmountFlag(defaultBalanceToMaintainAbsolute),
			VotingAddress:             cfgutil.NewAddressFlag(),
		},
	}

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.
	preCfg := cfg
	preParser := flags.NewParser(&preCfg, flags.Default)
	_, err := preParser.Parse()
	if err != nil {
		var e *flags.Error
		if errors.As(err, &e) && e.Type == flags.ErrHelp {
			os.Exit(0)
		}
		preParser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	// Show the version and exit if the version flag was specified.
	funcName := "loadConfig"
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	usageMessage := fmt.Sprintf("Use %s -h to show usage", appName)
	if preCfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n", appName,
			version.String(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Load additional config from file.
	var configFileError error
	parser := flags.NewParser(&cfg, flags.Default)
	configFilePath := preCfg.ConfigFile.Value
	if preCfg.ConfigFile.ExplicitlySet() {
		configFilePath = cleanAndExpandPath(configFilePath)
	} else {
		appDataDir := preCfg.AppDataDir.Value
		if appDataDir != defaultAppDataDir {
			configFilePath = filepath.Join(appDataDir, defaultConfigFilename)
		}
	}
	err = flags.NewIniParser(parser).ParseFile(configFilePath)
	if err != nil {
		var e *os.PathError
		if !errors.As(err, &e) {
			fmt.Fprintln(os.Stderr, err)
			parser.WriteHelp(os.Stderr)
			return loadConfigError(err)
		}
		configFileError = err
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		var e *flags.Error
		if !errors.As(err, &e) || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return loadConfigError(err)
	}

	// If an alternate data directory was specified, and paths with defaults
	// relative to the data dir are unchanged, modify each path to be
	// relative to the new data dir.
	if cfg.AppDataDir.ExplicitlySet() {
		cfg.AppDataDir.Value = cleanAndExpandPath(cfg.AppDataDir.Value)
		if !cfg.RPCKey.ExplicitlySet() {
			cfg.RPCKey.Value = filepath.Join(cfg.AppDataDir.Value, "rpc.key")
		}
		if !cfg.RPCCert.ExplicitlySet() {
			cfg.RPCCert.Value = filepath.Join(cfg.AppDataDir.Value, "rpc.cert")
		}
		if !cfg.LogDir.ExplicitlySet() {
			cfg.LogDir.Value = filepath.Join(cfg.AppDataDir.Value, defaultLogDirname)
		}
	}

	// Choose the active network params based on the selected network.
	// Multiple networks can't be selected simultaneously.
	numNets := 0
	if cfg.TestNet {
		activeNet = &netparams.TestNet3Params
		numNets++
	}
	if cfg.SimNet {
		activeNet = &netparams.SimNetParams
		numNets++
	}
	if numNets > 1 {
		str := "%s: The testnet and simnet params can't be used " +
			"together -- choose one"
		err := errors.Errorf(str, "loadConfig")
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Append the network type to the log directory so it is "namespaced"
	// per network.
	cfg.LogDir.Value = cleanAndExpandPath(cfg.LogDir.Value)
	cfg.LogDir.Value = filepath.Join(cfg.LogDir.Value, activeNet.Params.Name)

	// Special show command to list supported subsystems and exit.
	if cfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// Initialize log rotation.  After log rotation has been initialized, the
	// logger variables may be used.
	initLogRotator(filepath.Join(cfg.LogDir.Value, defaultLogFilename))

	// Check that no addresses were created for the wrong network
	for _, a := range []struct {
		flag *cfgutil.AddressFlag
		addr *dcrutil.Address
	}{
		{cfg.PoolAddress, &cfg.poolAddress},
		{cfg.TBOpts.VotingAddress, &cfg.TBOpts.votingAddress},
	} {
		addr, err := a.flag.Address(activeNet.Params)
		if err != nil {
			log.Error(err)
			return loadConfigError(err)
		}
		*a.addr = addr
	}

	// Parse, validate, and set debug log level(s).
	if err := parseAndSetDebugLevels(cfg.DebugLevel); err != nil {
		err := errors.Errorf("%s: %v", "loadConfig", err.Error())
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	// Error and shutdown if config file is specified on the command line
	// but cannot be found.
	if configFileError != nil && cfg.ConfigFile.ExplicitlySet() {
		if preCfg.ConfigFile.ExplicitlySet() || cfg.ConfigFile.ExplicitlySet() {
			log.Errorf("%v", configFileError)
			return loadConfigError(configFileError)
		}
	}

	// Warn about missing config file after the final command line parse
	// succeeds.  This prevents the warning on help messages and invalid
	// options.
	if configFileError != nil {
		log.Warnf("%v", configFileError)
	}

	// Check deprecated options.  The new options receive priority when both
	// are changed from the default.
	if cfg.ReuseAddresses {
		fmt.Fprintln(os.Stderr, "reuseaddresses option is deprecated; please "+
			"remove from your config")
	}
	if cfg.DisallowFree {
		fmt.Fprintln(os.Stderr, "disallowfree option is deprecated; please "+
			"remove from your config")
	}

	// Sanity check BalanceToMaintainAbsolute
	if cfg.TBOpts.BalanceToMaintainAbsolute.ToCoin() < 0 {
		str := "%s: balancetomaintainabsolute cannot be negative: %v"
		err := errors.Errorf(str, funcName, cfg.TBOpts.BalanceToMaintainAbsolute)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Exit if you try to use a simulation wallet with a standard
	// data directory.
	if !cfg.AppDataDir.ExplicitlySet() && cfg.CreateTemp {
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

	// Ensure the wallet exists or create it when the create flag is set.
	netDir := networkDir(cfg.AppDataDir.Value, activeNet.Params)
	dbPath := filepath.Join(netDir, walletDbName)

	if cfg.CreateTemp && cfg.Create {
		err := errors.Errorf("The flags --create and --createtemp can not " +
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
			if err := createSimulationWallet(ctx, &cfg); err != nil {
				fmt.Fprintln(os.Stderr, "Unable to create wallet:", err)
				return loadConfigError(err)
			}
		}
	} else if cfg.Create || cfg.CreateWatchingOnly {
		// Error if the create flag is set and the wallet already
		// exists.
		if dbFileExists {
			err := errors.Errorf("The wallet database file `%v` "+
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
		os.Stdout.Sync()
		if cfg.CreateWatchingOnly {
			err = createWatchingOnlyWallet(ctx, &cfg)
		} else {
			err = createWallet(ctx, &cfg)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Unable to create wallet:", err)
			return loadConfigError(err)
		}

		// Created successfully, so exit now with success.
		os.Exit(0)
	} else if !dbFileExists && !cfg.NoInitialLoad {
		err := errors.Errorf("The wallet does not exist.  Run with the " +
			"--create option to initialize and create it.")
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	if cfg.PoolFees != 0.0 {
		if !txrules.ValidPoolFeeRate(cfg.PoolFees) {
			err := errors.E(errors.Invalid, errors.Errorf("pool fee rate %v", cfg.PoolFees))
			fmt.Fprintln(os.Stderr, err.Error())
			fmt.Fprintln(os.Stderr, usageMessage)
			return loadConfigError(err)
		}
	}

	ipNet := func(cidr string) net.IPNet {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		return *ipNet
	}
	privNets := []net.IPNet{
		// IPv4 loopback
		ipNet("127.0.0.0/8"),

		// IPv6 loopback
		ipNet("::1/128"),

		// RFC 1918
		ipNet("10.0.0.0/8"),
		ipNet("172.16.0.0/12"),
		ipNet("192.168.0.0/16"),

		// RFC 4193
		ipNet("fc00::/7"),
	}

	// Set dialer and DNS lookup functions if proxy settings are provided.
	if cfg.Proxy != "" {
		proxy := socks.Proxy{
			Addr:         cfg.Proxy,
			Username:     cfg.ProxyUser,
			Password:     cfg.ProxyPass,
			TorIsolation: cfg.TorIsolation,
		}

		var proxyDialer func(context.Context, string, string) (net.Conn, error)
		var noproxyDialer net.Dialer
		if cfg.TorIsolation {
			proxyDialer = socks.NewPool(proxy, uint32(cfg.CircuitLimit)).DialContext
		} else {
			proxyDialer = proxy.DialContext
		}

		cfg.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if len(ip) == 4 || len(ip) == 16 {
				for i := range privNets {
					if privNets[i].Contains(ip) {
						return noproxyDialer.DialContext(ctx, network, address)
					}
				}
			}
			conn, err := proxyDialer(ctx, network, address)
			if err != nil {
				return nil, errors.Errorf("proxy dial %v %v: %w", network, address, err)
			}
			return conn, nil
		}
		cfg.lookup = func(host string) ([]net.IP, error) {
			ip, err := connmgr.TorLookupIP(host, cfg.Proxy)
			if err != nil {
				return nil, errors.Errorf("proxy lookup for %v: %w", host, err)
			}
			return ip, nil
		}
	}

	// Create CoinShuffle++ TLS dialer based on server name and certificate
	// authority settings.
	csppTLSConfig := new(tls.Config)
	if cfg.CSPPServer != "" {
		csppTLSConfig.ServerName, _, err = net.SplitHostPort(cfg.CSPPServer)
		if err != nil {
			err := errors.Errorf("Cannot parse CoinShuffle++ "+
				"server name %q: %v", cfg.CSPPServer, err)
			fmt.Fprintln(os.Stderr, err.Error())
			return loadConfigError(err)
		}
	}
	if cfg.CSPPServerCA != "" {
		ca, err := ioutil.ReadFile(cfg.CSPPServerCA)
		if err != nil {
			err := errors.Errorf("Cannot read CoinShuffle++ "+
				"Certificate Authority file: %v", err)
			fmt.Fprintln(os.Stderr, err.Error())
			return loadConfigError(err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		csppTLSConfig.RootCAs = pool
	}
	cfg.dialCSPPServer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := cfg.dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		conn = tls.Client(conn, csppTLSConfig)
		return conn, nil
	}

	// Parse mixedaccount account/branch
	if cfg.MixedAccount != "" {
		indexSlash := strings.LastIndex(cfg.MixedAccount, "/")
		if indexSlash == -1 {
			err := errors.Errorf("--mixedaccount must have form 'accountname/branch'")
			fmt.Fprintln(os.Stderr, err)
			return loadConfigError(err)
		}
		cfg.mixedAccount = cfg.MixedAccount[:indexSlash]
		switch cfg.MixedAccount[indexSlash+1:] {
		case "0":
			cfg.mixedBranch = 0
		case "1":
			cfg.mixedBranch = 1
		default:
			err := errors.Errorf("--mixedaccount branch must be 0 or 1")
			fmt.Fprintln(os.Stderr, err)
			return loadConfigError(err)
		}
	}
	// Use mixedaccount as default ticketsplitaccount if unset.
	if cfg.TicketSplitAccount == "" {
		cfg.TicketSplitAccount = cfg.mixedAccount
	}

	if cfg.RPCConnect == "" {
		cfg.RPCConnect = net.JoinHostPort("localhost", activeNet.JSONRPCClientPort)
	}

	// Add default port to connect flag if missing.
	cfg.RPCConnect, err = cfgutil.NormalizeAddress(cfg.RPCConnect,
		activeNet.JSONRPCClientPort)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Invalid rpcconnect network address: %v\n", err)
		return loadConfigError(err)
	}

	localhostListeners := map[string]struct{}{
		"localhost": {},
		"127.0.0.1": {},
		"::1":       {},
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
			err := errors.Errorf(str, funcName, cfg.RPCConnect)
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, usageMessage)
			return loadConfigError(err)
		}
	} else {
		// If CAFile is unset, choose either the copy or local dcrd cert.
		if !cfg.CAFile.ExplicitlySet() {
			cfg.CAFile.Value = filepath.Join(cfg.AppDataDir.Value, defaultCAFilename)

			// If the CA copy does not exist, check if we're connecting to
			// a local dcrd and switch to its RPC cert if it exists.
			certExists, err := cfgutil.FileExists(cfg.CAFile.Value)
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
						cfg.CAFile.Value = dcrdDefaultCAFile
					}
				}
			}
		}
	}

	if cfg.SPV && cfg.EnableVoting {
		err := errors.E("SPV voting is not possible: disable --spv or --enablevoting")
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}
	if !cfg.SPV && len(cfg.SPVConnect) > 0 {
		err := errors.E("--spvconnect requires --spv")
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}
	for i, p := range cfg.SPVConnect {
		cfg.SPVConnect[i], err = cfgutil.NormalizeAddress(p, activeNet.Params.DefaultPort)
		if err != nil {
			return loadConfigError(err)
		}
	}

	// Default to localhost listen addresses if no listeners were manually
	// specified.  When the RPC server is configured to be disabled, remove all
	// listeners so it is not started.
	localhostAddrs, err := net.LookupHost("localhost")
	if err != nil {
		return loadConfigError(err)
	}
	if len(cfg.GRPCListeners) == 0 && !cfg.NoGRPC {
		cfg.GRPCListeners = make([]string, 0, len(localhostAddrs))
		for _, addr := range localhostAddrs {
			cfg.GRPCListeners = append(cfg.GRPCListeners,
				net.JoinHostPort(addr, activeNet.GRPCServerPort))
		}
	} else if cfg.NoGRPC {
		cfg.GRPCListeners = nil
	}
	if len(cfg.LegacyRPCListeners) == 0 && !cfg.NoLegacyRPC {
		cfg.LegacyRPCListeners = make([]string, 0, len(localhostAddrs))
		for _, addr := range localhostAddrs {
			cfg.LegacyRPCListeners = append(cfg.LegacyRPCListeners,
				net.JoinHostPort(addr, activeNet.JSONRPCServerPort))
		}
	} else if cfg.NoLegacyRPC {
		cfg.LegacyRPCListeners = nil
	}

	// Add default port to all rpc listener addresses if needed and remove
	// duplicate addresses.
	cfg.LegacyRPCListeners, err = cfgutil.NormalizeAddresses(
		cfg.LegacyRPCListeners, activeNet.JSONRPCServerPort)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Invalid network address in legacy RPC listeners: %v\n", err)
		return loadConfigError(err)
	}
	cfg.GRPCListeners, err = cfgutil.NormalizeAddresses(
		cfg.GRPCListeners, activeNet.GRPCServerPort)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Invalid network address in RPC listeners: %v\n", err)
		return loadConfigError(err)
	}

	// Both RPC servers may not listen on the same interface/port, with the
	// exception of listeners using port 0.
	if len(cfg.LegacyRPCListeners) > 0 && len(cfg.GRPCListeners) > 0 {
		seenAddresses := make(map[string]struct{}, len(cfg.LegacyRPCListeners))
		for _, addr := range cfg.LegacyRPCListeners {
			seenAddresses[addr] = struct{}{}
		}
		for _, addr := range cfg.GRPCListeners {
			_, seen := seenAddresses[addr]
			if seen && !strings.HasSuffix(addr, ":0") {
				err := errors.Errorf("Address `%s` may not be "+
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
		allListeners := append(cfg.LegacyRPCListeners, cfg.GRPCListeners...)
		for _, addr := range allListeners {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				str := "%s: RPC listen interface '%s' is " +
					"invalid: %v"
				err := errors.Errorf(str, funcName, addr, err)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return loadConfigError(err)
			}
			if _, ok := localhostListeners[host]; !ok {
				str := "%s: the --noservertls option may not be used " +
					"when binding RPC to non localhost " +
					"addresses: %s"
				err := errors.Errorf(str, funcName, addr)
				fmt.Fprintln(os.Stderr, err)
				fmt.Fprintln(os.Stderr, usageMessage)
				return loadConfigError(err)
			}
		}
	}

	// Expand environment variable and leading ~ for filepaths.
	cfg.CAFile.Value = cleanAndExpandPath(cfg.CAFile.Value)
	cfg.RPCCert.Value = cleanAndExpandPath(cfg.RPCCert.Value)
	cfg.RPCKey.Value = cleanAndExpandPath(cfg.RPCKey.Value)

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

	// Warn if user still has an old ticket buyer configuration file.
	oldTBConfigFile := filepath.Join(cfg.AppDataDir.Value, "ticketbuyer.conf")
	if _, err := os.Stat(oldTBConfigFile); err == nil {
		log.Warnf("%s is no longer used and should be removed. "+
			"Please prepend 'ticketbuyer.' to each option and "+
			"move it under the [Ticket Buyer Options] section "+
			"of %s\n",
			oldTBConfigFile, configFilePath)
	}

	// Make list of old versions of testnet directories.
	var oldTestNets []string
	oldTestNets = append(oldTestNets, filepath.Join(cfg.AppDataDir.Value, "testnet"))
	// Warn if old testnet directory is present.
	for _, oldDir := range oldTestNets {
		oldDirExists, _ := cfgutil.FileExists(oldDir)
		if oldDirExists {
			log.Warnf("Wallet data from previous testnet"+
				" found (%v) and can probably be removed.",
				oldDir)
		}
	}

	return &cfg, remainingArgs, nil
}
