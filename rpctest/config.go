package rpctest

import (
	"io/ioutil"
	"fmt"
	"os"

	"github.com/decred/dcrwallet/internal/cfgutil"
)

// Test setup working directory
var WorkingDir = SetupWorkingDir()

func SetupWorkingDir() string {
	testWorkingDir, err := ioutil.TempDir("", "rpctest")
	if err != nil {
		fmt.Println("Unable to create working dir: ", err)
		os.Exit(-1)
	}
	return testWorkingDir
}

func DeleteWorkingDir() error {
	file := WorkingDir
	y, err := cfgutil.FileExists(file)
	if err != nil {
		return err
	}
	if y {
		fmt.Println("delete: " + file)
		return os.Remove(file)
	}
	return nil
}
