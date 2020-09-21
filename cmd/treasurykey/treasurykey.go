package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrutil/v3"
)

func generateKeys(params *chaincfg.Params) error {
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return err
	}

	keyBytes := key.Serialize()
	wif, err := dcrutil.NewWIF(keyBytes, params.PrivateKeyID,
		dcrec.STSchnorrSecp256k1)
	if err != nil {
		return err
	}

	fmt.Printf("Private key: %x\n", keyBytes)
	fmt.Printf("Public  key: %x\n", key.PubKey().SerializeCompressed())
	fmt.Printf("WIF        : %s\n", wif)

	return nil
}

func main() {
	mainnet := flag.Bool("mainnet", false, "use mainnet parameters")
	simnet := flag.Bool("simnet", false, "use simnet parameters")
	regnet := flag.Bool("regnet", false, "use regnet parameters")
	testnet := flag.Bool("testnet", false, "use testnet parameters")
	flag.Parse()

	var net *chaincfg.Params
	flags := 0
	if *mainnet {
		flags++
		net = chaincfg.MainNetParams()
	}
	if *testnet {
		flags++
		net = chaincfg.TestNet3Params()
	}
	if *simnet {
		flags++
		net = chaincfg.SimNetParams()
	}
	if *regnet {
		flags++
		net = chaincfg.RegNetParams()
	}
	if flags != 1 {
		fmt.Println("One and only one flag must be selected")
		flag.Usage()
		os.Exit(1)
	}

	if err := generateKeys(net); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
