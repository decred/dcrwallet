// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"os"
	"time"
	"path/filepath"
	"strings"

	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/internal/cfgutil"
)

// assertNotNil does sanity check
func assertNotNil(tag string, value interface{}) {
	if value == nil {
		ReportTestSetupMalfunction(
			errors.Errorf("Invalid state: <%v> is nil", tag))
	}
}

// assertNotEmpty does sanity check
func assertNotEmpty(tag string, value string) {
	if value == "" {
		ReportTestSetupMalfunction(
			errors.Errorf("Invalid state: string <%v> is empty", tag))
	}
}

// ReportTestSetupMalfunction is used to bring
// attention to undesired program behaviour.
// This function is expected to be called never.
// The fact that it is called indicates a serious
// bug in the test setup and requires investigation.
func ReportTestSetupMalfunction(err error) error {
	//This includes removing all temporary directories,
	// and shutting down any created processes.
	fmt.Println(err.Error())

	externalProcessesList.emergencyKillAll()
	e := DeleteWorkingDir()
	if err != nil {
		fmt.Println("Failed to delete working dir: " + e.Error())
	}

	panic(fmt.Sprintf("Test setup malfuction: %v", err))
	return err
}

// CheckTestSetupMalfunction reports error when one is present
func CheckTestSetupMalfunction(err error) {
	if err != nil {
		ReportTestSetupMalfunction(err)
	}
}

func Sleep(milliseconds int64) {
	//fmt.Println("sleep: " + strconv.FormatInt(milliseconds, 10))
	time.Sleep(time.Duration(milliseconds * int64(time.Millisecond)))
}

func FileExists(path string) bool {
	e, err := cfgutil.FileExists(path)
	if err != nil {
		return false
	}
	return e
}

// WaitForFile sleeps until target file is created
// or timeout is reached
func WaitForFile(file string, maxSecondsToWait int) {
	counter := maxSecondsToWait
	for !FileExists(file) {
		fmt.Println("waiting for: " + file)
		Sleep(1000)
		counter--
		if counter < 0 {
			err := errors.Errorf("File not found: %v", file)
			ReportTestSetupMalfunction(err)
		}
	}
}

func MakeDirs(dir string) {
	sep := string(os.PathSeparator)
	steps := strings.Split(dir, sep)
	for i := 1; i < len(steps); i++ {
		pathI := filepath.Join(steps[:i]...)
		if pathI == "" {
			continue
		}
		if !FileExists(pathI) {
			err := os.Mkdir(pathI, 0700)
			CheckTestSetupMalfunction(err)
		}
	}
}

func DeleteFile(file string) {
	fmt.Println("delete: " + file)
	err := os.Remove(file)
	CheckTestSetupMalfunction(err)
}

func ListContainsString(list []string, a string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
