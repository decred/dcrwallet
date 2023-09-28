// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"fmt"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/wallet/udb"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

type VSPTicket struct {
	// Fields set during creation and never change.
	hash           *chainhash.Hash
	commitmentAddr stdaddr.StakeAddress
	votingAddr     stdaddr.StakeAddress
	votingKey      string

	wallet *Wallet
}

// NewVSPTicket ensures the provided hash refers to a ticket with exactly 3
// outputs. It returns a VSPTicket instance containing all of the information
// necessary to register the ticket with a VSP.
func (w *Wallet) NewVSPTicket(ctx context.Context, hash *chainhash.Hash) (*VSPTicket, error) {
	txs, _, err := w.GetTransactionsByHashes(ctx, []*chainhash.Hash{hash})
	if err != nil {
		return nil, err
	}

	ticketTx := txs[0]

	if !stake.IsSStx(ticketTx) {
		return nil, fmt.Errorf("%v is not a ticket", hash)
	}

	if len(ticketTx.TxOut) != 3 {
		return nil, fmt.Errorf("ticket %v has multiple commitments", hash)
	}

	_, addrs := stdscript.ExtractAddrs(ticketTx.TxOut[0].Version, ticketTx.TxOut[0].PkScript, w.chainParams)
	if len(addrs) != 1 {
		return nil, fmt.Errorf("cannot parse voting addr for ticket %v", hash)
	}

	var votingAddr stdaddr.StakeAddress
	switch addr := addrs[0].(type) {
	case stdaddr.StakeAddress:
		votingAddr = addr
	default:
		return nil, fmt.Errorf("address cannot be used for voting rights: %v", err)
	}

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, w.chainParams)
	if err != nil {
		return nil, fmt.Errorf("cannot parse commitment address: %w", err)
	}

	votingKey, err := w.DumpWIFPrivateKey(ctx, votingAddr)
	if err != nil {
		return nil, err
	}

	return &VSPTicket{
		hash:           hash,
		votingAddr:     votingAddr,
		commitmentAddr: commitmentAddr,
		votingKey:      votingKey,

		wallet: w,
	}, nil
}

func (v *VSPTicket) String() string {
	return v.hash.String()
}
func (v *VSPTicket) Hash() *chainhash.Hash {
	return v.hash
}
func (v *VSPTicket) CommitmentAddr() stdaddr.StakeAddress {
	return v.commitmentAddr
}
func (v *VSPTicket) VotingAddr() stdaddr.StakeAddress {
	return v.votingAddr
}
func (v *VSPTicket) VotingKey() string {
	return v.votingKey
}

func (v *VSPTicket) AgendaChoices(ctx context.Context) (map[string]string, error) {
	choices, _, err := v.wallet.AgendaChoices(ctx, v.hash)
	if err != nil {
		return nil, err
	}

	return choices, nil
}

// TSpendPolicyForTicket returns all of the tspend policies set for a single
// ticket. It does not consider the global wallet setting.
func (v *VSPTicket) TSpendPolicy() map[string]string {
	w := v.wallet
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	policies := make(map[string]string)
	for key, value := range w.vspTSpendPolicy {
		if key.Ticket.IsEqual(v.hash) {
			var choice string
			switch value {
			case stake.TreasuryVoteYes:
				choice = "yes"
			case stake.TreasuryVoteNo:
				choice = "no"
			default:
				choice = "abstain"
			}
			policies[key.TSpend.String()] = choice
		}
	}
	return policies
}

// TreasuryKeyPolicy returns all of the treasury key policies set for a single
// ticket. It does not consider the global wallet setting.
func (v *VSPTicket) TreasuryKeyPolicy() map[string]string {
	w := v.wallet
	w.stakeSettingsLock.Lock()
	defer w.stakeSettingsLock.Unlock()

	policies := make(map[string]string)
	for key, value := range w.vspTSpendKeyPolicy {
		if key.Ticket.IsEqual(v.hash) {
			var choice string
			switch value {
			case stake.TreasuryVoteYes:
				choice = "yes"
			case stake.TreasuryVoteNo:
				choice = "no"
			default:
				choice = "abstain"
			}
			policies[key.TreasuryKey] = choice
		}
	}
	return policies
}

func (v *VSPTicket) Spent(ctx context.Context) bool {
	ticketOut := wire.OutPoint{Hash: *v.hash, Index: 0, Tree: 1}
	_, _, err := v.wallet.Spender(ctx, &ticketOut)
	return err == nil
}

func (v *VSPTicket) TxBlock(ctx context.Context) (int32, error) {
	var height int32
	err := walletdb.View(ctx, v.wallet.db, func(dbtx walletdb.ReadTx) error {
		var err error
		height, err = v.wallet.txStore.TxBlockHeight(dbtx, v.hash)
		if err != nil {
			return err
		}
		if height == -1 {
			return nil
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return height, nil
}

func (v *VSPTicket) UpdateFeeConfirmed(ctx context.Context, feeHash chainhash.Hash, host string, pubkey []byte) error {
	return walletdb.Update(ctx, v.wallet.db, func(dbtx walletdb.ReadWriteTx) error {
		return udb.SetVSPTicket(dbtx, v.hash, &udb.VSPTicket{
			FeeHash:     feeHash,
			FeeTxStatus: uint32(udb.VSPFeeProcessConfirmed),
			Host:        host,
			PubKey:      pubkey,
		})
	})
}

func (v *VSPTicket) UpdateFeePaid(ctx context.Context, feeHash chainhash.Hash, host string, pubkey []byte) error {
	return walletdb.Update(ctx, v.wallet.db, func(dbtx walletdb.ReadWriteTx) error {
		return udb.SetVSPTicket(dbtx, v.hash, &udb.VSPTicket{
			FeeHash:     feeHash,
			FeeTxStatus: uint32(udb.VSPFeeProcessPaid),
			Host:        host,
			PubKey:      pubkey,
		})
	})
}

func (v *VSPTicket) UpdateFeeStarted(ctx context.Context, feeHash chainhash.Hash, host string, pubkey []byte) error {
	return walletdb.Update(ctx, v.wallet.db, func(dbtx walletdb.ReadWriteTx) error {
		return udb.SetVSPTicket(dbtx, v.hash, &udb.VSPTicket{
			FeeHash:     feeHash,
			FeeTxStatus: uint32(udb.VSPFeeProcessStarted),
			Host:        host,
			PubKey:      pubkey,
		})
	})
}

func (v *VSPTicket) UpdateFeeErrored(ctx context.Context, host string, pubkey []byte) error {
	return walletdb.Update(ctx, v.wallet.db, func(dbtx walletdb.ReadWriteTx) error {
		return udb.SetVSPTicket(dbtx, v.hash, &udb.VSPTicket{
			FeeHash:     chainhash.Hash{},
			FeeTxStatus: uint32(udb.VSPFeeProcessErrored),
			Host:        host,
			PubKey:      pubkey,
		})
	})
}

type TicketInfo struct {
	FeeHash     chainhash.Hash
	FeeTxStatus uint32
	VSPHostID   uint32
	Host        string
	PubKey      []byte
}

// VSPTicketInfo returns the various information for a given vsp ticket
func (v *VSPTicket) VSPTicketInfo(ctx context.Context) (*TicketInfo, error) {
	var data *udb.VSPTicket
	err := walletdb.View(ctx, v.wallet.db, func(dbtx walletdb.ReadTx) error {
		var err error
		data, err = udb.GetVSPTicket(dbtx, *v.hash)
		if err != nil {
			return err
		}
		return nil
	})
	if err == nil && data == nil {
		err = errors.E(errors.NotExist)
		return nil, err
	} else if data == nil {
		return nil, err
	}
	convertedData := &TicketInfo{
		FeeHash:     data.FeeHash,
		FeeTxStatus: data.FeeTxStatus,
		VSPHostID:   data.VSPHostID,
		Host:        data.Host,
		PubKey:      data.PubKey,
	}
	return convertedData, err
}
