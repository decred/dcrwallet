// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	dcrrpcclient "github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/internal/cfgutil"
	"github.com/decred/dcrwallet/netparams"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	walletDataDirectory = dcrutil.AppDataDir("dcrwallet", false)
	newlineBytes        = []byte{'\n'}
)

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Stderr.Write(newlineBytes)
	os.Exit(1)
}

func errContext(err error, context string) error {
	return fmt.Errorf("%s: %v", context, err)
}

// Flags.
var opts = struct {
	TestNet               bool                `long:"testnet" description:"Use the test decred network"`
	SimNet                bool                `long:"simnet" description:"Use the simulation decred network"`
	RPCConnect            string              `short:"c" long:"connect" description:"Hostname[:port] of wallet RPC server"`
	RPCUsername           string              `short:"u" long:"rpcuser" description:"Wallet RPC username"`
	RPCPassword           string              `short:"P" long:"rpcpass" description:"Wallet RPC password"`
	RPCCertificateFile    string              `long:"cafile" description:"Wallet RPC TLS certificate"`
	FeeRate               *cfgutil.AmountFlag `long:"feerate" description:"Transaction fee per kilobyte"`
	SourceAccount         string              `long:"sourceacct" description:"Account to sweep outputs from"`
	SourceAddress         string              `long:"sourceaddr" description:"Address to sweep outputs from"`
	DestinationAccount    string              `long:"destacct" description:"Account to send sweeped outputs to"`
	DestinationAddress    string              `long:"destaddr" description:"Address to send sweeped outputs to"`
	RequiredConfirmations int64               `long:"minconf" description:"Required confirmations to include an output"`
	DryRun                bool                `long:"dryrun" description:"Do not actually send any transactions but output what would have happened"`
}{
	TestNet:               false,
	SimNet:                false,
	RPCConnect:            "localhost",
	RPCUsername:           "",
	RPCPassword:           "",
	RPCCertificateFile:    filepath.Join(walletDataDirectory, "rpc.cert"),
	FeeRate:               cfgutil.NewAmountFlag(txrules.DefaultRelayFeePerKb),
	SourceAccount:         "",
	SourceAddress:         "",
	DestinationAccount:    "",
	DestinationAddress:    "",
	RequiredConfirmations: 2,
	DryRun:                false,
}

// Parse and validate flags.
func init() {
	// Unset localhost defaults if certificate file can not be found.
	certFileExists, err := cfgutil.FileExists(opts.RPCCertificateFile)
	if err != nil {
		fatalf("%v", err)
	}
	if !certFileExists {
		opts.RPCConnect = ""
		opts.RPCCertificateFile = ""
	}

	_, err = flags.Parse(&opts)
	if err != nil {
		os.Exit(1)
	}

	if opts.TestNet && opts.SimNet {
		fatalf("Multiple decred networks may not be used simultaneously")
	}
	var activeNet = &netparams.MainNetParams
	if opts.TestNet {
		activeNet = &netparams.TestNet3Params
	} else if opts.SimNet {
		activeNet = &netparams.SimNetParams
	}

	if opts.RPCConnect == "" {
		fatalf("RPC hostname[:port] is required")
	}
	rpcConnect, err := cfgutil.NormalizeAddress(opts.RPCConnect, activeNet.JSONRPCServerPort)
	if err != nil {
		fatalf("Invalid RPC network address `%v`: %v", opts.RPCConnect, err)
	}
	opts.RPCConnect = rpcConnect

	if opts.RPCUsername == "" {
		fatalf("RPC username is required")
	}

	certFileExists, err = cfgutil.FileExists(opts.RPCCertificateFile)
	if err != nil {
		fatalf("%v", err)
	}
	if !certFileExists {
		fatalf("RPC certificate file `%s` not found", opts.RPCCertificateFile)
	}

	if opts.FeeRate.Amount > 1e8 {
		fatalf("Fee rate `%v/kB` is exceptionally high", opts.FeeRate.Amount)
	}
	if opts.FeeRate.Amount < 1e2 {
		fatalf("Fee rate `%v/kB` is exceptionally low", opts.FeeRate.Amount)
	}
	if opts.SourceAccount == "" && opts.SourceAddress == "" {
		fatalf("A source is required")
	}
	if opts.SourceAccount != "" && opts.SourceAccount == opts.DestinationAccount {
		fatalf("Source and destination accounts should not be equal")
	}
	if opts.DestinationAccount == "" && opts.DestinationAddress == "" {
		fatalf("A destination is required")
	}
	if opts.DestinationAccount != "" && opts.DestinationAddress != "" {
		fatalf("Destination must be either an account or an address")
	}
	if opts.RequiredConfirmations < 0 {
		fatalf("Required confirmations must be non-negative")
	}
}

// noInputValue describes an error returned by the input source when no inputs
// were selected because each previous output value was zero.  Callers of
// txauthor.NewUnsignedTransaction need not report these errors to the user.
type noInputValue struct {
}

func (noInputValue) Error() string { return "no input value" }

// makeInputSource creates an InputSource that creates inputs for every unspent
// output with non-zero output values.  The target amount is ignored since every
// output is consumed.  The InputSource does not return any previous output
// scripts as they are not needed for creating the unsinged transaction and are
// looked up again by the wallet during the call to signrawtransaction.
func makeInputSource(outputs []dcrjson.ListUnspentResult) txauthor.InputSource {
	var (
		totalInputValue   dcrutil.Amount
		inputs            = make([]*wire.TxIn, 0, len(outputs))
		redeemScriptSizes = make([]int, 0, len(outputs))
		sourceErr         error
	)
	for _, output := range outputs {
		outputAmount, err := dcrutil.NewAmount(output.Amount)
		if err != nil {
			sourceErr = fmt.Errorf(
				"invalid amount `%v` in listunspent result",
				output.Amount)
			break
		}
		if outputAmount == 0 {
			continue
		}
		if !saneOutputValue(outputAmount) {
			sourceErr = fmt.Errorf(
				"impossible output amount `%v` in listunspent result",
				outputAmount)
			break
		}
		totalInputValue += outputAmount

		previousOutPoint, err := parseOutPoint(&output)
		if err != nil {
			sourceErr = fmt.Errorf(
				"invalid data in listunspent result: %v", err)
			break
		}

		txIn := wire.NewTxIn(&previousOutPoint, int64(outputAmount), nil)
		inputs = append(inputs, txIn)
	}

	if sourceErr == nil && totalInputValue == 0 {
		sourceErr = noInputValue{}
	}

	return func(dcrutil.Amount) (*txauthor.InputDetail, error) {
		inputDetail := txauthor.InputDetail{
			Amount:            totalInputValue,
			Inputs:            inputs,
			Scripts:           nil,
			RedeemScriptSizes: redeemScriptSizes,
		}
		return &inputDetail, sourceErr
	}
}

// destinationScriptSourceToAccount is a ChangeSource which is used to receive
// all correlated previous input value.
type destinationScriptSourceToAccount struct {
	accountName string
	rpcClient   *dcrrpcclient.Client
}

// Source creates a non-change address.
func (src *destinationScriptSourceToAccount) Script() ([]byte, uint16, error) {
	destinationAddress, err := src.rpcClient.GetNewAddress(src.accountName)
	if err != nil {
		return nil, 0, err
	}

	script, err := txscript.PayToAddrScript(destinationAddress)
	if err != nil {
		return nil, 0, err
	}

	return script, txscript.DefaultScriptVersion, nil
}

func (src *destinationScriptSourceToAccount) ScriptSize() int {
	return 25 // P2PKHPkScriptSize
}

// destinationScriptSourceToAddress s a ChangeSource which is used to
// receive all correlated previous input value.
type destinationScriptSourceToAddress struct {
	address string
}

// Source creates a non-change address.
func (src *destinationScriptSourceToAddress) Script() ([]byte, uint16, error) {
	destinationAddress, err := dcrutil.DecodeAddress(src.address)
	if err != nil {
		return nil, 0, err
	}
	script, err := txscript.PayToAddrScript(destinationAddress)
	return script, txscript.DefaultScriptVersion, err
}

func (src *destinationScriptSourceToAddress) ScriptSize() int {
	return 25 // P2PKHPkScriptSize
}

func main() {
	err := sweep()
	if err != nil {
		fatalf("%v", err)
	}
}

func sweep() error {
	rpcPassword := opts.RPCPassword

	if rpcPassword == "" {
		secret, err := promptSecret("Wallet RPC password")
		if err != nil {
			return errContext(err, "failed to read RPC password")
		}

		rpcPassword = secret
	}

	// Open RPC client.
	rpcCertificate, err := ioutil.ReadFile(opts.RPCCertificateFile)
	if err != nil {
		return errContext(err, "failed to read RPC certificate")
	}
	rpcClient, err := dcrrpcclient.New(&dcrrpcclient.ConnConfig{
		Host:         opts.RPCConnect,
		User:         opts.RPCUsername,
		Pass:         rpcPassword,
		Certificates: rpcCertificate,
		HTTPPostMode: true,
	}, nil)
	if err != nil {
		return errContext(err, "failed to create RPC client")
	}
	defer rpcClient.Shutdown()

	// Fetch all unspent outputs, ignore those not from the source
	// account, and group by their destination address.  Each grouping of
	// outputs will be used as inputs for a single transaction sending to a
	// new destination account address.
	unspentOutputs, err := rpcClient.ListUnspent()
	if err != nil {
		return errContext(err, "failed to fetch unspent outputs")
	}
	sourceOutputs := make(map[string][]dcrjson.ListUnspentResult)
	for _, unspentOutput := range unspentOutputs {
		if !unspentOutput.Spendable {
			continue
		}
		if unspentOutput.Confirmations < opts.RequiredConfirmations {
			continue
		}
		if opts.SourceAccount != "" && opts.SourceAccount != unspentOutput.Account {
			continue
		}
		if opts.SourceAddress != "" && opts.SourceAddress != unspentOutput.Address {
			continue
		}
		sourceAddressOutputs := sourceOutputs[unspentOutput.Address]
		sourceOutputs[unspentOutput.Address] = append(sourceAddressOutputs, unspentOutput)
	}

	for address, outputs := range sourceOutputs {
		outputNoun := pickNoun(len(outputs), "output", "outputs")
		fmt.Printf("Found %d matching unspent %s for address %s\n",
			len(outputs), outputNoun, address)
	}

	var privatePassphrase string
	if len(sourceOutputs) != 0 {
		privatePassphrase, err = promptSecret("Wallet private passphrase")
		if err != nil {
			return errContext(err, "failed to read private passphrase")
		}
	}

	var totalSwept dcrutil.Amount
	var numErrors int
	var reportError = func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, format, args...)
		os.Stderr.Write(newlineBytes)
		numErrors++
	}
	for _, previousOutputs := range sourceOutputs {
		inputSource := makeInputSource(previousOutputs)

		var destinationSourceToAccount *destinationScriptSourceToAccount
		var destinationSourceToAddress *destinationScriptSourceToAddress
		var atx *txauthor.AuthoredTx
		var err error

		if opts.DestinationAccount != "" {
			destinationSourceToAccount = &destinationScriptSourceToAccount{
				accountName: opts.DestinationAccount,
				rpcClient:   rpcClient,
			}
			atx, err = txauthor.NewUnsignedTransaction(nil, opts.FeeRate.Amount,
				inputSource, destinationSourceToAccount)
		}

		if opts.DestinationAddress != "" {
			destinationSourceToAddress = &destinationScriptSourceToAddress{
				address: opts.DestinationAddress,
			}
			atx, err = txauthor.NewUnsignedTransaction(nil, opts.FeeRate.Amount,
				inputSource, destinationSourceToAddress)
		}

		if err != nil {
			if err != (noInputValue{}) {
				reportError("Failed to create unsigned transaction: %v", err)
			}
			continue
		}

		// Unlock the wallet, sign the transaction, and immediately lock.
		err = rpcClient.WalletPassphrase(privatePassphrase, 60)
		if err != nil {
			reportError("Failed to unlock wallet: %v", err)
			continue
		}
		signedTransaction, complete, err := rpcClient.SignRawTransaction(atx.Tx)
		_ = rpcClient.WalletLock()
		if err != nil {
			reportError("Failed to sign transaction: %v", err)
			continue
		}
		if !complete {
			reportError("Failed to sign every input")
			continue
		}

		// Publish the signed sweep transaction.
		txHash := "DRYRUN"
		if opts.DryRun {
			fmt.Printf("DRY RUN: not actually sending transaction\n")
		} else {
			hash, err := rpcClient.SendRawTransaction(signedTransaction, false)
			if err != nil {
				reportError("Failed to publish transaction: %v", err)
				continue
			}

			txHash = hash.String()
		}

		outputAmount := dcrutil.Amount(atx.Tx.TxOut[0].Value)
		fmt.Printf("Swept %v to destination with transaction %v\n",
			outputAmount, txHash)
		totalSwept += outputAmount
	}

	numPublished := len(sourceOutputs) - numErrors
	transactionNoun := pickNoun(numErrors, "transaction", "transactions")
	if numPublished != 0 {
		fmt.Printf("Swept %v to destination across %d %s\n",
			totalSwept, numPublished, transactionNoun)
	}
	if numErrors > 0 {
		return fmt.Errorf("Failed to publish %d %s", numErrors, transactionNoun)
	}

	return nil
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

func saneOutputValue(amount dcrutil.Amount) bool {
	return amount >= 0 && amount <= dcrutil.MaxAmount
}

func parseOutPoint(input *dcrjson.ListUnspentResult) (wire.OutPoint, error) {
	txHash, err := chainhash.NewHashFromStr(input.TxID)
	if err != nil {
		return wire.OutPoint{}, err
	}
	return wire.OutPoint{Hash: *txHash, Index: input.Vout, Tree: input.Tree}, nil
}

func pickNoun(n int, singularForm, pluralForm string) string {
	if n == 1 {
		return singularForm
	}
	return pluralForm
}
