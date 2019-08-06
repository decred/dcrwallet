module github.com/decred/dcrwallet/spv/v3

require (
	github.com/decred/dcrd/addrmgr v1.0.2
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/gcs v1.1.0
	github.com/decred/dcrd/txscript/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrwallet/errors v1.1.0
	github.com/decred/dcrwallet/lru v1.0.0
	github.com/decred/dcrwallet/p2p/v2 v2.0.0-00010101000000-000000000000
	github.com/decred/dcrwallet/validate v1.0.2
	github.com/decred/dcrwallet/wallet/v3 v3.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
)

replace github.com/decred/dcrwallet/p2p/v2 => ../p2p

replace github.com/decred/dcrwallet/rpc/client => ../rpc/client

replace github.com/decred/dcrwallet/wallet/v3 => ../wallet

replace github.com/decred/dcrwallet/rpc/client/dcrd => ../rpc/client/dcrd

replace github.com/decred/dcrwallet/deployments/v2 => ../deployments
