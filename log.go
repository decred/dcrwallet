// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/decred/dcrd/connmgr"
	"github.com/decred/dcrwallet/chain/v3"
	"github.com/decred/dcrwallet/loader"
	"github.com/decred/dcrwallet/p2p/v2"
	"github.com/decred/dcrwallet/rpc/jsonrpc"
	"github.com/decred/dcrwallet/rpc/rpcserver"
	"github.com/decred/dcrwallet/spv/v3"
	"github.com/decred/dcrwallet/ticketbuyer/v4"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	logRotator.Write(p)
	return len(p), nil
}

// Loggers per subsystem.  A single backend logger is created and all subsytem
// loggers created from it will write to the backend.  When adding new
// subsystems, add the subsystem logger variable here and to the
// subsystemLoggers map.
//
// Loggers can not be used before the log rotator has been initialized with a
// log file.  This must be performed early during application startup by calling
// initLogRotator.
var (
	// backendLog is the logging backend used to create all subsystem loggers.
	// The backend must not be used before the log rotator has been initialized,
	// or data races and/or nil pointer dereferences will occur.
	backendLog = slog.NewBackend(logWriter{})

	// logRotator is one of the logging outputs.  It should be closed on
	// application shutdown.
	logRotator *rotator.Rotator

	log        = backendLog.Logger("DCRW")
	loaderLog  = backendLog.Logger("LODR")
	walletLog  = backendLog.Logger("WLLT")
	tkbyLog    = backendLog.Logger("TKBY")
	syncLog    = backendLog.Logger("SYNC")
	grpcLog    = backendLog.Logger("GRPC")
	jsonrpcLog = backendLog.Logger("RPCS")
	cmgrLog    = backendLog.Logger("CMGR")
)

// Initialize package-global logger variables.
func init() {
	loader.UseLogger(loaderLog)
	wallet.UseLogger(walletLog)
	udb.UseLogger(walletLog)
	ticketbuyer.UseLogger(tkbyLog)
	chain.UseLogger(syncLog)
	spv.UseLogger(syncLog)
	p2p.UseLogger(syncLog)
	rpcserver.UseLogger(grpcLog)
	jsonrpc.UseLogger(jsonrpcLog)
	connmgr.UseLogger(cmgrLog)
}

// subsystemLoggers maps each subsystem identifier to its associated logger.
var subsystemLoggers = map[string]slog.Logger{
	"DCRW": log,
	"LODR": loaderLog,
	"WLLT": walletLog,
	"TKBY": tkbyLog,
	"SYNC": syncLog,
	"GRPC": grpcLog,
	"RPCS": jsonrpcLog,
	"CMGR": cmgrLog,
}

// initLogRotator initializes the logging rotater to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func initLogRotator(logFile string) {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, 10*1024, false, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}

	logRotator = r
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
	logRotator.Close()
	os.Exit(1)
}
