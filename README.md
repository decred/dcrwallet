# Paymetheus

Paymetheus is a graphical Windows client for
[btcwallet](https://github.com/btcsuite/btcwallet) developed by the btcsuite
project.  Unlike other btcwallet clients, Paymetheus was developed in
conjunction with and uses the new btcwallet gRPC API. It is written using C# and
WPF and targets .NET Framework 4.5.

At the moment, Paymetheus is only usable on the Bitcoin testnet network (version
3).

The program is intended to appear clean, modern, and be easy to use for a user
unfamiliar with the technical details behind bitcoin.  It also attempts to hide
details that can be dangerous if used improperly.  As an example, to avoid
address reuse, addresses managed by the wallet are never displayed except when
generating new addresses to receive a payment.  Instead, the btcwallet BIP0044
account containing the address' corresponding key is displayed when viewing
transaction history.

The btcwallet binary is started in the background by Paymetheus so it is not
required to already have a running btcwallet to connect to.  However, as
btcwallet currently is only usable with btcd RPC (no SPV yet), a testnet3 btcd
server must already be running.  The connection details for this btcd server
will be prompted for when Paymetheus starts.

While the entire program will only run on Windows (WPF is Windows-specific), a
significant portion of the code not dealing with graphical layer (the Bitcoin
and RPC code) is written in portable C# and should be reusable on any operating
system.

Things are just getting started and there is much work left to do.  Check out
the project's Github issue tracker for known issues.

## Building

No official binary release is available due to the project still being
incomplete in many ways.  To build from source:

1. Install the latest btcwallet master branch.

   ```
   PS> go get https://github.com/btcsuite/btcwallet
   ```

   Make sure the installed binary is the first to be found according to your
   PATH environment variable.  Alternatively, this wallet binary can be copied
   to the Debug and/or Release bin Paymetheus directories so it will be used
   instead of another installed version of wallet.

2. Install Visual Studio 2015.  In theory, standalone Roslyn, NuGet, and msbuild
   installs will also be sufficient to build the project, but this is untested.
   The 2015 Community edition of Visual Studio (free to use for open source
   projects) can be obtained [here](https://www.visualstudio.com/en-us/products/visual-studio-community-vs.aspx).

3. Clone the project repo and open the solution file in VS.

4. Click Debug -> Start Debugging (or press F5) to build and execute the
   program.

## Testing

Paymetheus uses the xUnit.net test framework to define and execute test code and
the OpenCover and ReportGenerator tools to report test line coverage.  A
PowerShell script is included to run these tests, but require the
`Paymetheus.Tests.Bitcoin` project to be compiled first.  From VS, using the
Debug (not Release) solution configuration, build this project, and then run the
script.

```
PS> & .\cover.ps1
```

Test coverage can be viewed in a web browser by navigating to
`.\coverage\index.htm`.

## License

Paymetheus is licensed under the liberal ISC License.

## Acknowledgements

This project uses the BLAKE256 hashing functions from Dominik Reichl's
[BlakeSharp](http://www.dominik-reichl.de/projects/blakesharp/).
