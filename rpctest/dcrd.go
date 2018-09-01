// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"path/filepath"

	"github.com/decred/dcrwallet/errors"
)

type DcrdTestServer struct {
	rpcUser    string
	rpcPass    string
	listen     string
	rpcListen  string
	rpcConnect string
	profile    string
	debugLevel string
	appDir     string
	endpoint   string

	externalProcess *ExternalProcess

	RPCClient   *RPCConnection
}

func (server *DcrdTestServer) CertFile() string {
	return filepath.Join(server.appDir, "rpc.cert")
}

func (server *DcrdTestServer) KeyFile() string {
	return filepath.Join(server.appDir, "rpc.key")
}

func (server *DcrdTestServer) IsRunning() bool {
	return server.externalProcess.isRunning
}

func (n *DcrdTestServer) Start(extraArguments map[string]interface{}, debugOutput bool) {
	if n.IsRunning() {
		ReportTestSetupMalfunction(errors.Errorf("DcrdTestServer is already running"))
	}
	fmt.Println("Start DCRD process...")
	MakeDirs(n.appDir)

	dcrdExe := "dcrd"
	n.externalProcess.CommandName = dcrdExe
	n.externalProcess.Arguments = n.cookArguments(extraArguments)
	n.externalProcess.WorkingDir = n.appDir
	n.externalProcess.Launch(debugOutput)
}

func (n *DcrdTestServer) Stop() {
	if !n.IsRunning() {
		ReportTestSetupMalfunction(errors.Errorf("DcrdTestServer is not running"))
	}
	fmt.Println("Stop DCRD process...")
	err := n.externalProcess.Stop()
	CheckTestSetupMalfunction(err)
}

func (n *DcrdTestServer) cookArguments(extraArguments map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	result["txindex"] = NoArgumentValue
	result["addrindex"] = NoArgumentValue
	result["rpcuser"] = n.rpcUser
	result["rpcpass"] = n.rpcPass
	result["rpcconnect"] = n.rpcConnect
	result["rpclisten"] = n.rpcListen
	result["listen"] = n.listen
	result["appdata"] = n.appDir
	result["debuglevel"] = n.debugLevel
	result["profile"] = n.profile
	result["rpccert"] = n.CertFile()
	result["rpckey"] = n.KeyFile()

	ArgumentsCopyTo(extraArguments, result)
	return result
}

func (server *DcrdTestServer) FullConsoleCommand() string {
	return server.externalProcess.FullConsoleCommand()
}
