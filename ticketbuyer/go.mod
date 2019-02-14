module github.com/decred/dcrwallet/ticketbuyer/v3

require (
	github.com/decred/dcrd/chaincfg/chainhash v1.0.1
	github.com/decred/dcrd/dcrutil v1.2.0
	github.com/decred/dcrwallet/errors v1.0.1
	github.com/decred/dcrwallet/wallet/v2 v2.0.0
	github.com/decred/slog v1.0.0
)

replace github.com/decred/dcrwallet/wallet/v2 => ../wallet
