module github.com/decred/dcrwallet/wallet

require (
	github.com/boltdb/bolt v1.3.1
	github.com/decred/dcrd/blockchain v1.0.2
	github.com/decred/dcrd/blockchain/stake v1.0.1
	github.com/decred/dcrd/chaincfg v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrec v0.0.0-20180809193022-9536f0c88fa8
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.0
	github.com/decred/dcrd/dcrjson v1.0.0
	github.com/decred/dcrd/dcrutil v1.1.1
	github.com/decred/dcrd/gcs v1.0.2
	github.com/decred/dcrd/hdkeychain v1.1.0
	github.com/decred/dcrd/mempool v1.0.1
	github.com/decred/dcrd/mining v1.0.1 // indirect
	github.com/decred/dcrd/rpcclient v1.0.1
	github.com/decred/dcrd/txscript v1.0.1
	github.com/decred/dcrd/wire v1.1.0
	github.com/decred/dcrwallet/deployments v1.0.0
	github.com/decred/dcrwallet/errors v1.0.0
	github.com/decred/dcrwallet/internal/helpers v1.0.0
	github.com/decred/dcrwallet/internal/zero v1.0.0
	github.com/decred/dcrwallet/validate v1.0.0
	github.com/decred/slog v1.0.0
	github.com/jrick/bitset v1.0.0
	golang.org/x/crypto v0.0.0-20180808211826-de0752318171
	golang.org/x/sync v0.0.0-20180314180146-1d60e4601c6f
)

replace github.com/decred/dcrwallet/errors => ../errors

replace github.com/decred/dcrwallet/deployments => ../deployments

replace github.com/decred/dcrwallet/validate => ../validate

replace github.com/decred/dcrwallet/internal/helpers => ../internal/helpers

replace github.com/decred/dcrwallet/internal/zero => ../internal/zero
