// Copyright (c) 2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// serviceControlPipeRx reads from the file descriptor fd of a read end pipe.
// This is intended to be used as a simple control mechanism for parent
// processes to communicate with and and manage the lifetime of a dcrd child
// process using a unidirectional pipe (on Windows, this is an anonymous pipe,
// not a named pipe).
//
// When the pipe is closed or any other errors occur reading the control
// message, shutdown begins.  This prevents dcrd from continuing to run
// unsupervised after the parent process closes unexpectedly.
//
// No control messages are currently defined and the only use for the pipe is to
// start clean shutdown when the pipe is closed.  Control messages that follow
// the pipe message format can be added later as needed.
func serviceControlPipeRx(fd uintptr) {
	pipe := os.NewFile(fd, fmt.Sprintf("|%v", fd))
	r := bufio.NewReader(pipe)
	for {
		_, err := r.Discard(1024)
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			log.Errorf("Failed to read from pipe: %v", err)
			break
		}
	}

	select {
	case shutdownRequestChannel <- struct{}{}:
	default:
	}
}
