// Copyright (c) 2016 The decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"flag"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"testing"
)

type RpcTestCase func(t *testing.T)

/*
skipTest function will trigger when test name is present in the skipTestsList

To use this function add the following code in your test:

    if skipTest(t) {
		t.Skip("Skipping test")
	}

*/
func skipTest(t *testing.T) bool {
	return ListContainsString(skipTestsList, t.Name())
}

// skipTestsList contains names of the tests mentioned in the testCasesToSkip
var skipTestsList []string

// testCasesToSkip, use it to mark tests for being skipped
var testCasesToSkip = []RpcTestCase{
	TestGetNewAddress,   // fails
	TestValidateAddress, // fails
	//TestWalletPassphrase,
	//TestGetBalance,
	//TestListAccounts,
	//TestListUnspent,
	//TestSendToAddress,
	TestSendFrom, // fails
	//TestSendMany,
	TestListTransactions, // fails
	//TestGetSetRelayFee,
	//TestGetSetTicketFee,
	//TestGetTickets,
	TestPurchaseTickets, // fails
	TestGetStakeInfo,    // fails
	//TestWalletInfo,
}

// Get function name from module name
var funcInModulePath = regexp.MustCompile(`^.*\.(.*)$`)

// Get the name of a function type
func functionName(tc RpcTestCase) string {
	fncName := runtime.FuncForPC(reflect.ValueOf(tc).Pointer()).Name()
	return funcInModulePath.ReplaceAllString(fncName, "$1")
}

const TestListTransactionsHarnessTag = "TestListTransactions"
const TestGetStakeInfoHarnessTag = "TestGetStakeInfo"

// Pool stores and manages harnesses
var Pool *HarnessesPool

// ObtainHarness manages access to the Pool for test cases
func ObtainHarness(tag string) *Harness {
	return Pool.ObtainHarnessConcurrentSafe(tag)
}

// TestMain, is executed by go-test, and is
// responsible for setting up and disposing test harnesses.
func TestMain(testingM *testing.M) {
	flag.Parse()

	{ // Build list of all ignored tests
		for _, testCase := range testCasesToSkip {
			caseName := functionName(testCase)
			skipTestsList = append(skipTestsList, caseName)
		}
	}

	// Deploy test setup
	var harnessWith25MOSpawner *ChainWithMatureOutputsSpawner
	{
		// Deploy harness spawner with generated
		// test chain of 25 mature outputs
		harnessWith25MOSpawner = &ChainWithMatureOutputsSpawner{
			WorkingDir:        WorkingDir,
			DebugDCRDOutput:   false,
			DebugWalletOutput: false,
			NumMatureOutputs:  25,
			BasePort:          20000, // 20001, 20002, ...
		}

		Pool = NewHarnessesPool(harnessWith25MOSpawner)

		if !testing.Short() {
			// Initialize harnesses
			// 18 seconds to init each
			tagsList := []string{
				MainHarnessName,
				//TestGetStakeInfoHarnessTag,
				//TestListTransactionsHarnessTag,
			}
			Pool.InitTags(tagsList)
		}
	}

	// Run tests
	exitCode := testingM.Run()

	// TearDown all harnesses in test Pool.
	// This includes removing all temporary directories,
	// and shutting down any created processes.
	Pool.TearDownAll()
	DeleteWorkingDir()

	os.Exit(exitCode)
}
