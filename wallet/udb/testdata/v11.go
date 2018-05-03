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
	"fmt"
	"io"
	"os"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/internal/walletdb"
	_ "github.com/decred/dcrwallet/wallet/internal/walletdb/bdb"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletseed"
	"golang.org/x/crypto/ripemd160"
)

const dbname = "v11.db"

var (
	epoch           time.Time
	pubPass         = []byte("public")
	privPass        = []byte("private")
	privKey         = []byte{31: 1}
	byteOrder       = binary.BigEndian
	bucketMultisig  = []byte("ms")
	bucketTxRecords = []byte("t")
)

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
	const op errors.Op = "udb.V11DatabaseSetup"

	var chainParams = &chaincfg.TestNet3Params
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

		_, pk := secp256k1.PrivKeyFromBytes(privKey)

		// Add a block
		prevBlock := chainParams.GenesisHash
		buf := bytes.Buffer{}
		header := &wire.BlockHeader{
			Version:      1,
			PrevBlock:    *prevBlock,
			StakeVersion: 1,
			VoteBits:     1,
			Height:       uint32(1),
		}
		err = header.Serialize(&buf)
		if err != nil {
			return err
		}

		headerData := &udb.BlockHeaderData{
			BlockHash: header.BlockHash(),
		}
		copy(headerData.SerializedHeader[:], buf.Bytes())

		filters := emptyFilters(1)
		err = header.Deserialize(bytes.NewReader(headerData.SerializedHeader[:]))
		if err != nil {
			return err
		}
		err = txmgr.ExtendMainChain(txmgrns, header, filters[0])
		if err != nil {
			return err
		}

		// Add 2 mso with TX and 4 without
		for count := 1; count <= 6; count++ {
			msgTx := wire.NewMsgTx()

			address1, err := dcrutil.NewAddressSecpPubKeyCompressed(pk, chainParams)

			msScript, err := txscript.MultiSigScript([]*dcrutil.AddressSecpPubKey{address1}, 1)
			if err != nil {
				return err
			}

			_, err = amgr.ImportScript(amgrns, msScript)
			if err != nil {
				// We don't care if we've already used this address.
				if err != nil && !errors.Is(errors.Exist, err) {
					return errors.E(op, err)
				}
			}
			err = txmgr.InsertTxScript(txmgrns, msScript)
			if err != nil {
				return err
			}
			scAddr, err := dcrutil.NewAddressScriptHash(msScript, chainParams)
			if err != nil {
				return err
			}
			p2shScript, err := txscript.PayToAddrScript(scAddr)
			if err != nil {
				return err
			}

			msgTx.AddTxOut(wire.NewTxOut(int64(dcrutil.Amount(1*count)), p2shScript))
			msgTx.Expiry = wire.NoExpiryValue
			rec, err := udb.NewTxRecordFromMsgTx(msgTx, epoch)
			if err != nil {
				return err
			}

			// put 2 tx records
			if count <= 2 {
				err = putTxRecord(txmgrns, rec, &udb.Block{
					Hash:   headerData.BlockHash,
					Height: 1,
				})
				if err != nil {
					return err
				}
			}

			var p2shScriptHash [ripemd160.Size]byte
			key := keyMultisigOut(rec.Hash, 0)
			val := valueMultisigOut(p2shScriptHash, 1, 1, false, 0, headerData.BlockHash,
				1, dcrutil.Amount(0), chainhash.Hash{}, 0xFFFFFFFF, rec.Hash)

			err = putMultisigOutRawValues(txmgrns, key, val)
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

func emptyFilters(n int) []*gcs.Filter {
	f := make([]*gcs.Filter, n)
	for i := range f {
		f[i], _ = gcs.FromBytes(0, blockcf.P, nil)
	}
	return f
}

func keyMultisigOut(hash chainhash.Hash, index uint32) []byte {
	return canonicalOutPoint(&hash, index)
}

func valueMultisigOut(sh [ripemd160.Size]byte, m uint8, n uint8,
	spent bool, tree int8, blockHash chainhash.Hash,
	blockHeight uint32, amount dcrutil.Amount, spentBy chainhash.Hash,
	sbi uint32, txHash chainhash.Hash) []byte {
	v := make([]byte, 135)

	copy(v[0:20], sh[0:20])
	v[20] = m
	v[21] = n
	v[22] = uint8(0)

	if spent {
		v[22] |= 1 << 0
	}

	if tree == wire.TxTreeStake {
		v[22] |= 1 << 1
	}

	copy(v[23:55], blockHash[:])
	byteOrder.PutUint32(v[55:59], blockHeight)
	byteOrder.PutUint64(v[59:67], uint64(amount))

	copy(v[67:99], spentBy[:])
	byteOrder.PutUint32(v[99:103], sbi)

	copy(v[103:135], txHash[:])

	return v
}

func canonicalOutPoint(txHash *chainhash.Hash, index uint32) []byte {
	k := make([]byte, 36)
	copy(k, txHash[:])
	byteOrder.PutUint32(k[32:36], index)
	return k
}

func putMultisigOutRawValues(ns walletdb.ReadWriteBucket, k []byte, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketMultisig).Put(k, v)
	if err != nil {
		return err
	}
	return nil
}

func keyTxRecord(txHash *chainhash.Hash, block *udb.Block) []byte {
	k := make([]byte, 68)
	copy(k, txHash[:])
	byteOrder.PutUint32(k[32:36], uint32(block.Height))
	copy(k[36:68], block.Hash[:])
	return k
}

func putTxRecord(ns walletdb.ReadWriteBucket, rec *udb.TxRecord, block *udb.Block) error {
	k := keyTxRecord(&rec.Hash, block)
	v, err := valueTxRecord(rec)
	if err != nil {
		return err
	}
	err = ns.NestedReadWriteBucket(bucketTxRecords).Put(k, v)
	if err != nil {
		str := fmt.Sprintf("%s: put failed for %v", bucketTxRecords, rec.Hash)
		return errors.New(str)
	}
	return nil
}

func valueTxRecord(rec *udb.TxRecord) ([]byte, error) {
	var v []byte
	if rec.SerializedTx == nil {
		txSize := rec.MsgTx.SerializeSize()
		v = make([]byte, 8, 8+txSize)
		err := rec.MsgTx.Serialize(bytes.NewBuffer(v[8:]))
		if err != nil {
			str := fmt.Sprintf("unable to serialize transaction %v", rec.Hash)
			return nil, errors.New(str)
		}
		v = v[:cap(v)]
	} else {
		v = make([]byte, 8+len(rec.SerializedTx))
		copy(v[8:], rec.SerializedTx)
	}
	byteOrder.PutUint64(v, uint64(rec.Received.Unix()))
	return v, nil
}
