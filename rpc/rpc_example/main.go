package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrwallet/v5/rpc/client/dcrwallet"
	"decred.org/dcrwallet/v5/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/rpcclient/v8"
)

const (
	host = "localhost:9113"
	user = "user"
	pass = "pass"
)

var (
	certFile = filepath.Join(dcrutil.AppDataDir("dcrwallet", false), "rpc.cert")
	params   = chaincfg.TestNet3Params()
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}

func connectWallet(host, user, pass, certFile string) (*dcrwallet.Client, error) {
	cert, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	connCfg := &rpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws",
		User:         user,
		Pass:         pass,
		Certificates: cert,
	}
	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		return nil, err
	}
	return dcrwallet.NewClient(dcrwallet.RawRequestCaller(client), params), nil
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := connectWallet(host, user, pass, certFile)
	if err != nil {
		return err
	}

	for _, script := range importScripts {
		scriptB, err := hex.DecodeString(script)
		if err != nil {
			return err
		}
		// NOTE: These scripts will forever be imported into your
		// testing wallet.
		err = client.ImportScript(ctx, scriptB)
		if err != nil {
			return err
		}
	}

	// Add an owned address to validated addresses.
	addr, err := client.GetNewAddress(ctx, "default")
	if err != nil {
		return err
	}
	validateAddrs = append(validateAddrs, addr.String())

	for _, addr := range validateAddrs {
		var res types.ValidateAddressResult
		err := client.Call(ctx, "validateaddress", &res, addr)
		if err != nil {
			return err
		}
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	}
	return nil
}

var importScripts = []string{
	// TSPEND
	"c376a914d17043c104a57393aa7353e1510e39eab811e3db88ac",
	// 2 of 2 multisig
	"522103d484eb60ad03549e731ae9045281f8ee14ff6ea11b697f32cde3d8a18992261b210342b0b9c0ecb53cb9761beb0d010bbf08b5049d2a4d3bea5d3a1d95eb664931cb52ae",
	// NonStandard
	"01",
}

var validateAddrs = []string{
	"abcd", // invalid address
	"TkKkYvSrnu8orwhtedcJGkD7guarvZUbUAtjr4iKqD9Y8pNEf8iHu",              // PubKeyEcdsaSecp256k1V0
	"Tsp18L8qTcjzigYXrD5GSdwDmhVYBpKmfUL",                                // PubKeyHashEcdsaSecp256k1V0
	"TkKnVfd6EvzEYAqiELWstkASHgVyYH8JK3gNvAxUX79C9CrnsV8W6",              // PubKeyEd25519V0
	"Tead9n1wLBgaUR7AZrrtt6WWeDfetbF3dpy",                                // PubKeyHashEd25519V0
	"TkKpSQoKgxqfDPyXp3RTWk7ktTR69zn19vU1zHCdD18r9bMTvDKT3",              // PubKeySchnorrSecp256k1V0
	"TSs3jHQMbbZGPyftUh4cgaALzgDZhfXGtxn",                                // PubKeyHashSchnorrSecp256k1V0
	"TcvVou7ooM4rJRWNeJYwehJ9fQq1HTc5pbK",                                // ScriptHashV0
	"020000000000000000000000000000000000000000000000000000000000000001", // PubKeyEcdsaSecp256k1V0 by pubkey

	// These are owned imported scripts.
	"TcfdqCrK2fiFJBZnGj5N6xs6rMsbQBsJBYf", // TSPEND
	"TcrzaAVMbFURm1PpukWru8yE2uBTjvQePoa", // 2 of 2 multisig
	"TckSpBht36nMZgnDDjv7xaHUrgCyJpxQiLA", // NonStandard
}
