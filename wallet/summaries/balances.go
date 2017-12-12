package summaries

import (
	"fmt"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/blockchain/stake"
	"time"

	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

const (
	BalanceSeriesSpendable = iota
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

func (bs *BalancesSummary) previousBalance(beginHeight int32) (SummaryResult, error) {

	res := SummaryResult{
		BalanceSeriesSpendable: &SummaryResultDataPoint{},
		BalanceSeriesTotal: &SummaryResultDataPoint{},
	}

	if beginHeight < 1 {
		return res, nil
	}

	f := func (ts time.Time, dps SummaryResult) (bool, error) {
		res = SummaryResult{
			BalanceSeriesSpendable: &SummaryResultDataPoint{IntValue: dps[BalanceSeriesSpendable].IntValue},
			BalanceSeriesTotal: &SummaryResultDataPoint{IntValue: dps[BalanceSeriesTotal].IntValue},
		}
		return false, nil
	}

	err := bs.Calculate(0, beginHeight-1, SummaryResolutionAll, f)

	return res, err
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

	res[BalanceSeriesSpendable].IntValue = int64(spendable)
	res[BalanceSeriesTotal].IntValue = int64(total)

	return nil
}

func (bs *BalancesSummary) Calculate(beginHeight, endHeight int32,
	resolution SummaryResolution, f SummaryResultFunc) error {

	dps, err := bs.previousBalance(beginHeight)
	if err != nil {
		return err
	}
	txChange := SummaryResult{
		BalanceSeriesSpendable: &SummaryResultDataPoint{IntValue: 0},
		BalanceSeriesTotal: &SummaryResultDataPoint{IntValue: 0},
	}

	lastRefTime := epochTime

	return walletdb.View(bs.sm.getDb(), func (dbtx walletdb.ReadTx) error {
		rangeFn := func (details []udb.TxDetails) (bool, error) {
			refTime := referenceTime(details[0].Block.Time, resolution)
			if (!refTime.Equal(lastRefTime))  {
				if (!lastRefTime.Equal(epochTime)) {
					brk, err := f(lastRefTime, dps)
					if (err != nil) || brk {
						return brk, err
					}
				}
				lastRefTime = refTime
			}

			for _, tx := range details {
				err := bs.txBalanceChange(tx, txChange)
				if err != nil {
					return true, err
				}

				// FIXME: remove (here just to help out during development)
				// fmt.Printf("%s %14.8f %14.8f  change \n", refTime.Format(time.RFC3339),
				// 	float64(txChange[BalanceSeriesSpendable].IntValue) / 10e7,
				// 	float64(txChange[BalanceSeriesTotal].IntValue) / 10e7)

				dps[BalanceSeriesSpendable].IntValue += int64(txChange[BalanceSeriesSpendable].IntValue)
				dps[BalanceSeriesTotal].IntValue += int64(txChange[BalanceSeriesTotal].IntValue)
			}

			return false, nil
		}

		ns := dbtx.ReadBucket(bs.sm.getTxMgrNs())
		err := bs.sm.getTxStore().RangeTransactions(ns, beginHeight, endHeight, rangeFn)
		if err != nil {
			return err
		}

		if (lastRefTime != epochTime) && (resolution != SummaryResolutionAll) {
			_, err = f(lastRefTime, dps)
		}

		return err
	})
}
