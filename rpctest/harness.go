// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"io/ioutil"
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

	DcrdClient   *RPCConnection
	WalletClient *RPCConnection

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

// WalletConnectionConfig creates new connection config for RPC client
func (n *Harness) WalletConnectionConfig() rpcclient.ConnConfig {
	file := n.WalletServer.CertFile()
	fmt.Println("reading: " + file)
	cert, err := ioutil.ReadFile(file)
	CheckTestSetupMalfunction(err)

	return rpcclient.ConnConfig{
		Host:                 n.WalletServer.rpcListen,
		Endpoint:             n.WalletServer.endpoint,
		User:                 n.WalletServer.rpcUser,
		Pass:                 n.WalletServer.rpcPass,
		Certificates:         cert,
		DisableAutoReconnect: true,
	}
}

// DcrdConnectionConfig creates new connection config for RPC client
func (n *Harness) DcrdConnectionConfig() rpcclient.ConnConfig {
	file := n.DcrdServer.CertFile()
	fmt.Println("reading: " + file)
	cert, err := ioutil.ReadFile(file)
	CheckTestSetupMalfunction(err)

	return rpcclient.ConnConfig{
		Host:                 n.DcrdServer.rpcListen,
		Endpoint:             n.DcrdServer.endpoint,
		User:                 n.DcrdServer.rpcUser,
		Pass:                 n.DcrdServer.rpcPass,
		Certificates:         cert,
		DisableAutoReconnect: true,
		HTTPPostMode:         false,
	}
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
	}

	wallet := &WalletTestServer{
		rpcConnect:      net.JoinHostPort(config.DcrdRPCHost, strconv.Itoa(config.DcrdRPCPort)),
		rpcListen:       net.JoinHostPort(config.WalletRPCHost, strconv.Itoa(config.WalletRPCPort)),
		rpcUser:         "user",
		rpcPass:         "pass",
		appDir:          filepath.Join(config.WorkingDir, "dcrwallet"),
		endpoint:        "ws",
		externalProcess: &ExternalProcess{CommandName: "dcrwallet"},
	}

	h := &Harness{
		Config:       config,
		DcrdClient:   &RPCConnection{MaxConnRetries: 20},
		WalletClient: &RPCConnection{MaxConnRetries: 20},
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
	return h.WalletClient.rpcClient
}

// DcrdRPCClient manages access to the RPCClient,
// test cases suppose to use it when the need access to the Dcrd RPC
func (h *Harness) DcrdRPCClient() *rpcclient.Client {
	return h.DcrdClient.rpcClient
}

func (harness *Harness) DeleteWorkingDir() error {
	dir := harness.Config.WorkingDir
	fmt.Println("delete: " + dir)
	err := os.RemoveAll(dir)
	return err
}
