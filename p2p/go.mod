module github.com/decred/dcrwallet/p2p

require (
	github.com/decred/dcrd/addrmgr v1.0.2
	github.com/decred/dcrd/chaincfg v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/connmgr v1.0.1
	github.com/decred/dcrd/dcrec v0.0.0-20180809193022-9536f0c88fa8 // indirect
	github.com/decred/dcrd/dcrec/edwards v0.0.0-20180809193022-9536f0c88fa8 // indirect
	github.com/decred/dcrd/gcs v1.0.1
	github.com/decred/dcrd/wire v1.1.0
	github.com/decred/dcrwallet/errors v1.0.0
	github.com/decred/dcrwallet/lru v1.0.0
	github.com/decred/dcrwallet/version v1.0.0
	github.com/decred/slog v1.0.0
	github.com/fsnotify/fsnotify v1.4.7 // indirect
	github.com/golang/protobuf v1.1.0 // indirect
	github.com/hpcloud/tail v1.0.0 // indirect
	golang.org/x/crypto v0.0.0-20180808211826-de0752318171 // indirect
	golang.org/x/sync v0.0.0-20180314180146-1d60e4601c6f
	golang.org/x/sys v0.0.0-20180810070207-f0d5e33068cb // indirect
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)

replace (
	github.com/decred/dcrwallet/errors => ../errors
	github.com/decred/dcrwallet/lru => ../lru
	github.com/decred/dcrwallet/version => ../version
)
