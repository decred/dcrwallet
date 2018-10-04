package rpctest

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/decred/dcrwallet/errors"
)

type ExternalProcess struct {
	CommandName string
	Arguments   map[string]interface{}
	WorkingDir  string

	isRunning bool

	runningCommand *exec.Cmd
}

// externalProcessesList keeps track of all running processes
// to execute emergency killProcess in case of the test setup malfunction
var externalProcessesList = &ExternalProcessesList{
	set: make(map[*ExternalProcess]bool),
}

type ExternalProcessesList struct {
	set map[*ExternalProcess]bool
}

func VerifyNoExternalProcessesLeft() {
	N := len(externalProcessesList.set)
	if N > 0 {
		for k := range externalProcessesList.set {
			fmt.Fprintln(
				os.Stderr,
				fmt.Sprintf(
					"External process leak, running command: %s",
					k.FullConsoleCommand(),
				))
		}
		ReportTestSetupMalfunction(
			errors.Errorf(
				"Incorrect state: %v external processes left running.",
				N,
			))
	}
}

// emergencyKillAll is used to terminate all the external processes
// created within this test setup in case of panic.
// Otherwise, they all will persist unless explicitly killed.
// Should be used only in case of test setup malfunction.
func (list *ExternalProcessesList) emergencyKillAll() {
	for k := range list.set {
		err := killProcess(k, os.Stderr)
		if err != nil {
			fmt.Fprintln(
				os.Stderr,
				fmt.Sprintf(
					"Failed to kill process %v",
					err,
				))
		}
	}
}

func (list *ExternalProcessesList) add(process *ExternalProcess) {
	list.set[process] = true
}

func (list *ExternalProcessesList) remove(process *ExternalProcess) {
	delete(list.set, process)
}

func (p *ExternalProcess) FullConsoleCommand() string {
	cmd := p.runningCommand
	args := strings.Join(cmd.Args[1:], " ")
	return cmd.Path + " " + args
}

func (p *ExternalProcess) ClearArguments() {
	for key := range p.Arguments {
		delete(p.Arguments, key)
	}
}

func (process *ExternalProcess) Launch(debugOutput bool) {
	if process.isRunning {
		ReportTestSetupMalfunction(errors.Errorf("Process is already running: %v", process.runningCommand))
	}
	process.isRunning = true

	process.runningCommand = exec.Command(process.CommandName, ArgumentsToStringArray(process.Arguments)...)
	cmd := process.runningCommand
	fmt.Println("run command # " + cmd.Path)
	fmt.Println(strings.Join(cmd.Args[1:], "\n"))
	fmt.Println()
	if debugOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	err := cmd.Start()
	CheckTestSetupMalfunction(err)

	externalProcessesList.add(process)
}


// Stop interrupts the running process, and waits until it exits properly.
// It is important that the process be stopped via Stop(), otherwise,
// it will persist unless explicitly killed.
func (process *ExternalProcess) Stop() error {
	if !process.isRunning {
		ReportTestSetupMalfunction(errors.Errorf("Process is not running: %v", process.runningCommand))
	}
	process.isRunning = false

	externalProcessesList.remove(process)

	return killProcess(process, os.Stdout)
}

// On windows, interrupt is not supported, so a kill signal is used instead.
func killProcess(process *ExternalProcess, logStream *os.File) error {
	cmd := process.runningCommand
	defer cmd.Wait()

	fmt.Fprintln(
		logStream,
		fmt.Sprintf(
			"Killing process: %v",
			process.FullConsoleCommand(),
		))

	osProcess := cmd.Process
	if runtime.GOOS == "windows" {
		err := osProcess.Signal(os.Kill)
		return err
	} else {
		err := osProcess.Signal(os.Interrupt)
		return err
	}
}

var (
	// NoArgumentValue indicates flag has name but no value to provide,
	// example: "--someflag"
	NoArgumentValue interface{} = &struct{}{} // stub object

	// NoArgument and NoArgumentNil indicates the key should be ignored
	NoArgument                = ""
	NoArgumentNil interface{} = nil

	// See ArgumentsToStringArray to understand how these constants are used
)

// ArgumentsToStringArray converts map to an array of command line arguments
// taking in account NoArgumentValue and NoArgument indicators above
func ArgumentsToStringArray(args map[string]interface{}) []string {
	var result []string
	for key, value := range args {
		if value == NoArgument || value == NoArgumentNil {
			// skip key
		} else if value == NoArgumentValue {
			// --%key%
			str := fmt.Sprintf("--%s", key)
			result = append(result, str)
		} else {
			// --%key%=%value%
			str := fmt.Sprintf("--%s=%s", key, value)
			result = append(result, str)
		}
	}
	return result
}

func ArgumentsCopyTo(from map[string]interface{}, to map[string]interface{}) map[string]interface{} {
	for key, value := range from {
		to[key] = value
	}
	return to
}
