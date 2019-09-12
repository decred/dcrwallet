module github.com/decred/dcrwallet/walletseed

go 1.12

require (
	github.com/decred/dcrd/hdkeychain/v2 v2.0.1
	github.com/decred/dcrwallet/errors/v2 v2.0.0-00010101000000-000000000000
	github.com/decred/dcrwallet/pgpwordlist v1.0.0
)

replace github.com/decred/dcrwallet/errors/v2 => ../errors
