package main

import (
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrwallet/ticketbuyer"
	"github.com/decred/dcrwallet/wallet"
)

// blockChanSize is the size of the buffered block connected channel.
const blockChanSize = 100

// purchaseManager is the main handler of websocket notifications to
// pass to the purchaser and internal quit notifications.
type purchaseManager struct {
	purchaser *ticketbuyer.TicketPurchaser
	blockChan chan int64
	quit      chan struct{}
}

// newPurchaseManager creates a new purchaseManager.
func newPurchaseManager(purchaser *ticketbuyer.TicketPurchaser,
	blockChan chan int64,
	quit chan struct{}) *purchaseManager {
	return &purchaseManager{
		purchaser: purchaser,
		blockChan: blockChan,
		quit:      quit,
	}
}

// blockConnectedHandler handles block connected notifications, which trigger
// ticket purchases.
func (p *purchaseManager) blockConnectedHandler() {
out:
	for {
		select {
		case height := <-p.blockChan:
			tkbyLog.Infof("Block height %v connected", height)
			purchaseInfo, err := p.purchaser.Purchase(height)
			if err != nil {
				tkbyLog.Errorf("Failed to purchase tickets this round: %s",
					err.Error())
				continue
			}
			tkbyLog.Debugf("Purchased %v tickets this round", purchaseInfo.Purchased)
		case <-p.quit:
			break out
		}
	}
}

// startTicketPurchase launches ticketbuyer to start purchasing tickets.
func startTicketPurchase(w *wallet.Wallet, dcrdClient *dcrrpcclient.Client, ticketbuyerCfg *ticketbuyer.Config) {
	p, err := ticketbuyer.NewTicketPurchaser(ticketbuyerCfg,
		dcrdClient, w, activeNet.Params)
	if err != nil {
		tkbyLog.Errorf("Error starting ticketbuyer: %v", err)
		return
	}
	blockChan := make(chan int64, blockChanSize)
	quit := make(chan struct{})
	pm := newPurchaseManager(p, blockChan, quit)
	go pm.blockConnectedHandler()
	addInterruptHandler(func() {
		dcrdClient.Disconnect()
	})
}
