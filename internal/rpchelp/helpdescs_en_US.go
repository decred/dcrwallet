// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build !generate

package rpchelp

var helpDescsEnUS = map[string]string{
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

	// AddMultisigAddressCmd help.
	"addmultisigaddress--synopsis": "Generates and imports a multisig address and redeeming script to the 'imported' account.",
	"addmultisigaddress-account":   "DEPRECATED -- Unused (all imported addresses belong to the imported account)",
	"addmultisigaddress-keys":      "Pubkeys and/or pay-to-pubkey-hash addresses to partially control the multisig address",
	"addmultisigaddress-nrequired": "The number of signatures required to redeem outputs paid to this address",
	"addmultisigaddress--result0":  "The imported pay-to-script-hash address",

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

	// DumpPrivKeyCmd help.
	"dumpprivkey--synopsis": "Returns the private key in WIF encoding that controls some wallet address.",
	"dumpprivkey-address":   "The address to return a private key for",
	"dumpprivkey--result0":  "The WIF-encoded private key",

	// GenerateVote help.
	"generatevote--synopsis":   "Returns the vote transaction encoded as a hexadecimal string",
	"generatevote-blockhash":   "Block hash for the ticket",
	"generatevote-height":      "Block height for the ticket",
	"generatevote-tickethash":  "The hash of the ticket",
	"generatevote-votebits":    "The voteBits to set for the ticket",
	"generatevote-votebitsext": "The extended voteBits to set for the ticket",
	"generatevoteresult-hex":   "The hex encoded transaction",

	// GetAccountCmd help.
	"getaccount--synopsis": "DEPRECATED -- Lookup the account name that some wallet address belongs to.",
	"getaccount-address":   "The address to query the account for",
	"getaccount--result0":  "The name of the account that 'address' belongs to",

	// GetAccountAddressCmd help.
	"getaccountaddress--synopsis": "DEPRECATED -- Returns the most recent external payment address for an account that has not been seen publicly.\n" +
		"A new address is generated for the account if the most recently generated address has been seen on the blockchain or in mempool.",
	"getaccountaddress-account":  "The account of the returned address",
	"getaccountaddress--result0": "The unused address for 'account'",

	// GetAddressesByAccountCmd help.
	"getaddressesbyaccount--synopsis": "DEPRECATED -- Returns all addresses strings controlled by a single account.",
	"getaddressesbyaccount-account":   "Account name to fetch addresses for",
	"getaddressesbyaccount--result0":  "All addresses controlled by 'account'",

	// GetBalanceCmd help.
	"getbalance--synopsis": "Calculates and returns the balance of all accounts.",
	"getbalance-minconf":   "Minimum number of block confirmations required before an unspent output's value is included in the balance",
	"getbalance-account":   "DEPRECATED -- The account name to query the balance for, or \"*\" to consider all accounts (default=\"*\")",

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

	// GetBestBlockHashCmd help.
	"getbestblockhash--synopsis": "Returns the hash of the newest block in the best chain that wallet has finished syncing with.",
	"getbestblockhash--result0":  "The hash of the most recent synced-to block",

	// GetBlockCountCmd help.
	"getblockcount--synopsis": "Returns the blockchain height of the newest block in the best chain that wallet has finished syncing with.",
	"getblockcount--result0":  "The blockchain height of the most recent synced-to block",

	// GetBlockHashCmd help.
	"getblockhash--synopsis": "Returns the hash of a main chain block at some height",
	"getblockhash-index":     "The block height",
	"getblockhash--result0":  "The main chain block hash",

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

	// GetTickets help.
	"gettickets--synopsis":       "Returning the hashes of the tickets currently owned by wallet.",
	"gettickets-includeimmature": "If true include immature tickets in the results.",

	// GetVoteChoices help.
	"getvotechoices--synopsis": "Retrieve the currently configured vote choices for the latest supported stake agendas",

	// GetVoteChoicesResult help.
	"getvotechoicesresult-version": "The latest stake version supported by the software and the version of the included agendas",
	"getvotechoicesresult-choices": "The currently configured agenda vote choices, including abstaining votes",

	// VoteChoice help.
	"votechoice-agendaid":          "The ID for the agenda the choice concerns",
	"votechoice-agendadescription": "A description of the agenda the choice concerns",
	"votechoice-choiceid":          "The ID of the current choice for this agenda",
	"votechoice-choicedescription": "A description of the current choice for this agenda",

	// GetTicketMaxPrice help.
	"getticketmaxprice--synopsis": "Returns the max price the wallet will pay for a ticket.",
	"getticketmaxprice--result0":  "Max price wallet will spend on a ticket.",

	// GetTicketsResult help.
	"getticketsresult-hashes": "Hashes of the tickets owned by the wallet encoded as strings",

	// InfoWalletResult help.
	"infowalletresult-version":         "The version of the server",
	"infowalletresult-protocolversion": "The latest supported protocol version",
	"infowalletresult-blocks":          "The number of blocks processed",
	"infowalletresult-timeoffset":      "The time offset",
	"infowalletresult-connections":     "The number of connected peers",
	"infowalletresult-proxy":           "The proxy used by the server",
	"infowalletresult-difficulty":      "The current target difficulty",
	"infowalletresult-testnet":         "Whether or not server is using testnet",
	"infowalletresult-relayfee":        "The minimum relay fee for non-free transactions in DCR/KB",
	"infowalletresult-errors":          "Any current errors",
	"infowalletresult-paytxfee":        "The fee per kB of the serialized tx size used each time more fee is required for an authored transaction",
	"infowalletresult-balance":         "The balance of all accounts calculated with one block confirmation",
	"infowalletresult-walletversion":   "The version of the address manager database",
	"infowalletresult-unlocked_until":  "Unset",
	"infowalletresult-keypoolsize":     "Unset",
	"infowalletresult-keypoololdest":   "Unset",

	// GetNewAddressCmd help.
	"getnewaddress--synopsis": "Generates and returns a new payment address.",
	"getnewaddress-account":   "Account name the new address will belong to (default=\"default\")",
	"getnewaddress-gappolicy": `String defining the policy to use when the BIP0044 gap limit would be violated, may be "error", "ignore", or "wrap"`,
	"getnewaddress--result0":  "The payment address",

	// GetRawChangeAddressCmd help.
	"getrawchangeaddress--synopsis": "Generates and returns a new internal payment address for use as a change address in raw transactions.",
	"getrawchangeaddress-account":   "Account name the new internal address will belong to (default=\"default\")",
	"getrawchangeaddress--result0":  "The internal payment address",

	// GetReceivedByAccountCmd help.
	"getreceivedbyaccount--synopsis": "DEPRECATED -- Returns the total amount received by addresses of some account, including spent outputs.",
	"getreceivedbyaccount-account":   "Account name to query total received amount for",
	"getreceivedbyaccount-minconf":   "Minimum number of block confirmations required before an output's value is included in the total",
	"getreceivedbyaccount--result0":  "The total received amount valued in decred",

	// GetReceivedByAddressCmd help.
	"getreceivedbyaddress--synopsis": "Returns the total amount received by a single address, including spent outputs.",
	"getreceivedbyaddress-address":   "Payment address which received outputs to include in total",
	"getreceivedbyaddress-minconf":   "Minimum number of block confirmations required before an output's value is included in the total",
	"getreceivedbyaddress--result0":  "The total received amount valued in decred",

	// GetTransactionCmd help.
	"gettransaction--synopsis":        "Returns a JSON object with details regarding a transaction relevant to this wallet.",
	"gettransaction-txid":             "Hash of the transaction to query",
	"gettransaction-includewatchonly": "Also consider transactions involving watched addresses",

	// HelpCmd help.
	"help--synopsis":   "Returns a list of all commands or help for a specified command.",
	"help-command":     "The command to retrieve help for",
	"help--condition0": "no command provided",
	"help--condition1": "command specified",
	"help--result0":    "List of commands",
	"help--result1":    "Help for specified command",

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

	// GetTransactionDetailsResult help.
	"gettransactiondetailsresult-account":           "DEPRECATED -- Unset",
	"gettransactiondetailsresult-address":           "The address an output was paid to, or the empty string if the output is nonstandard or this detail is regarding a transaction input",
	"gettransactiondetailsresult-category":          `The kind of detail: "send" for sent transactions, "immature" for immature coinbase outputs, "generate" for mature coinbase outputs, or "recv" for all other received outputs`,
	"gettransactiondetailsresult-amount":            "The amount of a received output",
	"gettransactiondetailsresult-fee":               "The included fee for a sent transaction",
	"gettransactiondetailsresult-vout":              "The transaction output index",
	"gettransactiondetailsresult-involveswatchonly": "Unset",

	// ImportPrivKeyCmd help.
	"importprivkey--synopsis": "Imports a WIF-encoded private key to the 'imported' account.",
	"importprivkey-privkey":   "The WIF-encoded private key",
	"importprivkey-label":     "Unused (must be unset or 'imported')",
	"importprivkey-rescan":    "Rescan the blockchain (since the genesis block, or scanfrom block) for outputs controlled by the imported key",
	"importprivkey-scanfrom":  "Block number for where to start rescan from",

	// ImportScript help.
	"importscript--synopsis": "Import a redeem script.",
	"importscript-hex":       "Hex encoded script to import",
	"importscript-rescan":    "Rescansfdsfd the blockchain (since the genesis block, or scanfrom block) for outputs controlled by the imported key",
	"importscript-scanfrom":  "Block number for where to start rescan from",

	// KeypoolRefillCmd help.
	"keypoolrefill--synopsis": "DEPRECATED -- This request does nothing since no keypool is maintained.",
	"keypoolrefill-newsize":   "Unused",

	// ListAccountsCmd help.
	"listaccounts--synopsis":       "DEPRECATED -- Returns a JSON object of all accounts and their balances.",
	"listaccounts-minconf":         "Minimum number of block confirmations required before an unspent output's value is included in the balance",
	"listaccounts--result0--desc":  "JSON object with account names as keys and decred amounts as values",
	"listaccounts--result0--key":   "The account name",
	"listaccounts--result0--value": "The account balance valued in decred",

	// ListLockUnspentCmd help.
	"listlockunspent--synopsis": "Returns a JSON array of outpoints marked as locked (with lockunspent) for this wallet session.",

	// TransactionInput help.
	"transactioninput-amount": "The the previous output amount",
	"transactioninput-txid":   "The transaction hash of the referenced output",
	"transactioninput-vout":   "The output index of the referenced output",
	"transactioninput-tree":   "The tree to generate transaction for",

	// ListReceivedByAccountCmd help.
	"listreceivedbyaccount--synopsis":        "DEPRECATED -- Returns a JSON array of objects listing all accounts and the total amount received by each account.",
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

	// ListTransactionsCmd help.
	"listtransactions--synopsis":        "Returns a JSON array of objects containing verbose details for wallet transactions.",
	"listtransactions-account":          "DEPRECATED -- Unused (must be unset or \"*\")",
	"listtransactions-count":            "Maximum number of transactions to create results from",
	"listtransactions-from":             "Number of transactions to skip before results are created",
	"listtransactions-includewatchonly": "Unused",

	// ListUnspentCmd help.
	"listunspent--synopsis": "Returns a JSON array of objects representing unlocked unspent outputs controlled by wallet keys.",
	"listunspent-minconf":   "Minimum number of block confirmations required before a transaction output is considered",
	"listunspent-maxconf":   "Maximum number of block confirmations required before a transaction output is excluded",
	"listunspent-addresses": "If set, limits the returned details to unspent outputs received by any of these payment addresses",

	// ListUnspentResult help.
	"listunspentresult-txid":          "The transaction hash of the referenced output",
	"listunspentresult-vout":          "The output index of the referenced output",
	"listunspentresult-address":       "The payment address that received the output",
	"listunspentresult-account":       "The account associated with the receiving payment address",
	"listunspentresult-scriptPubKey":  "The output script encoded as a hexadecimal string",
	"listunspentresult-redeemScript":  "Unset",
	"listunspentresult-amount":        "The amount of the output valued in decred",
	"listunspentresult-confirmations": "The number of block confirmations of the transaction",
	"listunspentresult-spendable":     "Whether the output is entirely controlled by wallet keys/scripts (false for partially controlled multisig outputs or outputs to watch-only addresses)",
	"listunspentresult-txtype":        "The type of the transaction",
	"listunspentresult-tree":          "The tree the transaction comes from",

	// LockUnspentCmd help.
	"lockunspent--synopsis": "Locks or unlocks an unspent output.\n" +
		"Locked outputs are not chosen for transaction inputs of authored transactions and are not included in 'listunspent' results.\n" +
		"Locked outputs are volatile and are not saved across wallet restarts.\n" +
		"If unlock is true and no transaction outputs are specified, all locked outputs are marked unlocked.",
	"lockunspent-unlock":       "True to unlock outputs, false to lock",
	"lockunspent-transactions": "Transaction outputs to lock or unlock",
	"lockunspent--result0":     "The boolean 'true'",

	// SendFromCmd help.
	"sendfrom--synopsis": "DEPRECATED -- Authors, signs, and sends a transaction that outputs some amount to a payment address.\n" +
		"A change output is automatically included to send extra output value back to the original account.",
	"sendfrom-fromaccount": "Account to pick unspent outputs from",
	"sendfrom-toaddress":   "Address to pay",
	"sendfrom-amount":      "Amount to send to the payment address valued in decred",
	"sendfrom-minconf":     "Minimum number of block confirmations required before a transaction output is eligible to be spent",
	"sendfrom-comment":     "Unused",
	"sendfrom-commentto":   "Unused",
	"sendfrom--result0":    "The transaction hash of the sent transaction",

	// SendManyCmd help.
	"sendmany--synopsis": "Authors, signs, and sends a transaction that outputs to many payment addresses.\n" +
		"A change output is automatically included to send extra output value back to the original account.",
	"sendmany-fromaccount":    "DEPRECATED -- Account to pick unspent outputs from",
	"sendmany-amounts":        "Pairs of payment addresses and the output amount to pay each",
	"sendmany-amounts--desc":  "JSON object using payment addresses as keys and output amounts valued in decred to send to each address",
	"sendmany-amounts--key":   "Address to pay",
	"sendmany-amounts--value": "Amount to send to the payment address valued in decred",
	"sendmany-minconf":        "Minimum number of block confirmations required before a transaction output is eligible to be spent",
	"sendmany-comment":        "Unused",
	"sendmany--result0":       "The transaction hash of the sent transaction",

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

	// SetGenerate help
	"setgenerate--synopsis":    "Enable or disable stake mining",
	"setgenerate-generate":     "True to enable stake mining, false to disable.",
	"setgenerate-genproclimit": "Not used for stake mining",

	// SetTicketMaxPrice help.
	"setticketmaxprice--synopsis": "Set the max price user is willing to pay for a ticket.",
	"setticketmaxprice-max":       "The max price (in dcr).",

	// SetTxFeeCmd help.
	"settxfee--synopsis": "Modify the fee per kB of the serialized tx size used each time more fee is required for an authored transaction.",
	"settxfee-amount":    "The new fee per kB of the serialized tx size valued in decred",
	"settxfee--result0":  "The boolean 'true'",

	// SetVoteChoice help.
	"setvotechoice--synopsis": "Sets choices for defined agendas in the latest stake version supported by this software",
	"setvotechoice-agendaid":  "The ID for the agenda to modify",
	"setvotechoice-choiceid":  "The ID for the choice to choose",

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

	// SignRawTransactionResult help.
	"signrawtransactionresult-hex":      "The resulting transaction encoded as a hexadecimal string",
	"signrawtransactionresult-complete": "Whether all input signatures have been created",
	"signrawtransactionresult-errors":   "Script verification errors (if exists)",

	// StartAutoBuyerCmd Help.
	"startautobuyer--synopsis":         "Starts the wallet's ticket buyer.",
	"startautobuyer-account":           "The account to use for purchasing tickets",
	"startautobuyer-passphrase":        "The private passphrase of the wallet",
	"startautobuyer-balancetomaintain": "The minimum amount of funds to never dip below when purchasing tickets",
	"startautobuyer-maxfeeperkb":       "The maximum ticket fee amount per KB",
	"startautobuyer-maxpricerelative":  "The scaling factor for setting the maximum ticket price, multiplied by the average price",
	"startautobuyer-maxpriceabsolute":  "The maximum absolute ticket price",
	"startautobuyer-votingaddress":     "The address to delegate voting rights to",
	"startautobuyer-pooladdress":       "The stake pool address where ticket fees will go to",
	"startautobuyer-poolfees":          "The absolute per ticket fee mandated by the stake pool as a percent",
	"startautobuyer-maxperblock":       "The maximum tickets per block. Negative number indicates one ticket every n blocks",

	// StopAutoBuyerCmd Help.
	"stopautobuyer--synopsis": "Stops the wallet's ticket buyer.",

	// SignRawTransactionError help.
	"signrawtransactionerror-error":     "Verification or signing error related to the input",
	"signrawtransactionerror-sequence":  "Script sequence number",
	"signrawtransactionerror-scriptSig": "The hex-encoded signature script",
	"signrawtransactionerror-txid":      "The transaction hash of the referenced previous output",
	"signrawtransactionerror-vout":      "The output index of the referenced previous output",

	// SignRawTransactions help.
	"signrawtransactions--synopsis": "Signs transaction inputs using private keys from this wallet and request for a list of transactions.\n",
	"signrawtransactions-send":      "Set true to send the transactions after signing.",
	"signrawtransactions-rawtxs":    "A list of transactions to sign (and optionally send).",

	// SignRawTransactionsResults help.
	"signrawtransactionsresult-results": "Returned values from the signrawtransactions command.",
	"signedtransaction-txhash":          "The hash of the signed tx.",
	"signedtransaction-sent":            "Tells if the transaction was sent.",
	"signedtransaction-signingresult":   "Success or failure of signing.",

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

	// ValidateAddressCmd help.
	"validateaddress--synopsis": "Verify that an address is valid.\n" +
		"Extra details are returned if the address is controlled by this wallet.\n" +
		"The following fields are valid only when the address is controlled by this wallet (ismine=true): isscript, pubkey, iscompressed, account, addresses, hex, script, and sigsrequired.\n" +
		"The following fields are only valid when address has an associated public key: pubkey, iscompressed.\n" +
		"The following fields are only valid when address is a pay-to-script-hash address: addresses, hex, and script.\n" +
		"If the address is a multisig address controlled by this wallet, the multisig fields will be left unset if the wallet is locked since the redeem script cannot be decrypted.",
	"validateaddress-address": "Address to validate",

	// ValidateAddressWalletResult help.
	"validateaddresswalletresult-isvalid":      "Whether or not the address is valid",
	"validateaddresswalletresult-address":      "The payment address (only when isvalid is true)",
	"validateaddresswalletresult-ismine":       "Whether this address is controlled by the wallet (only when isvalid is true)",
	"validateaddresswalletresult-iswatchonly":  "Unset",
	"validateaddresswalletresult-isscript":     "Whether the payment address is a pay-to-script-hash address (only when isvalid is true)",
	"validateaddresswalletresult-pubkey":       "The associated public key of the payment address, if any (only when isvalid is true)",
	"validateaddresswalletresult-iscompressed": "Whether the address was created by hashing a compressed public key, if any (only when isvalid is true)",
	"validateaddresswalletresult-account":      "The account this payment address belongs to (only when isvalid is true)",
	"validateaddresswalletresult-addresses":    "All associated payment addresses of the script if address is a multisig address (only when isvalid is true)",
	"validateaddresswalletresult-pubkeyaddr":   "The pubkey for this payment address (only when isvalid is true)",
	"validateaddresswalletresult-hex":          "The redeem script ",
	"validateaddresswalletresult-script":       "The class of redeem script for a multisig address",
	"validateaddresswalletresult-sigsrequired": "The number of required signatures to redeem outputs to the multisig address",

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

	// WalletLockCmd help.
	"walletlock--synopsis": "Lock the wallet.",

	// WalletPassphraseCmd help.
	"walletpassphrase--synopsis":  "Unlock the wallet.",
	"walletpassphrase-passphrase": "The wallet passphrase",
	"walletpassphrase-timeout":    "The number of seconds to wait before the wallet automatically locks",

	// WalletPassphraseChangeCmd help.
	"walletpassphrasechange--synopsis":     "Change the wallet passphrase.",
	"walletpassphrasechange-oldpassphrase": "The old wallet passphrase",
	"walletpassphrasechange-newpassphrase": "The new wallet passphrase",

	// CreateNewAccountCmd help.
	"createnewaccount--synopsis": "Creates a new account.\n" +
		"The wallet must be unlocked for this request to succeed.",
	"createnewaccount-account": "Name of the new account",

	// ExportWatchingWalletCmd help.
	"exportwatchingwallet--synopsis": "Creates and returns a duplicate of the wallet database without any private keys to be used as a watching-only wallet.",
	"exportwatchingwallet-account":   "Unused (must be unset or \"*\")",
	"exportwatchingwallet-download":  "Unused",
	"exportwatchingwallet--result0":  "The watching-only database encoded as a base64 string",

	// GetBestBlockCmd help.
	"getbestblock--synopsis": "Returns the hash and height of the newest block in the best chain that wallet has finished syncing with.",

	// GetBestBlockResult help.
	"getbestblockresult-hash":   "The hash of the block",
	"getbestblockresult-height": "The blockchain height of the block",

	// GetUnconfirmedBalanceCmd help.
	"getunconfirmedbalance--synopsis": "Calculates the unspent output value of all unmined transaction outputs for an account.",
	"getunconfirmedbalance-account":   "The account to query the unconfirmed balance for (default=\"default\")",
	"getunconfirmedbalance--result0":  "Total amount of all unmined unspent outputs of the account valued in decred.",

	// ListAddressTransactionsCmd help.
	"listaddresstransactions--synopsis": "Returns a JSON array of objects containing verbose details for wallet transactions pertaining some addresses.",
	"listaddresstransactions-addresses": "Addresses to filter transaction results by",
	"listaddresstransactions-account":   "Unused (must be unset or \"*\")",

	// ListAllTransactionsCmd help.
	"listalltransactions--synopsis": "Returns a JSON array of objects in the same format as 'listtransactions' without limiting the number of returned objects.",
	"listalltransactions-account":   "Unused (must be unset or \"*\")",

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

	// RescanWallet help.
	"rescanwallet--synopsis":   "Rescan the block chain for wallet data, blocking until the rescan completes or exits with an error",
	"rescanwallet-beginheight": "The height of the first block to begin the rescan from",

	// RevokeTickets help.
	"revoketickets--synopsis": "Requests the wallet create revovactions for any previously missed tickets.  Wallet must be unlocked.",

	// RenameAccountCmd help.
	"renameaccount--synopsis":  "Renames an account.",
	"renameaccount-oldaccount": "The old account name to rename",
	"renameaccount-newaccount": "The new name for the account",

	// WalletIsLockedCmd help.
	"walletislocked--synopsis": "Returns whether or not the wallet is locked.",
	"walletislocked--result0":  "Whether the wallet is locked",

	// WalletInfoCmd help.
	"walletinfo--synopsis":              "Returns global information about the wallet",
	"walletinforesult-daemonconnected":  "Whether or not the wallet is currently connected to the daemon RPC",
	"walletinforesult-unlocked":         "Whether or not the wallet is unlocked",
	"walletinforesult-cointype":         "Active coin type. Not available for watching-only wallets.",
	"walletinforesult-txfee":            "Transaction fee per kB of the serialized tx size in coins",
	"walletinforesult-ticketfee":        "Ticket fee per kB of the serialized tx size in coins",
	"walletinforesult-ticketpurchasing": "Whether or not the wallet is currently purchasing tickets",
	"walletinforesult-votebits":         "Vote bits setting",
	"walletinforesult-votebitsextended": "Extended vote bits setting",
	"walletinforesult-voteversion":      "Version of votes that will be generated",
	"walletinforesult-voting":           "Whether or not the wallet is currently voting tickets",

	// TODO Alphabetize

	// AddTicketCmd help.
	"addticket--synopsis": "Add a ticket to the wallet for vote and revocation creation.  Added tickets are auxiliary to transaction history and do not appear in getstakeinfo stats.",
	"addticket-tickethex": "Hex-encoded serialized transaction",

	// GetWalletFeeCmd help.
	"getwalletfee--synopsis": "Get currently set transaction fee for the wallet",
	"getwalletfee--result0":  "Current tx fee (in DCR)",

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

	// ListScriptsCmd help.
	"listscripts--synopsis": "List all scripts that have been added to wallet",

	"listscriptsresult-scripts": "A list of the imported scripts",

	"scriptinfo-redeemscript": "The redeem script",
	"scriptinfo-address":      "The script address",
	"scriptinfo-hash160":      "The script hash",

	// TicketsForAddressCmd help.
	"ticketsforaddress--synopsis": "Request all the tickets for an address.",
	"ticketsforaddress-address":   "Address to look for.",
	"ticketsforaddress--result0":  "Tickets owned by the specified address.",

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
	"purchaseticket-ticketfee":          "The transaction fee rate (DCR/kB) to use (overrides fees set by the wallet config or settxfee RPC)",

	// SetTicketFeeCmd help.
	"setticketfee--synopsis": "Modify the fee per kB of the serialized tx size used each time more fee is required for an authored stake transaction.",
	"setticketfee-fee":       "The new fee per kB of the serialized tx size valued in decred",
	"setticketfee--result0":  "The boolean 'true'",

	// GetTicketFeeCmd help.
	"getticketfee--synopsis": "Get the current fee per kB of the serialized tx size used for an authored stake transaction.",
	"getticketfee--result0":  "The current fee",

	// SetBalanceToMaintainCmd help.
	"setbalancetomaintain--synopsis": "Modify the balance for wallet to maintain for automatic ticket purchasing",
	"setbalancetomaintain-balance":   "The new balance for wallet to maintain for automatic ticket purchasing",
	"setbalancetomaintain--result0":  "Should return nothing",

	// GetBalanceToMaintainCmd help.
	"getbalancetomaintain--synopsis": "Get the current balance to maintain",
	"getbalancetomaintain--result0":  "The current balancetomaintain",
}
