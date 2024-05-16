// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package loggers

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	if logRotator != nil {
		logRotator.Write(p)
	}
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

	MainLog    = backendLog.Logger("DCRW")
	LoaderLog  = backendLog.Logger("LODR")
	WalletLog  = backendLog.Logger("WLLT")
	TkbyLog    = backendLog.Logger("TKBY")
	SyncLog    = backendLog.Logger("SYNC")
	PeerLog    = backendLog.Logger("PEER")
	GrpcLog    = backendLog.Logger("GRPC")
	JsonrpcLog = backendLog.Logger("RPCS")
	CmgrLog    = backendLog.Logger("CMGR")
	MixcLog    = backendLog.Logger("MIXC")
	MixpLog    = backendLog.Logger("MIXP")
	VspcLog    = backendLog.Logger("VSPC")
)

// InitLogRotator initializes the logging rotater to write logs to logFile and
// create roll files in the same directory.  logSize is the size in KiB after
// which a log file will be rotated and compressed.
//
// This function must be called before the package-global log rotater variables
// are used.
func InitLogRotator(logFile string, logSize int64) {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, logSize, false, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}

	logRotator = r
}

// CloseLogRotator closes the log rotator, syncing all file writes, if the
// rotator was initialized.
func CloseLogRotator() error {
	if logRotator == nil {
		return nil
	}

	return logRotator.Close()
}
