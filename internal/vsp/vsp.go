package vsp

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
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

type VSP struct {
	vspURL          *url.URL
	pubKey          ed25519.PublicKey
	httpClient      *http.Client
	params          *chaincfg.Params
	c               *wallet.ConfirmationNotificationsClient
	w               *wallet.Wallet
	purchaseAccount uint32
	changeAccount   uint32

	queueMtx sync.Mutex
	queue    chan *queueEntry

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

func New(ctx context.Context, vspURL, pubKeyStr string, purchaseAccount, changeAccount uint32, dialer DialFunc, w *wallet.Wallet, params *chaincfg.Params) (*VSP, error) {
	u, err := url.Parse(vspURL)
	if err != nil {
		return nil, err
	}
	pubKey, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		return nil, err
	}

	transport := http.Transport{
		DialContext: dialer,
	}
	httpClient := &http.Client{
		Transport: &transport,
	}

	c := w.NtfnServer.ConfirmationNotifications(ctx)
	v := &VSP{
		vspURL:          u,
		pubKey:          ed25519.PublicKey(pubKey),
		httpClient:      httpClient,
		params:          params,
		w:               w,
		c:               c,
		queue:           make(chan *queueEntry, 256),
		purchaseAccount: purchaseAccount,
		changeAccount:   changeAccount,
		outpoints:       make(map[chainhash.Hash]*wire.MsgTx),
		feeToTicketMap:  make(map[chainhash.Hash]chainhash.Hash),
		ticketToFeeMap:  make(map[chainhash.Hash]PendingFee),
	}

	go func() {
		for {
			ntfns, err := c.Recv()
			if err != nil {
				log.Errorf("Recv failed: %v", err)
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
						v.Queue(ctx, *txHash, nil)
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
						txs = append(txs, &feeHash)
					}
					c.Watch(txs, requiredConfs)
				}()
			}
		}
	}()

	// Launch routine to process notifications
	go func() {
		t := w.NtfnServer.TransactionNotifications()
		defer t.Done()
		r := w.NtfnServer.RemovedTransactionNotifications()
		defer r.Done()
		for {
			select {
			case <-ctx.Done():
				break
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
							w.UnlockOutpoint(&outpoint.Hash, outpoint.Index)
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
				break

			case queuedItem := <-v.queue:
				feeTx, err := v.Process(ctx, queuedItem.TicketHash, nil)
				if err != nil {
					log.Warnf("Failed to process queued ticket %v, err: %v", queuedItem.TicketHash, err)
					if queuedItem.FeeTx != nil {
						for _, input := range queuedItem.FeeTx.TxIn {
							outpoint := input.PreviousOutPoint
							go func() { w.UnlockOutpoint(&outpoint.Hash, outpoint.Index) }()
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

func (v *VSP) Queue(ctx context.Context, ticketHash chainhash.Hash, feeTx *wire.MsgTx) {
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
	_, blockHeight := v.w.MainChainTip(ctx)

	if blockHeight < 0 {
		return
	}

	startBlockNum := blockHeight - int32(v.params.TicketExpiry+uint32(v.params.TicketMaturity)-requiredConfs)
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
			v.Queue(ctx, *ticketSummary.Ticket.Hash, nil)
		}

		return false, nil
	}

	err := v.w.GetTickets(ctx, f, startBlock, endBlock)
	if err != nil {
		log.Errorf("failed to sync tickets: %v", err)
	}
}

func (v *VSP) Process(ctx context.Context, ticketHash chainhash.Hash, credits []wallet.Input) (*wire.MsgTx, error) {
	feeAmount, err := v.GetFeeAddress(ctx, ticketHash)
	if err != nil {
		if strings.Contains(err.Error(), "ticket transaction could not be broadcast") {
			time.Sleep(2 * time.Minute)
			v.Queue(ctx, ticketHash, nil)
		}
		return nil, err
	}

	var totalValue int64
	if credits == nil {
		const minconf = 1
		credits, err = v.w.ReserveOutputsForAmount(ctx, v.purchaseAccount, feeAmount, minconf)
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
	err = v.w.SetPublished(ctx, &feeHash, false)
	if err != nil {
		return nil, err
	}
	paidTx, err := v.PayFee(ctx, ticketHash, feeTx)
	if err != nil {
		return nil, err
	}
	err = v.w.UpdateVspTicketFeeToPaid(ctx, &ticketHash, &feeHash)
	if err != nil {
		return nil, err
	}
	return paidTx, nil
}
