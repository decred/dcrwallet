module github.com/decred/dcrwallet/walletseed

require github.com/decred/dcrwallet/errors v1.0.0

require (
	github.com/decred/dcrd/dcrec v0.0.0-20180809193022-9536f0c88fa8 // indirect
	github.com/decred/dcrd/dcrec/edwards v0.0.0-20180809193022-9536f0c88fa8 // indirect
	github.com/decred/dcrd/hdkeychain v1.1.0
	github.com/decred/dcrwallet/pgpwordlist v1.0.0
	golang.org/x/crypto v0.0.0-20180808211826-de0752318171 // indirect
)

replace (
	github.com/decred/dcrwallet/errors v1.0.0 => ../errors
	github.com/decred/dcrwallet/pgpwordlist => ../pgpwordlist
)
