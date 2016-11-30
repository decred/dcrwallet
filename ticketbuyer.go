package main

import (
	"errors"
	"io/ioutil"
	"time"

	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrticketbuyer/ticketbuyer"
	"github.com/decred/dcrwallet/wallet"
)

// getWalletRPCClient returns a rpc client connected to the local wallet instance.
func getWalletRPCClient() (*dcrrpcclient.Client, error) {
	// Connect to the dcrwallet server RPC client.
	var dcrwCerts []byte
	if !cfg.DisableClientTLS {
		var err error
		dcrwCerts, err = ioutil.ReadFile(cfg.RPCCert)
		if err != nil {
			return nil, err
		}
	}
	if len(cfg.LegacyRPCListeners) < 1 {
		return nil, errors.New("No RPC listeners configured")
	}
	connCfgWallet := &dcrrpcclient.ConnConfig{
		Host:         cfg.LegacyRPCListeners[0],
		Endpoint:     "ws",
		User:         cfg.Username,
		Pass:         cfg.Password,
		Certificates: dcrwCerts,
		DisableTLS:   cfg.DisableClientTLS,
	}
	tkbyLog.Debugf("Attempting to connect to dcrwallet RPC %s as user %s "+
		"using certificate located in %s",
		"localhost", cfg.Username, cfg.Password)
	dcrwClient, err := dcrrpcclient.New(connCfgWallet, nil)
	if err != nil {
		return nil, err
	}

	wi, err := dcrwClient.WalletInfo()
	if err != nil {
		return nil, err
	}
	if !wi.DaemonConnected {
		tkbyLog.Warnf("Wallet was not connected to a daemon at start up! " +
			"Please ensure wallet has proper connectivity.")
	}
	if !wi.Unlocked {
		tkbyLog.Warnf("Wallet is not unlocked! You will need to unlock " +
			"wallet for tickets to be purchased.")
	}
	return dcrwClient, nil
}

// startTicketPurchase launches ticketbuyer to start purchasing tickets.
func startTicketPurchase(w *wallet.Wallet) {
	retryDuration := time.Second * 5
	for {
		if w.ChainClient() != nil {
			break
		}
		time.Sleep(retryDuration)
		tkbyLog.Debugf("Retrying chain client connection in %v", retryDuration)
	}
	dcrwClient, err := getWalletRPCClient()
	if err != nil {
		tkbyLog.Errorf("Error fetching wallet rpc client: %v", err)
		return
	}
	dcrdClient := w.ChainClient().Client
	purchaser, err := ticketbuyer.NewTicketPurchaser(&ticketbuyer.Config{
		AccountName:        cfg.AccountName,
		AvgPriceMode:       cfg.AvgPriceMode,
		AvgPriceVWAPDelta:  cfg.AvgPriceVWAPDelta,
		BalanceToMaintain:  cfg.BalanceToMaintain,
		BlocksToAvg:        cfg.BlocksToAvg,
		DontWaitForTickets: cfg.DontWaitForTickets,
		ExpiryDelta:        cfg.ExpiryDelta,
		FeeSource:          cfg.FeeSource,
		FeeTargetScaling:   cfg.FeeTargetScaling,
		HighPricePenalty:   cfg.HighPricePenalty,
		MinFee:             cfg.MinFee,
		MinPriceScale:      cfg.MinPriceScale,
		MaxFee:             cfg.MaxFee,
		MaxPerBlock:        cfg.MaxPerBlock,
		MaxPriceAbsolute:   cfg.MaxPriceAbsolute,
		MaxPriceScale:      cfg.MaxPriceScale,
		MaxInMempool:       cfg.MaxInMempool,
		PoolAddress:        cfg.PoolAddress,
		PoolFees:           cfg.PoolFees,
		PriceTarget:        cfg.PriceTarget,
		TicketAddress:      cfg.TicketAddress,
		TxFee:              cfg.TicketFee,
	}, dcrdClient, dcrwClient, activeNet.Params)
	if err != nil {
		tkbyLog.Errorf("Error starting ticketbuyer: %v", err)
		return
	}
	tkbyLog.Debugf("Initialized ticket auto-ticketpurchaser")
	w.SetTicketPurchaser(purchaser)
	addInterruptHandler(func() {
		dcrdClient.Disconnect()
		dcrwClient.Disconnect()
	})
}
