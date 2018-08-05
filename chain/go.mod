module github.com/decred/dcrwallet/chain

require (
	github.com/decred/dcrd/chaincfg v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrutil v1.1.1
	github.com/decred/dcrd/gcs v1.0.1
	github.com/decred/dcrd/rpcclient v1.0.1
	github.com/decred/dcrd/wire v1.1.0
	github.com/decred/dcrwallet/errors v1.0.0
	github.com/decred/dcrwallet/wallet v1.0.0
	github.com/decred/slog v1.0.0
	golang.org/x/sync v0.0.0-20180314180146-1d60e4601c6f
)

replace (
	github.com/decred/dcrwallet/deployments => ../deployments
	github.com/decred/dcrwallet/errors => ../errors
	github.com/decred/dcrwallet/internal/helpers => ../internal/helpers
	github.com/decred/dcrwallet/internal/zero => ../internal/zero
	github.com/decred/dcrwallet/validate => ../validate
	github.com/decred/dcrwallet/wallet => ../wallet
)
