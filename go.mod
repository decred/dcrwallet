module decred.org/dcrwallet

go 1.12

require (
	github.com/decred/dcrd/addrmgr v1.0.2
	github.com/decred/dcrd/blockchain/stake v1.2.1
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2
	github.com/decred/dcrd/blockchain/standalone v1.1.0
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrd/chaincfg v1.5.2
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/connmgr v1.0.2
	github.com/decred/dcrd/connmgr/v2 v2.0.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrjson/v2 v2.2.0
	github.com/decred/dcrd/dcrjson/v3 v3.0.1
	github.com/decred/dcrd/dcrutil v1.4.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/hdkeychain/v2 v2.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.1
	github.com/decred/dcrd/rpcclient/v2 v2.1.0
	github.com/decred/dcrd/txscript v1.1.0
	github.com/decred/dcrd/txscript/v2 v2.1.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrwallet/chain/v3 v3.0.0-00010101000000-000000000000
	github.com/decred/dcrwallet/errors/v2 v2.0.0
	github.com/decred/dcrwallet/p2p/v2 v2.0.0
	github.com/decred/dcrwallet/rpc/client/dcrd v1.0.0
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.3.0
	github.com/decred/dcrwallet/rpc/walletrpc v0.2.0
	github.com/decred/dcrwallet/spv/v3 v3.0.0-00010101000000-000000000000
	github.com/decred/dcrwallet/ticketbuyer/v4 v4.0.0-00010101000000-000000000000
	github.com/decred/dcrwallet/version v1.0.1
	github.com/decred/dcrwallet/wallet/v3 v3.0.0
	github.com/decred/dcrwallet/walletseed v1.0.1
	github.com/decred/go-socks v1.0.1-0.20191001171050-775b73941e73
	github.com/decred/slog v1.0.0
	github.com/gorilla/websocket v1.4.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	google.golang.org/grpc v1.22.0
)

replace (
	github.com/decred/dcrwallet/errors/v2 => ./errors
	github.com/decred/dcrwallet/lru => ./lru
	github.com/decred/dcrwallet/p2p/v2 => ./p2p
	github.com/decred/dcrwallet/pgpwordlist => ./pgpwordlist
	github.com/decred/dcrwallet/rpc/jsonrpc/types => ./rpc/jsonrpc/types
	github.com/decred/dcrwallet/rpc/walletrpc => ./rpc/walletrpc
	github.com/decred/dcrwallet/validate => ./validate
	github.com/decred/dcrwallet/version => ./version
	github.com/decred/dcrwallet/wallet/v3 => ./wallet
	github.com/decred/dcrwallet/walletseed => ./walletseed
)

replace github.com/decred/dcrwallet/spv/v3 => ./spv

replace github.com/decred/dcrwallet/ticketbuyer/v4 => ./ticketbuyer

replace github.com/decred/dcrwallet/chain/v3 => ./chain

replace github.com/decred/dcrwallet/rpc/client/dcrd => ./rpc/client/dcrd

replace github.com/decred/dcrwallet/deployments/v2 => ./deployments
