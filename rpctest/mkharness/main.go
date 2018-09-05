// mkharness brings up a simnet node and wallet, prints the commands, and waits
// for a keypress to terminate them.

package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/rpctest"
)

func main() {

	harnessMOSpawner := &rpctest.ChainWithMatureOutputsSpawner{
		WorkingDir:        rpctest.WorkingDir,
		DebugDCRDOutput:   true,
		DebugWalletOutput: true,
		NumMatureOutputs:  25,
		BasePort:          20000,
	}

	harness := harnessMOSpawner.NewInstance(rpctest.MainHarnessName)

	fmt.Printf("Dcrd command:\n\t%s\n", harness.DcrdServer.FullConsoleCommand())
	fmt.Printf("Wallet command:\n\t%s\n", harness.WalletServer.FullConsoleCommand())

	cn := harness.DcrdConnectionConfig()
	nodeCertFile := harness.DcrdServer.CertFile()
	fmt.Println("Command for dcrd's dcrctl:")
	fmt.Printf("\tdcrctl -u %s -P %s -s %s -c %s\n", cn.User, cn.Pass,
		cn.Host, nodeCertFile)

	cw := harness.WalletConnectionConfig()
	walletCertFile := harness.WalletServer.CertFile()
	fmt.Println("Command for wallet's dcrctl:")
	fmt.Printf("\tdcrctl -u %s -P %s -s %s -c %s --wallet\n", cw.User, cw.Pass,
		cw.Host, walletCertFile)

	fmt.Print("Press Enter to terminate harness.")
	bufio.NewReader(os.Stdin).ReadBytes('\n')

	if err := harnessMOSpawner.Dispose(harness); err != nil {
		fmt.Println("Unable to teardown test chain: ", err)
		os.Exit(-1)
	}

	if err := rpctest.DeleteWorkingDir(); err != nil {
		fmt.Println("Unable to teardown test chain: ", err)
		os.Exit(-1)
	}

	verifyCorrectExit()
	os.Exit(0)
}

// verifyCorrectExit is an additional safety check to ensure required
// teardown routines were properly performed.
func verifyCorrectExit() {
	rpctest.VerifyNoExternalProcessesLeft()

	file := rpctest.WorkingDir
	if rpctest.FileExists(file) {
		rpctest.ReportTestSetupMalfunction(
			errors.Errorf(
				"Incorrect state: "+
					"Working dir should be deleted before exit. %v",
				file,
			))
	}
}
