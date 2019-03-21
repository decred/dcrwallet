module github.com/decred/dcrwallet

go 1.11

require (
	github.com/decred/dcrd/addrmgr v1.0.2
	github.com/decred/dcrd/blockchain v1.1.1
	github.com/decred/dcrd/blockchain/stake v1.1.0
	github.com/decred/dcrd/certgen v1.0.2
	github.com/decred/dcrd/chaincfg v1.3.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/connmgr v1.0.2
	github.com/decred/dcrd/dcrec v0.0.0-20190214012338-9265b4051009
	github.com/decred/dcrd/dcrjson/v2 v2.0.0
	github.com/decred/dcrd/dcrutil v1.2.0
	github.com/decred/dcrd/hdkeychain v1.1.1
	github.com/decred/dcrd/rpcclient/v2 v2.0.0
	github.com/decred/dcrd/txscript v1.0.2
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrwallet/chain/v2 v2.0.0
	github.com/decred/dcrwallet/errors v1.0.1
	github.com/decred/dcrwallet/internal/helpers v1.0.1
	github.com/decred/dcrwallet/internal/zero v1.0.1
	github.com/decred/dcrwallet/p2p v1.0.1
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrwallet/rpc/walletrpc v0.2.0
	github.com/decred/dcrwallet/spv/v2 v2.0.0
	github.com/decred/dcrwallet/ticketbuyer/v3 v3.0.0
	github.com/decred/dcrwallet/version v1.0.1
	github.com/decred/dcrwallet/wallet/v2 v2.0.0
	github.com/decred/dcrwallet/walletseed v1.0.1
	github.com/decred/slog v1.0.0
	github.com/gorilla/websocket v1.4.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	golang.org/x/crypto v0.0.0-20190320223903-b7391e95e576
	google.golang.org/grpc v1.18.0
)

replace (
	github.com/decred/dcrwallet/chain/v2 => ./chain
	github.com/decred/dcrwallet/deployments => ./deployments
	github.com/decred/dcrwallet/errors => ./errors
	github.com/decred/dcrwallet/internal/helpers => ./internal/helpers
	github.com/decred/dcrwallet/internal/zero => ./internal/zero
	github.com/decred/dcrwallet/lru => ./lru
	github.com/decred/dcrwallet/p2p => ./p2p
	github.com/decred/dcrwallet/pgpwordlist => ./pgpwordlist
	github.com/decred/dcrwallet/rpc/jsonrpc/types => ./rpc/jsonrpc/types
	github.com/decred/dcrwallet/rpc/walletrpc => ./rpc/walletrpc
	github.com/decred/dcrwallet/spv/v2 => ./spv
	github.com/decred/dcrwallet/ticketbuyer/v3 => ./ticketbuyer
	github.com/decred/dcrwallet/validate => ./validate
	github.com/decred/dcrwallet/version => ./version
	github.com/decred/dcrwallet/wallet/v2 => ./wallet
	github.com/decred/dcrwallet/walletseed => ./walletseed
)
