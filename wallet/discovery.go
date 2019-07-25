// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs/blockcf"
	hd "github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	"github.com/decred/dcrwallet/validate"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"golang.org/x/sync/errgroup"
)

// blockCommitmentCache records exact output scripts committed by block filters,
// keyed by block hash, to check for GCS false positives.
type blockCommitmentCache map[chainhash.Hash]map[string]struct{}

func blockCommitments(block *wire.MsgBlock) map[string]struct{} {
	c := make(map[string]struct{})
	for _, tx := range block.Transactions {
		for _, out := range tx.TxOut {
			c[string(out.PkScript)] = struct{}{}
		}
	}
	for _, tx := range block.STransactions {
		switch stake.DetermineTxType(tx) {
		case stake.TxTypeSStx: // Ticket purchase
			for i := 2; i < len(tx.TxOut); i += 2 { // Iterate change outputs
				out := tx.TxOut[i]
				if out.Value != 0 {
					script := out.PkScript[1:] // Slice off stake opcode
					c[string(script)] = struct{}{}
				}
			}
		case stake.TxTypeSSGen: // Vote
			for _, out := range tx.TxOut[2:] { // Iterate generated coins
				script := out.PkScript[1:] // Slice off stake opcode
				c[string(script)] = struct{}{}
			}
		case stake.TxTypeSSRtx: // Revocation
			for _, out := range tx.TxOut {
				script := out.PkScript[1:] // Slice off stake opcode
				c[string(script)] = struct{}{}
			}
		}
	}
	return c
}

func cacheMissingCommitments(ctx context.Context, p Peer, cache blockCommitmentCache, include []*chainhash.Hash) error {
	for i := 0; i < len(include); i += wire.MaxBlocksPerMsg {
		include := include[i:]
		if len(include) > wire.MaxBlocksPerMsg {
			include = include[:wire.MaxBlocksPerMsg]
		}

		var fetchBlocks []*chainhash.Hash
		for _, b := range include {
			if _, ok := cache[*b]; !ok {
				fetchBlocks = append(fetchBlocks, b)
			}
		}
		if len(fetchBlocks) == 0 {
			return nil
		}
		blocks, err := p.Blocks(ctx, fetchBlocks)
		if err != nil {
			return err
		}
		for i, b := range blocks {
			cache[*fetchBlocks[i]] = blockCommitments(b)
		}
	}
	return nil
}

type accountUsage struct {
	extkey, intkey *hd.ExtendedKey
	extLastUsed    uint32
	intLastUsed    uint32
	extlo, intlo   uint32
	exthi, inthi   uint32 // Set to lo - 1 when finished, be cautious of unsigned underflow
}

type scriptPath struct {
	account, branch, index uint32
}

type addrFinder struct {
	w           *Wallet
	gaplimit    uint32
	segments    uint32
	usage       []accountUsage
	commitments blockCommitmentCache
	mu          sync.RWMutex
}

func newAddrFinder(w *Wallet) (*addrFinder, error) {
	a := &addrFinder{
		w:           w,
		gaplimit:    uint32(w.gapLimit),
		segments:    hd.HardenedKeyStart / uint32(w.gapLimit),
		commitments: make(blockCommitmentCache),
	}
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
		lastAcct, err := w.Manager.LastAccount(ns)
		if err != nil {
			return err
		}
		a.usage = make([]accountUsage, lastAcct+1)
		for acct := uint32(0); acct <= lastAcct; acct++ {
			extkey, err := w.Manager.AccountBranchExtendedPubKey(dbtx, acct, 0)
			if err != nil {
				return err
			}
			intkey, err := w.Manager.AccountBranchExtendedPubKey(dbtx, acct, 1)
			if err != nil {
				return err
			}
			props, err := w.Manager.AccountProperties(ns, acct)
			if err != nil {
				return err
			}
			var extlo, intlo uint32
			if props.LastUsedExternalIndex != ^uint32(0) {
				extlo = props.LastUsedExternalIndex / a.gaplimit
			}
			if props.LastUsedInternalIndex != ^uint32(0) {
				intlo = props.LastUsedInternalIndex / a.gaplimit
			}
			a.usage[acct] = accountUsage{
				extkey:      extkey,
				intkey:      intkey,
				extLastUsed: props.LastUsedExternalIndex,
				intLastUsed: props.LastUsedInternalIndex,
				extlo:       extlo,
				exthi:       a.segments - 1,
				intlo:       intlo,
				inthi:       a.segments - 1,
			}
		}
		return nil
	})
	return a, err
}

func (a *addrFinder) find(ctx context.Context, start *chainhash.Hash, p Peer) error {
	// Load main chain cfilters beginning with start.
	var fs []*udb.BlockCFilter
	err := walletdb.View(a.w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		h, err := a.w.TxStore.GetBlockHeader(dbtx, start)
		if err != nil {
			return err
		}
		_, tipHeight := a.w.TxStore.MainChainTip(ns)
		storage := make([]*udb.BlockCFilter, tipHeight-int32(h.Height))
		fs, err = a.w.TxStore.GetMainChainCFilters(dbtx, start, true, storage)
		return err
	})
	if err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Derive one bsearch iteration of filter data for all branches.
		// Map address scripts to their HD path.
		var data [][]byte
		scrPaths := make(map[string]scriptPath)
		addBranch := func(branchPub *hd.ExtendedKey, acct, branch, lo, hi uint32) error {
			if lo > hi || hi >= a.segments { // Terminating condition
				return nil
			}
			mid := (hi + lo) / 2
			begin := mid * a.gaplimit
			addrs, err := deriveChildAddresses(branchPub, begin, a.gaplimit, a.w.chainParams)
			if err != nil {
				return err
			}
			for i, addr := range addrs {
				scr, _, err := addressScript(addr)
				if err != nil {
					log.Errorf("addressScript(%v): %v", addr, err)
					continue
				}
				data = append(data, scr)
				scrPaths[string(scr)] = scriptPath{
					account: acct,
					branch:  branch,
					index:   mid*a.gaplimit + uint32(i),
				}
			}
			return nil
		}
		for i := range a.usage {
			acct := uint32(i)
			u := &a.usage[i]
			err = addBranch(u.extkey, acct, 0, u.extlo, u.exthi)
			if err != nil {
				return err
			}
			err = addBranch(u.intkey, acct, 1, u.intlo, u.inthi)
			if err != nil {
				return err
			}
		}

		if len(data) == 0 {
			return nil
		}

		// Record committed scripts of matching filters.
		err := a.filter(ctx, fs, data, p)
		if err != nil {
			return err
		}

		var wg sync.WaitGroup
		wg.Add(len(a.commitments))
		for hash, commitments := range a.commitments {
			hash, commitments := hash, commitments
			go func() {
				for _, scr := range data {
					if _, ok := commitments[string(scr)]; !ok {
						continue
					}

					// Found address script in this block.  Look up address path
					// and record usage.
					path := scrPaths[string(scr)]
					log.Debugf("Found match for script %x path %v in block %v", scr, path, &hash)
					u := &a.usage[path.account]
					a.mu.Lock()
					switch path.branch {
					case 0: // external
						if u.extLastUsed == ^uint32(0) || path.index > u.extLastUsed {
							u.extLastUsed = path.index
						}
					case 1: // internal
						if u.intLastUsed == ^uint32(0) || path.index > u.intLastUsed {
							u.intLastUsed = path.index
						}
					}
					a.mu.Unlock()
				}
				wg.Done()
			}()
		}
		wg.Wait()

		// Update hi/lo segments for next bisect iteration
		for i := range a.usage {
			u := &a.usage[i]
			if u.extlo <= u.exthi {
				mid := (u.exthi + u.extlo) / 2
				// When the last used index is in this segment's index half open
				// range [begin,end) then an address was found in this segment.
				begin := mid * a.gaplimit
				end := begin + a.gaplimit
				if u.extLastUsed >= begin && u.extLastUsed < end {
					u.extlo = mid + 1
				} else {
					u.exthi = mid - 1
				}
			}
			if u.intlo <= u.inthi {
				mid := (u.inthi + u.intlo) / 2
				begin := mid * a.gaplimit
				end := begin + a.gaplimit
				if u.intLastUsed >= begin && u.intLastUsed < end {
					u.intlo = mid + 1
				} else {
					u.inthi = mid - 1
				}
			}
		}
	}
}

func (a *addrFinder) filter(ctx context.Context, fs []*udb.BlockCFilter, data blockcf.Entries, p Peer) error {
	g, ctx := errgroup.WithContext(ctx)
	for i := 0; i < len(fs); i += wire.MaxBlocksPerMsg {
		fs := fs[i:]
		if len(fs) > wire.MaxBlocksPerMsg {
			fs = fs[:wire.MaxBlocksPerMsg]
		}
		g.Go(func() error {
			var fetch []*chainhash.Hash
			var fetchidx []int
			for i, f := range fs {
				if f.Filter.N() == 0 {
					continue
				}
				a.mu.RLock()
				_, ok := a.commitments[f.BlockHash]
				a.mu.RUnlock()
				if ok {
					continue // Previously fetched block
				}
				if f.Filter.MatchAny(blockcf.Key(&f.BlockHash), data) {
					fetch = append(fetch, &f.BlockHash)
					fetchidx = append(fetchidx, i)
				}
			}
			if len(fetch) == 0 {
				return nil
			}
			blocks, err := p.Blocks(ctx, fetch)
			if err != nil {
				return err
			}
			for i, b := range blocks {
				i, b := i, b
				g.Go(func() error {
					// validate blocks
					err := validate.MerkleRoots(b)
					if err != nil {
						return err
					}
					err = validate.RegularCFilter(b, fs[fetchidx[i]].Filter)
					if err != nil {
						return err
					}

					c := blockCommitments(b)
					a.mu.Lock()
					a.commitments[*fetch[i]] = c
					a.mu.Unlock()
					return nil
				})
			}
			return nil
		})
	}
	return g.Wait()
}

// filterBlocks returns the block hashes of all blocks in the main chain,
// starting at startBlock, whose cfilters match against data.
func (w *Wallet) filterBlocks(ctx context.Context, startBlock *chainhash.Hash, data blockcf.Entries) ([]*chainhash.Hash, error) {
	var matches []*chainhash.Hash
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(runtime.NumCPU())
	c := make(chan []*udb.BlockCFilter, runtime.NumCPU())
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for blocks := range c {
				for _, b := range blocks {
					if b.Filter.N() == 0 {
						continue
					}
					key := blockcf.Key(&b.BlockHash)
					if b.Filter.MatchAny(key, data) {
						h := b.BlockHash
						mu.Lock()
						matches = append(matches, &h)
						mu.Unlock()
					}
				}
			}
			wg.Done()
		}()
	}
	startHash := startBlock
	inclusive := true
	for {
		if ctx.Err() != nil {
			// Can return before workers finish
			close(c)
			return nil, ctx.Err()
		}
		storage := make([]*udb.BlockCFilter, 2000)
		var filters []*udb.BlockCFilter
		err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
			var err error
			filters, err = w.TxStore.GetMainChainCFilters(dbtx, startHash,
				inclusive, storage)
			return err
		})
		if err != nil {
			return nil, err
		}
		if len(filters) == 0 {
			break
		}
		c <- filters
		startHash = &filters[len(filters)-1].BlockHash
		inclusive = false
	}
	close(c)
	wg.Wait()
	return matches, ctx.Err()
}

func (w *Wallet) findLastUsedAccount(ctx context.Context, p Peer, blockCache blockCommitmentCache, coinTypeXpriv *hd.ExtendedKey) (uint32, error) {
	var (
		gapLimit     = uint32(w.gapLimit)
		acctGapLimit = uint32(w.accountGapLimit)
		addrScripts  = make([][]byte, 0, acctGapLimit*gapLimit*2*2)
	)

	lastUsedInRange := func(begin, end uint32) (uint32, error) { // [begin,end)
		addrScripts = addrScripts[:0]
		addrScriptAccts := make(map[string]uint32)
		if end >= hd.HardenedKeyStart {
			end = hd.HardenedKeyStart - 1
		}
		for acct := begin; acct < end; acct++ {
			xpriv, err := coinTypeXpriv.Child(hd.HardenedKeyStart + acct)
			if err != nil {
				return 0, err
			}
			xpub, err := xpriv.Neuter()
			if err != nil {
				xpriv.Zero()
				return 0, err
			}
			extKey, intKey, err := deriveBranches(xpub)
			if err != nil {
				xpriv.Zero()
				return 0, err
			}
			addrs, err := deriveChildAddresses(extKey, 0, gapLimit, w.chainParams)
			xpriv.Zero()
			if err != nil {
				return 0, err
			}
			for _, a := range addrs {
				script, _, err := addressScript(a)
				if err != nil {
					log.Warnf("Failed to create output script for address %v: %v", a, err)
					continue
				}
				addrScriptAccts[string(script)] = acct
				addrScripts = append(addrScripts, script)
			}
			addrs, err = deriveChildAddresses(intKey, 0, gapLimit, w.chainParams)
			if err != nil {
				return 0, err
			}
			for _, a := range addrs {
				script, _, err := addressScript(a)
				if err != nil {
					log.Warnf("Failed to create output script for address %v: %v", a, err)
					continue
				}
				addrScriptAccts[string(script)] = acct
				addrScripts = append(addrScripts, script)
			}
		}

		searchBlocks, err := w.filterBlocks(ctx, &w.chainParams.GenesisHash, addrScripts)
		if err != nil {
			return 0, err
		}

		// Fetch blocks that have not been fetched yet, and reduce them to a set
		// of output script commitments.
		err = cacheMissingCommitments(ctx, p, blockCache, searchBlocks)
		if err != nil {
			return 0, err
		}

		// Search matching blocks for account usage.
		var lastUsed uint32
		for _, b := range searchBlocks {
			commitments := blockCache[*b]
			for _, script := range addrScripts {
				if _, ok := commitments[string(script)]; !ok {
					continue
				}

				// Filter match was not a false positive and an output pays to a
				// matching address in the block.  Look up the account of the
				// script and increase the last used account when necessary.
				acct := addrScriptAccts[string(script)]
				log.Debugf("Found match for script %x account %v in block %v",
					script, acct, b)
				if lastUsed < acct {
					lastUsed = acct
				}
			}
		}
		return lastUsed, nil
	}

	// A binary search may be needed to efficiently find the last used account
	// in the case where many accounts are used.  However, for most users, only
	// a small number of accounts are ever created so a linear scan is performed
	// first.  Search through the first two segments of accounts, and when the
	// last used account is not in the second segment, the bsearch is
	// unnecessary.
	lastUsed, err := lastUsedInRange(0, acctGapLimit*2)
	if err != nil {
		return 0, err
	}
	if lastUsed < acctGapLimit {
		return lastUsed, nil
	}

	// Fallback to a binary search, starting in the third segment
	var lo, hi uint32 = 2, hd.HardenedKeyStart / acctGapLimit
	for lo <= hi {
		mid := (hi + lo) / 2
		begin := mid * acctGapLimit
		end := begin + acctGapLimit
		last, err := lastUsedInRange(begin, end)
		if err != nil {
			return 0, err
		}
		if last > lastUsed {
			lastUsed = last
		}
		if mid == 0 {
			break
		}
		hi = mid - 1
	}
	return lastUsed, nil
}

// existsAddrIndexFinder implements address and account discovery using the
// exists address index of a trusted dcrd RPC server.
type existsAddrIndexFinder struct {
	wallet *Wallet
	rpc    *dcrd.RPC
}

func (f *existsAddrIndexFinder) findLastUsedAccount(ctx context.Context, coinTypeXpriv *hd.ExtendedKey) (uint32, error) {
	scanLen := uint32(f.wallet.accountGapLimit)
	var (
		lastUsed uint32
		lo, hi   uint32 = 0, hd.HardenedKeyStart / scanLen
	)
Bsearch:
	for lo <= hi {
		mid := (hi + lo) / 2
		type result struct {
			used    bool
			account uint32
			err     error
		}
		var results = make([]result, scanLen)
		var wg sync.WaitGroup
		for i := int(scanLen) - 1; i >= 0; i-- {
			i := i
			account := mid*scanLen + uint32(i)
			if account >= hd.HardenedKeyStart {
				continue
			}
			xpriv, err := coinTypeXpriv.Child(hd.HardenedKeyStart + account)
			if err != nil {
				return 0, err
			}
			xpub, err := xpriv.Neuter()
			if err != nil {
				xpriv.Zero()
				return 0, err
			}
			wg.Add(1)
			go func() {
				used, err := f.accountUsed(ctx, xpub)
				xpriv.Zero()
				results[i] = result{used, account, err}
				wg.Done()
			}()
		}
		wg.Wait()
		for i := int(scanLen) - 1; i >= 0; i-- {
			if results[i].err != nil {
				return 0, results[i].err
			}
			if results[i].used {
				lastUsed = results[i].account
				lo = mid + 1
				continue Bsearch
			}
		}
		if mid == 0 {
			break
		}
		hi = mid - 1
	}
	return lastUsed, nil
}

func (f *existsAddrIndexFinder) accountUsed(ctx context.Context, xpub *hd.ExtendedKey) (bool, error) {
	extKey, intKey, err := deriveBranches(xpub)
	if err != nil {
		return false, err
	}
	type result struct {
		used bool
		err  error
	}
	results := make(chan result, 2)
	merge := func(used bool, err error) {
		results <- result{used, err}
	}
	go func() { merge(f.branchUsed(ctx, extKey)) }()
	go func() { merge(f.branchUsed(ctx, intKey)) }()
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			return false, err
		}
		if r.used {
			return true, nil
		}
	}
	return false, nil
}

func (f *existsAddrIndexFinder) branchUsed(ctx context.Context, branchXpub *hd.ExtendedKey) (bool, error) {
	addrs, err := deriveChildAddresses(branchXpub, 0, uint32(f.wallet.gapLimit), f.wallet.chainParams)
	if err != nil {
		return false, err
	}
	bits, err := f.rpc.UsedAddresses(ctx, addrs)
	if err != nil {
		return false, err
	}
	for _, b := range bits {
		if b != 0 {
			return true, nil
		}
	}
	return false, nil
}

// findLastUsedAddress returns the child index of the last used child address
// derived from a branch key.  If no addresses are found, ^uint32(0) is
// returned.
func (f *existsAddrIndexFinder) findLastUsedAddress(ctx context.Context, xpub *hd.ExtendedKey) (uint32, error) {
	var (
		lastUsed        = ^uint32(0)
		scanLen         = uint32(f.wallet.gapLimit)
		segments        = hd.HardenedKeyStart / scanLen
		lo, hi   uint32 = 0, segments - 1
	)
Bsearch:
	for lo <= hi {
		mid := (hi + lo) / 2
		addrs, err := deriveChildAddresses(xpub, mid*scanLen, scanLen, f.wallet.chainParams)
		if err != nil {
			return 0, err
		}
		existsBits, err := f.rpc.UsedAddresses(ctx, addrs)
		if err != nil {
			return 0, err
		}
		for i := len(addrs) - 1; i >= 0; i-- {
			if existsBits.Get(i) {
				lastUsed = mid*scanLen + uint32(i)
				lo = mid + 1
				continue Bsearch
			}
		}
		if mid == 0 {
			break
		}
		hi = mid - 1
	}
	return lastUsed, nil
}

func (f *existsAddrIndexFinder) find(ctx context.Context, finder *addrFinder) error {
	var g errgroup.Group
	lastUsed := func(acct, branch uint32, index *uint32) error {
		var k *hd.ExtendedKey
		err := walletdb.View(f.wallet.db, func(tx walletdb.ReadTx) error {
			var err error
			k, err = f.wallet.Manager.AccountBranchExtendedPubKey(tx, acct, branch)
			return err
		})
		if err != nil {
			return err
		}
		lastUsed, err := f.findLastUsedAddress(ctx, k)
		if err != nil {
			return err
		}
		*index = lastUsed
		return nil
	}
	for i := range finder.usage {
		acct := uint32(i)
		u := &finder.usage[i]
		g.Go(func() error { return lastUsed(acct, 0, &u.extLastUsed) })
		g.Go(func() error { return lastUsed(acct, 1, &u.intLastUsed) })
	}
	return g.Wait()
}

func rpcFromPeer(p Peer) (*dcrd.RPC, bool) {
	switch p := p.(type) {
	case Caller:
		return dcrd.New(p), true
	default:
		return nil, false
	}
}

// DiscoverActiveAddresses searches for future wallet address usage in all
// blocks starting from startBlock.  If discoverAccts is true, used accounts
// will be discovered as well.  This feature requires the wallet to be unlocked
// in order to derive hardened account extended pubkeys.
//
// If the wallet is currently on the legacy coin type and no address or account
// usage is observed and coin type upgrades are not disabled, the wallet will be
// upgraded to the SLIP0044 coin type and the address discovery will occur
// again.
func (w *Wallet) DiscoverActiveAddresses(ctx context.Context, p Peer, startBlock *chainhash.Hash, discoverAccts bool) error {
	const op errors.Op = "wallet.DiscoverActiveAddresses"
	_, slip0044CoinType := udb.CoinTypes(w.chainParams)
	var activeCoinType uint32
	var coinTypeKnown, isSLIP0044CoinType bool
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		activeCoinType, err = w.Manager.CoinType(dbtx)
		if errors.Is(errors.WatchingOnly, err) {
			return nil
		}
		if err != nil {
			return err
		}
		coinTypeKnown = true
		isSLIP0044CoinType = activeCoinType == slip0044CoinType
		log.Debugf("DiscoverActiveAddresses: activeCoinType=%d", activeCoinType)
		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}

	// Map block hashes to a set of output scripts from the block.  This map is
	// queried to avoid fetching the same block multiple times, and blocks are
	// reduced to a set of committed scripts as that is the only thing being
	// searched for.
	blockAddresses := make(blockCommitmentCache)

	// Start by rescanning the accounts and determining what the current account
	// index is. This scan should only ever be performed if we're restoring our
	// wallet from seed.
	if discoverAccts {
		log.Infof("Discovering used accounts")
		var coinTypePrivKey *hd.ExtendedKey
		defer func() {
			if coinTypePrivKey != nil {
				coinTypePrivKey.Zero()
			}
		}()
		err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
			var err error
			coinTypePrivKey, err = w.Manager.CoinTypePrivKey(tx)
			return err
		})
		if err != nil {
			return errors.E(op, err)
		}
		var lastUsed uint32
		rpc, ok := rpcFromPeer(p)
		if ok {
			f := existsAddrIndexFinder{w, rpc}
			lastUsed, err = f.findLastUsedAccount(ctx, coinTypePrivKey)
		} else {
			lastUsed, err = w.findLastUsedAccount(ctx, p, blockAddresses, coinTypePrivKey)
		}
		if err != nil {
			return errors.E(op, err)
		}
		if lastUsed != 0 {
			var lastRecorded uint32
			acctXpubs := make(map[uint32]*hd.ExtendedKey)
			w.addressBuffersMu.Lock()
			err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
				ns := tx.ReadWriteBucket(waddrmgrNamespaceKey)
				var err error
				lastRecorded, err = w.Manager.LastAccount(ns)
				if err != nil {
					return err
				}
				for acct := lastRecorded + 1; acct <= lastUsed; acct++ {
					acct, err := w.Manager.NewAccount(ns, fmt.Sprintf("account-%d", acct))
					if err != nil {
						return err
					}
					xpub, err := w.Manager.AccountExtendedPubKey(tx, acct)
					if err != nil {
						return err
					}
					acctXpubs[acct] = xpub
				}
				return nil
			})
			if err != nil {
				w.addressBuffersMu.Unlock()
				return errors.E(op, err)
			}
			for acct := lastRecorded + 1; acct <= lastUsed; acct++ {
				_, ok := w.addressBuffers[acct]
				if !ok {
					xpub := acctXpubs[acct]
					extKey, intKey, err := deriveBranches(xpub)
					if err != nil {
						w.addressBuffersMu.Unlock()
						return errors.E(op, err)
					}
					w.addressBuffers[acct] = &bip0044AccountData{
						xpub:        xpub,
						albExternal: addressBuffer{branchXpub: extKey},
						albInternal: addressBuffer{branchXpub: intKey},
					}
				}
			}
			w.addressBuffersMu.Unlock()
		}
	}

	var lastAcct uint32
	err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		lastAcct, err = w.Manager.LastAccount(ns)
		return err
	})
	if err != nil {
		return errors.E(op, err)
	}

	// Discover address usage within known accounts
	// Usage recorded in finder.usage
	log.Infof("Discovering used addresses for %d account(s)", lastAcct+1)
	finder, err := newAddrFinder(w)
	if err != nil {
		return errors.E(op, err)
	}
	lastUsed := append([]accountUsage(nil), finder.usage...)
	rpc, ok := rpcFromPeer(p)
	if ok {
		f := existsAddrIndexFinder{w, rpc}
		err = f.find(ctx, finder)
	} else {
		err = finder.find(ctx, startBlock, p)
	}
	if err != nil {
		return errors.E(op, err)
	}
	for i := range finder.usage {
		u := &finder.usage[i]
		log.Infof("Account %d next child indexes: external:%d internal:%d",
			i, u.extLastUsed+1, u.intLastUsed+1)
	}

	// Save discovered addresses for each account plus additional future
	// addresses that may be used by other wallets sharing the same seed.
	// Multiple updates are used to allow cancellation.
	log.Infof("Updating DB with discovered addresses...")
	gapLimit := uint32(w.gapLimit)
	for i := range finder.usage {
		u := &finder.usage[i]
		acct := uint32(i)

		const N = 256
		max := u.extLastUsed + gapLimit
		for j := lastUsed[i].extLastUsed; ; j += N {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			to := j + N
			if to > max {
				to = max
			}
			err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
				ns := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
				return w.Manager.SyncAccountToAddrIndex(ns, acct, to, 0)
			})
			if err != nil {
				return errors.E(op, err)
			}
			if to == max {
				break
			}
		}

		max = u.intLastUsed + gapLimit
		for j := lastUsed[i].intLastUsed; ; j += N {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			to := j + N
			if to > max {
				to = max
			}
			err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
				ns := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
				return w.Manager.SyncAccountToAddrIndex(ns, acct, to, 1)
			})
			if err != nil {
				return errors.E(op, err)
			}
			if to == max {
				break
			}
		}

		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
			if u.extLastUsed < hd.HardenedKeyStart {
				err = w.Manager.MarkUsedChildIndex(dbtx, acct, 0, u.extLastUsed)
				if err != nil {
					return err
				}
			}
			if u.intLastUsed < hd.HardenedKeyStart {
				err = w.Manager.MarkUsedChildIndex(dbtx, acct, 1, u.intLastUsed)
				if err != nil {
					return err
				}
			}

			props, err := w.Manager.AccountProperties(ns, acct)
			if err != nil {
				return err
			}

			// Update last used index and cursor for this account's address
			// buffers.
			w.addressBuffersMu.Lock()
			acctData := w.addressBuffers[acct]
			acctData.albExternal.lastUsed = props.LastUsedExternalIndex
			acctData.albExternal.cursor = props.LastReturnedExternalIndex - props.LastUsedExternalIndex
			acctData.albInternal.lastUsed = props.LastUsedInternalIndex
			acctData.albInternal.cursor = props.LastReturnedInternalIndex - props.LastUsedInternalIndex
			w.addressBuffersMu.Unlock()
			return nil
		})
		if err != nil {
			return errors.E(op, err)
		}
	}

	// If the wallet does not know the current coin type (e.g. it is a watching
	// only wallet created from an account master pubkey) or when the wallet
	// uses the SLIP0044 coin type, there is nothing more to do.
	if !coinTypeKnown || isSLIP0044CoinType {
		log.Infof("Finished address discovery")
		return nil
	}

	// Do not upgrade legacy coin type wallets if there are returned or used
	// addresses or coin type upgrades are disabled.
	if !isSLIP0044CoinType && (w.disableCoinTypeUpgrades ||
		len(finder.usage) != 1 ||
		finder.usage[0].extLastUsed != ^uint32(0) ||
		finder.usage[0].intLastUsed != ^uint32(0)) {
		log.Infof("Finished address discovery")
		log.Warnf("Wallet contains addresses derived for the legacy BIP0044 " +
			"coin type and seed restores may not work with some other wallet " +
			"software")
		return nil
	}

	// Upgrade the coin type.
	log.Infof("Upgrading wallet from legacy coin type %d to SLIP0044 coin type %d",
		activeCoinType, slip0044CoinType)
	err = w.UpgradeToSLIP0044CoinType()
	if err != nil {
		log.Errorf("Coin type upgrade failed: %v", err)
		log.Warnf("Continuing with legacy BIP0044 coin type -- seed restores " +
			"may not work with some other wallet software")
		return nil
	}
	log.Infof("Upgraded coin type.")

	// Perform address discovery a second time using the upgraded coin type.
	return w.DiscoverActiveAddresses(ctx, p, startBlock, discoverAccts)
}
