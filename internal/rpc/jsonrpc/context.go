// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package jsonrpc

import "context"

type contextKey string

func withRemoteAddr(parent context.Context, remoteAddr string) context.Context {
	return context.WithValue(parent, contextKey("remote-addr"), remoteAddr)
}

func remoteAddr(ctx context.Context) string {
	v := ctx.Value(contextKey("remote-addr"))
	if v == nil {
		return "<unknown>"
	}
	return v.(string)
}
