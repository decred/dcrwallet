module github.com/decred/dcrwallet/wallet/v3

go 1.12

require (
	decred.org/cspp v0.2.0
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2
	github.com/decred/dcrd/blockchain/standalone v1.1.0
	github.com/decred/dcrd/blockchain/v2 v2.1.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/gcs v1.1.0
	github.com/decred/dcrd/hdkeychain/v2 v2.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.1
	github.com/decred/dcrd/txscript/v2 v2.1.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrwallet/deployments/v2 v2.0.0
	github.com/decred/dcrwallet/errors/v2 v2.0.0
	github.com/decred/dcrwallet/rpc/client/dcrd v1.0.0
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.3.0
	github.com/decred/dcrwallet/validate v1.1.1
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.0.0
	github.com/onsi/ginkgo v1.7.0 // indirect
	github.com/onsi/gomega v1.4.3 // indirect
	go.etcd.io/bbolt v1.3.3
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	gopkg.in/yaml.v2 v2.2.2 // indirect
)
