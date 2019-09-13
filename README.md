dcrwallet
=========

dcrwallet is a daemon handling Decred wallet functionality.  All interaction
with the wallet is performed over RPC.

Public and private keys are derived using the hierarchical
deterministic format described by
[BIP0032](https://github.com/bitcoin/bips/blob/master/bip-0032.mediawiki).
Unencrypted private keys are not supported and are never written to
disk.  dcrwallet uses the
`m/44'/<coin type>'/<account>'/<branch>/<address index>`
HD path for all derived addresses, as described by
[BIP0044](https://github.com/bitcoin/bips/blob/master/bip-0044.mediawiki).

Due to the sensitive nature of public data in a BIP0032 wallet,
dcrwallet provides the option of encrypting not just private keys, but
public data as well.  This is intended to thwart privacy risks where a
wallet file is compromised without exposing all current and future
addresses (public keys) managed by the wallet. While access to this
information would not allow an attacker to spend or steal coins, it
does mean they could track all transactions involving your addresses
and therefore know your exact balance.  In a future release, public data
encryption will extend to transactions as well.

dcrwallet provides two modes of operation to connect to the Decred
network.  The first (and default) is to communicate with a single
trusted `dcrd` instance using JSON-RPC.  The second is a
privacy-preserving Simplified Payment Verification (SPV) mode (enabled
with the `--spv` flag) where the wallet connects either to specified
peers (with `--spvconnect`) or peers discovered from seeders and other
peers. Both modes can be switched between with just a restart of the
wallet.  It is advised to avoid SPV mode for heavily-used wallets
which require downloading most blocks regardless.

Not all functionality is available when running in SPV mode.  Some of
these features may become available in future versions, but only if a
consensus vote passes to activate the required changes.  Currently,
the following features are disabled or unavailable to SPV wallets:

  * Voting

  * Revoking tickets before expiry

  * Determining exact number of live and missed tickets (as opposed to
    simply unspent).

Wallet clients interact with the wallet using one of two RPC servers:

  1. A JSON-RPC server inspired by the Bitcoin Core rpc server

     The JSON-RPC server exists to ease the migration of wallet applications
     from Core, but complete compatibility is not guaranteed.  Some portions of
     the API (and especially accounts) have to work differently due to other
     design decisions (mostly due to BIP0044).  However, if you find a
     compatibility issue and feel that it could be reasonably supported, please
     report an issue.  This server is enabled by default as long as a username
     and password are provided.

  2. A gRPC server

     The gRPC server uses a new API built for dcrwallet, but the API is not
     stabilized.  This server is enabled by default and may be disabled with
     the config option `--nogrpc`.  If you don't mind applications breaking
     due to API changes, don't want to deal with issues of the JSON-RPC API, or
     need notifications for changes to the wallet, this is the RPC server to
     use. The gRPC server is documented [here](./rpc/documentation/README.md).

## Installation and updating

### Binaries (Windows/Linux/macOS)

Binary releases are provided for common operating systems and architectures:

https://github.com/decred/decred-binaries/releases

### Build from source (all platforms)

Building or updating from source requires the following build dependencies:

- **Go 1.12 or 1.13**

  Installation instructions can be found here: https://golang.org/doc/install.
  It is recommended to add `$GOPATH/bin` to your `PATH` at this point.

To build and install from a checked-out repo, run `go install` in the repo's
root directory.  Some notes:

* Set the `GO111MODULE=on` environment variable if building from within
  `GOPATH`.

* The `dcrwallet` executable will be installed to `$GOPATH/bin`.  `GOPATH`
  defaults to `$HOME/go` (or `%USERPROFILE%\go` on Windows) if unset.

## Docker

All tests and linters may be run in a docker container using the script
`run_tests.sh`.  This script defaults to using the current supported version of
go.  You can run it with the major version of go you would like to use as the
only argument to test a previous on a previous version of go (generally decred
supports the current version of go and the previous one).

```
./run_tests.sh 1.12
```

To run the tests locally without docker:

```
./run_tests.sh local
```

## Getting Started

The following instructions detail how to get started with dcrwallet connecting
to a localhost dcrd.  Commands should be run in `cmd.exe` or PowerShell on
Windows, or any terminal emulator on *nix.

- Run the following command to start dcrd:

```
dcrd -u rpcuser -P rpcpass
```

- Run the following command to create a wallet:

```
dcrwallet -u rpcuser -P rpcpass --create
```

- Run the following command to start dcrwallet:

```
dcrwallet -u rpcuser -P rpcpass
```

If everything appears to be working, it is recommended at this point to
copy the sample dcrd and dcrwallet configurations and update with your
RPC username and password.

PowerShell (Installed from source):
```
PS> cp $env:GOPATH\src\github.com\decred\dcrd\sample-dcrd.conf $env:LOCALAPPDATA\Dcrd\dcrd.conf
PS> cp $env:GOPATH\src\github.com\decred\dcrwallet\sample-dcrwallet.conf $env:LOCALAPPDATA\Dcrwallet\dcrwallet.conf
PS> $editor $env:LOCALAPPDATA\Dcrd\dcrd.conf
PS> $editor $env:LOCALAPPDATA\Dcrwallet\dcrwallet.conf
```

Linux/BSD/POSIX (Installed from source):
```bash
$ cp $GOPATH/src/github.com/decred/dcrd/sample-dcrd.conf ~/.dcrd/dcrd.conf
$ cp $GOPATH/src/github.com/decred/dcrwallet/sample-dcrwallet.conf ~/.dcrwallet/dcrwallet.conf
$ $EDITOR ~/.dcrd/dcrd.conf
$ $EDITOR ~/.dcrwallet/dcrwallet.conf
```

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrwallet/issues)
is used for this project.

## License

dcrwallet is licensed under the liberal ISC License.
