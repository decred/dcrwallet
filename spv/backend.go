// Copyright (c) 2018-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package spv

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/p2p"
	"decred.org/dcrwallet/v5/validate"
	"decred.org/dcrwallet/v5/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v4"
	"github.com/decred/dcrd/mixing"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"golang.org/x/sync/errgroup"
)

var _ wallet.NetworkBackend = (*Syncer)(nil)

// TODO: When using the Syncer as a NetworkBackend, keep track of in-flight
// blocks and cfilters.  If one is already incoming, wait on that response.  If
// that peer is lost, try a different peer.  Optionally keep a cache of fetched
// data so it can be immediately returned without another call.

func pickAny(*p2p.RemotePeer) bool { return true }

// pickForGetHeaders returns a function to use in waitForRemotes which selects
// peers that may have headers that are more recent than the passed tipHeight.
func pickForGetHeaders(tipHeight int32) func(rp *p2p.RemotePeer) bool {
	return func(rp *p2p.RemotePeer) bool {
		// We are interested in this peer's headers if they announced a
		// height greater than the current tip height and if we haven't
		// yet fetched all the headers that it announced.
		return rp.InitialHeight() > tipHeight && rp.LastHeight() < rp.InitialHeight()
	}
}

// pickForGetCfilters returns a function to use in waitForRemotes which selects
// peers that should have cfilters up to the passed lastHeaderHeight.
func pickForGetCfilters(lastHeaderHeight int32) func(rp *p2p.RemotePeer) bool {
	return func(rp *p2p.RemotePeer) bool {
		// When performing initial sync, it could be the case that
		// blocks are generated while performing the sync, therefore
		// the initial advertised peer height would be lower than the
		// last header height.  Therefore, accept peers that are
		// close, but not quite at the tip.
		return rp.InitialHeight() >= lastHeaderHeight-6
	}
}

// Blocks implements the Blocks method of the wallet.Peer interface.
func (s *Syncer) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.waitForRemote(ctx, pickAny, true)
		if err != nil {
			return nil, err
		}
		blocks, err := rp.Blocks(ctx, blockHashes)
		if err != nil {
			log.Debugf("unable to fetch blocks from %v: %v", rp, err)
			continue
		}
		return blocks, nil
	}
}

// filterProof is an alias to the same anonymous struct as wallet package's
// FilterProof struct.
type filterProof = struct {
	Filter     *gcs.FilterV2
	ProofIndex uint32
	Proof      []chainhash.Hash
}

// CFiltersV2 implements the CFiltersV2 method of the wallet.Peer interface.
// This function blocks until a valid peer in the syncer returns the
// appropriate CFilters.
func (s *Syncer) CFiltersV2(ctx context.Context, blockHashes []*chainhash.Hash) ([]filterProof, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.waitForRemote(ctx, pickAny, true)
		if err != nil {
			return nil, err
		}
		fs, err := rp.CFiltersV2(ctx, blockHashes)
		if err != nil {
			log.Debugf("Error while fetching cfilters from %v: %v",
				rp, err)
			continue
		}
		return fs, nil
	}
}

// errCfilterWatchdogTriggered is an internal error generated when a batch
// of cfilters takes too long to be fetched.
var errCfilterWatchdogTriggered = errors.New("getCFilters watchdog triggered")

// cfiltersV2FromNodes fetches cfilters for all the specified nodes from a
// remote peer.
func (s *Syncer) cfiltersV2FromNodes(ctx context.Context, nodes []*wallet.BlockNode) error {
	if len(nodes) == 0 {
		return nil
	}

	cnet := s.wallet.ChainParams().Net

	// Split fetching into batches of a max size.
	const cfilterBatchSize = wire.MaxCFiltersV2PerBatch
	if len(nodes) > cfilterBatchSize {
		g, ctx := errgroup.WithContext(ctx)
		for len(nodes) > cfilterBatchSize {
			batch := nodes[:cfilterBatchSize]
			g.Go(func() error { return s.cfiltersV2FromNodes(ctx, batch) })
			nodes = nodes[cfilterBatchSize:]
		}
		g.Go(func() error { return s.cfiltersV2FromNodes(ctx, nodes) })
		return g.Wait()
	}

	nodeHashes := make([]*chainhash.Hash, len(nodes))
	for i := range nodes {
		nodeHashes[i] = nodes[i].Hash
	}
	lastHeight := nodes[len(nodes)-1].Header.Height

	// Specially once we get close to the tip, we may have a header in the
	// best sidechain that has been reorged out and thus no peer will have
	// its corresponding CFilters.  To recover from this case in a timely
	// manner, we setup a special watchdog context that, if triggered, will
	// make us clean up the sidechain forest, forcing a request of fresh
	// headers from all remote peers.
	//
	// Peers have a 30s stall timeout protection, therefore a 2 minute
	// watchdog interval means we'll try at least 4 different peers before
	// resetting.
	const watchdogTimeoutInterval = 2 * time.Minute
	watchdogCtx, cancelWatchdog := context.WithTimeout(ctx, watchdogTimeoutInterval)
	defer cancelWatchdog()

nextTry:
	for ctx.Err() == nil {
		// Select a peer that should have these cfilters.
		rp, err := s.waitForRemote(watchdogCtx, pickForGetCfilters(int32(lastHeight)), true)
		if watchdogCtx.Err() != nil && ctx.Err() == nil {
			// Watchdog timer triggered.  Reset sidechain forest.
			lastNode := nodes[len(nodes)-1]
			log.Warnf("Batch of CFilters ending on block %s at "+
				"height %d not received within %s. Clearing "+
				"sidechain forest to retry with different "+
				"headers", lastNode.Hash, lastNode.Header.Height,
				watchdogTimeoutInterval)
			s.sidechainMu.Lock()
			s.sidechains.PruneAll()
			s.sidechainMu.Unlock()
			return errCfilterWatchdogTriggered
		}
		if err != nil {
			return err
		}

		startTime := time.Now()

		// If the node supports batched fetch, use it.  Otherwise,
		// fetch one by one.
		var filters []wallet.FilterProof
		if rp.Pver() >= wire.BatchedCFiltersV2Version {
			filters, err = rp.BatchedCFiltersV2(ctx, nodes[0].Hash, nodes[len(nodes)-1].Hash)
			if err == nil && len(filters) != len(nodes) {
				errMsg := fmt.Errorf("peer returned unexpected "+
					"number of filters (got %d, want %d)",
					len(filters), len(nodes))
				err = errors.E(errors.Protocol, errMsg)
				rp.Disconnect(err)
				continue nextTry
			}
		} else {
			filters, err = rp.CFiltersV2(ctx, nodeHashes)
		}
		if err != nil {
			log.Tracef("Unable to fetch cfilter batch for "+
				"from %v: %v", rp, err)
			continue nextTry
		}

		for i := range nodes {
			err = validate.CFilterV2HeaderCommitment(cnet, nodes[i].Header,
				filters[i].Filter, filters[i].ProofIndex, filters[i].Proof)
			if err != nil {
				errMsg := fmt.Sprintf("CFilter for block %v (height %d) "+
					"received from %v failed validation: %v",
					nodes[i].Hash, nodes[i].Header.Height,
					rp, err)
				log.Warnf(errMsg)
				err := errors.E(errors.Protocol, errMsg)
				rp.Disconnect(err)
				continue nextTry
			}
		}

		s.sidechainMu.Lock()
		for i := range nodes {
			nodes[i].FilterV2 = filters[i].Filter
		}
		s.sidechainMu.Unlock()
		log.Tracef("Fetched %d new cfilters(s) ending at height %d "+
			"from %v (request took %s)",
			len(nodes), nodes[len(nodes)-1].Header.Height, rp,
			time.Since(startTime).Truncate(time.Millisecond))
		return nil
	}

	return ctx.Err()
}

// headersBatch is a batch of headers fetched during initial sync.
type headersBatch struct {
	done      bool
	nodes     []*wallet.BlockNode
	bestChain []*wallet.BlockNode
	rp        *p2p.RemotePeer
}

// getHeaders returns a batch of headers from a remote peer for initial
// syncing.
//
// This function returns a batch with the done flag set to true when no peers
// have more recent blocks for syncing.
func (s *Syncer) getHeaders(ctx context.Context, likelyBestChain []*wallet.BlockNode) (*headersBatch, error) {
	cnet := s.wallet.ChainParams().Net

nextbatch:
	for ctx.Err() == nil {
		_, tipHeight := s.wallet.MainChainTip(ctx)

		// Determine if there are any peers from which to request newer
		// headers.
		rp, err := s.waitForRemote(ctx, pickForGetHeaders(tipHeight), false)
		if err != nil {
			return nil, err
		}
		if rp == nil {
			return &headersBatch{done: true}, nil
		}
		log.Tracef("Attempting next batch of headers from %v", rp)

		// Request headers from the selected peer.
		locators, locatorHeight, err := s.wallet.BlockLocators(ctx, likelyBestChain)
		if err != nil {
			return nil, err
		}
		headers, err := rp.Headers(ctx, locators, &hashStop)
		if err != nil {
			log.Debugf("Unable to fetch headers from %v: %v", rp, err)
			continue nextbatch
		}

		if len(headers) == 0 {
			// Ensure that the peer provided headers through the
			// height advertised during handshake, unless our own
			// locators were up to date (in which case we actually
			// do not expect any headers).
			if rp.LastHeight() < rp.InitialHeight() && locatorHeight < rp.InitialHeight() {
				err := errors.E(errors.Protocol, "peer did not provide "+
					"headers through advertised height")
				rp.Disconnect(err)
				continue nextbatch
			}

			// Try to pick a different peer with a higher advertised
			// height or check there are no such peers (thus we're
			// done with fetching headers for initial sync).
			log.Tracef("Skipping to next batch due to "+
				"len(headers) == 0 from %v", rp)
			continue nextbatch
		}

		nodes := make([]*wallet.BlockNode, len(headers))
		for i := range headers {
			// Determine the hash of the header. It is safe to use
			// PrevBlock (instead of recalculating) because the
			// lower p2p level already asserted the headers connect
			// to each other.
			var hash *chainhash.Hash
			if i == len(headers)-1 {
				bh := headers[i].BlockHash()
				hash = &bh
			} else {
				hash = &headers[i+1].PrevBlock
			}
			nodes[i] = wallet.NewBlockNode(headers[i], hash, nil)
			if wallet.BadCheckpoint(cnet, hash, int32(headers[i].Height)) {
				nodes[i].BadCheckpoint()
			}
		}

		// Verify the sidechain that includes the received headers has
		// the correct difficulty.
		s.sidechainMu.Lock()
		fullsc, err := s.sidechains.FullSideChain(nodes)
		if err != nil {
			s.sidechainMu.Unlock()
			return nil, err
		}
		_, err = s.wallet.ValidateHeaderChainDifficulties(ctx, fullsc, 0)
		if err != nil {
			s.sidechainMu.Unlock()
			rp.Disconnect(err)
			if !errors.Is(err, context.Canceled) {
				log.Warnf("Disconnecting from %v due to header "+
					"validation error: %v", rp, err)
			}
			continue nextbatch
		}

		// Add new headers to the sidechain forest.
		var added int
		for _, n := range nodes {
			haveBlock, _, _ := s.wallet.BlockInMainChain(ctx, n.Hash)
			if haveBlock {
				continue
			}
			if s.sidechains.AddBlockNode(n) {
				added++
			}
		}

		// Determine if this extends the best known chain.
		bestChain, err := s.wallet.EvaluateBestChain(ctx, &s.sidechains)
		if err != nil {
			s.sidechainMu.Unlock()
			rp.Disconnect(err)
			continue nextbatch
		}
		if len(bestChain) == 0 {
			s.sidechainMu.Unlock()
			continue nextbatch
		}

		log.Debugf("Fetched %d new header(s) ending at height %d from %v",
			added, headers[len(headers)-1].Height, rp)

		s.sidechainMu.Unlock()

		// Batch fetched.
		return &headersBatch{
			nodes:     nodes,
			bestChain: bestChain,
			rp:        rp,
		}, nil
	}

	return nil, ctx.Err()
}

func (s *Syncer) String() string {
	// This method is part of the wallet.Peer interface and will typically
	// specify the remote address of the peer.  Since the syncer can encompass
	// multiple peers, just use the qualified type as the string.
	return "spv.Syncer"
}

// LoadTxFilter implements the LoadTxFilter method of the wallet.NetworkBackend
// interface.
//
// NOTE: due to blockcf2 *not* including the spent outpoints in the block, the
// addrs[] slice MUST include the addresses corresponding to the respective
// outpoints, otherwise they will not be returned during the rescan.
func (s *Syncer) LoadTxFilter(ctx context.Context, reload bool, addrs []stdaddr.Address, outpoints []wire.OutPoint) error {
	s.filterMu.Lock()
	if reload || s.rescanFilter == nil {
		s.rescanFilter = wallet.NewRescanFilter(nil, nil)
		s.filterData = nil
	}
	for _, addr := range addrs {
		_, pkScript := addr.PaymentScript()
		s.rescanFilter.AddAddress(addr)
		s.filterData.AddRegularPkScript(pkScript)
	}
	for i := range outpoints {
		s.rescanFilter.AddUnspentOutPoint(&outpoints[i])
	}
	s.filterMu.Unlock()
	return nil
}

// PublishTransactions implements the PublishTransaction method of the
// wallet.Peer interface.
func (s *Syncer) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	// Figure out transactions that are not stored by the wallet and create
	// an aux map so we can choose which need to be stored in the syncer's
	// mempool.
	walletBacked := make(map[chainhash.Hash]bool, len(txs))
	relevant, _, err := s.wallet.DetermineRelevantTxs(ctx, txs...)
	if err != nil {
		return err
	}
	for _, tx := range relevant {
		walletBacked[tx.TxHash()] = true
	}

	msg := wire.NewMsgInvSizeHint(uint(len(txs)))
	for _, tx := range txs {
		txHash := tx.TxHash()
		if !walletBacked[txHash] {
			// Load into the mempool and let the mempool handler
			// know of it.
			if _, loaded := s.mempool.LoadOrStore(txHash, tx); !loaded {
				select {
				case s.mempoolAdds <- &txHash:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		err := msg.AddInvVect(wire.NewInvVect(wire.InvTypeTx, &txHash))
		if err != nil {
			return errors.E(errors.Protocol, err)
		}
	}
	return s.forRemotes(func(rp *p2p.RemotePeer) error {
		for _, inv := range msg.InvList {
			rp.InvsSent().Add(inv.Hash)
		}
		return rp.SendMessage(ctx, msg)
	})
}

// PublishMixMessages implements the PublishMixMessages method of the
// wallet.NetworkBackend interface.
func (s *Syncer) PublishMixMessages(ctx context.Context, msgs ...mixing.Message) error {
	const op errors.Op = "spv.PublishMixMessages"

	// When we inventory a KE, also add our own PR hash from our KE
	// message to recently-inventoried message LRU, so they can be queried
	// by nodes that learn of them through the KE.
	var ownPRs []*chainhash.Hash

	msg := wire.NewMsgInvSizeHint(uint(len(msgs)))
	for _, mixMsg := range msgs {
		mixMsgHash := mixMsg.Hash()
		err := msg.AddInvVect(wire.NewInvVect(wire.InvTypeMix, &mixMsgHash))
		if err != nil {
			return errors.E(op, errors.Protocol, err)
		}
		if ke, ok := mixMsg.(*wire.MsgMixKeyExchange); ok {
			ownPRs = append(ownPRs, &ke.SeenPRs[ke.Pos])
		}
	}
	var mixingPeers int
	err := s.forRemotes(func(rp *p2p.RemotePeer) error {
		if rp.Pver() < wire.MixVersion {
			return nil
		}
		mixingPeers++
		for _, inv := range msg.InvList {
			rp.InvsSent().Add(inv.Hash)
		}
		for _, prHash := range ownPRs {
			rp.InvsSent().Add(*prHash)
		}
		return rp.SendMessage(ctx, msg)
	})
	if err != nil {
		return errors.E(op, err)
	}
	if mixingPeers == 0 {
		s := "no connected peers support the mixing protocol version"
		return errors.E(op, errors.Protocol, s)
	}
	return nil
}

// Rescan implements the Rescan method of the wallet.NetworkBackend interface.
func (s *Syncer) Rescan(ctx context.Context, blockHashes []chainhash.Hash, save func(*chainhash.Hash, []*wire.MsgTx) error) error {
	const op errors.Op = "spv.Rescan"

	cfilters := make([]*gcs.FilterV2, 0, len(blockHashes))
	cfilterKeys := make([][gcs.KeySize]byte, 0, len(blockHashes))
	for i := 0; i < len(blockHashes); i++ {
		k, f, err := s.wallet.CFilterV2(ctx, &blockHashes[i])
		if err != nil {
			return err
		}
		cfilters = append(cfilters, f)
		cfilterKeys = append(cfilterKeys, k)
	}

	blockMatches := make([]*wire.MsgBlock, len(blockHashes)) // Block assigned to slice once fetched

	// Read current filter data.
	s.filterMu.Lock()
	filterData := s.filterData
	s.filterMu.Unlock()

	var fmatches []*chainhash.Hash
	var fmatchidx []int
	var fmatchMu sync.Mutex

	// Spawn ncpu workers to check filter matches
	ncpu := runtime.NumCPU()
	c := make(chan int, ncpu)
	var wg sync.WaitGroup
	wg.Add(ncpu)
	for i := 0; i < ncpu; i++ {
		go func() {
			for i := range c {
				blockHash := &blockHashes[i]
				key := cfilterKeys[i]
				f := cfilters[i]
				if f.MatchAny(key, filterData) {
					fmatchMu.Lock()
					fmatches = append(fmatches, blockHash)
					fmatchidx = append(fmatchidx, i)
					fmatchMu.Unlock()
				}
			}
			wg.Done()
		}()
	}
	for i := 0; i < len(blockHashes); i++ {
		if blockMatches[i] != nil {
			// Already fetched this block
			continue
		}
		c <- i
	}
	close(c)
	wg.Wait()

	if len(fmatches) != 0 {
	PickPeer:
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			rp, err := s.waitForRemote(ctx, pickAny, true)
			if err != nil {
				return err
			}

			blocks, err := rp.Blocks(ctx, fmatches)
			if err != nil {
				continue PickPeer
			}

			for j, b := range blocks {
				// Validate fetched blocks before rescanning transactions.  PoW
				// and PoS difficulties have already been validated since the
				// header is saved by the wallet, and modifications to these in
				// the downloaded block would result in a different block hash
				// and failure to fetch the block.
				//
				// Block filters were also validated
				// against the header (assuming dcp0005
				// was activated).
				err = validate.MerkleRoots(b)
				if err != nil {
					err = validate.DCP0005MerkleRoot(b)
				}
				if err != nil {
					err := errors.E(op, err)
					rp.Disconnect(err)
					rp = nil
					continue PickPeer
				}

				i := fmatchidx[j]
				blockMatches[i] = b
			}
			break
		}
	}

	for i := 0; i < len(blockMatches); i++ {
		b := blockMatches[i]
		if b == nil {
			// No filter match, skip block
			continue
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		matchedTxs := s.rescanBlock(b)
		if len(matchedTxs) != 0 {
			err := save(&blockHashes[i], matchedTxs)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// StakeDifficulty implements the StakeDifficulty method of the
// wallet.NetworkBackend interface.
//
// This implementation of the method will always error as the stake difficulty
// is not queryable over wire protocol, and when the next stake difficulty is
// available in a header commitment, the wallet will be able to determine this
// itself without requiring the NetworkBackend.
func (s *Syncer) StakeDifficulty(ctx context.Context) (dcrutil.Amount, error) {
	return 0, errors.E(errors.Invalid, "stake difficulty is not queryable over wire protocol")
}

func (s *Syncer) Done() <-chan struct{} {
	s.doneMu.Lock()
	c := s.done
	s.doneMu.Unlock()
	return c
}

func (s *Syncer) Err() error {
	s.doneMu.Lock()
	c := s.done
	err := s.err
	s.doneMu.Unlock()

	select {
	case <-c:
		return err
	default:
		return nil
	}
}
