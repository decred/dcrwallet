// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file should compiled from the commit the file was introduced, otherwise
// it may not compile due to API changes, or may not create the database with
// the correct old version.  This file should not be updated for API changes.

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	_ "github.com/decred/dcrwallet/wallet/internal/bdb"
	"github.com/decred/dcrwallet/wallet/internal/txsizes"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrwallet/walletseed"
)

const dbname = "v5.db"

var (
	pubPass  = []byte("public")
	privPass = []byte("private")
	privKey  = []byte{31: 1}
	addr     dcrutil.Address
)

var chainParams = &chaincfg.TestNet2Params

var (
	epoch     time.Time
	votebits  = stake.VoteBits{Bits: 1}
	subsidies = blockchain.NewSubsidyCache(0, chainParams)
)

const ticketFeeLimits = 0x5800 // This is dark magic

func main() {
	err := setup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}
	err = compress()
	if err != nil {
		fmt.Fprintf(os.Stderr, "compress: %v\n", err)
		os.Exit(1)
	}
}

func setup() error {
	os.Remove(dbname)
	db, err := walletdb.Create("bdb", dbname)
	if err != nil {
		return err
	}
	defer db.Close()
	seed, err := walletseed.GenerateRandomSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		return err
	}
	err = udb.Initialize(db, chainParams, seed, pubPass, privPass)
	if err != nil {
		return err
	}

	amgr, txmgr, _, err := udb.Open(db, chainParams, pubPass)
	if err != nil {
		return err
	}

	return walletdb.Update(db, func(dbtx walletdb.ReadWriteTx) error {
		amgrns := dbtx.ReadWriteBucket([]byte("waddrmgr"))
		txmgrns := dbtx.ReadWriteBucket([]byte("wtxmgr"))

		err := amgr.Unlock(amgrns, privPass)
		if err != nil {
			return err
		}

		privKey, _ := secp256k1.PrivKeyFromBytes(secp256k1.S256(), privKey)
		wif, err := dcrutil.NewWIF(privKey, chainParams, chainec.ECTypeSecp256k1)
		if err != nil {
			return err
		}
		maddr, err := amgr.ImportPrivateKey(amgrns, wif)
		if err != nil {
			return err
		}
		addr = maddr.Address()

		// Add some fake blocks
		var prevBlock = chainParams.GenesisHash
		for height := 1; height < 8; height++ {
			var buf bytes.Buffer
			err := (&wire.BlockHeader{
				Version:      1,
				PrevBlock:    *prevBlock,
				StakeVersion: 1,
				VoteBits:     1,
				Height:       uint32(height),
			}).Serialize(&buf)
			if err != nil {
				return err
			}
			headerData := udb.BlockHeaderData{
				BlockHash: chainhash.Hash{31: byte(height)},
			}
			copy(headerData.SerializedHeader[:], buf.Bytes())
			err = txmgr.ExtendMainChain(txmgrns, &headerData)
			if err != nil {
				return err
			}
			prevBlock = &headerData.BlockHash
		}

		// Create 6 tickets:
		//
		//  0: Unmined, unspent
		//  1: Mined at height 2, unspent
		//  2: Mined at height 3, spent by unmined vote
		//  3: Mined at height 4, spent by mined vote in block 5
		//  4: Mined at height 5, spent by unmined revocation
		//  5: Mined at height 6, spent by mined revocation in block 7
		//
		// These tickets have the following hashes:
		//
		//  0: 7bc19eb0bf3a57be73d6879b6c411404b14b0156353dd47c5e0456768704bfd1
		//  1: a6abeb0127c347b5f38ebc2401134b324612d5b1ad9a9b8bdf6a91521842b7b1
		//  2: 1107757fa4f238803192c617c7b60bf35bdc57bc0fc94b408c71239ff9eaeb98
		//  3: 3fd00cda28c4d148e0cd38e1d646ba1365116b3ddd9a49aca4483bef80513ff9
		//  4: f4bdebefaa174470182960046fa53f554108b8ea09a86de5306a14c3a0124566
		//  5: bca8c2649860585f10b27d774b354ea7b80007e9ad79c090ea05596d63995cf5
		for i := uint32(0); i < 6; i++ {
			height := int32(i + 1)
			ticket, err := createUnsignedTicketPurchase(&wire.OutPoint{Index: i}, 50e8, 50e8-3e5)
			if err != nil {
				return err
			}
			ticketRec, err := udb.NewTxRecordFromMsgTx(ticket, epoch)
			if err != nil {
				return err
			}
			var block *udb.BlockMeta
			switch i {
			case 0:
				err := txmgr.InsertMemPoolTx(txmgrns, ticketRec)
				if err != nil {
					return err
				}
			default:
				block = &udb.BlockMeta{
					Block: udb.Block{
						Hash:   chainhash.Hash{31: byte(height)},
						Height: height,
					},
					VoteBits: 1,
				}
				err := txmgr.InsertMinedTx(txmgrns, amgrns, ticketRec, &block.Hash)
				if err != nil {
					return err
				}
			}
			err = txmgr.AddCredit(txmgrns, ticketRec, block, 0, false, udb.ImportedAddrAccount)
			if err != nil {
				return err
			}
			log.Printf("Added ticket %v", &ticketRec.Hash)

			switch i {
			case 0, 1:
				// Skip adding a spender
				continue
			}

			var spender *wire.MsgTx
			switch i {
			case 2, 3:
				spender, err = createUnsignedVote(&ticketRec.Hash, ticket,
					height+1, &chainhash.Hash{31: byte(height + 1)}, votebits,
					subsidies, chainParams)
			case 4, 5:
				spender, err = createUnsignedRevocation(&ticketRec.Hash, ticket, 1e5)
			}
			if err != nil {
				return err
			}
			spenderRec, err := udb.NewTxRecordFromMsgTx(spender, epoch)
			if err != nil {
				return err
			}
			block = nil
			switch i {
			case 2, 4:
				err := txmgr.InsertMemPoolTx(txmgrns, spenderRec)
				if err != nil {
					return err
				}
			case 3, 5:
				block = &udb.BlockMeta{
					Block: udb.Block{
						Hash:   chainhash.Hash{31: byte(height + 1)},
						Height: height + 1,
					},
					VoteBits: 1,
				}
				err := txmgr.InsertMinedTx(txmgrns, amgrns, spenderRec, &block.Hash)
				if err != nil {
					return err
				}
			}
			err = txmgr.AddCredit(txmgrns, spenderRec, block, 0, false, udb.ImportedAddrAccount)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func compress() error {
	db, err := os.Open(dbname)
	if err != nil {
		return err
	}
	defer os.Remove(dbname)
	defer db.Close()
	dbgz, err := os.Create(dbname + ".gz")
	if err != nil {
		return err
	}
	defer dbgz.Close()
	gz := gzip.NewWriter(dbgz)
	_, err = io.Copy(gz, db)
	if err != nil {
		return err
	}
	return gz.Close()
}

func createUnsignedTicketPurchase(prevOut *wire.OutPoint,
	inputAmount, ticketPrice dcrutil.Amount) (*wire.MsgTx, error) {

	tx := wire.NewMsgTx()
	txIn := wire.NewTxIn(prevOut, nil)
	txIn.ValueIn = inputAmount
	tx.AddTxIn(txIn)

	pkScript, err := txscript.PayToSStx(addr)
	if err != nil {
		return nil, err
	}

	tx.AddTxOut(wire.NewTxOut(int64(ticketPrice), pkScript))

	_, amountsCommitted, err := stake.SStxNullOutputAmounts(
		[]int64{int64(inputAmount)}, []int64{0}, int64(ticketPrice))
	if err != nil {
		return nil, err
	}

	pkScript, err = txscript.GenerateSStxAddrPush(addr,
		dcrutil.Amount(amountsCommitted[0]), ticketFeeLimits)
	if err != nil {
		return nil, err
	}
	tx.AddTxOut(wire.NewTxOut(0, pkScript))

	pkScript, err = txscript.PayToSStxChange(addr)
	if err != nil {
		return nil, err
	}
	tx.AddTxOut(wire.NewTxOut(0, pkScript))

	_, err = stake.IsSStx(tx)
	return tx, err
}

// newVoteScript generates a voting script from the passed VoteBits, for
// use in a vote.
func newVoteScript(voteBits stake.VoteBits) ([]byte, error) {
	b := make([]byte, 2+len(voteBits.ExtendedBits))
	binary.LittleEndian.PutUint16(b[0:2], voteBits.Bits)
	copy(b[2:], voteBits.ExtendedBits[:])
	return txscript.GenerateProvablyPruneableOut(b)
}

// createUnsignedVote creates an unsigned vote transaction that votes using the
// ticket specified by a ticket purchase hash and transaction with the provided
// vote bits.  The block height and hash must be of the previous block the vote
// is voting on.
func createUnsignedVote(ticketHash *chainhash.Hash, ticketPurchase *wire.MsgTx,
	blockHeight int32, blockHash *chainhash.Hash, voteBits stake.VoteBits,
	subsidyCache *blockchain.SubsidyCache, params *chaincfg.Params) (*wire.MsgTx, error) {

	// Parse the ticket purchase transaction to determine the required output
	// destinations for vote rewards or revocations.
	ticketPayKinds, ticketHash160s, ticketValues, _, _, _ :=
		stake.TxSStxStakeOutputInfo(ticketPurchase)

	// Calculate the subsidy for votes at this height.
	subsidy := blockchain.CalcStakeVoteSubsidy(subsidyCache, int64(blockHeight),
		params)

	// Calculate the output values from this vote using the subsidy.
	voteRewardValues := stake.CalculateRewards(ticketValues,
		ticketPurchase.TxOut[0].Value, subsidy)

	// Begin constructing the vote transaction.
	vote := wire.NewMsgTx()

	// Add stakebase input to the vote.
	stakebaseOutPoint := wire.NewOutPoint(&chainhash.Hash{}, ^uint32(0),
		wire.TxTreeRegular)
	stakebaseInput := wire.NewTxIn(stakebaseOutPoint, nil)
	stakebaseInput.ValueIn = subsidy
	vote.AddTxIn(stakebaseInput)

	// Votes reference the ticket purchase with the second input.
	ticketOutPoint := wire.NewOutPoint(ticketHash, 0, wire.TxTreeStake)
	ticketInput := wire.NewTxIn(ticketOutPoint, nil)
	ticketInput.ValueIn = ticketPurchase.TxOut[ticketOutPoint.Index].Value
	vote.AddTxIn(ticketInput)

	// The first output references the previous block the vote is voting on.
	// This function never errors.
	blockRefScript, _ := txscript.GenerateSSGenBlockRef(*blockHash,
		uint32(blockHeight))
	vote.AddTxOut(wire.NewTxOut(0, blockRefScript))

	// The second output contains the votebits encode as a null data script.
	voteScript, err := newVoteScript(voteBits)
	if err != nil {
		return nil, err
	}
	vote.AddTxOut(wire.NewTxOut(0, voteScript))

	// All remaining outputs pay to the output destinations and amounts tagged
	// by the ticket purchase.
	for i, hash160 := range ticketHash160s {
		scriptFn := txscript.PayToSSGenPKHDirect
		if ticketPayKinds[i] { // P2SH
			scriptFn = txscript.PayToSSGenSHDirect
		}
		// Error is checking for a nil hash160, just ignore it.
		script, _ := scriptFn(hash160)
		vote.AddTxOut(wire.NewTxOut(voteRewardValues[i], script))
	}

	return vote, nil
}

// createUnsignedRevocation creates an unsigned revocation transaction that
// revokes a missed or expired ticket.  Revocations must carry a relay fee and
// this function can error if the revocation contains no suitable output to
// decrease the estimated relay fee from.
func createUnsignedRevocation(ticketHash *chainhash.Hash, ticketPurchase *wire.MsgTx, feePerKB dcrutil.Amount) (*wire.MsgTx, error) {
	// Parse the ticket purchase transaction to determine the required output
	// destinations for vote rewards or revocations.
	ticketPayKinds, ticketHash160s, ticketValues, _, _, _ :=
		stake.TxSStxStakeOutputInfo(ticketPurchase)

	// Calculate the output values for the revocation.  Revocations do not
	// contain any subsidy.
	revocationValues := stake.CalculateRewards(ticketValues,
		ticketPurchase.TxOut[0].Value, 0)

	// Begin constructing the revocation transaction.
	revocation := wire.NewMsgTx()

	// Revocations reference the ticket purchase with the first (and only)
	// input.
	ticketOutPoint := wire.NewOutPoint(ticketHash, 0, wire.TxTreeStake)
	ticketInput := wire.NewTxIn(ticketOutPoint, nil)
	ticketInput.ValueIn = ticketPurchase.TxOut[ticketOutPoint.Index].Value
	revocation.AddTxIn(ticketInput)

	// All remaining outputs pay to the output destinations and amounts tagged
	// by the ticket purchase.
	for i, hash160 := range ticketHash160s {
		scriptFn := txscript.PayToSSRtxPKHDirect
		if ticketPayKinds[i] { // P2SH
			scriptFn = txscript.PayToSSRtxSHDirect
		}
		// Error is checking for a nil hash160, just ignore it.
		script, _ := scriptFn(hash160)
		revocation.AddTxOut(wire.NewTxOut(revocationValues[i], script))
	}

	// Revocations must pay a fee but do so by decreasing one of the output
	// values instead of increasing the input value and using a change output.
	// Calculate the estimated signed serialize size.
	sizeEstimate := txsizes.EstimateSerializeSize(1, revocation.TxOut, false)
	feeEstimate := txrules.FeeForSerializeSize(feePerKB, sizeEstimate)

	// Reduce the output value of one of the outputs to accommodate for the relay
	// fee.  To avoid creating dust outputs, a suitable output value is reduced
	// by the fee estimate only if it is large enough to not create dust.  This
	// code does not currently handle reducing the output values of multiple
	// commitment outputs to accommodate for the fee.
	for _, output := range revocation.TxOut {
		if dcrutil.Amount(output.Value) > feeEstimate {
			amount := dcrutil.Amount(output.Value) - feeEstimate
			if !txrules.IsDustAmount(amount, len(output.PkScript), feePerKB) {
				output.Value = int64(amount)
				return revocation, nil
			}
		}
	}
	return nil, errors.New("no suitable revocation outputs to pay relay fee")
}
