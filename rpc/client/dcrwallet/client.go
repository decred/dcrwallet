package dcrwallet

import (
	"context"
	"encoding/json"

	"github.com/decred/dcrd/chaincfg/v3"
)

// Caller provides a client interface to perform JSON-RPC remote procedure calls.
type Caller interface {
	// Call performs the remote procedure call defined by method and
	// waits for a response or a broken client connection.
	// Args provides positional parameters for the call.
	// Res must be a pointer to a struct, slice, or map type to unmarshal
	// a result (if any), or nil if no result is needed.
	Call(ctx context.Context, method string, res interface{}, args ...interface{}) error
}

// RawRequester synchronously performs a JSON-RPC method with positional
// parameters.
type RawRequester interface {
	RawRequest(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error)
}

// RawRequestCaller wraps a RawRequester to provide a Caller implementation.
func RawRequestCaller(req RawRequester) Caller {
	return &rawRequester{req}
}

type rawRequester struct {
	req RawRequester
}

func (r *rawRequester) Call(ctx context.Context, method string, res interface{}, args ...interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		param, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, param)
	}
	resp, err := r.req.RawRequest(ctx, method, params)
	if err != nil {
		return err
	}
	return json.Unmarshal(resp, res)
}

// Client provides convenience methods for type-safe dcrwallet JSON-RPC usage.
type Client struct {
	Caller
	net *chaincfg.Params
}

// NewClient creates a new RPC client instance from a caller.
func NewClient(caller Caller, net *chaincfg.Params) *Client {
	return &Client{Caller: caller, net: net}
}
