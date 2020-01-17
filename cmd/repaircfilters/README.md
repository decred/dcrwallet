repaircfilters
==============

repaircfilters is a tool that can be used to repair a wallet's pre-DCP0005
filters by importing known-good cfilters into the wallet's database.

In order to run it needs to be provided with a binary file filled with the
pre-DCP0005 cfilter data in a specific format: for each block height before the
activation height of DCP0005 in the given network the binary file must have a
record with the lenght of the filter plus the filter itself.

That is:

```
^ len ^      format       ^              field                   ^
|   2 | Big-endian uint16 | Length of the following cfilter data |
|   n | bytes             | Cfilter data for a block             |
```

One to generate a file in that format is this:
https://github.com/matheusd/cfilterv2hashes

## Building

Requires go 1.13.

## Usage

Invoke the tool passing the CLI arguments so that it can connect to the wallet
and import the provided cfilter data.

```
$ go run . -c localhost:19110 -u USER -p PASSWORD --cafile ~/.dcrwallet/rpc.cert
--cfiltersfile testnet-data.bin
```

