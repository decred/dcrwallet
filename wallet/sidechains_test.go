package wallet

import (
	"math/rand"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

// genTestBlockNodes generates test block nodes.  If parent is passed, then
// the blocks are successors of that node.
func genTestBlockNodes(parent *BlockNode, nb int) []*BlockNode {
	res := make([]*BlockNode, nb)
	var height uint32 = 1
	var prevHash chainhash.Hash
	if parent != nil {
		height = parent.Header.Height + 1
		prevHash = *parent.Hash
	}
	for i := 0; i < nb; i++ {
		n := &BlockNode{
			Header: &wire.BlockHeader{
				Height:    height,
				PrevBlock: prevHash,
			},
		}
		rand.Read(n.Header.ExtraData[:])
		hash := n.Header.BlockHash()
		n.Hash = &hash
		res[i] = n
		prevHash, height = hash, height+1
	}
	return res
}

// TestPruneChainFromSRT verifies the PruneChain method of sidechainRootedTree
// works as expected.
func TestPruneChainFromSRT(t *testing.T) {
	// Generate the following tree:
	//   a -> b0 -> c0 -> d0 -> e0
	//    \-> b1 -> c1 -> d1 -> e1
	//    \-> b2 -> c2 -> d2 -> e2
	//                \-> d3 -> e3
	chain0 := genTestBlockNodes(nil, 5)
	chain1 := genTestBlockNodes(chain0[0], 4)
	chain2 := genTestBlockNodes(chain0[0], 4)
	chain3 := genTestBlockNodes(chain2[1], 2)

	// Add everyone to the SRT.
	srt := newSideChainRootedTree(chain0[0])
	for i, c := range [][]*BlockNode{chain0[1:], chain1, chain2, chain3} {
		for j, n := range c {
			if !srt.maybeAttachNode(n) {
				t.Fatalf("node %d/%d was not attached", i, j)
			}
		}
	}

	// Helper to return the tips of multiple chains.
	tipsOf := func(chains ...[]*BlockNode) []*BlockNode {
		res := make([]*BlockNode, len(chains))
		for i := range chains {
			res[i] = chains[i][len(chains[i])-1]
		}
		return res
	}

	// Helper to assert a new SRT has the correct nodes and tips.
	assertSRT := func(srt *sidechainRootedTree, root *BlockNode, nodes []*BlockNode, tips []*BlockNode) {
		t.Helper()
		if len(nodes) != len(srt.children) {
			t.Fatalf("srt has wrong nb of children: got %d, want %d",
				len(srt.children), len(nodes))
		}
		if len(tips) != len(srt.tips) {
			t.Fatalf("srt has wrong nb of tips: got %d, want %d",
				len(srt.tips), len(tips))
		}
		if srt.root != root {
			t.Fatalf("srt has wrong root: got %d, want %d",
				srt.root.Header.Height, root.Header.Height)
		}
		for _, want := range nodes {
			got, ok := srt.children[*want.Hash]
			if !ok {
				t.Fatalf("srt does not have header %d as child",
					want.Header.Height)
			}
			if got != want {
				t.Fatalf("srt node at height %d not the expected one",
					want.Header.Height)
			}
		}
		for _, want := range tips {
			got, ok := srt.tips[*want.Hash]
			if !ok {
				t.Fatalf("srt does not have header %d as tip",
					want.Header.Height)
			}
			if got != want {
				t.Fatalf("srt node at height %d not the expected tip",
					want.Header.Height)
			}
		}
	}

	// Prune chain0. chain1 and chain2 are new rooted trees and chain3 is a
	// a tip in chain2.
	//    b1 -> c1 -> d1 -> e1
	//    b2 -> c2 -> d2 -> e2
	//            \-> d3 -> e3
	prune1 := srt.pruneChain(chain0)
	if len(prune1) != 2 {
		t.Fatalf("unexpected nb of chains: got %d, want %d", len(prune1), 2)
	}
	if prune1[0].root != chain1[0] { // Swap so chain1 is prune1[0]
		prune1[0], prune1[1] = prune1[1], prune1[0]
	}
	assertSRT(prune1[0], chain1[0], chain1[1:], tipsOf(chain1))
	assertSRT(prune1[1], chain2[0], append(chain2[1:], chain3...), tipsOf(chain2, chain3))

	// On the first pruned srt, prune again only up to c1.
	//   d2 -> e2
	prune2 := prune1[0].pruneChain(chain1[:2])
	if len(prune2) != 1 {
		t.Fatalf("unexpected nb of chains: got %d, want %d", len(prune2), 1)
	}
	assertSRT(prune2[0], chain1[2], chain1[3:], tipsOf(chain1[2:]))

	// Prune again, this time nothing remains.
	prune3 := prune2[0].pruneChain(chain1[2:])
	if len(prune3) != 0 {
		t.Fatalf("unexpected nb of chains: got %d, want %d", len(prune3), 0)
	}
}
