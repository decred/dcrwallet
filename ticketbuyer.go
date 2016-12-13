package main

import (
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrwallet/ticketbuyer"
	"github.com/decred/dcrwallet/wallet"
)

// purchaseManager is the main handler of websocket notifications to
// pass to the purchaser and internal quit notifications.
type purchaseManager struct {
	purchaser *ticketbuyer.TicketPurchaser
	ntfnChan  <-chan *wallet.TransactionNotifications
	quit      chan struct{}
}

// newPurchaseManager creates a new purchaseManager.
func newPurchaseManager(purchaser *ticketbuyer.TicketPurchaser,
	ntfnChan <-chan *wallet.TransactionNotifications,
	quit chan struct{}) *purchaseManager {
	return &purchaseManager{
		purchaser: purchaser,
		ntfnChan:  ntfnChan,
		quit:      quit,
	}
}

// ntfnHandler handles notifications, which trigger ticket purchases.
func (p *purchaseManager) ntfnHandler() {
out:
	for {
		select {
		case v := <-p.ntfnChan:
			if v != nil {
				for _, block := range v.AttachedBlocks {
					tkbyLog.Infof("Block height %v connected", block.Height)
					purchaseInfo, err := p.purchaser.Purchase(int64(block.Height))
					if err != nil {
						tkbyLog.Errorf("Failed to purchase tickets this round: %s",
							err.Error())
						continue
					}
					tkbyLog.Debugf("Purchased %v tickets this round", purchaseInfo.Purchased)
				}
			}
		case <-p.quit:
			break out
		}
	}
}

// startTicketPurchase launches ticketbuyer to start purchasing tickets.
func startTicketPurchase(w *wallet.Wallet, dcrdClient *dcrrpcclient.Client,
	ticketbuyerCfg *ticketbuyer.Config) {
	p, err := ticketbuyer.NewTicketPurchaser(ticketbuyerCfg,
		dcrdClient, w, activeNet.Params)
	if err != nil {
		tkbyLog.Errorf("Error starting ticketbuyer: %v", err)
		return
	}
	quit := make(chan struct{})
	n := w.NtfnServer.TransactionNotifications()
	pm := newPurchaseManager(p, n.C, quit)
	go pm.ntfnHandler()
	addInterruptHandler(func() {
		n.Done()
		close(pm.quit)
	})
}
