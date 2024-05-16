// Copyright (c) 2017-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/v4/errors"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v4"
	"github.com/decred/dcrd/mixing"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

// FilterProof specifies cfilterv2 data of an individual block during a
// Peer.CFiltersV2 call.
//
// Note: This is a type alias of an anonymous struct rather than a regular
// struct due to the packages that fulfill the Peer interface having a
// dependency graph (spv -> wallet -> rpc/client/dcrd) that prevents directly
// returning a struct.
type FilterProof = struct {
	Filter     *gcs.FilterV2
	ProofIndex uint32
	Proof      []chainhash.Hash
}

// NetworkBackend provides wallets with Decred network functionality.  Some
// wallet operations require the wallet to be associated with a network backend
// to complete.
type NetworkBackend interface {
	Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error)
	CFiltersV2(ctx context.Context, blockHashes []*chainhash.Hash) ([]FilterProof, error)
	PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error
	PublishMixMessages(ctx context.Context, msgs ...mixing.Message) error
	LoadTxFilter(ctx context.Context, reload bool, addrs []stdaddr.Address, outpoints []wire.OutPoint) error
	Rescan(ctx context.Context, blocks []chainhash.Hash, save func(block *chainhash.Hash, txs []*wire.MsgTx) error) error

	// This is impossible to determine over the wire protocol, and will always
	// error.  Use Wallet.NextStakeDifficulty to calculate the next ticket price
	// when the DCP0001 deployment is known to be active.
	StakeDifficulty(ctx context.Context) (dcrutil.Amount, error)

	// Synced returns whether the backend considers that it has synced
	// the wallet to the underlying network, and if not, it returns the
	// target height that it is attempting to sync to.
	Synced(ctx context.Context) (bool, int32)
}

// NetworkBackend returns the currently associated network backend of the
// wallet, or an error if the no backend is currently set.
func (w *Wallet) NetworkBackend() (NetworkBackend, error) {
	const op errors.Op = "wallet.NetworkBackend"

	w.networkBackendMu.Lock()
	n := w.networkBackend
	w.networkBackendMu.Unlock()
	if n == nil {
		return nil, errors.E(op, errors.NoPeers)
	}
	return n, nil
}

// SetNetworkBackend sets the network backend used by various functions of the
// wallet.
func (w *Wallet) SetNetworkBackend(n NetworkBackend) {
	w.networkBackendMu.Lock()
	w.networkBackend = n
	w.networkBackendMu.Unlock()
}

// Caller provides a client interface to perform remote procedure calls.
// Serialization and calling conventions are implementation-specific.
type Caller interface {
	// Call performs the remote procedure call defined by method and
	// waits for a response or a broken client connection.
	// Args provides positional parameters for the call.
	// Res must be a pointer to a struct, slice, or map type to unmarshal
	// a result (if any), or nil if no result is needed.
	Call(ctx context.Context, method string, res any, args ...any) error
}

var errOfflineNetworkBackend = errors.New("operation not supported in offline mode")

// OfflineNetworkBackend is a NetworkBackend that fails every call. It is meant
// to be used in wallets which will only perform local operations.
type OfflineNetworkBackend struct{}

func (o OfflineNetworkBackend) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	return nil, errOfflineNetworkBackend
}

func (o OfflineNetworkBackend) CFiltersV2(ctx context.Context, blockHashes []*chainhash.Hash) ([]FilterProof, error) {
	return nil, errOfflineNetworkBackend
}

func (o OfflineNetworkBackend) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	return errOfflineNetworkBackend
}

func (o OfflineNetworkBackend) PublishMixMessages(ctx context.Context, msgs ...mixing.Message) error {
	return errOfflineNetworkBackend
}

func (o OfflineNetworkBackend) LoadTxFilter(ctx context.Context, reload bool, addrs []stdaddr.Address, outpoints []wire.OutPoint) error {
	return errOfflineNetworkBackend
}

func (o OfflineNetworkBackend) Rescan(ctx context.Context, blocks []chainhash.Hash, save func(block *chainhash.Hash, txs []*wire.MsgTx) error) error {
	return errOfflineNetworkBackend
}

func (o OfflineNetworkBackend) StakeDifficulty(ctx context.Context) (dcrutil.Amount, error) {
	return 0, errOfflineNetworkBackend
}

func (o OfflineNetworkBackend) Synced(ctx context.Context) (bool, int32) {
	return true, 0
}

// Compile time check to ensure OfflineNetworkBackend fulfills the
// NetworkBackend interface.
var _ NetworkBackend = OfflineNetworkBackend{}
