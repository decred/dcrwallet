// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"fmt"
	"testing"

	"github.com/decred/dcrd/blockchain/v2/chaingen"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/wire"
)

type tg struct {
	*testing.T
	*chaingen.Generator
}

type tw struct {
	*testing.T
	*Wallet
}

type gblock struct {
	*wire.MsgBlock
	*BlockNode
}

func maketg(t *testing.T, params *chaincfg.Params) *tg {
	g, err := chaingen.MakeGenerator(params)
	if err != nil {
		t.Fatal(err)
	}
	return &tg{t, &g}
}

func (tg *tg) createBlockOne(name string) *gblock {
	blockOne := tg.CreateBlockOne(name, 0)
	f, err := blockcf.Regular(blockOne)
	if err != nil {
		tg.Fatal(err)
	}
	h := blockOne.BlockHash()
	n := &BlockNode{Header: &blockOne.Header, Hash: &h, Filter: f}
	return &gblock{blockOne, n}
}

func (tg *tg) nextBlock(blockName string, spend *chaingen.SpendableOut, ticketSpends []chaingen.SpendableOut) *gblock {
	b := tg.NextBlock(blockName, spend, ticketSpends)
	f, err := blockcf.Regular(b)
	if err != nil {
		tg.Fatal(err)
	}
	h := b.BlockHash()
	n := &BlockNode{Header: &b.Header, Hash: &h, Filter: f}
	return &gblock{b, n}
}

func (tg *tg) blockHashByName(name string) *chainhash.Hash {
	b := tg.BlockByName(name)
	h := b.BlockHash()
	return &h
}

func mustAddBlockNode(t *testing.T, forest *SidechainForest, n *BlockNode) {
	if !forest.AddBlockNode(n) {
		t.Fatalf("Could not add block %v to sidechain forest", n.Hash)
	}
}

func (tw *tw) evaluateBestChain(forest *SidechainForest, expectedBranchLen int, expectedTip *chainhash.Hash) []*BlockNode {
	bestChain, err := tw.EvaluateBestChain(forest)
	if err != nil {
		tw.Fatal(err)
	}
	if len(bestChain) != expectedBranchLen {
		tw.Fatalf("expected best chain len %v, got %v", expectedBranchLen, len(bestChain))
	}
	if len(bestChain) != 0 && *bestChain[len(bestChain)-1].Hash != *expectedTip {
		tw.Fatalf("expected best chain tip %v, got %v", expectedTip, bestChain[len(bestChain)-1].Hash)
	}
	return bestChain
}

func (tw *tw) assertNoBetterChain(forest *SidechainForest) {
	tw.evaluateBestChain(forest, 0, nil)
}

func (tw *tw) chainSwitch(forest *SidechainForest, chain []*BlockNode) {
	prevChain, err := tw.ChainSwitch(context.Background(), forest, chain, nil)
	if err != nil {
		tw.Fatal(err)
	}
	for _, n := range prevChain {
		forest.AddBlockNode(n)
	}
	tip, _ := tw.MainChainTip()
	if tip != *chain[len(chain)-1].Hash {
		tw.Fatalf("expected tip %v, got %v", chain[len(chain)-1].Hash, &tip)
	}
}

func (tw *tw) expectBlockInMainChain(hash *chainhash.Hash, have, invalidated bool) {
	haveBlock, isInvalidated, err := tw.BlockInMainChain(hash)
	if err != nil {
		tw.Fatal(err)
	}
	if haveBlock != have {
		tw.Fatalf("Expected block %v: %v, actually have block: %v", hash, have, haveBlock)
	}
	if isInvalidated != invalidated {
		tw.Fatalf("Expected block %v invalidated: %v, actually invalidated: %v", hash, invalidated, isInvalidated)
	}
}

func assertSidechainTree(t *testing.T, tree *sidechainRootedTree, root *chainhash.Hash, tips ...*chainhash.Hash) {
	if *tree.root.Hash != *root {
		t.Fatalf("expected root %v, got %v", root, tree.root.Hash)
	}
	if len(tips) != len(tree.tips) {
		t.Fatalf("expected %v tip(s), got %v", len(tips), len(tree.tips))
	}
	for _, tip := range tips {
		if _, ok := tree.tips[*tip]; !ok {
			t.Fatalf("missing tip %v", tip)
		}
	}
}

func TestReorg(t *testing.T) {
	t.Parallel()

	cfg := basicWalletConfig
	w, teardown := testWallet(t, &cfg)
	defer teardown()

	tg := maketg(t, cfg.Params)
	tw := &tw{t, w}
	forest := new(SidechainForest)

	blockOne := tg.createBlockOne("block-one")
	mustAddBlockNode(t, forest, blockOne.BlockNode)
	t.Logf("Generated block one %v", blockOne.Hash)

	bestChain := tw.evaluateBestChain(forest, 1, blockOne.Hash)
	tw.chainSwitch(forest, bestChain)
	t.Logf("Attached block one %v", blockOne.Hash)
	if len(forest.trees) != 0 {
		t.Fatalf("Did not prune block one from forest")
	}
	tw.assertNoBetterChain(forest)

	// Generate blocks 2a and 3a and attach to the wallet's main chain together.
	for i := 2; i <= 3; i++ {
		name := fmt.Sprintf("%va", i)
		b := tg.nextBlock(name, nil, nil)
		mustAddBlockNode(t, forest, b.BlockNode)
		t.Logf("Generated block %v name %q", b.Hash, name)
	}
	if len(forest.trees) != 1 {
		t.Fatalf("Expected one tree in forest")
	}
	b2aHash := tg.blockHashByName("2a")
	b3aHash := tg.blockHashByName("3a")
	assertSidechainTree(t, forest.trees[0], b2aHash, b3aHash)
	bestChain = tw.evaluateBestChain(forest, 2, b3aHash)
	tw.chainSwitch(forest, bestChain)
	if len(forest.trees) != 0 {
		t.Fatalf("Did not prune blocks 2a-3a from forest")
	}
	tw.assertNoBetterChain(forest)

	// Generate sidechain blocks 2b-3b and assert it does not create a better
	// chain.
	tg.SetTip("block-one")
	for i := 2; i <= 3; i++ {
		name := fmt.Sprintf("%vb", i)
		b := tg.nextBlock(name, nil, nil)
		mustAddBlockNode(t, forest, b.BlockNode)
		t.Logf("Generated block %v name %q", b.Hash, name)
	}
	if len(forest.trees) != 1 {
		t.Fatalf("Expected one tree in forest")
	}
	b2bHash := tg.blockHashByName("2b")
	b3bHash := tg.blockHashByName("3b")
	assertSidechainTree(t, forest.trees[0], b2bHash, b3bHash)
	tw.assertNoBetterChain(forest)

	// Generate sidechain block 4b, and attach the better chain 2b-4b to
	// wallet's main chain, reorging out 2a and 3a.
	name := "4b"
	b := tg.nextBlock(name, nil, nil)
	mustAddBlockNode(t, forest, b.BlockNode)
	t.Logf("Generated block %v name %q", b.Hash, name)
	b4bHash := b.Hash
	if len(forest.trees) != 1 {
		t.Fatalf("Expected one tree in forest")
	}
	assertSidechainTree(t, forest.trees[0], b2bHash, b4bHash)
	bestChain = tw.evaluateBestChain(forest, 3, b4bHash)
	tw.chainSwitch(forest, bestChain)
	if len(forest.trees) != 1 {
		t.Fatalf("Expected single tree in forest after reorg")
	}
	tw.assertNoBetterChain(forest)
	assertSidechainTree(t, forest.trees[0], b2aHash, b3aHash)
	tw.expectBlockInMainChain(b2aHash, false, false)
	tw.expectBlockInMainChain(b3aHash, false, false)
	tw.expectBlockInMainChain(b2bHash, true, false)
	tw.expectBlockInMainChain(b3bHash, true, false)
	tw.expectBlockInMainChain(b4bHash, true, false)
}
