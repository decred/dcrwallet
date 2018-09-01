// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"path/filepath"

	"github.com/decred/dcrwallet/errors"
)

type WalletTestServer struct {
	rpcUser    string
	rpcPass    string
	rpcConnect string
	rpcListen  string
	appDir     string
	debugLevel string
	endpoint   string

	externalProcess *ExternalProcess

	RPCClient *RPCConnection
}

func (server *WalletTestServer) CertFile() string {
	return filepath.Join(server.appDir, "rpc.cert")
}

func (server *WalletTestServer) KeyFile() string {
	return filepath.Join(server.appDir, "rpc.key")
}

func (server *WalletTestServer) IsRunning() bool {
	return server.externalProcess.isRunning
}

func (n *WalletTestServer) Start(dcrdCertificateFile string, extraArguments map[string]interface{}, debugOutput bool) {
	if n.IsRunning() {
		ReportTestSetupMalfunction(errors.Errorf("WalletTestServer is already running"))
	}
	fmt.Println("Start Wallet process...")
	MakeDirs(n.appDir)

	dcrwalletExe := "dcrwallet"
	n.externalProcess.CommandName = dcrwalletExe
	n.externalProcess.Arguments = n.cookArguments(dcrdCertificateFile, extraArguments)
	n.externalProcess.WorkingDir = n.appDir
	n.externalProcess.Launch(debugOutput)
}

func (n *WalletTestServer) Stop() {
	if !n.IsRunning() {
		ReportTestSetupMalfunction(errors.Errorf("WalletTestServer is not running"))
	}
	fmt.Println("Stop Wallet process...")
	err := n.externalProcess.Stop()
	CheckTestSetupMalfunction(err)
}

func (n *WalletTestServer) cookArguments(dcrdCertificateFile string, extraArguments map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["username"] = n.rpcUser
	result["password"] = n.rpcPass
	result["rpcconnect"] = n.rpcConnect
	result["rpclisten"] = n.rpcListen
	result["rpcconnect"] = n.rpcConnect
	result["appdata"] = n.appDir
	result["debuglevel"] = n.debugLevel
	result["cafile"] = dcrdCertificateFile
	result["rpccert"] = n.CertFile()
	result["rpckey"] = n.KeyFile()

	ArgumentsCopyTo(extraArguments, result)
	return result
}

func (server *WalletTestServer) FullConsoleCommand() string {
	return server.externalProcess.FullConsoleCommand()
}
