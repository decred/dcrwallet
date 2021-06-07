// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2015-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build !generate

package rpchelp

var helpDescsEnUS = map[string]string{
	// AbandonTransactionCmd help.
	"abandontransaction--synopsis": "Remove an unconfirmed transaction and all dependent transactions",
	"abandontransaction-hash":      "Hash of transaction to remove",

	// AccountAddressIndexCmd help.
	"accountaddressindex--synopsis": "Get the current address index for some account branch",
	"accountaddressindex-account":   "String for the account",
	"accountaddressindex-branch":    "Number for the branch (0=external, 1=internal)",
	"accountaddressindex--result0":  "The address index for this account branch",

	// AccountSyncAddressIndexCmd help.
	"accountsyncaddressindex--synopsis": "Synchronize an account branch to some passed address index",
	"accountsyncaddressindex-account":   "String for the account",
	"accountsyncaddressindex-branch":    "Number for the branch (0=external, 1=internal)",
	"accountsyncaddressindex-index":     "The address index to synchronize to",

	// AccountUnlockedCmd help.
	"accountunlocked--synopsis": "Report account encryption and locked status",
	"accountunlocked-account":   "Account name",

	// AccountUnlockedResult help.
	"accountunlockedresult-encrypted": "Whether the account is individually encrypted with a separate passphrase",
	"accountunlockedresult-unlocked":  "If the individually encrypted account is unlocked. Omitted for unencrypted accounts.",

	// AddMultisigAddressCmd help.
	"addmultisigaddress--synopsis": "Generates and imports a multisig address and redeeming script to the 'imported' account.",
	"addmultisigaddress-account":   "DEPRECATED -- Unused (all imported addresses belong to the imported account)",
	"addmultisigaddress-keys":      "Pubkeys and/or pay-to-pubkey-hash addresses to partially control the multisig address",
	"addmultisigaddress-nrequired": "The number of signatures required to redeem outputs paid to this address",
	"addmultisigaddress--result0":  "The imported pay-to-script-hash address",

	// AddTransactionCmd help.
	"addtransaction--synopsis":   "Manually record a transaction mined in a main chain block",
	"addtransaction-blockhash":   "Hash of block which mines transaction",
	"addtransaction-transaction": "Hex-encoded serialized transaction",

	// AuditReuseCmd help.
	"auditreuse--synopsis":       "Reports outputs identifying address reuse",
	"auditreuse-since":           "Only report reusage since some main chain block height",
	"auditreuse--result0--desc":  "Object keying reused addresses to arrays of outpoint strings",
	"auditreuse--result0--value": "Reused address",
	"auditreuse--result0--key":   "Array of outpoints referencing the reused address",

	// ConsolidateCmd help.
	"consolidate--synopsis": "Consolidate n many UTXOs into a single output in the wallet.",
	"consolidate-inputs":    "Number of UTXOs to consolidate as inputs",
	"consolidate-account":   "Optional: Account from which unspent outputs are picked. When no address specified, also the account used to obtain an output address.",
	"consolidate-address":   "Optional: Address to pay.  Default is obtained via getnewaddress from the account's address pool.",
	"consolidate--result0":  "Transaction hash for the consolidation transaction",

	// CreateMultisigCmd help.
	"createmultisig--synopsis": "Generate a multisig address and redeem script.",
	"createmultisig-keys":      "Pubkeys and/or pay-to-pubkey-hash addresses to partially control the multisig address",
	"createmultisig-nrequired": "The number of signatures required to redeem outputs paid to this address",

	// CreateMultisigResult help.
	"createmultisigresult-address":      "The generated pay-to-script-hash address",
	"createmultisigresult-redeemScript": "The script required to redeem outputs paid to the multisig address",

	// CreateMultisigResult help.
	"createsignatureresult-signature": "The hex encoded signature.",
	"createsignatureresult-publickey": "The hex encoded serialized compressed pubkey of the address.",

	// CreateNewAccountCmd help.
	"createnewaccount--synopsis": "Creates a new account.\n" +
		"The wallet must be unlocked for this request to succeed.",
	"createnewaccount-account": "Name of the new account",

	// CreateRawTransactionCmd help.
	"createrawtransaction--synopsis": "Returns a new transaction spending the provided inputs and sending to the provided addresses.\n" +
		"The transaction inputs are not signed in the created transaction.\n" +
		"The signrawtransaction RPC command provided by wallet must be used to sign the resulting transaction.",
	"createrawtransaction-inputs":         "The inputs to the transaction",
	"createrawtransaction-amounts":        "JSON object with the destination addresses as keys and amounts as values",
	"createrawtransaction-amounts--key":   "address",
	"createrawtransaction-amounts--value": "n.nnn",
	"createrawtransaction-amounts--desc":  "The destination address as the key and the amount in DCR as the value",
	"createrawtransaction-locktime":       "Locktime value; a non-zero value will also locktime-activate the inputs",
	"createrawtransaction-expiry":         "Expiry value; a non-zero value when the transaction expiry",
	"createrawtransaction--result0":       "Hex-encoded bytes of the serialized transaction",

	// CreateSignatureCmd help.
	"createsignature--synopsis":             "Generate a signature for a transaction input script.",
	"createsignature-address":               "The address of the private key to use to create the signature.",
	"createsignature-serializedtransaction": "The hex encoded transaction to add input signatures to.",
	"createsignature-inputindex":            "The index of the transaction input to sign.",
	"createsignature-hashtype":              "The signature hash flags to use.",
	"createsignature-previouspkscript":      "The the hex encoded previous output script or P2SH redeem script.",

	// DisapprovePercentCmd help.
	"disapprovepercent--synopsis": "Returns the wallet's current block disapprove percent per vote. i.e. 100 means that all votes disapprove the block they are called on. Only used for testing purposes.",
	"disapprovepercent--result0":  "The disapprove percent. When voting, this percent of votes will randomly disapprove the block they are called on.",

	// DiscoverUsageCmd help.
	"discoverusage--synopsis":        "Perform address and/or account discovery",
	"discoverusage-startblock":       "Hash of block to begin discovery from, or null to scan from the genesis block",
	"discoverusage-discoveraccounts": "Perform account discovery in addition to address discovery.  Requires unlocked wallet.",
	"discoverusage-gaplimit":         "Allowed unused address gap.",

	// DumpPrivKeyCmd help.
	"dumpprivkey--synopsis": "Returns the private key in WIF encoding that controls some wallet address.",
	"dumpprivkey-address":   "The address to return a private key for",
	"dumpprivkey--result0":  "The WIF-encoded private key",

	// FundRawTransactionCmd help.
	"fundrawtransaction--synopsis":            "Adds unsigned inputs and change output to a raw transaction",
	"fundrawtransaction-hexstring":            "Serialized transaction in hex encoding",
	"fundrawtransaction-fundaccount":          "Account of outputs to spend in transaction",
	"fundrawtransaction-options":              "Object to specify fixed change address, alternative fee rate, and confirmation target",
	"fundrawtransactionoptions-conf_target":   "Required confirmations of selected previous outputs",
	"fundrawtransactionoptions-feerate":       "Alternative fee rate",
	"fundrawtransactionoptions-changeaddress": "Provide a change address rather than deriving one from the funding account",
	"fundrawtransactionresult-hex":            "Funded transaction in hex encoding",
	"fundrawtransactionresult-fee":            "Absolute fee of funded transaction",

	// GenerateVote help.
	"generatevote--synopsis":   "Returns the vote transaction encoded as a hexadecimal string",
	"generatevote-blockhash":   "Block hash for the ticket",
	"generatevote-height":      "Block height for the ticket",
	"generatevote-tickethash":  "The hash of the ticket",
	"generatevote-votebits":    "The voteBits to set for the ticket",
	"generatevote-votebitsext": "The extended voteBits to set for the ticket",
	"generatevoteresult-hex":   "The hex encoded transaction",

	// GetAccountAddressCmd help.
	"getaccountaddress--synopsis": "DEPRECATED -- Returns the most recent external payment address for an account that has not been seen publicly.\n" +
		"A new address is generated for the account if the most recently generated address has been seen on the blockchain or in mempool.",
	"getaccountaddress-account":  "The account of the returned address",
	"getaccountaddress--result0": "The unused address for 'account'",

	// GetAccountCmd help.
	"getaccount--synopsis": "Lookup the account name that some wallet address belongs to.",
	"getaccount-address":   "The address to query the account for",
	"getaccount--result0":  "The name of the account that 'address' belongs to",

	// GetAddressesByAccountCmd help.
	"getaddressesbyaccount--synopsis": "DEPRECATED -- Returns all addresses strings controlled by a single account.",
	"getaddressesbyaccount-account":   "Account name to fetch addresses for",
	"getaddressesbyaccount--result0":  "All addresses controlled by 'account'",

	// GetBalanceCmd help.
	"getbalance--synopsis": "Calculates and returns the balance of all accounts.",
	"getbalance-minconf":   "Minimum number of block confirmations required before an unspent output's value is included in the balance",
	"getbalance-account":   "The account name to query the balance for, or \"*\" to consider all accounts (default=\"*\")",

	"getbalanceresult-balances":                       "Balances for all accounts.",
	"getaccountbalanceresult-accountname":             "Name of account.",
	"getaccountbalanceresult-immaturecoinbaserewards": "Immature Coinbase reward coins.",
	"getaccountbalanceresult-immaturestakegeneration": "Number of immature stake coins.",
	"getaccountbalanceresult-lockedbytickets":         "Coins locked by tickets.",
	"getaccountbalanceresult-spendable":               "Spendable number of coins.",
	"getaccountbalanceresult-total":                   "Total amount of coins.",
	"getaccountbalanceresult-unconfirmed":             "Unconfirmed number of coins.",
	"getaccountbalanceresult-votingauthority":         "Coins for voting authority.",
	"getbalanceresult-blockhash":                      "Block hash.",
	"getbalanceresult-totalimmaturecoinbaserewards":   "Total number of immature coinbase reward coins.",
	"getbalanceresult-totalimmaturestakegeneration":   "Total number of immature stake coins.",
	"getbalanceresult-totallockedbytickets":           "Total number of coins locked by tickets.",
	"getbalanceresult-totalspendable":                 "Total number of spendable number of coins.",
	"getbalanceresult-cumulativetotal":                "Total number of coins.",
	"getbalanceresult-totalunconfirmed":               "Total number of unconfirmed coins.",
	"getbalanceresult-totalvotingauthority":           "Total number of coins for voting authority.",

	// GetBalanceToMaintainCmd help.
	"getbalancetomaintain--synopsis": "Get the current balance to maintain",
	"getbalancetomaintain--result0":  "The current balancetomaintain",

	// GetBestBlockCmd help.
	"getbestblock--synopsis": "Returns the hash and height of the newest block in the best chain that wallet has finished syncing with.",

	// GetBestBlockHashCmd help.
	"getbestblockhash--synopsis": "Returns the hash of the newest block in the best chain that wallet has finished syncing with.",
	"getbestblockhash--result0":  "The hash of the most recent synced-to block",

	// GetBestBlockResult help.
	"getbestblockresult-hash":   "The hash of the block",
	"getbestblockresult-height": "The blockchain height of the block",

	// GetBlockCountCmd help.
	"getblockcount--synopsis": "Returns the blockchain height of the newest block in the best chain that wallet has finished syncing with.",
	"getblockcount--result0":  "The blockchain height of the most recent synced-to block",

	// GetBlockHashCmd help.
	"getblockhash--synopsis": "Returns the hash of a main chain block at some height",
	"getblockhash-index":     "The block height",
	"getblockhash--result0":  "The main chain block hash",

	// GetBlockCmd help.
	"getblock--synopsis":   "Returns information about a block given its hash.",
	"getblock-hash":        "The hash of the block",
	"getblock-verbose":     "Specifies the block is returned as a JSON object instead of hex-encoded string",
	"getblock-verbosetx":   "Specifies that each transaction is returned as a JSON object and only applies if the verbose flag is true (dcrd extension)",
	"getblock--condition0": "verbose=false",
	"getblock--condition1": "verbose=true",
	"getblock--result0":    "Hex-encoded bytes of the serialized block",

	// GetBlockVerboseResult help.
	"getblockverboseresult-hash":              "The hash of the block (same as provided)",
	"getblockverboseresult-confirmations":     "The number of confirmations",
	"getblockverboseresult-size":              "The size of the block",
	"getblockverboseresult-height":            "The height of the block in the block chain",
	"getblockverboseresult-version":           "The block version",
	"getblockverboseresult-merkleroot":        "Root hash of the merkle tree",
	"getblockverboseresult-tx":                "The transaction hashes (only when verbosetx=false)",
	"getblockverboseresult-rawtx":             "The transactions as JSON objects (only when verbosetx=true)",
	"getblockverboseresult-time":              "The block time in seconds since 1 Jan 1970 GMT",
	"getblockverboseresult-mediantime":        "The median block time over the last 11 blocks",
	"getblockverboseresult-nonce":             "The block nonce",
	"getblockverboseresult-bits":              "The bits which represent the block difficulty",
	"getblockverboseresult-difficulty":        "The proof-of-work difficulty as a multiple of the minimum difficulty",
	"getblockverboseresult-chainwork":         "The total number of hashes expected to produce the chain up to the block in hex",
	"getblockverboseresult-previousblockhash": "The hash of the previous block",
	"getblockverboseresult-nextblockhash":     "The hash of the next block (only if there is one)",
	"getblockverboseresult-sbits":             "The stake difficulty of the block",
	"getblockverboseresult-poolsize":          "The size of the live ticket pool",
	"getblockverboseresult-revocations":       "The number of revocations in the block",
	"getblockverboseresult-freshstake":        "The number of new tickets in the block",
	"getblockverboseresult-voters":            "The number votes in the block",
	"getblockverboseresult-votebits":          "The block's voting results",
	"getblockverboseresult-rawstx":            "The block's raw sstx hashes the were included",
	"getblockverboseresult-stx":               "The block's sstx hashes the were included",
	"getblockverboseresult-stakeroot":         "The block's sstx hashes the were included",
	"getblockverboseresult-finalstate":        "The block's finalstate",
	"getblockverboseresult-extradata":         "Extra data field for the requested block",
	"getblockverboseresult-stakeversion":      "Stake Version of the block",

	// TxRawResult help.
	"txrawresult-hex":           "Hex-encoded transaction",
	"txrawresult-txid":          "The hash of the transaction",
	"txrawresult-version":       "The transaction version",
	"txrawresult-locktime":      "The transaction lock time",
	"txrawresult-vin":           "The transaction inputs as JSON objects",
	"txrawresult-vout":          "The transaction outputs as JSON objects",
	"txrawresult-blockhash":     "The hash of the block that contains the transaction",
	"txrawresult-confirmations": "Number of confirmations of the block",
	"txrawresult-time":          "Transaction time in seconds since 1 Jan 1970 GMT",
	"txrawresult-blocktime":     "Block time in seconds since the 1 Jan 1970 GMT",
	"txrawresult-blockindex":    "The index within the array of transactions contained by the block",
	"txrawresult-blockheight":   "The height of the block that contains the transaction",
	"txrawresult-expiry":        "The transacion expiry",

	// Vin help.
	"vin-coinbase":      "The hex-encoded bytes of the signature script (coinbase txns only)",
	"vin-stakebase":     "The hex-encoded bytes of the signature script (vote txns only)",
	"vin-treasurybase":  "Whether or not the input is a treasury base (treasurybase txns only)",
	"vin-treasuryspend": "The hex-encoded bytes of the signature script (treasury spend txns only)",
	"vin-txid":          "The hash of the origin transaction (non-coinbase txns only)",
	"vin-vout":          "The index of the output being redeemed from the origin transaction (non-coinbase txns only)",
	"vin-scriptSig":     "The signature script used to redeem the origin transaction as a JSON object (non-coinbase txns only)",
	"vin-sequence":      "The script sequence number",
	"vin-tree":          "The tree of the transaction",
	"vin-blockindex":    "The block idx of the origin transaction",
	"vin-blockheight":   "The block height of the origin transaction",
	"vin-amountin":      "The amount in",

	// ScriptSig help.
	"scriptsig-asm": "Disassembly of the script",
	"scriptsig-hex": "Hex-encoded bytes of the script",

	// Vout help.
	"vout-value":        "The amount in DCR",
	"vout-n":            "The index of this transaction output",
	"vout-version":      "The version of the public key script",
	"vout-scriptPubKey": "The public key script used to pay coins as a JSON object",

	// ScriptPubKeyResult help.
	"scriptpubkeyresult-asm":       "Disassembly of the script",
	"scriptpubkeyresult-hex":       "Hex-encoded bytes of the script",
	"scriptpubkeyresult-reqSigs":   "The number of required signatures",
	"scriptpubkeyresult-type":      "The type of the script (e.g. 'pubkeyhash')",
	"scriptpubkeyresult-addresses": "The Decred addresses associated with this script",
	"scriptpubkeyresult-commitamt": "The ticket commitment value if the script is for a staking commitment",
	"scriptpubkeyresult-version":   "The script version",

	// GetCFilterV2Cmd help.
	"getcfilterv2--synopsis": "Returns the version 2 block filter for the given block along with the key required to query it for matches against committed scripts.",
	"getcfilterv2-blockhash": "The block hash of the filter to retrieve",

	// GetCFilterV2Result help.
	"getcfilterv2result-blockhash": "The block hash for which the filter includes data",
	"getcfilterv2result-filter":    "Hex-encoded bytes of the serialized filter",
	"getcfilterv2result-key":       "The key required to query the filter for matches against committed scripts",

	// SyncStatusCmd help.
	"syncstatus--synopsis": "Returns information about this wallet's synchronization to the network.",

	// SyncStatusResult help.
	"syncstatusresult-synced":               "Whether or not the wallet is fully caught up to the network.",
	"syncstatusresult-initialblockdownload": "Best guess of whether this wallet is in the initial block download mode used to catch up the blockchain when it is far behind.",
	"syncstatusresult-headersfetchprogress": "Estimated progress of the headers fetching stage of the current sync process.",

	// GetInfoCmd help.
	"getinfo--synopsis": "Returns a JSON object containing various state info.",

	// GetMasterPubkey help.
	"getmasterpubkey--synopsis": "Requests the master pubkey from the wallet.",
	"getmasterpubkey-account":   "The account to get the master pubkey for",
	"getmasterpubkey--result0":  "The master pubkey for the wallet",

	// GetMultisigOutInfo help.
	"getmultisigoutinfo--synopsis": "Returns information about a multisignature output.",
	"getmultisigoutinfo-index":     "Index of input.",
	"getmultisigoutinfo-hash":      "Input hash to check.",

	"getmultisigoutinforesult-amount":       "Amount of coins contained.",
	"getmultisigoutinforesult-spentbyindex": "Index of spending tx.",
	"getmultisigoutinforesult-spentby":      "Hash of spending tx.",
	"getmultisigoutinforesult-spent":        "If it has been spent.",
	"getmultisigoutinforesult-blockhash":    "Hash of the containing block.",
	"getmultisigoutinforesult-blockheight":  "Height of the containing block.",
	"getmultisigoutinforesult-txhash":       "txhash",
	"getmultisigoutinforesult-pubkeys":      "Associated pubkeys.",
	"getmultisigoutinforesult-n":            "n (in m-of-n)",
	"getmultisigoutinforesult-m":            "m (in m-of-n)",
	"getmultisigoutinforesult-redeemscript": "Hex of the redeeming script.",
	"getmultisigoutinforesult-address":      "Script address.",

	// GetNewAddressCmd help.
	"getnewaddress--synopsis": "Generates and returns a new payment address.",
	"getnewaddress-account":   "Account name the new address will belong to (default=\"default\")",
	"getnewaddress-gappolicy": `String defining the policy to use when the BIP0044 gap limit would be violated, may be "error", "ignore", or "wrap"`,
	"getnewaddress--result0":  "The payment address",

	// GetPeerInfoCmd help.
	"getpeerinfo--synopsis": "Returns data on remote peers when in spv mode.",

	// GetPeerInfoResult help.
	"getpeerinforesult-id":             "A unique node ID",
	"getpeerinforesult-addr":           "The remote IP address and port of the peer",
	"getpeerinforesult-addrlocal":      "The local IP address and port of the peer",
	"getpeerinforesult-services":       "Services bitmask which represents the services supported by the peer",
	"getpeerinforesult-version":        "The protocol version of the peer",
	"getpeerinforesult-subver":         "The user agent of the peer",
	"getpeerinforesult-startingheight": "The latest block height the peer knew about when the connection was established",
	"getpeerinforesult-banscore":       "The ban score",

	// GetRawChangeAddressCmd help.
	"getrawchangeaddress--synopsis": "Generates and returns a new internal payment address for use as a change address in raw transactions.",
	"getrawchangeaddress-account":   "Account name the new internal address will belong to (default=\"default\")",
	"getrawchangeaddress--result0":  "The internal payment address",

	// GetReceivedByAccountCmd help.
	"getreceivedbyaccount--synopsis": "Returns the total amount received by addresses of some account, including spent outputs.",
	"getreceivedbyaccount-account":   "Account name to query total received amount for",
	"getreceivedbyaccount-minconf":   "Minimum number of block confirmations required before an output's value is included in the total",
	"getreceivedbyaccount--result0":  "The total received amount valued in decred",

	// GetReceivedByAddressCmd help.
	"getreceivedbyaddress--synopsis": "Returns the total amount received by a single address, including spent outputs.",
	"getreceivedbyaddress-address":   "Payment address which received outputs to include in total",
	"getreceivedbyaddress-minconf":   "Minimum number of block confirmations required before an output's value is included in the total",
	"getreceivedbyaddress--result0":  "The total received amount valued in decred",

	// GetStakeInfo help.
	"getstakeinfo--synopsis": "Returns statistics about staking from the wallet.",

	// GetStakeInfoResult help.
	"getstakeinforesult-blockheight":      "Current block height for stake info.",
	"getstakeinforesult-poolsize":         "Number of live tickets in the ticket pool.",
	"getstakeinforesult-difficulty":       "Current stake difficulty.",
	"getstakeinforesult-allmempooltix":    "Number of tickets currently in the mempool",
	"getstakeinforesult-ownmempooltix":    "Number of tickets submitted by this wallet currently in mempool",
	"getstakeinforesult-immature":         "Number of tickets from this wallet that are in the blockchain but which are not yet mature",
	"getstakeinforesult-live":             "Number of mature, active tickets owned by this wallet",
	"getstakeinforesult-proportionlive":   "(Live / PoolSize)",
	"getstakeinforesult-voted":            "Number of votes cast by this wallet",
	"getstakeinforesult-totalsubsidy":     "Total amount of coins earned by proof-of-stake voting",
	"getstakeinforesult-missed":           "Number of missed tickets (failure to vote, not including expired)",
	"getstakeinforesult-proportionmissed": "(Missed / (Missed + Voted))",
	"getstakeinforesult-revoked":          "Number of missed tickets that were missed and then revoked",
	"getstakeinforesult-expired":          "Number of tickets that have expired",
	"getstakeinforesult-unspent":          "Number of unspent tickets",
	"getstakeinforesult-unspentexpired":   "Number of unspent tickets which are past expiry",

	// GetTicketMaxPrice help.
	"getticketmaxprice--synopsis": "Returns the max price the wallet will pay for a ticket.",
	"getticketmaxprice--result0":  "Max price wallet will spend on a ticket.",

	// GetTickets help.
	"gettickets--synopsis":       "Returning the hashes of the tickets currently owned by wallet.",
	"gettickets-includeimmature": "If true include immature tickets in the results.",

	// GetTicketsResult help.
	"getticketsresult-hashes": "Hashes of the tickets owned by the wallet encoded as strings",

	// GetTransactionCmd help.
	"gettransaction--synopsis":        "Returns a JSON object with details regarding a transaction relevant to this wallet.",
	"gettransaction-txid":             "Hash of the transaction to query",
	"gettransaction-includewatchonly": "Also consider transactions involving watched addresses",

	// GetTransactionDetailsResult help.
	"gettransactiondetailsresult-account":           "DEPRECATED -- Unset",
	"gettransactiondetailsresult-address":           "The address an output was paid to, or the empty string if the output is nonstandard or this detail is regarding a transaction input",
	"gettransactiondetailsresult-category":          `The kind of detail: "send" for sent transactions, "immature" for immature coinbase outputs, "generate" for mature coinbase outputs, or "recv" for all other received outputs`,
	"gettransactiondetailsresult-amount":            "The amount of a received output",
	"gettransactiondetailsresult-fee":               "The included fee for a sent transaction",
	"gettransactiondetailsresult-vout":              "The transaction output index",
	"gettransactiondetailsresult-involveswatchonly": "Unset",

	// GetTransactionResult help.
	"gettransactionresult-amount":          "The total amount this transaction credits to the wallet, valued in decred",
	"gettransactionresult-fee":             "The total input value minus the total output value, or 0 if 'txid' is not a sent transaction",
	"gettransactionresult-confirmations":   "The number of block confirmations of the transaction",
	"gettransactionresult-blockhash":       "The hash of the block this transaction is mined in, or the empty string if unmined",
	"gettransactionresult-blockindex":      "Unset",
	"gettransactionresult-blocktime":       "The Unix time of the block header this transaction is mined in, or 0 if unmined",
	"gettransactionresult-txid":            "The transaction hash",
	"gettransactionresult-walletconflicts": "Unset",
	"gettransactionresult-time":            "The earliest Unix time this transaction was known to exist",
	"gettransactionresult-timereceived":    "The earliest Unix time this transaction was known to exist",
	"gettransactionresult-details":         "Additional details for each recorded wallet credit and debit",
	"gettransactionresult-hex":             "The transaction encoded as a hexadecimal string",
	"gettransactionresult-type":            "The type of transaction (regular, ticket, vote, or revocation)",
	"gettransactionresult-ticketstatus":    "Status of ticket (if transaction is a ticket)",

	// GetUnconfirmedBalanceCmd help.
	"getunconfirmedbalance--synopsis": "Calculates the unspent output value of all unmined transaction outputs for an account.",
	"getunconfirmedbalance-account":   "The account to query the unconfirmed balance for (default=\"default\")",
	"getunconfirmedbalance--result0":  "Total amount of all unmined unspent outputs of the account valued in decred.",

	// GetVoteChoices help.
	"getvotechoices--synopsis":  "Retrieve the currently configured default vote choices for the latest supported stake agendas",
	"getvotechoices-tickethash": "The hash of the ticket to return vote choices for. If the ticket has no choices set, the default vote choices are returned",

	// GetVoteChoicesResult help.
	"getvotechoicesresult-version": "The latest stake version supported by the software and the version of the included agendas",
	"getvotechoicesresult-choices": "The currently configured agenda vote choices, including abstaining votes",

	// GetWalletFeeCmd help.
	"getwalletfee--synopsis": "Get currently set transaction fee for the wallet",
	"getwalletfee--result0":  "Current tx fee (in DCR)",

	// HelpCmd help.
	"help--synopsis":   "Returns a list of all commands or help for a specified command.",
	"help-command":     "The command to retrieve help for",
	"help--condition0": "no command provided",
	"help--condition1": "command specified",
	"help--result0":    "List of commands",
	"help--result1":    "Help for specified command",

	// GetTxOutCmd help.
	"gettxout--synopsis":      "Returns information about an unspent transaction output.",
	"gettxout-txid":           "The hash of the transaction",
	"gettxout-vout":           "The index of the output",
	"gettxout-tree":           "The tree of the transaction",
	"gettxout-includemempool": "Include the mempool when true",

	// GetTxOutResult help.
	"gettxoutresult-bestblock":     "The block hash that contains the transaction output",
	"gettxoutresult-confirmations": "The number of confirmations",
	"gettxoutresult-value":         "The transaction amount in DCR",
	"gettxoutresult-scriptPubKey":  "The public key script used to pay coins as a JSON object",
	"gettxoutresult-coinbase":      "Whether or not the transaction is a coinbase",

	// ImportCFiltersV2Cmd help.
	"importcfiltersv2--synopsis":   "Imports a list of v2 cfilters into the wallet. Does not perform validation on the filters",
	"importcfiltersv2-startheight": "The starting block height for this list of cfilters",
	"importcfiltersv2-filters":     "The list of hex-encoded cfilters",

	// ImportPrivKeyCmd help.
	"importprivkey--synopsis": "Imports a WIF-encoded private key to the 'imported' account.",
	"importprivkey-privkey":   "The WIF-encoded private key",
	"importprivkey-label":     "Unused (must be unset or 'imported')",
	"importprivkey-rescan":    "Rescan the blockchain (since the genesis block, or scanfrom block) for outputs controlled by the imported key",
	"importprivkey-scanfrom":  "Block number for where to start rescan from",

	// ImportScript help.
	"importscript--synopsis": "Import a redeem script.",
	"importscript-hex":       "Hex encoded script to import",
	"importscript-rescan":    "Rescans the blockchain (since the genesis block, or scanfrom block) for outputs controlled by the imported key",
	"importscript-scanfrom":  "Block number for where to start rescan from",

	// ImportXpub help.
	"importxpub--synopsis": "Import a HD extended public key as a new account.",
	"importxpub-name":      "Name of new account",
	"importxpub-xpub":      "Extended public key",

	// InfoResult help.
	"inforesult-version":         "The version of the server",
	"inforesult-protocolversion": "The latest supported protocol version",
	"inforesult-blocks":          "The number of blocks processed",
	"inforesult-timeoffset":      "The time offset",
	"inforesult-connections":     "The number of connected peers",
	"inforesult-proxy":           "The proxy used by the server",
	"inforesult-difficulty":      "The current target difficulty",
	"inforesult-testnet":         "Whether or not server is using testnet",
	"inforesult-relayfee":        "The minimum relay fee for non-free transactions in DCR/KB",
	"inforesult-errors":          "Any current errors",
	"inforesult-paytxfee":        "The fee per kB of the serialized tx size used each time more fee is required for an authored transaction",
	"inforesult-balance":         "The balance of all accounts calculated with one block confirmation",
	"inforesult-walletversion":   "The version of the address manager database",
	"inforesult-unlocked_until":  "Unset",
	"inforesult-keypoolsize":     "Unset",
	"inforesult-keypoololdest":   "Unset",

	// ListAccountsCmd help.
	"listaccounts--synopsis":       "DEPRECATED -- Returns a JSON object of all accounts and their balances.",
	"listaccounts-minconf":         "Minimum number of block confirmations required before an unspent output's value is included in the balance",
	"listaccounts--result0--desc":  "JSON object with account names as keys and decred amounts as values",
	"listaccounts--result0--key":   "The account name",
	"listaccounts--result0--value": "The account balance valued in decred",

	// ListAddressTransactionsCmd help.
	"listaddresstransactions--synopsis": "Returns a JSON array of objects containing verbose details for wallet transactions pertaining some addresses.",
	"listaddresstransactions-addresses": "Addresses to filter transaction results by",
	"listaddresstransactions-account":   "Unused (must be unset or \"*\")",

	// ListAllTransactionsCmd help.
	"listalltransactions--synopsis": "Returns a JSON array of objects in the same format as 'listtransactions' without limiting the number of returned objects.",
	"listalltransactions-account":   "Unused (must be unset or \"*\")",

	// ListLockUnspentCmd help.
	"listlockunspent--synopsis": "Returns a JSON array of outpoints marked as locked (with lockunspent) for this wallet session.",
	"listlockunspent-account":   "If set, only returns outpoints from this account that are marked as locked",

	// ListReceivedByAccountCmd help.
	"listreceivedbyaccount--synopsis":        "Returns a JSON array of objects listing all accounts and the total amount received by each account.",
	"listreceivedbyaccount-minconf":          "Minimum number of block confirmations required before a transaction is considered",
	"listreceivedbyaccount-includeempty":     "Unused",
	"listreceivedbyaccount-includewatchonly": "Unused",

	// ListReceivedByAccountResult help.
	"listreceivedbyaccountresult-account":       "The name of the account",
	"listreceivedbyaccountresult-amount":        "Total amount received by payment addresses of the account valued in decred",
	"listreceivedbyaccountresult-confirmations": "Number of block confirmations of the most recent transaction relevant to the account",

	// ListReceivedByAddressCmd help.
	"listreceivedbyaddress--synopsis":        "Returns a JSON array of objects listing wallet payment addresses and their total received amounts.",
	"listreceivedbyaddress-minconf":          "Minimum number of block confirmations required before a transaction is considered",
	"listreceivedbyaddress-includeempty":     "Unused",
	"listreceivedbyaddress-includewatchonly": "Unused",

	// ListReceivedByAddressResult help.
	"listreceivedbyaddressresult-account":           "DEPRECATED -- Unset",
	"listreceivedbyaddressresult-address":           "The payment address",
	"listreceivedbyaddressresult-amount":            "Total amount received by the payment address valued in decred",
	"listreceivedbyaddressresult-confirmations":     "Number of block confirmations of the most recent transaction relevant to the address",
	"listreceivedbyaddressresult-txids":             "Transaction hashes of all transactions involving this address",
	"listreceivedbyaddressresult-involvesWatchonly": "Unset",

	// ListSinceBlockCmd help.
	"listsinceblock--synopsis":           "Returns a JSON array of objects listing details of all wallet transactions after some block.",
	"listsinceblock-blockhash":           "Hash of the parent block of the first block to consider transactions from, or unset to list all transactions",
	"listsinceblock-targetconfirmations": "Minimum number of block confirmations of the last block in the result object.  Must be 1 or greater.  Note: The transactions array in the result object is not affected by this parameter",
	"listsinceblock-includewatchonly":    "Unused",
	"listsinceblock--condition0":         "blockhash specified",
	"listsinceblock--condition1":         "no blockhash specified",
	"listsinceblock--result0":            "Lists all transactions, including unmined transactions, since the specified block",
	"listsinceblock--result1":            "Lists all transactions since the genesis block",

	// ListSinceBlockResult help.
	"listsinceblockresult-transactions": "JSON array of objects containing verbose details of the each transaction",
	"listsinceblockresult-lastblock":    "Hash of the latest-synced block to be used in later calls to listsinceblock",

	// ListTransactionsCmd help.
	"listtransactions--synopsis":        "Returns a JSON array of objects containing verbose details for wallet transactions.",
	"listtransactions-account":          "DEPRECATED -- Unused (must be unset or \"*\")",
	"listtransactions-count":            "Maximum number of transactions to create results from",
	"listtransactions-from":             "Number of transactions to skip before results are created",
	"listtransactions-includewatchonly": "Unused",

	// ListTransactionsResult help.
	"listtransactionsresult-account":           "DEPRECATED -- Unset",
	"listtransactionsresult-address":           "Payment address for a transaction output",
	"listtransactionsresult-category":          `The kind of transaction: "send" for sent transactions, "immature" for immature coinbase outputs, "generate" for mature coinbase outputs, or "recv" for all other received outputs.  Note: A single output may be included multiple times under different categories`,
	"listtransactionsresult-amount":            "The value of the transaction output valued in decred",
	"listtransactionsresult-fee":               "The total input value minus the total output value for sent transactions",
	"listtransactionsresult-confirmations":     "The number of block confirmations of the transaction",
	"listtransactionsresult-generated":         "Whether the transaction output is a coinbase output",
	"listtransactionsresult-blockhash":         "The hash of the block this transaction is mined in, or the empty string if unmined",
	"listtransactionsresult-blockindex":        "Unset",
	"listtransactionsresult-blocktime":         "The Unix time of the block header this transaction is mined in, or 0 if unmined",
	"listtransactionsresult-txid":              "The hash of the transaction",
	"listtransactionsresult-vout":              "The transaction output index",
	"listtransactionsresult-walletconflicts":   "Unset",
	"listtransactionsresult-time":              "The earliest Unix time this transaction was known to exist",
	"listtransactionsresult-timereceived":      "The earliest Unix time this transaction was known to exist",
	"listtransactionsresult-involveswatchonly": "Unset",
	"listtransactionsresult-comment":           "Unset",
	"listtransactionsresult-otheraccount":      "Unset",
	"listtransactionsresult-txtype":            "The type of tx (regular tx, stake tx)",

	// ListUnspentCmd help.
	"listunspent--synopsis": "Returns a JSON array of objects representing unlocked unspent outputs controlled by wallet keys.",
	"listunspent-minconf":   "Minimum number of block confirmations required before a transaction output is considered",
	"listunspent-maxconf":   "Maximum number of block confirmations required before a transaction output is excluded",
	"listunspent-addresses": "If set, limits the returned details to unspent outputs received by any of these payment addresses",
	"listunspent-account":   "If set, only return unspent outputs from this account",

	// ListUnspentResult help.
	"listunspentresult-txid":          "The transaction hash of the referenced output",
	"listunspentresult-vout":          "The output index of the referenced output",
	"listunspentresult-address":       "The payment address that received the output",
	"listunspentresult-account":       "The account associated with the receiving payment address",
	"listunspentresult-scriptPubKey":  "The output script encoded as a hexadecimal string",
	"listunspentresult-redeemScript":  "The redeemScript if scriptPubKey is P2SH",
	"listunspentresult-amount":        "The amount of the output valued in decred",
	"listunspentresult-confirmations": "The number of block confirmations of the transaction",
	"listunspentresult-spendable":     "Whether the output is entirely controlled by wallet keys/scripts (false for partially controlled multisig outputs or outputs to watch-only addresses)",
	"listunspentresult-txtype":        "The type of the transaction",
	"listunspentresult-tree":          "The tree the transaction comes from",

	// LockAccountCmd help.
	"lockaccount--synopsis": "Lock an individually-encrypted account",
	"lockaccount-account":   "Account to lock",

	// LockUnspentCmd help.
	"lockunspent--synopsis": "Locks or unlocks an unspent output.\n" +
		"Locked outputs are not chosen for transaction inputs of authored transactions and are not included in 'listunspent' results.\n" +
		"Locked outputs are volatile and are not saved across wallet restarts.\n" +
		"If unlock is true and no transaction outputs are specified, all locked outputs are marked unlocked.",
	"lockunspent-unlock":       "True to unlock outputs, false to lock",
	"lockunspent-transactions": "Transaction outputs to lock or unlock",
	"lockunspent--result0":     "The boolean 'true'",

	// MixAccount help.
	"mixaccount--synopsis": "Mix all outputs of an account.",
	"mixaccount-account":   "Account to mix",

	// MixOutput help.
	"mixoutput--synopsis": "Mix a specific output.",
	"mixoutput-outpoint":  `Outpoint (in form "txhash:index") to mix`,

	// PurchaseTicketCmd help.
	"purchaseticket--synopsis":          "Purchase ticket using available funds.",
	"purchaseticket--result0":           "Hash of the resulting ticket",
	"purchaseticket-spendlimit":         "Limit on the amount to spend on ticket",
	"purchaseticket-fromaccount":        "The account to use for purchase (default=\"default\")",
	"purchaseticket-minconf":            "Minimum number of block confirmations required",
	"purchaseticket-ticketaddress":      "Override the ticket address to which voting rights are given",
	"purchaseticket-numtickets":         "The number of tickets to purchase",
	"purchaseticket-pooladdress":        "The address to pay stake pool fees to",
	"purchaseticket-poolfees":           "The amount of fees to pay to the stake pool",
	"purchaseticket-expiry":             "Height at which the purchase tickets expire",
	"purchaseticket-nosplittransaction": "Use ticket purchase change outputs instead of a split transaction",
	"purchaseticket-comment":            "Unused",
	"purchaseticket-dontsigntx":         "Return unsigned split and ticket transactions instead of signing and publishing",

	// RedeemMultiSigout help.
	"redeemmultisigout--synopsis": "Takes the input and constructs a P2PKH paying to the specified address.",
	"redeemmultisigout-address":   "Address to pay to.",
	"redeemmultisigout-tree":      "Tree the transaction is on.",
	"redeemmultisigout-index":     "Idx of the input transaction",
	"redeemmultisigout-hash":      "Hash of the input transaction",

	"redeemmultisigoutresult-errors":   "Any errors generated.",
	"redeemmultisigoutresult-complete": "Shows if opperation was completed.",
	"redeemmultisigoutresult-hex":      "Resulting hash.",

	// RedeemMultiSigouts help.
	"redeemmultisigouts--synopsis":      "Takes a hash, looks up all unspent outpoints and generates list artially signed transactions spending to either an address specified or internal addresses",
	"redeemmultisigouts-number":         "Number of outpoints found.",
	"redeemmultisigouts-toaddress":      "Address to look for (if not internal addresses).",
	"redeemmultisigouts-fromscraddress": "Input script hash address.",

	// RenameAccountCmd help.
	"renameaccount--synopsis":  "Renames an account.",
	"renameaccount-oldaccount": "The old account name to rename",
	"renameaccount-newaccount": "The new name for the account",

	// RescanWallet help.
	"rescanwallet--synopsis":   "Rescan the block chain for wallet data, blocking until the rescan completes or exits with an error",
	"rescanwallet-beginheight": "The height of the first block to begin the rescan from",

	// RevokeTickets help.
	"revoketickets--synopsis": "Requests the wallet create revovactions for any previously missed tickets.  Wallet must be unlocked.",

	// SendFromCmd help.
	"sendfrom--synopsis": "Authors, signs, and sends a transaction that outputs some amount to a payment address.\n" +
		"A change output is automatically included to send extra output value back to the original account.",
	"sendfrom-fromaccount": "Account to pick unspent outputs from",
	"sendfrom-toaddress":   "Address to pay",
	"sendfrom-amount":      "Amount to send to the payment address valued in decred",
	"sendfrom-minconf":     "Minimum number of block confirmations required before a transaction output is eligible to be spent",
	"sendfrom-comment":     "Unused",
	"sendfrom-commentto":   "Unused",
	"sendfrom--result0":    "The transaction hash of the sent transaction",

	// SendFromTreasuryCmd help.
	"sendfromtreasury--synopsis":      "Send from treasury balance to multiple recipients.",
	"sendfromtreasury-key":            "Politeia public key",
	"sendfromtreasury-amounts":        "Pairs of payment addresses and the output amount to pay each",
	"sendfromtreasury-amounts--desc":  "JSON object using payment addresses as keys and output amounts valued in decred to send to each address",
	"sendfromtreasury-amounts--key":   "Address to pay",
	"sendfromtreasury-amounts--value": "Amount to send to the payment address valued in decred",
	"sendfromtreasury--result0":       "The transaction hash of the sent transaction",

	// SendManyCmd help.
	"sendmany--synopsis": "Authors, signs, and sends a transaction that outputs to many payment addresses.\n" +
		"A change output is automatically included to send extra output value back to the original account.",
	"sendmany-fromaccount":    "Account to pick unspent outputs from",
	"sendmany-amounts":        "Pairs of payment addresses and the output amount to pay each",
	"sendmany-amounts--desc":  "JSON object using payment addresses as keys and output amounts valued in decred to send to each address",
	"sendmany-amounts--key":   "Address to pay",
	"sendmany-amounts--value": "Amount to send to the payment address valued in decred",
	"sendmany-minconf":        "Minimum number of block confirmations required before a transaction output is eligible to be spent",
	"sendmany-comment":        "Unused",
	"sendmany--result0":       "The transaction hash of the sent transaction",

	// SendRawTransactionCmd help.
	"sendrawtransaction--synopsis":     "Submits the serialized, hex-encoded transaction to the local peer and relays it to the network.",
	"sendrawtransaction-hextx":         "Serialized, hex-encoded signed transaction",
	"sendrawtransaction-allowhighfees": "Whether or not to allow insanely high fees",
	"sendrawtransaction--result0":      "The transaction hash of the sent transaction",

	// SendToAddressCmd help.
	"sendtoaddress--synopsis": "Authors, signs, and sends a transaction that outputs some amount to a payment address.\n" +
		"Unlike sendfrom, outputs are always chosen from the default account.\n" +
		"A change output is automatically included to send extra output value back to the original account.",
	"sendtoaddress-address":   "Address to pay",
	"sendtoaddress-amount":    "Amount to send to the payment address valued in decred",
	"sendtoaddress-comment":   "Unused",
	"sendtoaddress-commentto": "Unused",
	"sendtoaddress--result0":  "The transaction hash of the sent transaction",

	// SendToMultisigCmd help.
	"sendtomultisig--synopsis": "Authors, signs, and sends a transaction that outputs some amount to a multisig address.\n" +
		"Unlike sendfrom, outputs are always chosen from the default account.\n" +
		"A change output is automatically included to send extra output value back to the original account.",
	"sendtomultisig-minconf":     "Minimum number of block confirmations required",
	"sendtomultisig-nrequired":   "The number of signatures required to redeem outputs paid to this address",
	"sendtomultisig-pubkeys":     "Pubkey to send to.",
	"sendtomultisig-fromaccount": "Unused",
	"sendtomultisig-amount":      "Amount to send to the payment address valued in decred",
	"sendtomultisig-comment":     "Unused",
	"sendtomultisig--result0":    "The transaction hash of the sent transaction",

	// SendToTreasuryCmd help.
	"sendtotreasury--synopsis": "Send decred to treasury",
	"sendtotreasury-amount":    "Amount to send to treasury",
	"sendtotreasury--result0":  "The transaction hash of the sent transaction",

	// SetAccountPassphraseCmd help.
	"setaccountpassphrase--synopsis": "Individually encrypt or change per-account passphrase",
	"setaccountpassphrase-account":   "Account to modify",
	"setaccountpassphrase-passphrase": "New passphrase to use.\n" +
		"If this is the empty string, the account passphrase is removed and the account becomes encrypted by the global wallet passhprase.",

	// SetBalanceToMaintainCmd help.
	"setbalancetomaintain--synopsis": "Modify the balance for wallet to maintain for automatic ticket purchasing",
	"setbalancetomaintain-balance":   "The new balance for wallet to maintain for automatic ticket purchasing",
	"setbalancetomaintain--result0":  "Should return nothing",

	// SetDisapprovePercentCmd help.
	"setdisapprovepercent--synopsis": "Sets the wallet's block disapprove percent per vote. The wallet will randomly disapprove blocks with this percent of votes. Only used for testing purposes and will fail on mainnet.",
	"setdisapprovepercent-percent":   "The percent of votes to disapprove blocks. i.e. 100 means that all votes disapprove the block they are called on. Must be between zero and one hundred.",

	// SetGenerate help
	"setgenerate--synopsis":    "Enable or disable stake mining",
	"setgenerate-generate":     "True to enable stake mining, false to disable.",
	"setgenerate-genproclimit": "Not used for stake mining",

	// SetMixedAccountCmd help.
	"getcoinjoinsbyacct--synopsis":       "Get coinjoin outputs by account.",
	"getcoinjoinsbyacct--result0--desc":  "Return a map of account's name and its coinjoin outputs sum.",
	"getcoinjoinsbyacct--result0--value": "Coinjoin outputs sum.",
	"getcoinjoinsbyacct--result0--key":   "Accounts name",

	// SetTicketMaxPrice help.
	"setticketmaxprice--synopsis": "Set the max price user is willing to pay for a ticket.",
	"setticketmaxprice-max":       "The max price (in dcr).",

	// SetTreasuryPolicyCmd help.
	"settreasurypolicy--synopsis": "Set a voting policy for treasury spends by a particular key",
	"settreasurypolicy-key":       "Treasury key to set policy for",
	"settreasurypolicy-policy":    "Voting policy for a treasury key (invalid/abstain, yes, or no)",

	// SetTSpendPolicyCmd help.
	"settspendpolicy--synopsis": "Set a voting policy for a treasury spend transaction",
	"settspendpolicy-hash":      "Hash of treasury spend transaction to set policy for",
	"settspendpolicy-policy":    "Voting policy for a tspend transaction (invalid/abstain, yes, or no)",

	// SetTxFeeCmd help.
	"settxfee--synopsis": "Modify the fee per kB of the serialized tx size used each time more fee is required for an authored transaction.",
	"settxfee-amount":    "The new fee per kB of the serialized tx size valued in decred",
	"settxfee--result0":  "The boolean 'true'",

	// SetVoteChoice help.
	"setvotechoice--synopsis":  "Sets choices for defined agendas in the latest stake version supported by this software",
	"setvotechoice-agendaid":   "The ID for the agenda to modify",
	"setvotechoice-choiceid":   "The ID for the choice to choose",
	"setvotechoice-tickethash": "The hash of the ticket to set choices for",

	// SignMessageCmd help.
	"signmessage--synopsis": "Signs a message using the private key of a payment address.",
	"signmessage-address":   "Payment address of private key used to sign the message with",
	"signmessage-message":   "Message to sign",
	"signmessage--result0":  "The signed message encoded as a base64 string",

	// SignRawTransactionCmd help.
	"signrawtransaction--synopsis": "Signs transaction inputs using private keys from this wallet and request.\n" +
		"The valid flags options are ALL, NONE, SINGLE, ALL|ANYONECANPAY, NONE|ANYONECANPAY, and SINGLE|ANYONECANPAY.",
	"signrawtransaction-rawtx":    "Unsigned or partially unsigned transaction to sign encoded as a hexadecimal string",
	"signrawtransaction-inputs":   "Additional data regarding inputs that this wallet may not be tracking",
	"signrawtransaction-privkeys": "Additional WIF-encoded private keys to use when creating signatures",
	"signrawtransaction-flags":    "Sighash flags",

	// SignRawTransactionError help.
	"signrawtransactionerror-error":     "Verification or signing error related to the input",
	"signrawtransactionerror-sequence":  "Script sequence number",
	"signrawtransactionerror-scriptSig": "The hex-encoded signature script",
	"signrawtransactionerror-txid":      "The transaction hash of the referenced previous output",
	"signrawtransactionerror-vout":      "The output index of the referenced previous output",

	// SignRawTransactionResult help.
	"signrawtransactionresult-hex":      "The resulting transaction encoded as a hexadecimal string",
	"signrawtransactionresult-complete": "Whether all input signatures have been created",
	"signrawtransactionresult-errors":   "Script verification errors (if exists)",

	// SignRawTransactions help.
	"signrawtransactions--synopsis": "Signs transaction inputs using private keys from this wallet and request for a list of transactions.\n",
	"signrawtransactions-send":      "Set true to send the transactions after signing.",
	"signrawtransactions-rawtxs":    "A list of transactions to sign (and optionally send).",

	// SignRawTransactionsResults help.
	"signrawtransactionsresult-results": "Returned values from the signrawtransactions command.",
	"signedtransaction-txhash":          "The hash of the signed tx.",
	"signedtransaction-sent":            "Tells if the transaction was sent.",
	"signedtransaction-signingresult":   "Success or failure of signing.",

	// StakePoolUserInfoCmd help.
	"stakepooluserinfo--synopsis": "Get user info for stakepool",
	"stakepooluserinfo-user":      "The id of the user to be looked up",

	"stakepooluserinforesult-invalid": "A list of invalid tickets that the user has added",
	"stakepooluserinforesult-tickets": "A list of valid tickets that the user has added",

	"pooluserticket-spentbyheight": "The height in which the ticket was spent",
	"pooluserticket-spentby":       "The vote in which the ticket was spent",
	"pooluserticket-ticketheight":  "The height in which the ticket was added",
	"pooluserticket-ticket":        "The hash of the added ticket",
	"pooluserticket-status":        "The current status of the added ticket",

	// SweepAccount help.
	"sweepaccount--synopsis":             "Moves as much value as possible in a transaction from an account.\n",
	"sweepaccount-sourceaccount":         "The account to be swept.",
	"sweepaccount-destinationaddress":    "The destination address to pay to.",
	"sweepaccount-requiredconfirmations": "The minimum utxo confirmation requirement (optional).",
	"sweepaccount-feeperkb":              "The minimum relay fee policy (optional).",

	// SweepAccountResult help.
	"sweepaccountresult-unsignedtransaction":       "The hex encoded string of the unsigned transaction.",
	"sweepaccountresult-totalpreviousoutputamount": "The total transaction input amount.",
	"sweepaccountresult-totaloutputamount":         "The total transaction output amount.",
	"sweepaccountresult-estimatedsignedsize":       "The estimated size of the transaction when signed.",

	// TicketInfoCmd help.
	"ticketinfo--synopsis":           "Returns details of each wallet ticket transaction",
	"ticketinfo-startheight":         "Specify the starting block height to scan from",
	"ticketinfo--result0":            "Array of objects describing each ticket",
	"ticketinforesult-hash":          "Transaction hash of the ticket",
	"ticketinforesult-cost":          "Amount paid to purchase the ticket; this may be greater than the ticket price at time of purchase",
	"ticketinforesult-votingaddress": "Address of 0th output, which describes the requirements to spend the ticket",
	"ticketinforesult-status":        "Description of ticket status (unknown, unmined, immature, mature, live, voted, missed, expired, unspent, revoked)",
	"ticketinforesult-blockhash":     "Hash of block ticket is mined in",
	"ticketinforesult-blockheight":   "Height of block ticket is mined in",
	"ticketinforesult-vote":          "Transaction hash of vote which spends the ticket",
	"ticketinforesult-revocation":    "Transaction hash of revocation which spends the ticket",
	"ticketinforesult-choices":       "Vote preferences set for the ticket",

	// TicketsForAddressCmd help.
	"ticketsforaddress--synopsis": "Request all the tickets for an address.",
	"ticketsforaddress-address":   "Address to look for.",
	"ticketsforaddress--result0":  "Tickets owned by the specified address.",

	// TransactionInput help.
	"transactioninput-amount": "The the previous output amount",
	"transactioninput-txid":   "The transaction hash of the referenced output",
	"transactioninput-vout":   "The output index of the referenced output",
	"transactioninput-tree":   "The tree to generate transaction for",

	// TreasuryPolicyCmd help.
	"treasurypolicy--synopsis":   "Return voting policies for treasury spend transactions by key",
	"treasurypolicy-key":         "Return the policy for a particular key",
	"treasurypolicy--condition0": "no key provided",
	"treasurypolicy--condition1": "key specified",
	"treasurypolicy--result0":    "Array of all non-abstaining voting policies",
	"treasurypolicy--result1":    "Voting policy for a particular treasury key",

	"treasurypolicyresult-key":    "Treasury key associated with a policy",
	"treasurypolicyresult-policy": "Voting policy description (abstain, yes, or no)",

	// TSpendPolicyCmd help.
	"tspendpolicy--synopsis":   "Return voting policies for treasury spend transactions",
	"tspendpolicy-hash":        "Return the policy for a particular tspend hash",
	"tspendpolicy--condition0": "no tspend hash provided",
	"tspendpolicy--condition1": "tspend hash specified",
	"tspendpolicy--result0":    "Array of all non-abstaining policies for known tspends",
	"tspendpolicy--result1":    "Voting policy for a particular tspend hash",

	"tspendpolicyresult-hash":   "Treasury spend transaction hash",
	"tspendpolicyresult-policy": "Voting policy description (abstain, yes, or no)",

	// UnlockAccountCmd help.
	"unlockaccount--synopsis":  "Unlock an individually-encrypted account",
	"unlockaccount-account":    "Account to unlock",
	"unlockaccount-passphrase": "Account passphrase",

	// ValidateAddressCmd help.
	"validateaddress--synopsis": "Verify that an address is valid.\n" +
		"Extra details are returned if the address is controlled by this wallet.\n" +
		"The following fields are valid only when the address is controlled by this wallet (ismine=true): isscript, pubkey, iscompressed, account, addresses, hex, script, and sigsrequired.\n" +
		"The following fields are only valid when address has an associated public key: pubkey, iscompressed.\n" +
		"The following fields are only valid when address is a pay-to-script-hash address: addresses, hex, and script.\n" +
		"If the address is a multisig address controlled by this wallet, the multisig fields will be left unset if the wallet is locked since the redeem script cannot be decrypted.",
	"validateaddress-address": "Address to validate",

	// ValidateAddressResult help.
	"validateaddressresult-isvalid":      "Whether or not the address is valid",
	"validateaddressresult-address":      "The payment address (only when isvalid is true)",
	"validateaddressresult-ismine":       "Whether this address is controlled by the wallet (only when isvalid is true)",
	"validateaddressresult-iswatchonly":  "Unset",
	"validateaddressresult-isscript":     "Whether the payment address is a pay-to-script-hash address (only when isvalid is true)",
	"validateaddressresult-pubkey":       "The associated public key of the payment address, if any (only when isvalid is true)",
	"validateaddressresult-iscompressed": "Whether the address was created by hashing a compressed public key, if any (only when isvalid is true)",
	"validateaddressresult-account":      "The account this payment address belongs to (only when isvalid is true)",
	"validateaddressresult-addresses":    "All associated payment addresses of the script if address is a multisig address (only when isvalid is true)",
	"validateaddressresult-pubkeyaddr":   "The pubkey for this payment address (only when isvalid is true)",
	"validateaddressresult-hex":          "The redeem script ",
	"validateaddressresult-script":       "The class of redeem script for a multisig address",
	"validateaddressresult-sigsrequired": "The number of required signatures to redeem outputs to the multisig address",

	// ValidatePreDCP0005CFCmd help
	"validatepredcp0005cf--synopsis": "Validate whether all stored cfilters from before DCP0005 activation are correct according to the expected hardcoded hash",
	"validatepredcp0005cf--result0":  "Whether the cfilters are valid",

	// VerifyMessageCmd help.
	"verifymessage--synopsis": "Verify a message was signed with the associated private key of some address.",
	"verifymessage-address":   "Address used to sign message",
	"verifymessage-signature": "The signature to verify",
	"verifymessage-message":   "The message to verify",
	"verifymessage--result0":  "Whether the message was signed with the private key of 'address'",

	// Version help
	"version--synopsis":       "Returns application and API versions (semver) keyed by their names",
	"version--result0--desc":  "Version objects keyed by the program or API name",
	"version--result0--key":   "Program or API name",
	"version--result0--value": "Object containing the semantic version",

	// VoteChoice help.
	"votechoice-agendaid":          "The ID for the agenda the choice concerns",
	"votechoice-agendadescription": "A description of the agenda the choice concerns",
	"votechoice-choiceid":          "The ID of the current choice for this agenda",
	"votechoice-choicedescription": "A description of the current choice for this agenda",

	// WalletInfoCmd help.
	"walletinfo--synopsis":              "Returns global information about the wallet",
	"walletinforesult-daemonconnected":  "Whether or not the wallet is currently connected to the daemon RPC",
	"walletinforesult-unlocked":         "Whether or not the wallet is unlocked",
	"walletinforesult-cointype":         "Active coin type. Not available for watching-only wallets.",
	"walletinforesult-txfee":            "Transaction fee per kB of the serialized tx size in coins",
	"walletinforesult-votebits":         "Vote bits setting",
	"walletinforesult-votebitsextended": "Extended vote bits setting",
	"walletinforesult-voteversion":      "Version of votes that will be generated",
	"walletinforesult-voting":           "Whether or not the wallet is currently voting tickets",
	"walletinforesult-manualtickets":    "Whether or not the wallet is only accepting tickets manually",

	// WalletIsLockedCmd help.
	"walletislocked--synopsis": "Returns whether or not the wallet is locked.",
	"walletislocked--result0":  "Whether the wallet is locked",

	// WalletLockCmd help.
	"walletlock--synopsis": "Lock the wallet.",

	// WalletPassphraseChangeCmd help.
	"walletpassphrasechange--synopsis":     "Change the wallet passphrase.",
	"walletpassphrasechange-oldpassphrase": "The old wallet passphrase",
	"walletpassphrasechange-newpassphrase": "The new wallet passphrase",

	// WalletPassphraseCmd help.
	"walletpassphrase--synopsis":  "Unlock the wallet.",
	"walletpassphrase-passphrase": "The wallet passphrase",
	"walletpassphrase-timeout":    "The number of seconds to wait before the wallet automatically locks. 0 leaves the wallet unlocked indefinitely.",

	// WalletPubPassPhraseChangeCmd help
	"walletpubpassphrasechange--synopsis":     "Change the wallet's public passphrase.",
	"walletpubpassphrasechange-oldpassphrase": "The old wallet passphrase",
	"walletpubpassphrasechange-newpassphrase": "The new wallet passphrase",
}
