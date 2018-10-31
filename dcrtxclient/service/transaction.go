package service

import (
	"bytes"
	"time"

	"github.com/decred/dcrd/wire"
	pb "github.com/decred/dcrwallet/dcrtxclient/api/matcherrpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type (
	TxService struct {
		client pb.SplitTxMatcherServiceClient
	}
)

// NewTxService creates new transaction service.
func NewTxService(conn *grpc.ClientConn) *TxService {
	return &TxService{
		client: pb.NewSplitTxMatcherServiceClient(conn),
	}
}

// JoinSplitTx sends join transaction request to server.
// When reach minimum required participant and ticker time on server will start join session.
// Each participant sends their inputs outputs transaction to server for merging.
func (t *TxService) JoinSplitTx(tx *wire.MsgTx, timeout uint32) (*wire.MsgTx, string, []int32, []int32, string, error) {

	joinReq := &pb.FindMatchesRequest{
		Amount: uint64(0),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Minute)
	defer cancel()
	findRes, err := t.client.FindMatches(ctx, joinReq)
	if err != nil {
		return nil, "", nil, nil, "", err
	}

	buffTx := bytes.NewBuffer(nil)
	buffTx.Grow(tx.SerializeSize())
	err = tx.BtcEncode(buffTx, 0)
	if err != nil {
		return nil, "", nil, nil, "", err
	}

	publishReq := &pb.SubmitInputTxReq{
		SessionId: findRes.SessionId,
		JoinId:    findRes.JoinId,
		SplitTx:   buffTx.Bytes(),
	}

	publishRes, err := t.client.SubmitSplitTx(context.Background(), publishReq)
	if err != nil {
		return nil, "", nil, nil, "", err
	}

	var ticket wire.MsgTx
	rbuf := bytes.NewReader(publishRes.TicketTx)
	err = ticket.BtcDecode(rbuf, 0)
	if err != nil {
		return nil, "", nil, nil, "", err
	}

	return &ticket, findRes.SessionId, publishRes.InputsIds, publishRes.OutputIds, findRes.JoinId, nil
}

// SubmitSignedTx submits signed participant's inputs to server.
// Server will join signed inputs, outputs of all participants and send back.
func (t *TxService) SubmitSignedTx(tx *wire.MsgTx, sesID string, joinId string) (*wire.MsgTx, bool, error) {

	buffTx := bytes.NewBuffer(nil)
	buffTx.Grow(tx.SerializeSize())
	err := tx.BtcEncode(buffTx, 0)
	if err != nil {
		return nil, false, err
	}

	req := &pb.SignTransactionRequest{
		SplitTx:   buffTx.Bytes(),
		JoinId:    joinId,
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

// PublishResult sends published transaction to server.
// If participant is selected for publish transantion, real transaction data is sent to server.
// If not, only sending nil data and waiting for real transaction data back from server.
func (t *TxService) PublishResult(tx *wire.MsgTx, sesID string, joinId string) (*wire.MsgTx, error) {
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
			JoinId:    joinId,
		}
	} else {
		req = &pb.PublishResultRequest{
			JoinedTx:  nil,
			SessionId: sesID,
			JoinId:    joinId,
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
