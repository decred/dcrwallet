// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

// FundRawTransactionResult models the data from the fundrawtransaction command.
type FundRawTransactionResult struct {
	Hex string  `json:"hex"`
	Fee float64 `json:"fee"`
}

// GenerateVoteResult models the data from the generatevote command.
type GenerateVoteResult struct {
	Hex string `json:"hex"`
}

// GetAccountBalanceResult models the account data from the getbalance command.
type GetAccountBalanceResult struct {
	AccountName             string  `json:"accountname"`
	ImmatureCoinbaseRewards float64 `json:"immaturecoinbaserewards"`
	ImmatureStakeGeneration float64 `json:"immaturestakegeneration"`
	LockedByTickets         float64 `json:"lockedbytickets"`
	Spendable               float64 `json:"spendable"`
	Total                   float64 `json:"total"`
	Unconfirmed             float64 `json:"unconfirmed"`
	VotingAuthority         float64 `json:"votingauthority"`
}

// GetBalanceResult models the data from the getbalance command.
type GetBalanceResult struct {
	Balances                     []GetAccountBalanceResult `json:"balances"`
	BlockHash                    string                    `json:"blockhash"`
	TotalImmatureCoinbaseRewards float64                   `json:"totalimmaturecoinbaserewards,omitempty"`
	TotalImmatureStakeGeneration float64                   `json:"totalimmaturestakegeneration,omitempty"`
	TotalLockedByTickets         float64                   `json:"totallockedbytickets,omitempty"`
	TotalSpendable               float64                   `json:"totalspendable,omitempty"`
	CumulativeTotal              float64                   `json:"cumulativetotal,omitempty"`
	TotalUnconfirmed             float64                   `json:"totalunconfirmed,omitempty"`
	TotalVotingAuthority         float64                   `json:"totalvotingauthority,omitempty"`
}

// GetMultisigOutInfoResult models the data returned from the getmultisigoutinfo
// command.
type GetMultisigOutInfoResult struct {
	Address      string   `json:"address"`
	RedeemScript string   `json:"redeemscript"`
	M            uint8    `json:"m"`
	N            uint8    `json:"n"`
	Pubkeys      []string `json:"pubkeys"`
	TxHash       string   `json:"txhash"`
	BlockHeight  uint32   `json:"blockheight"`
	BlockHash    string   `json:"blockhash"`
	Spent        bool     `json:"spent"`
	SpentBy      string   `json:"spentby"`
	SpentByIndex uint32   `json:"spentbyindex"`
	Amount       float64  `json:"amount"`
}

// CreateMultiSigResult models the data returned from the createmultisig
// command.
type CreateMultiSigResult struct {
	Address      string `json:"address"`
	RedeemScript string `json:"redeemScript"`
}

// CreateSignatureResult models the data returned from the createsignature
// command.
type CreateSignatureResult struct {
	Signature string `json:"signature"`
	PublicKey string `json:"publickey"`
}

// GetPeerInfoResult models the data returned from the getpeerinfo command.
type GetPeerInfoResult struct {
	ID             int32  `json:"id"`
	Addr           string `json:"addr"`
	AddrLocal      string `json:"addrlocal"`
	Services       string `json:"services"`
	Version        uint32 `json:"version"`
	SubVer         string `json:"subver"`
	StartingHeight int64  `json:"startingheight"`
	BanScore       int32  `json:"banscore"`
}

// GetStakeInfoResult models the data returned from the getstakeinfo
// command.
type GetStakeInfoResult struct {
	BlockHeight  int64   `json:"blockheight"`
	Difficulty   float64 `json:"difficulty"`
	TotalSubsidy float64 `json:"totalsubsidy"`

	OwnMempoolTix  uint32 `json:"ownmempooltix"`
	Immature       uint32 `json:"immature"`
	Unspent        uint32 `json:"unspent"`
	Voted          uint32 `json:"voted"`
	Revoked        uint32 `json:"revoked"`
	UnspentExpired uint32 `json:"unspentexpired"`

	// Not available to SPV wallets
	PoolSize         uint32  `json:"poolsize,omitempty"`
	AllMempoolTix    uint32  `json:"allmempooltix,omitempty"`
	Live             uint32  `json:"live,omitempty"`
	ProportionLive   float64 `json:"proportionlive,omitempty"`
	Missed           uint32  `json:"missed,omitempty"`
	ProportionMissed float64 `json:"proportionmissed,omitempty"`
	Expired          uint32  `json:"expired,omitempty"`
}

// GetTicketsResult models the data returned from the gettickets
// command.
type GetTicketsResult struct {
	Hashes []string `json:"hashes"`
}

// GetTransactionDetailsResult models the details data from the gettransaction command.
//
// This models the "short" version of the ListTransactionsResult type, which
// excludes fields common to the transaction.  These common fields are instead
// part of the GetTransactionResult.
type GetTransactionDetailsResult struct {
	Account           string   `json:"account"`
	Address           string   `json:"address,omitempty"`
	Amount            float64  `json:"amount"`
	Category          string   `json:"category"`
	InvolvesWatchOnly bool     `json:"involveswatchonly,omitempty"`
	Fee               *float64 `json:"fee,omitempty"`
	Vout              uint32   `json:"vout"`
}

// GetTransactionResult models the data from the gettransaction command.
type GetTransactionResult struct {
	Amount          float64                       `json:"amount"`
	Fee             float64                       `json:"fee,omitempty"`
	Confirmations   int64                         `json:"confirmations"`
	BlockHash       string                        `json:"blockhash"`
	BlockIndex      int64                         `json:"blockindex"`
	BlockTime       int64                         `json:"blocktime"`
	TxID            string                        `json:"txid"`
	WalletConflicts []string                      `json:"walletconflicts"`
	Time            int64                         `json:"time"`
	TimeReceived    int64                         `json:"timereceived"`
	Details         []GetTransactionDetailsResult `json:"details"`
	Hex             string                        `json:"hex"`
	Type            string                        `json:"type"`
	TicketStatus    string                        `json:"ticketstatus,omitempty"`
}

// VoteChoice models the data for a vote choice in the getvotechoices result.
type VoteChoice struct {
	AgendaID          string `json:"agendaid"`
	AgendaDescription string `json:"agendadescription,omitempty"`
	ChoiceID          string `json:"choiceid"`
	ChoiceDescription string `json:"choicedescription,omitempty"`
}

// GetVoteChoicesResult models the data returned by the getvotechoices command.
type GetVoteChoicesResult struct {
	Version uint32       `json:"version"`
	Choices []VoteChoice `json:"choices"`
}

// InfoResult models the data returned by the wallet server getinfo
// command.
type InfoResult struct {
	Version         int32   `json:"version"`
	ProtocolVersion int32   `json:"protocolversion"`
	WalletVersion   int32   `json:"walletversion"`
	Balance         float64 `json:"balance"`
	Blocks          int32   `json:"blocks"`
	TimeOffset      int64   `json:"timeoffset"`
	Connections     int32   `json:"connections"`
	Proxy           string  `json:"proxy"`
	Difficulty      float64 `json:"difficulty"`
	TestNet         bool    `json:"testnet"`
	KeypoolOldest   int64   `json:"keypoololdest"`
	KeypoolSize     int32   `json:"keypoolsize"`
	UnlockedUntil   int64   `json:"unlocked_until"`
	PaytxFee        float64 `json:"paytxfee"`
	RelayFee        float64 `json:"relayfee"`
	Errors          string  `json:"errors"`
}

// InfoWalletResult aliases InfoResult.
type InfoWalletResult = InfoResult

// ScriptInfo is the structure representing a redeem script, its hash,
// and its address.
type ScriptInfo struct {
	Hash160      string `json:"hash160"`
	Address      string `json:"address"`
	RedeemScript string `json:"redeemscript"`
}

// ListTransactionsTxType defines the type used in the listtransactions JSON-RPC
// result for the TxType command field.
type ListTransactionsTxType string

const (
	// LTTTRegular indicates a regular transaction.
	LTTTRegular ListTransactionsTxType = "regular"

	// LTTTTicket indicates a ticket.
	LTTTTicket ListTransactionsTxType = "ticket"

	// LTTTVote indicates a vote.
	LTTTVote ListTransactionsTxType = "vote"

	// LTTTRevocation indicates a revocation.
	LTTTRevocation ListTransactionsTxType = "revocation"
)

// ListTransactionsResult models the data from the listtransactions command.
type ListTransactionsResult struct {
	Account           string                  `json:"account"`
	Address           string                  `json:"address,omitempty"`
	Amount            float64                 `json:"amount"`
	BlockHash         string                  `json:"blockhash,omitempty"`
	BlockIndex        *int64                  `json:"blockindex,omitempty"`
	BlockTime         int64                   `json:"blocktime,omitempty"`
	Category          string                  `json:"category"`
	Confirmations     int64                   `json:"confirmations"`
	Fee               *float64                `json:"fee,omitempty"`
	Generated         bool                    `json:"generated,omitempty"`
	InvolvesWatchOnly bool                    `json:"involveswatchonly,omitempty"`
	Time              int64                   `json:"time"`
	TimeReceived      int64                   `json:"timereceived"`
	TxID              string                  `json:"txid"`
	TxType            *ListTransactionsTxType `json:"txtype,omitempty"`
	Vout              uint32                  `json:"vout"`
	WalletConflicts   []string                `json:"walletconflicts"`
	Comment           string                  `json:"comment,omitempty"`
	OtherAccount      string                  `json:"otheraccount,omitempty"`
}

// ListReceivedByAccountResult models the data from the listreceivedbyaccount
// command.
type ListReceivedByAccountResult struct {
	Account       string  `json:"account"`
	Amount        float64 `json:"amount"`
	Confirmations uint64  `json:"confirmations"`
}

// ListReceivedByAddressResult models the data from the listreceivedbyaddress
// command.
type ListReceivedByAddressResult struct {
	Account           string   `json:"account"`
	Address           string   `json:"address"`
	Amount            float64  `json:"amount"`
	Confirmations     uint64   `json:"confirmations"`
	TxIDs             []string `json:"txids,omitempty"`
	InvolvesWatchonly bool     `json:"involvesWatchonly,omitempty"`
}

// ListSinceBlockResult models the data from the listsinceblock command.
type ListSinceBlockResult struct {
	Transactions []ListTransactionsResult `json:"transactions"`
	LastBlock    string                   `json:"lastblock"`
}

// ListUnspentResult models a successful response from the listunspent request.
// Contains Decred additions.
type ListUnspentResult struct {
	TxID          string  `json:"txid"`
	Vout          uint32  `json:"vout"`
	Tree          int8    `json:"tree"`
	TxType        int     `json:"txtype"`
	Address       string  `json:"address"`
	Account       string  `json:"account"`
	ScriptPubKey  string  `json:"scriptPubKey"`
	RedeemScript  string  `json:"redeemScript,omitempty"`
	Amount        float64 `json:"amount"`
	Confirmations int64   `json:"confirmations"`
	Spendable     bool    `json:"spendable"`
}

// RedeemMultiSigOutResult models the data returned from the redeemmultisigout
// command.
type RedeemMultiSigOutResult struct {
	Hex      string                    `json:"hex"`
	Complete bool                      `json:"complete"`
	Errors   []SignRawTransactionError `json:"errors,omitempty"`
}

// RedeemMultiSigOutsResult models the data returned from the redeemmultisigouts
// command.
type RedeemMultiSigOutsResult struct {
	Results []RedeemMultiSigOutResult `json:"results"`
}

// SendToMultiSigResult models the data returned from the sendtomultisig
// command.
type SendToMultiSigResult struct {
	TxHash       string `json:"txhash"`
	Address      string `json:"address"`
	RedeemScript string `json:"redeemscript"`
}

// SignRawTransactionError models the data that contains script verification
// errors from the signrawtransaction request.
type SignRawTransactionError struct {
	TxID      string `json:"txid"`
	Vout      uint32 `json:"vout"`
	ScriptSig string `json:"scriptSig"`
	Sequence  uint32 `json:"sequence"`
	Error     string `json:"error"`
}

// SignRawTransactionResult models the data from the signrawtransaction
// command.
type SignRawTransactionResult struct {
	Hex      string                    `json:"hex"`
	Complete bool                      `json:"complete"`
	Errors   []SignRawTransactionError `json:"errors,omitempty"`
}

// SignedTransaction is a signed transaction resulting from a signrawtransactions
// command.
type SignedTransaction struct {
	SigningResult SignRawTransactionResult `json:"signingresult"`
	Sent          bool                     `json:"sent"`
	TxHash        *string                  `json:"txhash,omitempty"`
}

// SignRawTransactionsResult models the data returned from the signrawtransactions
// command.
type SignRawTransactionsResult struct {
	Results []SignedTransaction `json:"results"`
}

// PoolUserTicket is the JSON struct corresponding to a stake pool user ticket
// object.
type PoolUserTicket struct {
	Status        string `json:"status"`
	Ticket        string `json:"ticket"`
	TicketHeight  uint32 `json:"ticketheight"`
	SpentBy       string `json:"spentby"`
	SpentByHeight uint32 `json:"spentbyheight"`
}

// StakePoolUserInfoResult models the data returned from the stakepooluserinfo
// command.
type StakePoolUserInfoResult struct {
	Tickets        []PoolUserTicket `json:"tickets"`
	InvalidTickets []string         `json:"invalid"`
}

// SweepAccountResult models the data returned from the sweepaccount
// command.
type SweepAccountResult struct {
	UnsignedTransaction       string  `json:"unsignedtransaction"`
	TotalPreviousOutputAmount float64 `json:"totalpreviousoutputamount"`
	TotalOutputAmount         float64 `json:"totaloutputamount"`
	EstimatedSignedSize       uint32  `json:"estimatedsignedsize"`
}

// TicketInfoResult models the data returned from the ticketinfo command.
type TicketInfoResult struct {
	Hash          string       `json:"hash"`
	Cost          float64      `json:"cost"`
	VotingAddress string       `json:"votingaddress"`
	Status        string       `json:"status"`
	BlockHash     string       `json:"blockhash,omitempty"`
	BlockHeight   int32        `json:"blockheight"`
	Vote          string       `json:"vote,omitempty"`
	Revocation    string       `json:"revocation,omitempty"`
	Choices       []VoteChoice `json:"choices,omitempty"`
}

// ValidateAddressResult models the data returned by the wallet server
// validateaddress command.
type ValidateAddressResult struct {
	IsValid      bool     `json:"isvalid"`
	Address      string   `json:"address,omitempty"`
	IsMine       bool     `json:"ismine,omitempty"`
	IsWatchOnly  bool     `json:"iswatchonly,omitempty"`
	IsScript     bool     `json:"isscript,omitempty"`
	PubKeyAddr   string   `json:"pubkeyaddr,omitempty"`
	PubKey       string   `json:"pubkey,omitempty"`
	IsCompressed bool     `json:"iscompressed,omitempty"`
	Account      string   `json:"account,omitempty"`
	Addresses    []string `json:"addresses,omitempty"`
	Hex          string   `json:"hex,omitempty"`
	Script       string   `json:"script,omitempty"`
	SigsRequired int32    `json:"sigsrequired,omitempty"`
}

// ValidateAddressWalletResult aliases ValidateAddressResult.
type ValidateAddressWalletResult = ValidateAddressResult

// WalletInfoResult models the data returned from the walletinfo command.
type WalletInfoResult struct {
	DaemonConnected  bool    `json:"daemonconnected"`
	Unlocked         bool    `json:"unlocked"`
	CoinType         uint32  `json:"cointype,omitempty"`
	TxFee            float64 `json:"txfee"`
	VoteBits         uint16  `json:"votebits"`
	VoteBitsExtended string  `json:"votebitsextended"`
	VoteVersion      uint32  `json:"voteversion"`
	Voting           bool    `json:"voting"`
	ManualTickets    bool    `json:"manualtickets"`
}
