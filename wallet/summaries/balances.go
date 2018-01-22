package summaries

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/dcrutil"

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
