// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
)

// Harness provides a unified platform for creating RPC-driven integration
// tests involving dcrd and dcrwallet.
// The active DcrdTestServer and WalletServer will typically be run in simnet
// mode to allow for easy generation of test blockchains.
type Harness struct {
	Config *HarnessConfig

	DcrdServer   *DcrdTestServer
	WalletServer *WalletTestServer

	MiningAddress dcrutil.Address
}

type HarnessConfig struct {
	Name string

	WorkingDir string

	ActiveNet *chaincfg.Params

	P2PHost string
	P2PPort int

	DcrdRPCHost string
	DcrdRPCPort int

	WalletRPCHost string
	WalletRPCPort int
}

// NewHarness creates a new instance of the rpc test harness.
func NewHarness(config *HarnessConfig) *Harness {
	dcrd := &DcrdTestServer{
		listen:          net.JoinHostPort(config.P2PHost, strconv.Itoa(config.P2PPort)),
		rpcListen:       net.JoinHostPort(config.DcrdRPCHost, strconv.Itoa(config.DcrdRPCPort)),
		rpcUser:         "user",
		rpcPass:         "pass",
		appDir:          filepath.Join(config.WorkingDir, "dcrd"),
		endpoint:        "ws",
		externalProcess: &ExternalProcess{CommandName: "dcrd"},
		RPCClient:       &RPCConnection{MaxConnRetries: 20},
	}

	wallet := &WalletTestServer{
		rpcConnect:      net.JoinHostPort(config.DcrdRPCHost, strconv.Itoa(config.DcrdRPCPort)),
		rpcListen:       net.JoinHostPort(config.WalletRPCHost, strconv.Itoa(config.WalletRPCPort)),
		rpcUser:         "user",
		rpcPass:         "pass",
		appDir:          filepath.Join(config.WorkingDir, "dcrwallet"),
		endpoint:        "ws",
		externalProcess: &ExternalProcess{CommandName: "dcrwallet"},
		RPCClient:       &RPCConnection{MaxConnRetries: 20},
	}

	h := &Harness{
		Config:       config,
		DcrdServer:   dcrd,
		WalletServer: wallet,
	}

	return h
}

// ActiveNet manages access to the ActiveNet variable,
// test cases suppose to use it when the need to access network params
func (harness *Harness) ActiveNet() *chaincfg.Params {
	return harness.Config.ActiveNet
}

// WalletRPCClient manages access to the RPCClient,
// test cases suppose to use it when the need access to the Wallet RPC
func (h *Harness) WalletRPCClient() *rpcclient.Client {
	return h.WalletServer.RPCClient.rpcClient
}

// DcrdRPCClient manages access to the RPCClient,
// test cases suppose to use it when the need access to the Dcrd RPC
func (h *Harness) DcrdRPCClient() *rpcclient.Client {
	return h.DcrdServer.RPCClient.rpcClient
}

func (harness *Harness) DeleteWorkingDir() error {
	dir := harness.Config.WorkingDir
	fmt.Println("delete: " + dir)
	err := os.RemoveAll(dir)
	return err
}
