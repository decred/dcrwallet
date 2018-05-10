package service

import (
	"bytes"
	"fmt"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	pb "github.com/raedahgroup/dcrtxmatcher/api/matcherrpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type (
	TransactionService struct {
		client pb.SplitTxMatcherServiceClient
	}
)

func NewTransactionService(conn *grpc.ClientConn) *TransactionService {
	return &TransactionService{
		client: pb.NewSplitTxMatcherServiceClient(conn),
	}
}

func (t *TransactionService) JoinSplitTx(tx *wire.MsgTx, voteAddress dcrutil.Address, ticketPrice dcrutil.Amount) (*wire.MsgTx, int32, []int32, error) {

	joinReq := &pb.FindMatchesRequest{
		Amount: uint64(ticketPrice),
	}

	findRes, err := t.client.FindMatches(context.Background(), joinReq)
	if err != nil {
		fmt.Println("FindMatches error", err)
		return nil, 0, nil, err
	}
	fmt.Println("FindMatches findRes \r\n", findRes.SessionId)

	buffTx := bytes.NewBuffer(nil)
	buffTx.Grow(tx.SerializeSize())
	err = tx.BtcEncode(buffTx, 0)
	if err != nil {
		return nil, 0, nil, err
	}

	publishReq := &pb.SubmitInputTxReq{
		SessionId: findRes.SessionId,
		SplitTx:   buffTx.Bytes(),
	}

	publishRes, err := t.client.PublishTicket(context.Background(), publishReq)
	if err != nil {
		return nil, 0, nil, err
	}

	var ticket wire.MsgTx
	rbuf := bytes.NewReader(publishRes.TicketTx)
	err = ticket.BtcDecode(rbuf, 0)
	if err != nil {
		return nil, 0, nil, err
	}

	fmt.Println("JoinSplitTx end", publishRes.InputsIds, findRes.SessionId)
	return &ticket, findRes.SessionId, publishRes.InputsIds, nil
}
func (t *TransactionService) PublishResult(tx *wire.MsgTx, sesID int32) (*wire.MsgTx, error) {
	req := &pb.PublishResultRequest{}
	if tx != nil {
		buffTx := bytes.NewBuffer(nil)
		buffTx.Grow(tx.SerializeSize())
		err := tx.BtcEncode(buffTx, 0)
		if err != nil {
			return nil, err
		}
		req = &pb.PublishResultRequest{
			JoinedTx:  buffTx.Bytes(),
			SessionId: sesID,
		}
	} else {
		req = &pb.PublishResultRequest{
			JoinedTx:  nil,
			SessionId: sesID,
		}
	}

	res, err := t.client.PublishResult(context.Background(), req)
	if err != nil {
		return nil, err
	}

	var signedTx wire.MsgTx
	buf := bytes.NewReader(res.TicketTx)
	err = signedTx.BtcDecode(buf, 0)
	if err != nil {
		return nil, err
	}

	return &signedTx, nil
}
func (t *TransactionService) SubmitSignedTx(tx *wire.MsgTx, sesID int32) (*wire.MsgTx, bool, error) {

	buffTx := bytes.NewBuffer(nil)
	buffTx.Grow(tx.SerializeSize())
	err := tx.BtcEncode(buffTx, 0)
	if err != nil {
		return nil, false, err
	}

	req := &pb.SignTransactionRequest{
		SplitTx:   buffTx.Bytes(),
		SessionId: sesID,
	}

	res, err := t.client.SubmitSignedTransaction(context.Background(), req)
	if err != nil {
		return nil, false, err
	}

	var signedTx wire.MsgTx
	buf := bytes.NewReader(res.TicketTx)
	err = signedTx.BtcDecode(buf, 0)
	if err != nil {
		return nil, false, err
	}

	return &signedTx, res.Publisher, nil
}

//func (t *TransactionService) JoinTransaction(tx *wire.MsgTx, voteAddress dcrutil.Address, ticketPrice dcrutil.Amount) (*wire.MsgTx, error) {
//	//canContribute := ticketPrice.MulF64(0.5) // Half of ticket price

//	fmt.Println("JoinTransaction begin \r\n")
//	joinReq := &pb.FindMatchesRequest{
//		Amount: uint64(ticketPrice),
//	}

//	findRes, err := t.client.FindMatches(context.Background(), joinReq)
//	fmt.Println("FindMatches findRes \r\n", findRes.SessionId)
//	if err != nil {
//		return nil, err
//	}

//	log.Println("tx.TxOut ", tx.TxOut[0].Value, tx.TxOut[1].Value, len(tx.TxOut))
//	log.Println("tx.TxIn ", tx.TxIn[0].ValueIn, len(tx.TxIn))

//	generateReq := &pb.GenerateTicketRequest{
//		SessionId: findRes.SessionId,
//		CommitmentOutput: &pb.TxOut{
//			Value:  uint64(tx.TxOut[0].Value),
//			Script: tx.TxOut[0].PkScript,
//		},
//		ChangeOutput: &pb.TxOut{
//			Value:  uint64(tx.TxOut[1].Value),
//			Script: tx.TxOut[1].PkScript,
//		},
//		VoteAddress: voteAddress.String(),
//	}

//	genRes, err := t.client.GenerateTicket(context.Background(), generateReq)
//	if err != nil {
//		return nil, err
//	}
//	log.Println("transaction.GenerateTicket ", uint64(tx.TxOut[0].Value), uint64(tx.TxOut[0].Value), genRes.OutputIndex)
//	buffTx := bytes.NewBuffer(nil)
//	buffTx.Grow(tx.SerializeSize())
//	err = tx.BtcEncode(buffTx, 0)
//	if err != nil {
//		return nil, err
//	}

//	publishReq := &pb.PublishTicketRequest{
//		SessionId:            findRes.SessionId,
//		SplitTx:              buffTx.Bytes(),
//		SplitTxOutputIndex:   genRes.OutputIndex,
//		TicketInputScriptsig: tx.TxIn[0].SignatureScript,
//	}

//	publishRes, err := t.client.PublishTicket(context.Background(), publishReq)
//	if err != nil {
//		return nil, err
//	}

//	var ticket wire.MsgTx
//	rbuf := bytes.NewReader(publishRes.TicketTx)
//	err = ticket.BtcDecode(rbuf, 0)
//	if err != nil {
//		return nil, err
//	}

//	fmt.Println("JoinTransaction end\r\n")
//	return &ticket, nil
//}
