package rpcservertest

import (
	"fmt"
	"github.com/decred/dcrwallet/errors"
	pb "github.com/decred/dcrwallet/rpc/walletrpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"path/filepath"
	"testing"
	// "github.com/decred/dcrd/dcrutil"
)

func TestHello(t *testing.T) {

	harnessMOSpawner := &ChainWithMatureOutputsSpawner{
		WorkingDir:        WorkingDir,
		DebugDCRDOutput:   true,
		DebugWalletOutput: true,
		NumMatureOutputs:  25,
		BasePort:          20000,
	}

	harness := harnessMOSpawner.NewInstance(MainHarnessName)

	// set up test cases
	testsRequests := []struct {
		accountNumber         uint32
		requiredConfirmations int32
		total                 int64
	}{
		{
			accountNumber:         0,
			requiredConfirmations: 1,
			total:                 1200000000000,
		},
	}
	var certificateFile = filepath.Join(WorkingDir, "harness-"+MainHarnessName, "dcrwallet", "rpc.cert")

	creds, err := credentials.NewClientTLSFromFile(certificateFile, "localhost")
	if err != nil {
		ReportTestSetupMalfunction(
			errors.Errorf(
				"Err: %v",
				err,
			))
		return
	}

	conn, err := grpc.Dial("localhost:19558", grpc.WithTransportCredentials(creds))
	if err != nil {
		ReportTestSetupMalfunction(
			errors.Errorf(
				"Err: %v",
				err,
			))
		return
	}
	defer conn.Close()
	c := pb.NewWalletServiceClient(conn)

	for _, tt := range testsRequests {
		req := &pb.BalanceRequest{
			AccountNumber:         tt.accountNumber,
			RequiredConfirmations: tt.requiredConfirmations,
		}
		balanceResponse, err := c.Balance(context.Background(), req)
		if err != nil {
			ReportTestSetupMalfunction(
				errors.Errorf(
					"Err: %v",
					err,
				))
			return
		}
		fmt.Printf("BAlance: %+v\n\n", balanceResponse)
		if balanceResponse.Total != tt.total {
			t.Errorf("Have %v, wanted %v", balanceResponse.Total, tt.total)
			return
		}
	}

	harnessMOSpawner.Dispose(harness)
	DeleteWorkingDir()
	verifyCorrectExit()

}

// verifyCorrectExit is an additional safety check to ensure required
// teardown routines were properly performed.
func verifyCorrectExit() {
	VerifyNoExternalProcessesLeft()

	if FileExists(WorkingDir) {
		ReportTestSetupMalfunction(
			errors.Errorf(
				"Incorrect state: "+
					"Working dir should be deleted before exit. %v",
				WorkingDir,
			))
	}
}
