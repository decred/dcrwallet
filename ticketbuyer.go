package main

import (
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/ticketbuyer"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wtxmgr"
)

// startTicketPurchase launches ticketbuyer to start purchasing tickets.
func startTicketPurchase(w *wallet.Wallet, dcrdClient *dcrrpcclient.Client, ticketbuyerCfg *ticketbuyer.Config) {
	walletCfg := &ticketbuyer.WalletCfg{
		GetOwnMempoolTix: func() (uint32, error) {
			sinfo, err := w.StakeInfo()
			if err != nil {
				return 0, err
			}
			return sinfo.OwnMempoolTix, nil
		},
		SetTxFee: func(fee dcrutil.Amount) error {
			w.SetRelayFee(fee)
			return nil
		},
		SetTicketFee: func(fee dcrutil.Amount) error {
			w.SetTicketFeeIncrement(fee)
			return nil
		},
		GetBalance: func() (dcrutil.Amount, error) {
			account, err := w.AccountNumber(cfg.PurchaseAccount)
			if err != nil {
				return 0, err
			}
			return w.CalculateAccountBalance(account, 0, wtxmgr.BFBalanceSpendable)
		},
		GetRawChangeAddress: func() (dcrutil.Address, error) {
			account, err := w.AccountNumber(cfg.PurchaseAccount)
			if err != nil {
				return nil, err
			}
			return w.NewAddress(account, waddrmgr.InternalBranch)
		},
		PurchaseTicket: func(
			spendLimit dcrutil.Amount,
			minConf *int,
			ticketAddress dcrutil.Address,
			numTickets *int,
			poolAddress dcrutil.Address,
			poolFees *dcrutil.Amount,
			expiry *int) ([]*chainhash.Hash, error) {
			account, err := w.AccountNumber(cfg.PurchaseAccount)
			if err != nil {
				return nil, err
			}
			hashes, err := w.PurchaseTickets(0, spendLimit, int32(*minConf), ticketAddress,
				account, *numTickets, poolAddress, poolFees.ToCoin(), int32(*expiry), w.RelayFee(),
				w.TicketFeeIncrement())
			if err != nil {
				return nil, err
			}
			hashesTyped, ok := hashes.([]*chainhash.Hash)
			if !ok {
				return nil, fmt.Errorf("Unable to cast response as a slice " +
					"of hash strings")
			}
			return hashesTyped, err
		},
	}
	purchaser, err := ticketbuyer.NewTicketPurchaser(ticketbuyerCfg,
		dcrdClient, walletCfg, activeNet.Params)
	if err != nil {
		tkbyLog.Errorf("Error starting ticketbuyer: %v", err)
		return
	}
	tkbyLog.Debugf("Initialized ticket auto-ticketpurchaser")
	w.SetTicketPurchaser(purchaser)
	addInterruptHandler(func() {
		dcrdClient.Disconnect()
	})
}
