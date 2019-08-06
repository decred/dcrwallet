module github.com/decred/dcrwallet/wallet/v3

require (
	github.com/decred/dcrd/blockchain v1.1.1
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.0
	github.com/decred/dcrd/chaincfg v1.5.2
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/chaincfg/v2 v2.1.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.2
	github.com/decred/dcrd/dcrjson/v2 v2.2.0 // indirect
	github.com/decred/dcrd/dcrjson/v3 v3.0.0
	github.com/decred/dcrd/dcrutil v1.4.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/gcs v1.1.0
	github.com/decred/dcrd/hdkeychain/v2 v2.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrd/txscript/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrwallet/deployments/v2 v2.0.0-00010101000000-000000000000
	github.com/decred/dcrwallet/errors v1.1.0
	github.com/decred/dcrwallet/internal/helpers v1.0.1
	github.com/decred/dcrwallet/internal/zero v1.0.1
	github.com/decred/dcrwallet/rpc/client/dcrd v0.0.0-00010101000000-000000000000
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrwallet/validate v1.0.2
	github.com/decred/slog v1.0.0
	go.etcd.io/bbolt v1.3.2
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
)

replace github.com/decred/dcrwallet/rpc/client/dcrd => ../rpc/client/dcrd

replace github.com/decred/dcrwallet/deployments/v2 => ../deployments
