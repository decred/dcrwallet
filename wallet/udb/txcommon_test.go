// Copyright (c) 2016-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/wire"
	_ "github.com/decred/dcrwallet/wallet/v3/drivers/bdb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

func tempDB(t *testing.T) (db walletdb.DB, teardown func()) {
	f, err := ioutil.TempFile("", "udb")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	db, err = walletdb.Create("bdb", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	teardown = func() {
		db.Close()
		os.Remove(f.Name())
	}
	return
}

type blockGenerator struct {
	lastHash   chainhash.Hash
	lastHeight int32
}

func makeBlockGenerator() blockGenerator {
	return blockGenerator{lastHash: chaincfg.TestNet3Params().GenesisHash}
}

func (g *blockGenerator) generate(voteBits uint16) *wire.BlockHeader {
	h := &wire.BlockHeader{
		PrevBlock: g.lastHash,
		VoteBits:  voteBits,
		Height:    uint32(g.lastHeight + 1),
	}
	g.lastHash = h.BlockHash()
	g.lastHeight++
	return h
}

func makeHeaderData(h *wire.BlockHeader) BlockHeaderData {
	var b bytes.Buffer
	b.Grow(wire.MaxBlockHeaderPayload)
	err := h.Serialize(&b)
	if err != nil {
		panic(err)
	}
	d := BlockHeaderData{BlockHash: h.BlockHash()}
	copy(d.SerializedHeader[:], b.Bytes())
	return d
}

func makeHeaderDataSlice(headers ...*wire.BlockHeader) []BlockHeaderData {
	data := make([]BlockHeaderData, 0, len(headers))
	for _, h := range headers {
		data = append(data, makeHeaderData(h))
	}
	return data
}

func emptyFilters(n int) []*gcs.Filter {
	f := make([]*gcs.Filter, n)
	for i := range f {
		f[i], _ = gcs.FromBytes(0, blockcf.P, nil)
	}
	return f
}

func makeBlockMeta(h *wire.BlockHeader) *BlockMeta {
	return &BlockMeta{
		Block: Block{
			Hash:   h.BlockHash(),
			Height: int32(h.Height),
		},
		Time: time.Time{},
	}
}

func decodeHash(reversedHash string) *chainhash.Hash {
	h, err := chainhash.NewHashFromStr(reversedHash)
	if err != nil {
		panic(err)
	}
	return h
}
