module github.com/decred/dcrwallet/rpc/rpcservertest

require (
	github.com/decred/dcrd/chaincfg v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrjson v1.0.0
	github.com/decred/dcrd/dcrutil v1.1.1
	github.com/decred/dcrd/rpcclient v1.0.2
	github.com/decred/dcrwallet/errors v1.0.0
	github.com/decred/dcrwallet/internal/cfgutil v1.0.0
	github.com/decred/dcrwallet/rpc/walletrpc v0.1.0
	github.com/fsnotify/fsnotify v1.4.7 // indirect
	github.com/hpcloud/tail v1.0.0 // indirect
	github.com/kr/pretty v0.1.0 // indirect
	golang.org/x/net v0.0.0-20181005035420-146acd28ed58
	google.golang.org/grpc v1.15.0
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)

replace github.com/decred/dcrwallet/internal/cfgutil => ../../internal/cfgutil
