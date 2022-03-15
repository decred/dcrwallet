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

## Installing and updating

### Binaries (Windows/Linux/macOS)

Binary releases are provided for common operating systems and architectures.
Please note that dcrwallet is CLI only. It is included in the
[CLI app suite](https://github.com/decred/decred-release/releases/latest).
If you would prefer a graphical user interface (GUI) instead, consider
downloading the GUI wallet [Decrediton](https://github.com/decred/decrediton).

https://decred.org/downloads/

* How to verify binaries before installing: https://docs.decred.org/advanced/verifying-binaries/
* How to install the CLI Suite: https://docs.decred.org/wallets/cli/cli-installation/
* How to install Decrediton: https://docs.decred.org/wallets/decrediton/decrediton-setup/

### Build from source (all platforms)

- **Install Go 1.17 or 1.18**

  Installation instructions can be found here: https://golang.org/doc/install.
  Ensure Go was installed properly and is a supported version:
  ```sh
  $ go version
  $ go env GOROOT GOPATH
  ```
  NOTE: `GOROOT` and `GOPATH` must not be on the same path. It is recommended
  to add `$GOPATH/bin` to your `PATH` according to the Golang.org instructions.

- **Build or Update dcrwallet**

  Since dcrwallet is a single Go module, it's possible to use a single command
  to download, build, and install without needing to clone the repo. Run:

  ```sh
  $ go install decred.org/dcrwallet/v2@master
  ```

  to build the latest master branch, or:

  ```sh
  $ go install decred.org/dcrwallet@latest
  ```

  for the latest released version.

  Any version, branch, or tag may be appended following a `@` character after
  the package name.  The implicit default is to build `@latest`, which is the
  latest semantic version tag.  Building `@master` will build the latest
  development version.  The module name, including any `/vN` suffix, must match
  the `module` line in the `go.mod` at that version.  See `go help install`
  for more details.

  The `dcrwallet` executable will be installed to `$GOPATH/bin`.  `GOPATH`
  defaults to `$HOME/go` (or `%USERPROFILE%\go` on Windows).

## Getting Started

dcrwallet can connect to the Decred blockchain using either [dcrd](https://github.com/decred/dcrd)
or by running in [Simple Payment Verification (SPV)](https://docs.decred.org/wallets/spv/)
mode. Commands should be run in `cmd.exe` or PowerShell on Windows, or any
terminal emulator on *nix.

- Run the following command to create a wallet:

```sh
dcrwallet --create
```

- To use dcrwallet in SPV mode:

```sh
dcrwallet --spv
```

dcrwallet will find external full node peers. It will take a few minutes to
download the blockchain headers and filters, but it will not download full blocks.

- To use dcrwallet using a localhost dcrd:

You will need to install both [dcrd](https://github.com/decred/dcrd) and
[dcrctl](https://github.com/decred/dcrctl). `dcrctl` is the client that controls
`dcrd` and `dcrwallet` via remote procedure call (RPC).

Please follow the instructions in the documentation, beginning with
[Startup Basics](https://docs.decred.org/wallets/cli/startup-basics/)

## Running Tests

All tests may be run using the script `run_tests.sh`. Generally, Decred only
supports the current and previous major versions of Go.

```sh
./run_tests.sh
```

## Contact

If you have any further questions you can find us at:

https://decred.org/community/

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrwallet/issues)
is used for this project.

## Documentation

The documentation for dcrwallet is a work-in-progress.  It is located in the
[docs](https://github.com/decred/dcrwallet/tree/master/docs) folder.

Additional documentation can be found on
[docs.decred.org](https://docs.decred.org/wallets/cli/dcrwallet-setup/).

## License

dcrwallet is licensed under the liberal ISC License.
