/*
 * Copyright (c) 2016-2020 The Decred developers
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v2/wallet/txauthor"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/wire"
)

// params is the global representing the chain parameters. It is assigned
// in main.
var params *chaincfg.Params

// configJSON is a configuration file used for transaction generation.
type configJSON struct {
	TxFee         int64  `json:"txfee"`
	SendToAddress string `json:"sendtoaddress"`
	Network       string `json:"network"`
	DcrctlArgs    string `json:"dcrctlargs"`
}

func saneOutputValue(amount dcrutil.Amount) bool {
	return amount >= 0 && amount <= dcrutil.MaxAmount
}

func parseOutPoint(input *types.ListUnspentResult) (wire.OutPoint, error) {
	txHash, err := chainhash.NewHashFromStr(input.TxID)
	if err != nil {
		return wire.OutPoint{}, err
	}
	return wire.OutPoint{Hash: *txHash, Index: input.Vout, Tree: input.Tree}, nil
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
func makeInputSource(outputs []types.ListUnspentResult) (dcrutil.Amount, txauthor.InputSource) {
	var (
		totalInputValue   dcrutil.Amount
		inputs            = make([]*wire.TxIn, 0, len(outputs))
		redeemScriptSizes = make([]int, 0, len(outputs))
		sourceErr         error
	)
	for _, output := range outputs {

		outputAmount, err := dcrutil.NewAmount(output.Amount)
		if err != nil {
			sourceErr = fmt.Errorf("invalid amount `%v` in listunspent result",
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

	return totalInputValue, func(dcrutil.Amount) (*txauthor.InputDetail, error) {
		inputDetail := txauthor.InputDetail{
			Amount:            totalInputValue,
			Inputs:            inputs,
			Scripts:           nil,
			RedeemScriptSizes: redeemScriptSizes,
		}
		return &inputDetail, sourceErr
	}
}

func main() {
	// Create an inputSource from the loaded utxos.
	unspentFile, err := os.Open("unspent.json")
	if err != nil {
		fmt.Printf("error opening unspent file unspent.json: %v", err)
		return
	}

	var utxos []types.ListUnspentResult

	jsonParser := json.NewDecoder(unspentFile)
	if err = jsonParser.Decode(&utxos); err != nil {
		fmt.Printf("error parsing unspent file: %v", err)
		return
	}

	targetAmount, inputSource := makeInputSource(utxos)

	// Load and parse the movefunds config.
	configFile, err := os.Open("config.json")
	if err != nil {
		fmt.Printf("error opening config file config.json: %v", err)
		return
	}

	cfg := new(configJSON)

	jsonParser = json.NewDecoder(configFile)
	if err = jsonParser.Decode(cfg); err != nil {
		fmt.Printf("error parsing config file: %v", err)
		return
	}

	switch cfg.Network {
	case "testnet":
		params = chaincfg.TestNet3Params()
	case "mainnet":
		params = chaincfg.MainNetParams()
	case "simnet":
		params = chaincfg.SimNetParams()
	default:
		fmt.Printf("unknown network specified: %s", cfg.Network)
		return
	}

	addr, err := dcrutil.DecodeAddress(cfg.SendToAddress, params)
	if err != nil {
		fmt.Printf("failed to parse address %s: %v", cfg.SendToAddress, err)
		return
	}

	fee, err := dcrutil.NewAmount(float64(cfg.TxFee))
	if err != nil {
		fmt.Printf("invalid fee rate: %v", err)
		return
	}

	// Create the unsigned transaction.
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		fmt.Printf("failed to generate pkScript: %s", err)
		return
	}

	txOuts := []*wire.TxOut{wire.NewTxOut(int64(targetAmount-fee), pkScript)}
	atx, err := txauthor.NewUnsignedTransaction(txOuts, fee, inputSource, nil, params.MaxTxSize)
	if err != nil {
		fmt.Printf("failed to create unsigned transaction: %s", err)
		return
	}

	if atx.Tx.SerializeSize() > params.MaxTxSize {
		fmt.Printf("tx too big: got %v, max %v", atx.Tx.SerializeSize(),
			params.MaxTxSize)
		return
	}

	// Generate the signrawtransaction command.
	txB, err := atx.Tx.Bytes()
	if err != nil {
		fmt.Println("Failed to serialize tx: ", err.Error())
		return
	}

	// The command to sign the transaction.
	var buf bytes.Buffer
	buf.WriteString("dcrctl ")
	buf.WriteString(cfg.DcrctlArgs)
	buf.WriteString(" signrawtransaction ")
	buf.WriteString(hex.EncodeToString(txB))
	buf.WriteString(" '[")
	last := len(atx.Tx.TxIn) - 1
	for i, in := range atx.Tx.TxIn {
		buf.WriteString("{\"txid\":\"")
		buf.WriteString(in.PreviousOutPoint.Hash.String())
		buf.WriteString("\",\"vout\":")
		buf.WriteString(fmt.Sprintf("%v", in.PreviousOutPoint.Index))
		buf.WriteString(",\"tree\":")
		buf.WriteString(fmt.Sprintf("%v", in.PreviousOutPoint.Tree))
		buf.WriteString(",\"scriptpubkey\":\"")
		buf.WriteString(hex.EncodeToString(in.SignatureScript))
		buf.WriteString("\",\"redeemscript\":\"\"}")
		if i != last {
			buf.WriteString(",")
		}
	}
	buf.WriteString("]' ")
	buf.WriteString("| jq -r .hex")
	err = os.WriteFile("sign.sh", buf.Bytes(), 0755)
	if err != nil {
		fmt.Println("Failed to write signing script: ", err.Error())
		return
	}

	fmt.Println("Successfully wrote transaction to sign script.")
}
