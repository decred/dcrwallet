treasurykey
===========

THIS TOOL IS FOR NETWORK OPERATORS ONLY.

**DO NOT USE**

The `treasurykey` utility is used to generate treasury keys that enable
spending from the treasury account.

The tool takes a single flag to designate which network it is generating a key
for.

It prints out three values:
1. A serialized private key
2. The serialized public key
3. The WIF (Wallet Interchange Format) version of the private key.

This information must be stored in a secure location.

The public key will be hardcoded inside `dcrd` and `dcrwallet`. Both daemons
will use it to identify valid treasury spend transactions.

The WIF must be imported into the dcrwallet that will generate TSPEND
transactions.

For example:

Generate key.
```
$ treasurykey -mainnet  
Private key: 9bcf82e4b267585b3f0c54632d7e8eef2fc13407dfdbf7a80398085f5851baa0
Public  key: 03f31aa7013b3c3e8568eb6415a895d8662b5ddceff12a62bf77c01e3e451a332f
WIF        : PmQeT3VmoxDRjgsfwVJLV13ioNyxmd9XuQmQu86UPiQQAn5XkRfLN
```

Import key into treasury wallet.
```
dcrctl --wallet importprivkey PmQeT3VmoxDRjgsfwVJLV13ioNyxmd9XuQmQu86UPiQQAn5XkRfLN imported false
```

It is suggested to repeat this process so that there are at least two valid
keys.

Why?
====

This approach was chosen in order to generate *independent* random keys. If the
treasury wallet machine were to be compromised the seed would be compromised as
well.
