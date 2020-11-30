package vsp

const (
	codeBadRequest = iota
	codeInternalErr
	codeVspClosed
	codeFeeAlreadyReceived
	codeInvalidFeeTx
	codeFeeTooSmall
	codeUnknownTicket
	codeTicketCannotVote
	codeFeeExpired
	codeInvalidVoteChoices
	codeBadSignature
	codeInvalidPrivKey
	codeFeeNotReceived
	codeInvalidTicket
	codeCannotBroadcastTicket
	codeCannotBroadcastFee
	codeCannotBroadcastFeeUnknownOutputs
)
