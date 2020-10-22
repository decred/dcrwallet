package vsp

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"decred.org/dcrwallet/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
)

const (
	apiVSPInfo = "/api/v3/vspinfo"

	serverSignature = "VSP-Server-Signature"
	requiredConfs   = 6 + 2
)

type PendingFee struct {
	CommitmentAddress dcrutil.Address
	VotingAddress     dcrutil.Address
	FeeAddress        dcrutil.Address
	FeeAmount         dcrutil.Amount
	FeeTx             *wire.MsgTx
}

type Config struct {
	// URL specifies the base URL of the VSP
	URL string

	// PubKey specifies the VSP's base64 encoded public key
	PubKey string

	// PurchaseAccount specifies the purchase account to pay fees from.
	PurchaseAccount uint32

	// ChangeAccount specifies the change account when creating fee transactions.
	ChangeAccount uint32

	// Dialer specifies an optional dialer when connecting to the VSP.
	Dialer DialFunc

	// Wallet specifies a loaded wallet.
	Wallet *wallet.Wallet

	// Params specifies the configured network.
	Params *chaincfg.Params
}

type VSP struct {
	cfg *Config

	vspURL     *url.URL
	pubKey     ed25519.PublicKey
	httpClient *http.Client
	c          *wallet.ConfirmationNotificationsClient

	queue chan *queueEntry

	outpointsMu sync.Mutex
	outpoints   map[chainhash.Hash]*wire.MsgTx

	ticketToFeeMu  sync.Mutex
	feeToTicketMap map[chainhash.Hash]chainhash.Hash
	ticketToFeeMap map[chainhash.Hash]PendingFee
}

type VSPTicket struct {
	FeeHash     chainhash.Hash
	FeeTxStatus uint32
}

type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func New(ctx context.Context, cfg Config) (*VSP, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	pubKey, err := base64.StdEncoding.DecodeString(cfg.PubKey)
	if err != nil {
		return nil, err
	}
	if cfg.Wallet == nil {
		return nil, fmt.Errorf("wallet option not set")
	}
	if cfg.Params == nil {
		return nil, fmt.Errorf("params option not set")
	}

	c := cfg.Wallet.NtfnServer.ConfirmationNotifications(ctx)
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: cfg.Dialer,
		},
	}
	v := &VSP{
		cfg:            &cfg,
		vspURL:         u,
		pubKey:         ed25519.PublicKey(pubKey),
		httpClient:     httpClient,
		c:              c,
		queue:          make(chan *queueEntry, 256),
		outpoints:      make(map[chainhash.Hash]*wire.MsgTx),
		feeToTicketMap: make(map[chainhash.Hash]chainhash.Hash),
		ticketToFeeMap: make(map[chainhash.Hash]PendingFee),
	}

	go func() {
		for {
			ntfns, err := c.Recv()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Errorf("Recv failed: %v", err)
				}
				break
			}
			watch := make(map[chainhash.Hash]chainhash.Hash)
			v.ticketToFeeMu.Lock()
			for _, confirmed := range ntfns {
				if confirmed.Confirmations != requiredConfs {
					continue
				}
				txHash := confirmed.TxHash

				// Is it a ticket?
				if pendingFee, exists := v.ticketToFeeMap[*txHash]; exists {
					delete(v.ticketToFeeMap, *txHash)
					feeTx := pendingFee.FeeTx
					feeHash := feeTx.TxHash()

					// check VSP
					ticketStatus, err := v.TicketStatus(ctx, txHash)
					if err != nil {
						log.Errorf("Failed to get ticket status for %v: %v", txHash, err)
						continue
					}
					switch ticketStatus.FeeTxStatus {
					case "broadcast":
						log.Infof("VSP has successfully sent the fee tx for %v", txHash)

						// Begin watching for feetx
						v.feeToTicketMap[feeHash] = *txHash
						watch[*txHash] = feeHash

					case "confirmed":
						log.Infof("VSP has successfully confirmed the fee tx for %v", txHash)
					case "error":
						log.Warnf("VSP failed to broadcast feetx for %v -- restarting process", txHash)
						v.queueItem(ctx, *txHash, nil)
					default:
						log.Warnf("VSP responded with %v for %v", ticketStatus.FeeTxStatus, txHash)
						continue
					}
				} else if ticketHash, exists := v.feeToTicketMap[*txHash]; exists {
					delete(v.feeToTicketMap, *txHash)
					log.Infof("Fee transaction %s for ticket %s confirmed", txHash, ticketHash)

					ticketStatus, err := v.TicketStatus(ctx, &ticketHash)
					if err != nil {
						log.Errorf("Failed to check status of ticket '%s': %v", ticketHash, err)
						continue
					}
					switch ticketStatus.FeeTxStatus {
					case "confirmed":
						log.Infof("VSP has successfully confirmed fee tx for %v", ticketHash)
					default:
						log.Warnf("Unexpected VSP server status for ticket '%s': %q", ticketHash, ticketStatus.FeeTxStatus)
					}
				}
			}
			v.ticketToFeeMu.Unlock()

			if len(watch) > 0 {
				go func() {
					var txs []*chainhash.Hash
					for txHash, feeHash := range watch {
						log.Infof("Ticket %s reached %d confirmations -- watching for feetx %s", txHash, requiredConfs, feeHash)
						hash := feeHash
						txs = append(txs, &hash)
					}
					c.Watch(txs, requiredConfs)
				}()
			}
		}
	}()

	// Launch routine to process notifications
	go func() {
		t := cfg.Wallet.NtfnServer.TransactionNotifications()
		defer t.Done()
		r := cfg.Wallet.NtfnServer.RemovedTransactionNotifications()
		defer r.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case added := <-t.C:
				v.outpointsMu.Lock()
				for _, addedHash := range added.UnminedTransactionHashes {
					delete(v.outpoints, *addedHash)
				}
				v.outpointsMu.Unlock()
			case removed := <-r.C:
				txHash := removed.TxHash

				v.outpointsMu.Lock()
				credits, exists := v.outpoints[txHash]
				if exists {
					delete(v.outpoints, txHash)
					go func() {
						for _, input := range credits.TxIn {
							outpoint := input.PreviousOutPoint
							cfg.Wallet.UnlockOutpoint(&outpoint.Hash, outpoint.Index)
							log.Infof("unlocked outpoint %v for deleted ticket %s",
								outpoint, txHash)
						}
					}()
				}
				v.outpointsMu.Unlock()
			}
		}
	}()

	// Launch routine to pay fee of processed tickets.
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(v.queue)
				return

			case queuedItem := <-v.queue:
				feeTx, err := v.Process(ctx, queuedItem.TicketHash, nil)
				if err != nil {
					log.Warnf("Failed to process queued ticket %v, err: %v", queuedItem.TicketHash, err)
					if queuedItem.FeeTx != nil {
						for _, input := range queuedItem.FeeTx.TxIn {
							outpoint := input.PreviousOutPoint
							go func() { cfg.Wallet.UnlockOutpoint(&outpoint.Hash, outpoint.Index) }()
						}
					}
					continue
				}
				v.outpointsMu.Lock()
				v.outpoints[feeTx.TxHash()] = queuedItem.FeeTx
				v.outpointsMu.Unlock()
			}
		}
	}()

	return v, nil
}

type queueEntry struct {
	TicketHash chainhash.Hash
	FeeTx      *wire.MsgTx
}

func (v *VSP) queueItem(ctx context.Context, ticketHash chainhash.Hash, feeTx *wire.MsgTx) {
	queuedTicket := &queueEntry{
		TicketHash: ticketHash,
		FeeTx:      feeTx,
	}
	select {
	case v.queue <- queuedTicket:
	case <-ctx.Done():
	}
}

func (v *VSP) Sync(ctx context.Context) {
	_, blockHeight := v.cfg.Wallet.MainChainTip(ctx)

	if blockHeight < 0 {
		return
	}

	startBlockNum := blockHeight - int32(v.cfg.Params.TicketExpiry+uint32(v.cfg.Params.TicketMaturity)-requiredConfs)
	startBlock := wallet.NewBlockIdentifierFromHeight(startBlockNum)
	endBlock := wallet.NewBlockIdentifierFromHeight(blockHeight)

	f := func(ticketSummaries []*wallet.TicketSummary, _ *wire.BlockHeader) (bool, error) {
		for _, ticketSummary := range ticketSummaries {
			switch ticketSummary.Status {
			case wallet.TicketStatusLive:
			case wallet.TicketStatusImmature:
			case wallet.TicketStatusUnspent:
			default:
				continue
			}
			// TODO get unpublished fee txs here and sync them
			v.queueItem(ctx, *ticketSummary.Ticket.Hash, nil)
		}

		return false, nil
	}

	err := v.cfg.Wallet.GetTickets(ctx, f, startBlock, endBlock)
	if err != nil {
		log.Errorf("failed to sync tickets: %v", err)
	}
}

func (v *VSP) Process(ctx context.Context, ticketHash chainhash.Hash, credits []wallet.Input) (*wire.MsgTx, error) {
	var feeAmount dcrutil.Amount
	var err error
	for i := 0; i < 2; i++ {
		feeAmount, err = v.GetFeeAddress(ctx, ticketHash)
		if err == nil {
			break
		}
		const broadcastMsg = "ticket transaction could not be broadcast"
		if err != nil && i == 0 && strings.Contains(err.Error(), broadcastMsg) {
			time.Sleep(2 * time.Minute)
		}
	}
	if err != nil {
		return nil, err
	}

	var totalValue int64
	if credits == nil {
		const minconf = 1
		credits, err = v.cfg.Wallet.ReserveOutputsForAmount(ctx, v.cfg.PurchaseAccount, feeAmount, minconf)
		if err != nil {
			return nil, err
		}
		for _, credit := range credits {
			totalValue += credit.PrevOut.Value
		}
		if dcrutil.Amount(totalValue) < feeAmount {
			return nil, fmt.Errorf("reserved credits insufficient: %v < %v",
				dcrutil.Amount(totalValue), feeAmount)
		}
	}

	feeTx, err := v.CreateFeeTx(ctx, ticketHash, credits)
	if err != nil {
		return nil, err
	}
	// set fee tx as unpublished, because it will be published by the vsp.
	feeHash := feeTx.TxHash()
	err = v.cfg.Wallet.SetPublished(ctx, &feeHash, false)
	if err != nil {
		return nil, err
	}
	paidTx, err := v.PayFee(ctx, ticketHash, feeTx)
	if err != nil {
		return nil, err
	}
	err = v.cfg.Wallet.UpdateVspTicketFeeToPaid(ctx, &ticketHash, &feeHash)
	if err != nil {
		return nil, err
	}
	return paidTx, nil
}
