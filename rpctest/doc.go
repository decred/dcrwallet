// Package rpctest contains tests for dcrwallet's RPC server and a harness used
// to facilitate the tests by setting up a temporary DcrdTestServer and WalletServer.
//
//
// Package structure:
//
// - `TestMain()` is executed by go-test, and is responsible for setting up
// and disposing test harnesses. See `rpctest_test.go` for details.
//
// - Test cases are located in `enumerated _test.go` files. Each test runs
// independently. The enumeration specifies test execution order.
//
// - Test cases retrieve harnesses via `ObtainHarness()` function, which
// redirects it's calls to dedicated harnesses `Pool`
//
// - `HarnessesPool` as defined in `harnessespool.go` keeps track of reusable
// harness instances.
//
// - The `Pool` is configured to produce harnesses with a test chain
// of desired height, providing `numMatureOutputs` coinbases to allow
// spending from for testing purposes. See `chainwithmo.go` for details.
//
package rpctest
