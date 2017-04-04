// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ticketbuyer

import (
	"sync"

	"github.com/decred/dcrwallet/wallet"
)

// PurchaseManager is the main handler of websocket notifications to
// pass to the purchaser and internal quit notifications.
type PurchaseManager struct {
	w          *wallet.Wallet
	purchaser  *TicketPurchaser
	ntfnChan   <-chan *wallet.MainTipChangedNotification
	passphrase []byte
	wg         sync.WaitGroup
	quitMtx    sync.Mutex
	quit       chan struct{}
}

// NewPurchaseManager creates a new PurchaseManager.
func NewPurchaseManager(w *wallet.Wallet, purchaser *TicketPurchaser,
	ntfnChan <-chan *wallet.MainTipChangedNotification, passphrase []byte) *PurchaseManager {
	return &PurchaseManager{
		w:          w,
		purchaser:  purchaser,
		ntfnChan:   ntfnChan,
		passphrase: passphrase,
		quit:       make(chan struct{}),
	}
}

// purchase purchases the tickets for the given block height.
func (p *PurchaseManager) purchase(height int64) {
	err := p.w.Unlock(p.passphrase, nil)
	if err != nil {
		log.Errorf("Failed to purchase tickets this round: %v", err)
		return
	}
	purchaseInfo, err := p.purchaser.Purchase(height)
	if err != nil {
		log.Errorf("Failed to purchase tickets this round: %v", err)
		return
	}
	// Since we don't know if the wallet had been unlocked before we unlocked
	// it, avoid locking it here, even though we don't need it to remain
	// unlocked.
	log.Debugf("Purchased %v tickets this round", purchaseInfo.Purchased)
}

// Purchaser returns the ticket buyer instance associated with the purchase
// manager.
func (p *PurchaseManager) Purchaser() *TicketPurchaser {
	return p.purchaser
}

// NotificationHandler handles notifications, which trigger ticket purchases.
func (p *PurchaseManager) NotificationHandler() {
	p.quitMtx.Lock()
	quit := p.quit
	p.quitMtx.Unlock()

	s1, s2 := make(chan struct{}), make(chan struct{})
	close(s1) // unblock first worker
out:
	for {
		select {
		case v, ok := <-p.ntfnChan:
			if !ok {
				break out
			}
			p.wg.Add(1)
			go func(s1, s2 chan struct{}) {
				defer p.wg.Done()
				select {
				case <-s1: // wait for previous worker to finish
				case <-quit:
					return
				}
				// Purchase tickets for each attached block, not just for the
				// update to the main chain.  This is probably not optimal but
				// it matches how dcrticketbuyer worked.
				for h := v.NewHeight - int32(len(v.AttachedBlocks)) + 1; h <= v.NewHeight; h++ {
					p.purchase(int64(h))
				}

				close(s2) // unblock next worker
			}(s1, s2)
			s1, s2 = s2, make(chan struct{})
		case <-quit:
			break out
		}
	}
	p.wg.Done()
}

// Start starts the purchase manager goroutines.
func (p *PurchaseManager) Start() {
	p.wg.Add(1)
	go p.NotificationHandler()

	log.Infof("Starting ticket buyer")
}

// WaitForShutdown blocks until all purchase manager goroutines have finished executing.
func (p *PurchaseManager) WaitForShutdown() {
	p.wg.Wait()
}

// Stop signals all purchase manager goroutines to shutdown.
func (p *PurchaseManager) Stop() {
	p.quitMtx.Lock()
	quit := p.quit

	log.Infof("Stopping ticket buyer")

	select {
	case <-quit:
	default:
		close(quit)
	}
	p.quitMtx.Unlock()
}
