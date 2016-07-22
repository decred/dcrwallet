# Paymetheus

Paymetheus is a Decred wallet for Windows, written with C# and WPF, and
utilizing [dcrwallet](https://github.com/decred/dcrwallet) for transaction and
key management.  It targets .NET Framework 4.5 and has been tested on Windows 7
and 10.

The program is intended to appear clean, modern, and be easy to use for a user
unfamiliar with the technical details behind Decred or other cryptocurrencies.
It also attempts to hide details that can be dangerous if used improperly.  As
an example, to avoid address reuse, addresses managed by the wallet are never
displayed except when generating new addresses to receive a payment.  Instead,
the account name containing the address' corresponding key is displayed when
viewing transaction history.

Paymetheus runs dcrwallet as a background process and connects to it using gRPC.
Currently, dcrwallet is only usable with a dcrd RPC connection (dcrwallet does
not have any SPV support) so the user must already have a dcrd server running to
provide blockchain services.  The connection details for this dcrd server will
be prompted for when Paymetheus starts.

While the entire program will only run on Windows (WPF is Windows-specific), a
significant portion of the code not dealing with graphical layer (the Decred and
RPC code) is written in portable C# and should be reusable on other operating
systems running under either Mono or .NET Core.

Things are just getting started and there is much work left to do.  Check out
the project's Github issue tracker for known issues.

## Binary Installation and Usage

Paymetheus is available as a component in the Decred Windows installer.  The
latest installer can be obtained
[here](https://github.com/decred/decred-release/releases).

For instructions on how to use dcrd and Paymetheus after installation, see the
[quick start guide](./QUICKSTART.md).

## Building from Source

To build the development version from source:

1. Install the latest dcrwallet master branch (requires git, Go, and glide):

   ```
   PS> $dcrwallet = "$env:GOPATH/src/github.com/decred/dcrwallet"
   PS> mkdir $dcrwallet
   PS> git clone https://github.com/decred/dcrwallet $dcrwallet
   PS> cd $dcrwallet && glide install && go install
   ```

   Make sure the installed binary is the first to be found according to your
   `PATH` environment variable.  Alternatively, this wallet binary can be copied
   to the Debug and/or Release bin Paymetheus directories so it will be used
   instead of another installed version of wallet.

2. Install Visual Studio 2015.  In theory, standalone Roslyn, NuGet, and msbuild
   installs will also be sufficient to build the project, but this is untested.
   The 2015 Community edition of Visual Studio (free to use for open source
   projects) can be obtained [here](https://www.visualstudio.com/en-us/products/visual-studio-community-vs.aspx).

3. Clone the project repo and open the solution file in VS.

4. Open the project properties (Project -> Paymetheus Properties...), click
   Debug, and add the following command line parameters:

   `-testnet -searchpath`

   This will instruct the program to run in testnet mode rather than mainnet,
   and will search `PATH` for the dcrwallet executable instead of requiring it
   to be installed in Program Files.

5. Click Debug -> Start Debugging (or press F5) to build and execute the
   program.

## Testing

Paymetheus uses the xUnit.net test framework to define and execute test code and
the OpenCover and ReportGenerator tools to report test line coverage.  A
PowerShell script is included to run these tests, but require the
`Paymetheus.Tests.Decred` project to be compiled first.  From VS, using the
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
