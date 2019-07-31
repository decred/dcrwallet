// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Messages sent over a pipe are encoded using a simple binary message format:
//
//   - Protocol version (1 byte, currently 1)
//   - Message type length (1 byte)
//   - Message type string (encoded as UTF8, no longer than 255 bytes)
//   - Message payload length (4 bytes, little endian)
//   - Message payload bytes (no longer than 2^32 - 1 bytes)
type pipeMessage interface {
	Type() string
	PayloadSize() uint32
	WritePayload(w io.Writer) error
}

var outgoingPipeMessages = make(chan pipeMessage)

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
			break
		}
		if err != nil {
			log.Errorf("Failed to read from pipe: %v", err)
			break
		}
	}

	shutdownRequestChannel <- struct{}{}
}

// serviceControlPipeTx sends pipe messages to the file descriptor fd of a write
// end pipe.  This is intended to be a simple response and notification system
// for a child dcrd process to communicate with a parent process without the
// need to go through the RPC server.
//
// See the comment on the pipeMessage interface for the binary encoding of a
// pipe message.
func serviceControlPipeTx(fd uintptr) {
	defer drainOutgoingPipeMessages()

	pipe := os.NewFile(fd, fmt.Sprintf("|%v", fd))
	w := bufio.NewWriter(pipe)
	headerBuffer := make([]byte, 0, 1+1+255+4) // capped to max header size
	var err error
	for m := range outgoingPipeMessages {
		const protocolVersion byte = 1

		mtype := m.Type()
		psize := m.PayloadSize()

		headerBuffer = append(headerBuffer, protocolVersion)
		headerBuffer = append(headerBuffer, byte(len(mtype)))
		headerBuffer = append(headerBuffer, mtype...)
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, psize)
		headerBuffer = append(headerBuffer, buf...)

		_, err = w.Write(headerBuffer)
		if err != nil {
			break
		}

		err = m.WritePayload(w)
		if err != nil {
			break
		}

		err = w.Flush()
		if err != nil {
			break
		}

		headerBuffer = headerBuffer[:0]
	}

	log.Errorf("Failed to write to pipe: %v", err)
}

func drainOutgoingPipeMessages() {
	for range outgoingPipeMessages {
	}
}

// The jsonrpcListenerEvent is used to notify the listener addresses used for
// the JSON-RPC server.  The message type is "jsonrpclistener".  This event is
// most notably useful when parent processes start the wallet with listener
// addresses bound on port 0 to cause the operating system to select an unused
// port.
//
// The payload is the UTF8 bytes of the listener address, and the payload size
// is the byte length of the string.
type jsonrpcListenerEvent string

var _ pipeMessage = jsonrpcListenerEvent("")

func (jsonrpcListenerEvent) Type() string          { return "jsonrpclistener" }
func (e jsonrpcListenerEvent) PayloadSize() uint32 { return uint32(len(e)) }
func (e jsonrpcListenerEvent) WritePayload(w io.Writer) error {
	_, err := w.Write([]byte(e))
	return err
}

type jsonrpcListenerEventServer chan<- pipeMessage

func newJSONRPCListenerEventServer(outChan chan<- pipeMessage) jsonrpcListenerEventServer {
	return jsonrpcListenerEventServer(outChan)
}

func (s jsonrpcListenerEventServer) notify(laddr string) {
	if s == nil {
		return
	}
	s <- jsonrpcListenerEvent(laddr)
}

// The grpcListenerEvent is used to notify the listener addresses used for the
// gRPC server.  The message type is "grpclistener".  This event is most notably
// useful when parent processes start the wallet with listener addresses bound
// on port 0 to cause the operating system to select an unused port.
//
// The payload is the UTF8 bytes of the listener address, and the payload size
// is the byte length of the string.
type grpcListenerEvent string

var _ pipeMessage = grpcListenerEvent("")

func (grpcListenerEvent) Type() string          { return "grpclistener" }
func (e grpcListenerEvent) PayloadSize() uint32 { return uint32(len(e)) }
func (e grpcListenerEvent) WritePayload(w io.Writer) error {
	_, err := w.Write([]byte(e))
	return err
}

type grpcListenerEventServer chan<- pipeMessage

func newGRPCListenerEventServer(outChan chan<- pipeMessage) grpcListenerEventServer {
	return grpcListenerEventServer(outChan)
}

func (s grpcListenerEventServer) notify(laddr string) {
	if s == nil {
		return
	}
	s <- grpcListenerEvent(laddr)
}
