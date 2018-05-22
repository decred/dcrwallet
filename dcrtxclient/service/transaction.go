package service

import (
	"bytes"

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

func (t *TransactionService) JoinSplitTx(tx *wire.MsgTx,
	voteAddress dcrutil.Address, ticketPrice dcrutil.Amount) (*wire.MsgTx, string, []int32, []int32, error) {

	joinReq := &pb.FindMatchesRequest{
		Amount: uint64(ticketPrice),
	}

	findRes, err := t.client.FindMatches(context.Background(), joinReq)
	if err != nil {
		return nil, "", nil, nil, err
	}
	//log.Infof("SessionID %v", findRes.SessionId)

	buffTx := bytes.NewBuffer(nil)
	buffTx.Grow(tx.SerializeSize())
	err = tx.BtcEncode(buffTx, 0)
	if err != nil {
		return nil, "", nil, nil, err
	}

	publishReq := &pb.SubmitInputTxReq{
		SessionId: findRes.SessionId,
		SplitTx:   buffTx.Bytes(),
	}

	publishRes, err := t.client.SubmitSplitTx(context.Background(), publishReq)
	if err != nil {
		return nil, "", nil, nil, err
	}

	var ticket wire.MsgTx
	rbuf := bytes.NewReader(publishRes.TicketTx)
	err = ticket.BtcDecode(rbuf, 0)
	if err != nil {
		return nil, "", nil, nil, err
	}

	//fmt.Println("JoinSplitTx end", publishRes.InputsIds, findRes.SessionId)
	return &ticket, findRes.SessionId, publishRes.InputsIds, publishRes.OutputIds, nil
}
func (t *TransactionService) PublishResult(tx *wire.MsgTx, sesID string) (*wire.MsgTx, error) {
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
func (t *TransactionService) SubmitSignedTx(tx *wire.MsgTx, sesID string) (*wire.MsgTx, bool, error) {

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
