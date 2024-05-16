// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import "github.com/decred/slog"

// log is a logger that is initialized with no output filters.  This
// means the package will not perform any logging by default until the caller
// requests it.
var log = slog.Disabled

// UseLogger uses a specified Logger to output package logging info.
// This should be used in preference to SetLogWriter if the caller is also
// using slog.
func UseLogger(logger slog.Logger) {
	log = logger
}

type debugLogger struct{}

var debugLog debugLogger

func (debugLogger) Print(args ...any)                 { log.Debug(args...) }
func (debugLogger) Printf(format string, args ...any) { log.Debugf(format, args...) }
func (debugLogger) Log(args ...any)                   { log.Debug(args...) }
func (debugLogger) Logf(format string, args ...any)   { log.Debugf(format, args...) }

type infoLogger struct{}

var infoLog infoLogger

func (infoLogger) Print(args ...any)                 { log.Info(args...) }
func (infoLogger) Printf(format string, args ...any) { log.Infof(format, args...) }
func (infoLogger) Log(args ...any)                   { log.Info(args...) }
func (infoLogger) Logf(format string, args ...any)   { log.Infof(format, args...) }
