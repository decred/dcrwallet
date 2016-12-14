package main

import (
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrwallet/ticketbuyer"
	"github.com/decred/dcrwallet/wallet"
)

const (
	defaultTicketAddress      = ""
	defaultMaxFee             = 1.0
	defaultMinFee             = 0.01
	defaultMaxPriceScale      = 2.0
	defaultMinPriceScale      = 0.7
	defaultPriceTarget        = 0.0
	defaultAvgVWAPPriceDelta  = 2880
	defaultMaxPerBlock        = 3
	defaultHighPricePenalty   = 1.3
	defaultTicketFeeInfo      = false
	defaultBlocksToAvg        = 11
	defaultFeeTargetScaling   = 1.05
	defaultDontWaitForTickets = false
	defaultMaxInMempool       = 0
	defaultExpiryDelta        = 16
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

// purchase purchases the tickets for the given block height.
func (p *purchaseManager) purchase(height int64) {
	tkbyLog.Infof("Block height %v connected", height)
	purchaseInfo, err := p.purchaser.Purchase(height)
	if err != nil {
		tkbyLog.Errorf("Failed to purchase tickets this round: %v", err)
		return
	}
	tkbyLog.Debugf("Purchased %v tickets this round", purchaseInfo.Purchased)
}

// ntfnHandler handles notifications, which trigger ticket purchases.
func (p *purchaseManager) ntfnHandler() {
out:
	for {
		select {
		case v := <-p.ntfnChan:
			if v != nil {
				for _, block := range v.AttachedBlocks {
					go p.purchase(int64(block.Height))
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
