bdb
===

Package bdb implements an driver for walletdb that uses boltdb for the backing
datastore.  Package bdb is licensed under the copyfree ISC license.

## Usage

This package is only a driver to the walletdb package and provides the database
type of "bdb".  The only parameter the Open and Create functions take is the
database path as a string:

```Go
db, err := walletdb.Open("bdb", "path/to/database.db")
if err != nil {
	// Handle error
}
```

```Go
db, err := walletdb.Create("bdb", "path/to/database.db")
if err != nil {
	// Handle error
}
```

## Documentation

[![GoDoc](https://godoc.org/github.com/decred/dcrwallet/wallet/v2/internal/bdb?status.png)](https://godoc.org/github.com/decred/dcrwallet/wallet/v2/internal/bdb)

Full `go doc` style documentation for the project can be viewed online without
installing this package by using the GoDoc site here:
https://godoc.org/github.com/decred/dcrwallet/wallet/v2/internal/bdb

You can also view the documentation locally once the package is installed with
the `godoc` tool by running `godoc -http=":6060"` and pointing your browser to
http://localhost:6060/pkg/github.com/decred/dcrwallet/wallet/v2/internal/bdb

## License

Package bdb is licensed under the [copyfree](http://copyfree.org) ISC
License.
