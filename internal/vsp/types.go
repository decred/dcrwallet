package vsp

type FeeAddressRequest struct {
	Timestamp  int64  `json:"timestamp" `
	TicketHash string `json:"tickethash"`
	TicketHex  string `json:"tickethex"`
}

type FeeAddressResponse struct {
	Timestamp  int64             `json:"timestamp"`
	FeeAddress string            `json:"feeaddress"`
	FeeAmount  int64             `json:"feeamount"`
	Expiration int64             `json:"expiration"`
	Request    FeeAddressRequest `json:"request"`
}

type PayFeeRequest struct {
	Timestamp   int64             `json:"timestamp"`
	TicketHash  string            `json:"tickethash"`
	FeeTx       string            `json:"feetx"`
	VotingKey   string            `json:"votingkey" `
	VoteChoices map[string]string `json:"votechoices" `
}

type TicketStatusRequest struct {
	TicketHash string `json:"tickethash" `
}

type TicketStatusResponse struct {
	Timestamp       int64               `json:"timestamp"`
	TicketConfirmed bool                `json:"ticketconfirmed"`
	FeeTxStatus     string              `json:"feetxstatus"`
	FeeTxHash       string              `json:"feetxhash"`
	VoteChoices     map[string]string   `json:"votechoices"`
	Request         TicketStatusRequest `json:"request"`
}

type vspInfoResponse struct {
	Timestamp     int64   `json:"timestamp"`
	PubKey        []byte  `json:"pubkey"`
	FeePercentage float64 `json:"feepercentage"`
	VspClosed     bool    `json:"vspclosed"`
	Network       string  `json:"network"`
}
