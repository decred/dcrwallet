// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/decred/bitset"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

func (w *Wallet) findLastUsedAccount(client *chain.RPCClient, coinTypeXpriv *hdkeychain.ExtendedKey) (uint32, error) {
	const scanLen = 100
	var (
		lastUsed uint32
		lo, hi   uint32 = 0, hdkeychain.HardenedKeyStart / scanLen
	)
Bsearch:
	for lo <= hi {
		mid := (hi + lo) / 2
		type result struct {
			used    bool
			account uint32
			err     error
		}
		var results [scanLen]result
		var wg sync.WaitGroup
		for i := scanLen - 1; i >= 0; i-- {
			i := i
			account := mid*scanLen + uint32(i)
			if account >= hdkeychain.HardenedKeyStart {
				continue
			}
			xpriv, err := coinTypeXpriv.Child(hdkeychain.HardenedKeyStart + account)
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
				used, err := w.accountUsed(client, xpub)
				xpriv.Zero()
				results[i] = result{used, account, err}
				wg.Done()
			}()
		}
		wg.Wait()
		for i := scanLen - 1; i >= 0; i-- {
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

func (w *Wallet) accountUsed(client *chain.RPCClient, xpub *hdkeychain.ExtendedKey) (bool, error) {
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
	go func() { merge(w.branchUsed(client, extKey)) }()
	go func() { merge(w.branchUsed(client, intKey)) }()
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

func (w *Wallet) branchUsed(client *chain.RPCClient, branchXpub *hdkeychain.ExtendedKey) (bool, error) {
	addrs, err := deriveChildAddresses(branchXpub, 0, uint32(w.gapLimit), w.chainParams)
	if err != nil {
		return false, err
	}
	existsBitsHex, err := client.ExistsAddresses(addrs)
	if err != nil {
		return false, err
	}
	for _, r := range existsBitsHex {
		if r != '0' {
			return true, nil
		}
	}
	return false, nil
}

func (w *Wallet) findLastUsedAddress(client *chain.RPCClient, xpub *hdkeychain.ExtendedKey) (uint32, error) {
	var (
		lastUsed uint32
		scanLen         = uint32(w.gapLimit)
		segments        = hdkeychain.HardenedKeyStart / scanLen
		lo, hi   uint32 = 0, segments - 1
	)
Bsearch:
	for lo <= hi {
		mid := (hi + lo) / 2
		addrs, err := deriveChildAddresses(xpub, mid*scanLen, scanLen, w.chainParams)
		if err != nil {
			return 0, err
		}
		existsBitsHex, err := client.ExistsAddresses(addrs)
		if err != nil {
			return 0, err
		}
		existsBits, err := hex.DecodeString(existsBitsHex)
		if err != nil {
			return 0, err
		}
		for i := len(addrs) - 1; i >= 0; i-- {
			if bitset.Bytes(existsBits).Get(i) {
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

// DiscoverActiveAddresses accesses the consensus RPC server to discover all the
// addresses that have been used by an HD keychain stemming from this wallet. If
// discoverAccts is true, used accounts will be discovered as well.  This
// feature requires the wallet to be unlocked in order to derive hardened
// account extended pubkeys.
//
// A transaction filter (re)load and rescan should be performed after discovery.
func (w *Wallet) DiscoverActiveAddresses(chainClient *chain.RPCClient, discoverAccts bool) error {
	// Start by rescanning the accounts and determining what the
	// current account index is. This scan should only ever be
	// performed if we're restoring our wallet from seed.
	if discoverAccts {
		log.Infof("Discovering used accounts")
		var coinTypePrivKey *hdkeychain.ExtendedKey
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
			return err
		}
		lastUsed, err := w.findLastUsedAccount(chainClient, coinTypePrivKey)
		if err != nil {
			return err
		}
		if lastUsed != 0 {
			var lastRecorded uint32
			acctXpubs := make(map[uint32]*hdkeychain.ExtendedKey)
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
				return err
			}
			for acct := lastRecorded + 1; acct <= lastUsed; acct++ {
				_, ok := w.addressBuffers[acct]
				if !ok {
					extKey, intKey, err := deriveBranches(acctXpubs[acct])
					if err != nil {
						w.addressBuffersMu.Unlock()
						return err
					}
					w.addressBuffers[acct] = &bip0044AccountData{
						albExternal: addressBuffer{branchXpub: extKey},
						albInternal: addressBuffer{branchXpub: intKey},
					}
				}
			}
			w.addressBuffersMu.Unlock()
		}
	}

	var lastAcct uint32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		lastAcct, err = w.Manager.LastAccount(ns)
		return err
	})
	if err != nil {
		return err
	}

	log.Infof("Discovering used addresses for %d account(s)", lastAcct+1)

	// Rescan addresses for the both the internal and external
	// branches of the account.
	errs := make(chan error, lastAcct+1)
	var wg sync.WaitGroup
	wg.Add(int(lastAcct + 1))
	for acct := uint32(0); acct <= lastAcct; acct++ {
		// Address usage discovery for each account can be performed
		// concurrently.
		acct := acct
		go func() {
			defer wg.Done()
			// Do this for both external (0) and internal (1) branches.
			for branch := uint32(0); branch < 2; branch++ {
				var branchXpub *hdkeychain.ExtendedKey
				err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
					var err error
					branchXpub, err = w.Manager.AccountBranchExtendedPubKey(tx, acct, branch)
					return err
				})
				if err != nil {
					errs <- err
					return
				}

				lastUsed, err := w.findLastUsedAddress(chainClient, branchXpub)
				if err != nil {
					errs <- err
					return
				}

				// Save discovered addresses for the account plus additional
				// addresses that may be used by other wallets sharing the same
				// seed.
				err = walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
					ns := tx.ReadWriteBucket(waddrmgrNamespaceKey)

					// SyncAccountToAddrIndex never removes derived addresses from
					// an account, and can be called with just the discovered last
					// used child index, plus the gap limit.  Cap it to the highest
					// child index.
					gapLimit := uint32(w.gapLimit)
					err := w.Manager.SyncAccountToAddrIndex(ns, acct,
						minUint32(lastUsed+gapLimit, hdkeychain.HardenedKeyStart-1),
						branch)
					if err != nil {
						return err
					}
					err = w.Manager.MarkUsedChildIndex(tx, acct, branch, lastUsed)
					if err != nil {
						return err
					}

					w.addressBuffersMu.Lock()
					acctData := w.addressBuffers[acct]
					buf := &acctData.albExternal
					if branch == udb.InternalBranch {
						buf = &acctData.albInternal
					}
					buf.lastUsed = lastUsed
					buf.cursor = 0
					w.addressBuffersMu.Unlock()

					log.Infof("Synchronized account %d branch %d to last used child index %v",
						acct, branch, lastUsed)
					return nil
				})
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-errs:
		// Drain remaining
		go func() {
			for {
				select {
				case <-errs:
				default:
					return
				}
			}
		}()
		return err
	default:
		log.Infof("Finished address discovery")
		return nil
	}
}
