// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ticketbuyer

import (
	"github.com/decred/dcrwallet/wallet"
)

// PurchaseManager is the main handler of websocket notifications to
// pass to the purchaser and internal quit notifications.
type PurchaseManager struct {
	w         *wallet.Wallet
	purchaser *TicketPurchaser
	ntfnChan  <-chan *wallet.MainTipChangedNotification
	quit      chan struct{}
}

// NewPurchaseManager creates a new PurchaseManager.
func NewPurchaseManager(w *wallet.Wallet, purchaser *TicketPurchaser,
	ntfnChan <-chan *wallet.MainTipChangedNotification,
	quit chan struct{}) *PurchaseManager {
	return &PurchaseManager{
		w:         w,
		purchaser: purchaser,
		ntfnChan:  ntfnChan,
		quit:      quit,
	}
}

// purchase purchases the tickets for the given block height.
func (p *PurchaseManager) purchase(height int64) {
	purchaseInfo, err := p.purchaser.Purchase(height)
	if err != nil {
		log.Errorf("Failed to purchase tickets this round: %v", err)
		return
	}
	log.Debugf("Purchased %v tickets this round", purchaseInfo.Purchased)
}

// NotificationHandler handles notifications, which trigger ticket purchases.
func (p *PurchaseManager) NotificationHandler() {
	s1, s2 := make(chan struct{}), make(chan struct{})
	close(s1) // unblock first worker
out:
	for {
		select {
		case v := <-p.ntfnChan:
			go func(s1, s2 chan struct{}) {
				<-s1 // wait for previous worker to finish

				// Purchase tickets for each attached block, not just for the
				// update to the main chain.  This is probably not optimal but
				// it matches how dcrticketbuyer worked.
				for h := v.NewHeight - int32(len(v.AttachedBlocks)) + 1; h <= v.NewHeight; h++ {
					p.purchase(int64(h))
				}

				close(s2) // unblock next worker
			}(s1, s2)
			s1, s2 = s2, make(chan struct{})
		case <-p.quit:
			break out
		}
	}
}
