// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
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
	"github.com/decred/dcrwallet/ticketbuyer"
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
	defaultEnableTicketBuyer   = false
	defaultEnableVoting        = false
	defaultReuseAddresses      = false
	defaultRollbackTest        = false
	defaultPruneTickets        = false
	defaultPurchaseAccount     = "default"
	defaultAutomaticRepair     = false
	defaultPromptPass          = false
	defaultPromptPublicPass    = false
	defaultAddrIdxScanLen      = wallet.DefaultGapLimit
	defaultStakePoolColdExtKey = ""
	defaultAllowHighFees       = false

	// ticket buyer options
	defaultMaxFee                    dcrutil.Amount = 1e7
	defaultMinFee                    dcrutil.Amount = 1e6
	defaultMaxPriceScale                            = 0.0
	defaultAvgVWAPPriceDelta                        = 2880
	defaultMaxPerBlock                              = 5
	defaultBlocksToAvg                              = 11
	defaultFeeTargetScaling                         = 1.0
	defaultMaxInMempool                             = 40
	defaultExpiryDelta                              = 16
	defaultFeeSource                                = ticketbuyer.TicketFeeMedian
	defaultAvgPriceMode                             = ticketbuyer.PriceTargetVWAP
	defaultMaxPriceAbsolute                         = 0
	defaultMaxPriceRelative                         = 1.25
	defaultPriceTarget                              = 0
	defaultBalanceToMaintainAbsolute                = 0
	defaultBalanceToMaintainRelative                = 0.3

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
	TestNet            bool     `long:"testnet" description:"Use the test network"`
	SimNet             bool     `long:"simnet" description:"Use the simulation test network"`
	NoInitialLoad      bool     `long:"noinitialload" description:"Defer wallet creation/opening on startup and enable loading wallets over RPC"`
	DebugLevel         string   `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}"`
	LogDir             string   `long:"logdir" description:"Directory to log output."`
	Profile            []string `long:"profile" description:"Enable HTTP profiling this interface/port"`
	MemProfile         string   `long:"memprofile" description:"Write mem profile to the specified file"`
	RollbackTest       bool     `long:"rollbacktest" description:"Rollback testing is a simnet testing mode that eventually stops wallet and examines wtxmgr database integrity"`
	AutomaticRepair    bool     `long:"automaticrepair" description:"Attempt to repair the wallet automatically if a database inconsistency is found"`

	// Wallet options
	WalletPass          string              `long:"walletpass" default-mask:"-" description:"The public wallet password -- Only required if the wallet was created with one"`
	PromptPass          bool                `long:"promptpass" description:"The private wallet password is prompted for at start up, so the wallet starts unlocked without a time limit"`
	PromptPublicPass    bool                `long:"promptpublicpass" description:"The public wallet password is prompted for at start up"`
	DisallowFree        bool                `long:"disallowfree" description:"Force transactions to always include a fee"`
	EnableTicketBuyer   bool                `long:"enableticketbuyer" description:"Enable the automatic ticket buyer"`
	EnableVoting        bool                `long:"enablevoting" description:"Enable creation of votes and revocations for owned tickets"`
	ReuseAddresses      bool                `long:"reuseaddresses" description:"Reuse addresses for ticket purchase to cut down on address overuse"`
	PruneTickets        bool                `long:"prunetickets" description:"Prune old tickets from the wallet and restore their inputs"`
	PurchaseAccount     string              `long:"purchaseaccount" description:"Name of the account to buy tickets from"`
	TicketAddress       string              `long:"ticketaddress" description:"Send all ticket outputs to this address (P2PKH or P2SH only)"`
	PoolAddress         string              `long:"pooladdress" description:"The ticket pool address where ticket fees will go to"`
	PoolFees            float64             `long:"poolfees" description:"The per-ticket fee mandated by the ticket pool as a percent (e.g. 1.00 for 1.00% fee)"`
	AddrIdxScanLen      int                 `long:"addridxscanlen" description:"The width of the scan for last used addresses on wallet restore and start up"`
	StakePoolColdExtKey string              `long:"stakepoolcoldextkey" description:"Enables the wallet as a stake pool with an extended key in the format of \"xpub...:index\" to derive cold wallet addresses to send fees to"`
	AllowHighFees       bool                `long:"allowhighfees" description:"Force the RPC client to use the 'allowHighFees' flag when sending transactions"`
	RelayFee            *cfgutil.AmountFlag `long:"txfee" description:"Sets the wallet's tx fee per kb"`
	TicketFee           *cfgutil.AmountFlag `long:"ticketfee" description:"Sets the wallet's ticket fee per kb"`
	PipeRx              *uint               `long:"piperx" description:"File descriptor of read end pipe to enable parent -> child process communication"`

	// RPC client options
	RPCConnect       string `short:"c" long:"rpcconnect" description:"Hostname/IP and port of dcrd RPC server to connect to"`
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
	LegacyRPCListeners     []string           `long:"rpclisten" description:"Listen for legacy RPC connections on this interface/port"`
	LegacyRPCMaxClients    int64              `long:"rpcmaxclients" description:"Max number of legacy RPC clients for standard connections"`
	LegacyRPCMaxWebsockets int64              `long:"rpcmaxwebsockets" description:"Max number of legacy RPC websocket connections"`
	Username               string             `short:"u" long:"username" description:"Username for legacy RPC and dcrd authentication (if dcrdusername is unset)"`
	Password               string             `short:"P" long:"password" default-mask:"-" description:"Password for legacy RPC and dcrd authentication (if dcrdpassword is unset)"`

	TBOpts ticketBuyerOptions `group:"Ticket Buyer Options" namespace:"ticketbuyer"`
	tbCfg  ticketbuyer.Config

	// EXPERIMENTAL RPC server options
	//
	// These options will change (and require changes to config files, etc.)
	// when the new gRPC server is enabled.
	ExperimentalRPCListeners []string `long:"experimentalrpclisten" description:"Listen for RPC connections on this interface/port"`

	// Deprecated options
	DataDir string `short:"b" long:"datadir" default-mask:"-" description:"DEPRECATED -- use appdata instead"`
}

type ticketBuyerOptions struct {
	AvgPriceMode              string              `long:"avgpricemode" description:"The mode to use for calculating the average price if pricetarget is disabled (vwap, pool, dual)"`
	AvgPriceVWAPDelta         int                 `long:"avgpricevwapdelta" description:"The number of blocks to use from the current block to calculate the VWAP"`
	MaxFee                    *cfgutil.AmountFlag `long:"maxfee" description:"Maximum ticket fee per KB"`
	MinFee                    *cfgutil.AmountFlag `long:"minfee" description:"Minimum ticket fee per KB"`
	FeeSource                 string              `long:"feesource" description:"The fee source to use for ticket fee per KB (median or mean)"`
	MaxPerBlock               int                 `long:"maxperblock" description:"Maximum tickets per block, with negative numbers indicating buy one ticket every 1-in-n blocks"`
	BlocksToAvg               int                 `long:"blockstoavg" description:"Number of blocks to average for fees calculation"`
	FeeTargetScaling          float64             `long:"feetargetscaling" description:"Scaling factor for setting the ticket fee, multiplies by the average fee"`
	DontWaitForTickets        bool                `long:"dontwaitfortickets" description:"Don't wait until your last round of tickets have entered the blockchain to attempt to purchase more"`
	SpreadTicketPurchases     bool                `long:"spreadticketpurchases" description:"Spread ticket purchases evenly throughout the window"`
	MaxInMempool              int                 `long:"maxinmempool" description:"The maximum number of your tickets allowed in mempool before purchasing more tickets"`
	ExpiryDelta               int                 `long:"expirydelta" description:"Number of blocks in the future before the ticket expires"`
	MaxPriceAbsolute          *cfgutil.AmountFlag `long:"maxpriceabsolute" description:"Maximum absolute price to purchase a ticket"`
	MaxPriceRelative          float64             `long:"maxpricerelative" description:"Scaling factor for setting the maximum price, multiplies by the average price"`
	BalanceToMaintainAbsolute *cfgutil.AmountFlag `long:"balancetomaintainabsolute" description:"Amount of funds to keep in wallet when stake mining"`
	BalanceToMaintainRelative float64             `long:"balancetomaintainrelative" description:"Proportion of funds to leave in wallet when stake mining"`

	// Deprecated options
	MaxPriceScale float64             `long:"maxpricescale" description:"DEPRECATED -- Attempt to prevent the stake difficulty from going above this multiplier (>1.0) by manipulation, 0 to disable"`
	PriceTarget   *cfgutil.AmountFlag `long:"pricetarget" description:"DEPRECATED -- A target to try to seek setting the stake price to rather than meeting the average price, 0 to disable"`
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
		PromptPublicPass:       defaultPromptPublicPass,
		RPCKey:                 defaultRPCKeyFile,
		RPCCert:                defaultRPCCertFile,
		TLSCurve:               cfgutil.NewCurveFlag(cfgutil.CurveP521),
		LegacyRPCMaxClients:    defaultRPCMaxClients,
		LegacyRPCMaxWebsockets: defaultRPCMaxWebsockets,
		EnableTicketBuyer:      defaultEnableTicketBuyer,
		EnableVoting:           defaultEnableVoting,
		ReuseAddresses:         defaultReuseAddresses,
		RollbackTest:           defaultRollbackTest,
		PruneTickets:           defaultPruneTickets,
		PurchaseAccount:        defaultPurchaseAccount,
		AutomaticRepair:        defaultAutomaticRepair,
		AddrIdxScanLen:         defaultAddrIdxScanLen,
		StakePoolColdExtKey:    defaultStakePoolColdExtKey,
		AllowHighFees:          defaultAllowHighFees,
		RelayFee:               cfgutil.NewAmountFlag(txrules.DefaultRelayFeePerKb),
		TicketFee:              cfgutil.NewAmountFlag(txrules.DefaultRelayFeePerKb),

		// TODO: DEPRECATED - remove.
		DataDir: defaultAppDataDir,

		// Ticket Buyer Options
		TBOpts: ticketBuyerOptions{
			MaxPriceScale:             defaultMaxPriceScale,
			AvgPriceMode:              defaultAvgPriceMode,
			AvgPriceVWAPDelta:         defaultAvgVWAPPriceDelta,
			MaxFee:                    cfgutil.NewAmountFlag(defaultMaxFee),
			MinFee:                    cfgutil.NewAmountFlag(defaultMinFee),
			FeeSource:                 defaultFeeSource,
			MaxPerBlock:               defaultMaxPerBlock,
			BlocksToAvg:               defaultBlocksToAvg,
			FeeTargetScaling:          defaultFeeTargetScaling,
			MaxInMempool:              defaultMaxInMempool,
			ExpiryDelta:               defaultExpiryDelta,
			MaxPriceAbsolute:          cfgutil.NewAmountFlag(defaultMaxPriceAbsolute),
			MaxPriceRelative:          defaultMaxPriceRelative,
			PriceTarget:               cfgutil.NewAmountFlag(defaultPriceTarget),
			BalanceToMaintainAbsolute: cfgutil.NewAmountFlag(defaultBalanceToMaintainAbsolute),
			BalanceToMaintainRelative: defaultBalanceToMaintainRelative,
		},
	}

	// Pre-parse the command line options to see if an alternative config
	// file or the version flag was specified.
	preCfg := cfg
	preParser := flags.NewParser(&preCfg, flags.Default)
	_, err := preParser.Parse()
	if err != nil {
		e, ok := err.(*flags.Error)
		if ok && e.Type == flags.ErrHelp {
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
		fmt.Printf("%s version %s (Go version %s)\n", appName, version(), runtime.Version())
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
		activeNet = &netparams.TestNet2Params
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

	// Make sure the fee source type given is valid.
	switch cfg.TBOpts.FeeSource {
	case ticketbuyer.TicketFeeMean:
	case ticketbuyer.TicketFeeMedian:
	default:
		str := "%s: Invalid fee source '%s'"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.FeeSource)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Make sure a valid average price mode is given.
	switch cfg.TBOpts.AvgPriceMode {
	case ticketbuyer.PriceTargetVWAP:
	case ticketbuyer.PriceTargetPool:
	case ticketbuyer.PriceTargetDual:
	default:
		str := "%s: Invalid average price mode '%s'"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.AvgPriceMode)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Sanity check MaxPriceRelative
	if cfg.TBOpts.MaxPriceRelative < 0 {
		str := "%s: maxpricerelative cannot be negative: %v"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.MaxPriceRelative)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Sanity check MinFee and MaxFee
	if cfg.TBOpts.MinFee.ToCoin() > cfg.TBOpts.MaxFee.ToCoin() {
		str := "%s: minfee cannot be higher than maxfee: (min %v, max %v)"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.MinFee, cfg.TBOpts.MaxFee)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}
	if cfg.TBOpts.MaxFee.ToCoin() < 0 {
		str := "%s: maxfee cannot be less than zero: %v"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.MaxFee)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}
	if cfg.TBOpts.MinFee.ToCoin() < 0 {
		str := "%s: minfee cannot be less than zero: %v"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.MinFee)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Sanity check BalanceToMaintainAbsolute
	if cfg.TBOpts.BalanceToMaintainAbsolute.ToCoin() < 0 {
		str := "%s: balancetomaintainabsolute cannot be negative: %v"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.BalanceToMaintainAbsolute)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Sanity check BalanceToMaintainRelative
	if cfg.TBOpts.BalanceToMaintainRelative < 0 {
		str := "%s: balancetomaintainabsolute cannot be negative: %v"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.BalanceToMaintainRelative)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}
	if cfg.TBOpts.BalanceToMaintainRelative > 1 {
		str := "%s: balancetomaintainrelative cannot be greater then 1: %v"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.BalanceToMaintainRelative)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Sanity check ExpiryDelta
	if cfg.TBOpts.ExpiryDelta <= 0 {
		str := "%s: expirydelta must be greater then zero: %v"
		err := fmt.Errorf(str, funcName, cfg.TBOpts.ExpiryDelta)
		fmt.Fprintln(os.Stderr, err)
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
		backendLog.Flush()
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

	// Add default port to connect flag if missing.
	cfg.RPCConnect, err = cfgutil.NormalizeAddress(cfg.RPCConnect,
		activeNet.RPCClientPort)
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

	// Warn if user still has an old ticket buyer configuration file.
	oldTBConfigFile := filepath.Join(cfg.AppDataDir, "ticketbuyer.conf")
	if _, err := os.Stat(oldTBConfigFile); err == nil {
		log.Warnf("%s is no longer used and should be removed. "+
			"Please prepend 'ticketbuyer.' to each option and "+
			"move it under the [Ticket Buyer Options] section "+
			"of %s\n",
			oldTBConfigFile, configFilePath)
	}

	// Build ticketbuyer config
	cfg.tbCfg = ticketbuyer.Config{
		AccountName:               cfg.PurchaseAccount,
		AvgPriceMode:              cfg.TBOpts.AvgPriceMode,
		AvgPriceVWAPDelta:         cfg.TBOpts.AvgPriceVWAPDelta,
		BalanceToMaintainAbsolute: int64(cfg.TBOpts.BalanceToMaintainAbsolute.Amount),
		BalanceToMaintainRelative: cfg.TBOpts.BalanceToMaintainRelative,
		BlocksToAvg:               cfg.TBOpts.BlocksToAvg,
		DontWaitForTickets:        cfg.TBOpts.DontWaitForTickets,
		ExpiryDelta:               cfg.TBOpts.ExpiryDelta,
		FeeSource:                 cfg.TBOpts.FeeSource,
		FeeTargetScaling:          cfg.TBOpts.FeeTargetScaling,
		MinFee:                    int64(cfg.TBOpts.MinFee.Amount),
		MaxFee:                    int64(cfg.TBOpts.MaxFee.Amount),
		MaxPerBlock:               cfg.TBOpts.MaxPerBlock,
		MaxPriceAbsolute:          int64(cfg.TBOpts.MaxPriceAbsolute.Amount),
		MaxPriceRelative:          cfg.TBOpts.MaxPriceRelative,
		MaxInMempool:              cfg.TBOpts.MaxInMempool,
		PoolAddress:               cfg.PoolAddress,
		PoolFees:                  cfg.PoolFees,
		SpreadTicketPurchases:     cfg.TBOpts.SpreadTicketPurchases,
		TicketAddress:             cfg.TicketAddress,
		TxFee:                     int64(cfg.RelayFee.Amount),
	}

	// Make list of old versions of testnet directories.
	var oldTestNets []string
	oldTestNets = append(oldTestNets, filepath.Join(cfg.AppDataDir, "testnet"))
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
