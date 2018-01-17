package summaries

import (
	"fmt"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

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

func NewManager(db walletdb.DB, txStore *udb.Store, params *chaincfg.Params,
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
