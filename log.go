// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"

	"decred.org/dcrwallet/v3/chain"
	"decred.org/dcrwallet/v3/internal/loader"
	"decred.org/dcrwallet/v3/internal/loggers"
	"decred.org/dcrwallet/v3/internal/rpc/jsonrpc"
	"decred.org/dcrwallet/v3/internal/rpc/rpcserver"
	"decred.org/dcrwallet/v3/internal/vsp"
	"decred.org/dcrwallet/v3/p2p"
	"decred.org/dcrwallet/v3/spv"
	"decred.org/dcrwallet/v3/ticketbuyer"
	"decred.org/dcrwallet/v3/wallet"
	"decred.org/dcrwallet/v3/wallet/udb"
	"github.com/decred/dcrd/connmgr/v3"
	"github.com/decred/slog"
)

var log = loggers.MainLog

// Initialize package-global logger variables.
func init() {
	loader.UseLogger(loggers.LoaderLog)
	wallet.UseLogger(loggers.WalletLog)
	udb.UseLogger(loggers.WalletLog)
	ticketbuyer.UseLogger(loggers.TkbyLog)
	chain.UseLogger(loggers.SyncLog)
	spv.UseLogger(loggers.SyncLog)
	p2p.UseLogger(loggers.SyncLog)
	rpcserver.UseLogger(loggers.GrpcLog)
	jsonrpc.UseLogger(loggers.JsonrpcLog)
	connmgr.UseLogger(loggers.CmgrLog)
	vsp.UseLogger(loggers.VspcLog)
}

// subsystemLoggers maps each subsystem identifier to its associated logger.
var subsystemLoggers = map[string]slog.Logger{
	"DCRW": loggers.MainLog,
	"LODR": loggers.LoaderLog,
	"WLLT": loggers.WalletLog,
	"TKBY": loggers.TkbyLog,
	"SYNC": loggers.SyncLog,
	"GRPC": loggers.GrpcLog,
	"RPCS": loggers.JsonrpcLog,
	"CMGR": loggers.CmgrLog,
	"VSPC": loggers.VspcLog,
}

// setLogLevel sets the logging level for provided subsystem.  Invalid
// subsystems are ignored.  Uninitialized subsystems are dynamically created as
// needed.
func setLogLevel(subsystemID string, logLevel string) {
	// Ignore invalid subsystems.
	logger, ok := subsystemLoggers[subsystemID]
	if !ok {
		return
	}

	// Defaults to info if the log level is invalid.
	level, _ := slog.LevelFromString(logLevel)
	logger.SetLevel(level)
}

// setLogLevels sets the log level for all subsystem loggers to the passed
// level.  It also dynamically creates the subsystem loggers as needed, so it
// can be used to initialize the logging system.
func setLogLevels(logLevel string) {
	// Configure all sub-systems with the new logging level.  Dynamically
	// create loggers as needed.
	for subsystemID := range subsystemLoggers {
		setLogLevel(subsystemID, logLevel)
	}
}

// fatalf logs a message, flushes the logger, and finally exit the process with
// a non-zero return code.
func fatalf(format string, args ...interface{}) {
	log.Errorf(format, args...)
	os.Stdout.Sync()
	loggers.CloseLogRotator()
	os.Exit(1)
}
