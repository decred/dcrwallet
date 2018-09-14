// +build windows

package main

import (
	"net"
	"strings"

	winio "github.com/Microsoft/go-winio"
	"github.com/decred/dcrwallet/errors"
)

func serviceControlOpenNamedPipeRx(fname string) (net.Listener, error) {
	if strings.Index(fname, `\\.\pipe\`) != 0 {
		return nil, errors.E("pipe path does not start with correct prefix")
	}
	return winio.ListenPipe(fname, nil)
}
