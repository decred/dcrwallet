# Quick Start Guide

Paymetheus is alpha software and the Decred developers recognize that using it
with `dcrd` on Windows is not as intuitive as it could be.  This guide exists to
explain how to correctly use Paymetheus and `dcrd` from the 0.2.0-alpha release
on a single computer, as the same user.  Our goal is to improve the software in
later versions to make the instructions in this guide obsolete.

## Starting `dcrd`

After installation, a shortcut or tile is placed in the Windows start menu for
`dcrd`.  `dcrd` is a Decred full node and Paymetheus requires it to be running
in order to send and receive transactions on the Decred network.  Open `dcrd`
first and wait until the program has finished syncing the blockchain.  `dcrd`
will not tell you when this process has completed, but you will see the latest
processed block times approach the current date.  The latest blocks can also be
viewed on the web at https://mainnet.decred.org/.

## Starting Paymetheus

Once `dcrd` is running, Paymetheus can be started.  Open Paymetheus from the
start menu.

The first thing you should see after opening Paymetheus is a dialog for `dcrd`
connection information.  Since `dcrd` was run first, it automatically generated
random connection information for local connections.  This dialog has been
automatically filled in for you with this information.  Click the continue
button to proceed to create a wallet.

While there is nothing wrong with starting Paymetheus before `dcrd` is ever run,
doing so would require manually finding and entering RPC connection information.
Opening `dcrd` first simply streamlines the process.

## Shutting down `dcrd`

Extra care must be taken to close `dcrd` in order to avoid excessive startup
times to resync the ticket database the next time the program is started.

The following actions can result in unclean shutdown and should be avoided:

* Clicking the X to close the `dcrd` console window
* Logging out or shutting down without stopping `dcrd` first
* Terminating the `dcrd` background process from the Task Manager

Instead, to cleanly shutdown `dcrd`, press Ctrl-C in the `dcrd` console window.

After `dcrd` is closed, Paymetheus will no longer be able to send or receive
Decred transactions, and it should be closed as well.  The order in which `dcrd`
and Paymetheus are closed does not matter.
