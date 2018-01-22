package wallet

import (
	"fmt"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

type SummaryResolution uint8

const (
	SummaryResolutionBlock SummaryResolution = iota
	SummaryResolutionHour
	SummaryResolutionDay
	SummaryResolutionMonth
	SummaryResolutionYear
	SummaryResolutionAll
)

type SummaryName string
type SeriesID uint8

type SummaryResultDataPoint struct {
	IntValue   int64
	FloatValue float64
}
type SummaryResult map[SeriesID]*SummaryResultDataPoint

type SummaryResultFunc func(time.Time, SummaryResult) (bool, error)

type Summary interface {
	Calculate(beginHeight, endHeight int32, resolution SummaryResolution, f SummaryResultFunc) error
}

var epochTime = time.Date(0, 0, 0, 0, 0, 0, 0, time.Local)

// referenceTime returns the reference time a block belongs to, given a
// resolution for summaries.
//
// The reference time is the time of the start of a summary bucket/grouping. The
// given resolutions are mapped to the following times:
// SummaryResolutionBlock: The block time itself
// SummaryResolutionHour: The hour the block was mined (minutes/seconds are
// zeroed)
// SummaryResolutionDay: The day the block was mined (hour is zeroed)
// SummaryResolutionMonth: The month the block was mined (day is zeroed)
// SummaryResolutionYear: The year the block was mined (month is zeroed)
// SummaryResolutionAll: The zero (epoch) time is returned
func referenceTime(blockTime time.Time, resolution SummaryResolution) time.Time {
	switch resolution {
	case SummaryResolutionBlock:
		return blockTime
	case SummaryResolutionHour:
		return blockTime.Truncate(time.Hour)
	case SummaryResolutionDay:
		return time.Date(blockTime.Year(),
			blockTime.Month(), blockTime.Day(), 0, 0, 0, 0, blockTime.Location())
	case SummaryResolutionMonth:
		return time.Date(blockTime.Year(),
			blockTime.Month(), 0, 0, 0, 0, 0, blockTime.Location())
	case SummaryResolutionYear:
		return time.Date(blockTime.Year(),
			0, 0, 0, 0, 0, 0, blockTime.Location())
	case SummaryResolutionAll:
		return epochTime
	default:
		panic("Unimplemented resolution in referenceTime(). Please fix this.")
	}
}

type SummaryManager interface {
	getDb() walletdb.DB
	getTxStore() *udb.Store
	getTxMgrNs() []byte
}

type Manager struct {
	db        walletdb.DB
	txStore   *udb.Store
	txMgrNs   []byte
	summaries map[SummaryName]Summary
	params    *chaincfg.Params
}

func NewSummariesManager(db walletdb.DB, txStore *udb.Store, params *chaincfg.Params,
	txMgrNs []byte) *Manager {

	return &Manager{
		db:        db,
		txStore:   txStore,
		txMgrNs:   txMgrNs,
		params:    params,
		summaries: make(map[SummaryName]Summary),
	}
}

func (sm *Manager) getDb() walletdb.DB {
	return sm.db
}
func (sm *Manager) getTxStore() *udb.Store {
	return sm.txStore
}
func (sm *Manager) getTxMgrNs() []byte {
	return sm.txMgrNs
}

func (sm *Manager) Calculate(name SummaryName, beginHeight, endHeight int32,
	resolution SummaryResolution, f SummaryResultFunc) error {

	var sum Summary
	if _, has := sm.summaries[name]; has {
		sum = sm.summaries[name]
	} else {
		switch name {
		case BalancesSummaryName:
			sum = NewBalancesSummary(sm)
		default:
			return fmt.Errorf("Unknown summary: %s", name)
		}
		sm.summaries[name] = sum
	}

	return sum.Calculate(beginHeight, endHeight, resolution, f)
}

const (
	BalanceSeriesSpendable SeriesID = iota
	BalanceSeriesTotal
)

const BalancesSummaryName = "balancesSummary"

type BalancesSummary struct {
	sm SummaryManager
}

func NewBalancesSummary(sm SummaryManager) *BalancesSummary {
	return &BalancesSummary{
		sm: sm,
	}
}

func (bs *BalancesSummary) txBalanceChange(tx udb.TxDetails, res SummaryResult) error {
	// TODO: immatureStakeGeneration, etc
	var spendable dcrutil.Amount
	var total dcrutil.Amount
	var creditSum dcrutil.Amount
	var debitSum dcrutil.Amount

	for _, credit := range tx.Credits {
		creditSum += credit.Amount
	}
	for _, debit := range tx.Debits {
		debitSum += debit.Amount
	}

	switch tx.TxType {
	case stake.TxTypeRegular:
		spendable += creditSum - debitSum
		total += creditSum - debitSum
	case stake.TxTypeSStx:
		spendable -= debitSum
		total -= (debitSum - creditSum) // tx fee
	case stake.TxTypeSSGen:
		spendable += creditSum
		total += creditSum - debitSum // ticket return - stake submission
	case stake.TxTypeSSRtx:
		spendable += creditSum
		total += creditSum - debitSum // stake submission - ticket return
	default:
		return fmt.Errorf("Unknown tx type in includeTx()")
	}

	res[BalanceSeriesSpendable].IntValue += int64(spendable)
	res[BalanceSeriesTotal].IntValue += int64(total)

	return nil
}

func (bs *BalancesSummary) calculate(dbtx walletdb.ReadTx, beginHeight, endHeight int32,
	resolution SummaryResolution, f SummaryResultFunc) error {

	res := SummaryResult{
		BalanceSeriesSpendable: &SummaryResultDataPoint{IntValue: 0},
		BalanceSeriesTotal:     &SummaryResultDataPoint{IntValue: 0},
	}

	lastRefTime := epochTime
	refTime := lastRefTime
	gotData := false

	rangeFn := func(details []udb.TxDetails) (bool, error) {
		if details[0].Block.Height > beginHeight {
			refTime = referenceTime(details[0].Block.Time, resolution)
			gotData = true
		}

		if !refTime.Equal(lastRefTime) && !lastRefTime.Equal(epochTime) {
			brk, err := f(lastRefTime, res)
			if (err != nil) || brk {
				return brk, err
			}
		}
		lastRefTime = refTime

		for _, tx := range details {
			err := bs.txBalanceChange(tx, res)
			if err != nil {
				return true, err
			}
		}

		return false, nil
	}

	ns := dbtx.ReadBucket(bs.sm.getTxMgrNs())
	err := bs.sm.getTxStore().RangeTransactions(ns, 0, endHeight, rangeFn)
	if err != nil {
		return err
	}

	if gotData {
		_, err = f(lastRefTime, res)
	}

	return err
}

func (bs *BalancesSummary) Calculate(beginHeight, endHeight int32,
	resolution SummaryResolution, f SummaryResultFunc) error {

	return walletdb.View(bs.sm.getDb(), func(dbtx walletdb.ReadTx) error {
		return bs.calculate(dbtx, beginHeight, endHeight, resolution, f)
	})
}
