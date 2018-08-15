module github.com/decred/dcrwallet

require (
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792
	github.com/decred/dcrd/addrmgr v1.0.2
	github.com/decred/dcrd/blockchain v1.0.1
	github.com/decred/dcrd/blockchain/stake v1.0.1
	github.com/decred/dcrd/certgen v1.0.1
	github.com/decred/dcrd/chaincfg v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/connmgr v1.0.1
	github.com/decred/dcrd/dcrec v0.0.0-20180809193022-9536f0c88fa8
	github.com/decred/dcrd/dcrjson v1.0.0
	github.com/decred/dcrd/dcrutil v1.1.1
	github.com/decred/dcrd/hdkeychain v1.1.0
	github.com/decred/dcrd/rpcclient v1.0.1
	github.com/decred/dcrd/txscript v1.0.1
	github.com/decred/dcrd/wire v1.1.0
	github.com/decred/dcrwallet/chain v1.0.0
	github.com/decred/dcrwallet/errors v1.0.0
	github.com/decred/dcrwallet/internal/helpers v1.0.0
	github.com/decred/dcrwallet/internal/zero v1.0.0
	github.com/decred/dcrwallet/p2p v1.0.0
	github.com/decred/dcrwallet/spv v1.0.0
	github.com/decred/dcrwallet/ticketbuyer v1.0.0
	github.com/decred/dcrwallet/ticketbuyer/v2 v2.0.0
	github.com/decred/dcrwallet/validate v1.0.1 // indirect
	github.com/decred/dcrwallet/version v1.0.0
	github.com/decred/dcrwallet/wallet v1.0.0
	github.com/decred/dcrwallet/walletseed v1.0.0
	github.com/decred/slog v1.0.0
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b // indirect
	github.com/golang/protobuf v1.1.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	golang.org/x/crypto v0.0.0-20180808211826-de0752318171
	golang.org/x/net v0.0.0-20180808004115-f9ce57c11b24
	google.golang.org/genproto v0.0.0-20180808183934-383e8b2c3b9e // indirect
	google.golang.org/grpc v1.14.0
)

replace (
	github.com/decred/dcrwallet/chain => ./chain
	github.com/decred/dcrwallet/deployments => ./deployments
	github.com/decred/dcrwallet/errors => ./errors
	github.com/decred/dcrwallet/internal/helpers => ./internal/helpers
	github.com/decred/dcrwallet/internal/zero => ./internal/zero
	github.com/decred/dcrwallet/lru => ./lru
	github.com/decred/dcrwallet/p2p => ./p2p
	github.com/decred/dcrwallet/pgpwordlist => ./pgpwordlist
	github.com/decred/dcrwallet/spv => ./spv
	github.com/decred/dcrwallet/ticketbuyer => ./ticketbuyer
	github.com/decred/dcrwallet/ticketbuyer/v2 => ./ticketbuyer/v2
	github.com/decred/dcrwallet/validate => ./validate
	github.com/decred/dcrwallet/version => ./version
	github.com/decred/dcrwallet/wallet => ./wallet
	github.com/decred/dcrwallet/walletseed => ./walletseed
)
