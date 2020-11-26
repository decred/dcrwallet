// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
	"github.com/jessevdk/go-flags"
	"github.com/jrick/wsrpc/v2"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	walletDataDirectory = dcrutil.AppDataDir("dcrwallet", false)
	newlineBytes        = []byte{'\n'}
)

var opts = struct {
	TestNet            bool   `long:"testnet" description:"Use the test decred network"`
	RPCConnect         string `short:"c" long:"connect" description:"Hostname[:port] of wallet RPC server"`
	RPCUsername        string `short:"u" long:"rpcuser" description:"Wallet RPC username"`
	RPCPassword        string `short:"P" long:"rpcpass" description:"Wallet RPC password"`
	RPCCertificateFile string `long:"cafile" description:"Wallet RPC TLS certificate"`
	CFiltersFile       string `long:"cfiltersfile" description:"Binary file with pre-dcp0005 filter data"`
}{
	TestNet:            false,
	RPCConnect:         "localhost",
	RPCUsername:        "",
	RPCPassword:        "",
	RPCCertificateFile: filepath.Join(walletDataDirectory, "rpc.cert"),
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Stderr.Write(newlineBytes)
	os.Exit(1)
}

func errContext(err error, context string) error {
	return fmt.Errorf("%s: %v", context, err)
}

// normalizeAddress returns the normalized form of the address, adding a
// default port if necessary.  An error is returned if the address, even
// without a port, is not valid.
func normalizeAddress(addr string, defaultPort string) (hostport string, err error) {
	// If the first SplitHostPort errors because of a missing port and not
	// for an invalid host, add the port.  If the second SplitHostPort
	// fails, then a port is not missing and the original error should be
	// returned.
	host, port, origErr := net.SplitHostPort(addr)
	if origErr == nil {
		return net.JoinHostPort(host, port), nil
	}
	addr = net.JoinHostPort(addr, defaultPort)
	_, _, err = net.SplitHostPort(addr)
	if err != nil {
		return "", origErr
	}
	return addr, nil
}

func walletPort(net *chaincfg.Params) string {
	switch net.Net {
	case wire.MainNet:
		return "9110"
	case wire.TestNet3:
		return "19110"
	case wire.SimNet:
		return "19557"
	default:
		return ""
	}
}

// Parse and validate flags.
func init() {
	// Unset localhost defaults if certificate file can not be found.
	_, err := os.Stat(opts.RPCCertificateFile)
	if err != nil {
		opts.RPCConnect = ""
		opts.RPCCertificateFile = ""
	}

	_, err = flags.Parse(&opts)
	if err != nil {
		os.Exit(1)
	}

	var activeNet = chaincfg.MainNetParams()
	if opts.TestNet {
		activeNet = chaincfg.TestNet3Params()
	}

	if opts.RPCConnect == "" {
		fatalf("RPC hostname[:port] is required")
	}
	rpcConnect, err := normalizeAddress(opts.RPCConnect, walletPort(activeNet))
	if err != nil {
		fatalf("Invalid RPC network address `%v`: %v", opts.RPCConnect, err)
	}
	opts.RPCConnect = rpcConnect

	if opts.RPCUsername == "" {
		fatalf("RPC username is required")
	}

	_, err = os.Stat(opts.RPCCertificateFile)
	if err != nil {
		fatalf("RPC certificate file `%s` not found", opts.RPCCertificateFile)
	}

	if opts.CFiltersFile == "" {
		fatalf("CFilters file is required")
	}
}

func promptSecret(what string) (string, error) {
	fmt.Printf("%s: ", what)
	fd := int(os.Stdin.Fd())
	input, err := terminal.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(input), nil
}

func repair() error {
	rpcPassword := opts.RPCPassword

	if rpcPassword == "" {
		secret, err := promptSecret("Wallet RPC password")
		if err != nil {
			return errContext(err, "failed to read RPC password")
		}

		rpcPassword = secret
	}

	cffile, err := os.Open(opts.CFiltersFile)
	if err != nil {
		return errContext(err, "failed to open cfilters file")
	}
	defer cffile.Close()

	ctx := context.Background()
	rpcCertificate, err := ioutil.ReadFile(opts.RPCCertificateFile)
	if err != nil {
		return errContext(err, "failed to read RPC certificate")
	}
	rpcopts := make([]wsrpc.Option, 0, 5)
	rpcopts = append(rpcopts, wsrpc.WithBasicAuth(opts.RPCUsername, rpcPassword))
	rpcopts = append(rpcopts, wsrpc.WithoutPongDeadline())
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(rpcCertificate)
	tc := &tls.Config{
		RootCAs: pool,
	}
	addr := "wss://" + opts.RPCConnect + "/ws"
	rpcopts = append(rpcopts, wsrpc.WithTLSConfig(tc))
	client, err := wsrpc.Dial(ctx, addr, rpcopts...)
	if err != nil {
		return errContext(err, "failed to connect to the wallet")
	}
	defer client.Close()

	hasher := blake256.New()

	// Read in 64KiB steps. No individual cfilter will be larger than this.
	cfbuf := make([]byte, 65536)
	readOffset := 0
	height := int32(0)
	for {
		n, ioErr := cffile.Read(cfbuf[readOffset:])
		if err != nil && !errors.Is(ioErr, io.EOF) {
			return errContext(err, "cfiltersfile read error")
		}

		var filters []string

		// Split the buffer into as many filters as needed for the next
		// cmd. Each "record" in the file is 2 bytes for an uint16
		// (size of cfilter) + n bytes for the cfilter data.
		nextcf := cfbuf[:readOffset+n]
		for len(nextcf) > 1 {
			cflen := binary.BigEndian.Uint16(nextcf)
			if int(cflen) > len(nextcf)-2 {
				// Reached the end of this block of cfilters.
				break
			}

			var cf []byte
			cf, nextcf = nextcf[2:cflen+2], nextcf[2+cflen:]
			hasher.Write(cf)
			cfhex := hex.EncodeToString(cf)
			filters = append(filters, cfhex)
		}

		// Import this batch of cfilters.
		if len(filters) > 0 {
			err = client.Call(ctx, "importcfiltersv2", nil, height, filters)
			if err != nil {
				return errContext(err, "failed to import cfilters")
			}
		}

		// Advance to next batch.
		height += int32(len(filters))
		copy(cfbuf[0:], nextcf[:])
		readOffset = len(nextcf)

		// Finish only after processing any data that might have been
		// returned at the last buffered read.
		if errors.Is(ioErr, io.EOF) {
			break
		}
	}

	var cfsetHash chainhash.Hash
	cfsetHash.SetBytes(hasher.Sum(nil))
	fmt.Printf("Hash of cf data sent: %s\n", cfsetHash)
	fmt.Printf("Height: %d\n", height-1)

	return nil
}

func main() {
	err := repair()
	if err != nil {
		fatalf("%v", err)
	}
}
