package integration

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrwallet/v4/rpc/client/dcrwallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/peer/v3"
	"github.com/decred/dcrtest/dcrwtest"
	"github.com/decred/slog"
	"matheusd.com/testctx"
)

func init() {
	// NOTE: This MUST be updated when the main go.mod file is bumped.
	dcrwtest.SetDcrwalletMainPkg("decred.org/dcrwallet/v4")
}

// loggerWriter is an slog backend that writes to a test output.
type loggerWriter struct {
	l testing.TB
}

func (lw loggerWriter) Write(b []byte) (int, error) {
	bt := bytes.TrimRight(b, "\r\n")
	lw.l.Logf(string(bt))
	return len(b), nil

}

// setTestLogger sets the logger to log into the test. Cannot be used in
// parallel tests.
func setTestLogger(t testing.TB) {
	// Add logging to ease debugging this test.
	lw := loggerWriter{l: t}
	bknd := slog.NewBackend(lw)
	logger := bknd.Logger("TEST")
	logger.SetLevel(slog.LevelTrace)
	dcrwtest.UseLogger(logger)
	peer.UseLogger(logger)
	t.Cleanup(func() {
		dcrwtest.UseLogger(slog.Disabled)
	})
}

func decodeHashes(strs []string) ([]*chainhash.Hash, error) {
	hashes := make([]*chainhash.Hash, len(strs))
	for i, s := range strs {
		if len(s) != 2*chainhash.HashSize {
			return nil, fmt.Errorf("decode hex error")
		}
		hashes[i] = new(chainhash.Hash)
		_, err := hex.Decode(hashes[i][:], []byte(s))
		if err != nil {
			return nil, fmt.Errorf("decode hex error")
		}
		// unreverse hash string bytes
		for j := 0; j < 16; j++ {
			hashes[i][j], hashes[i][31-j] = hashes[i][31-j], hashes[i][j]
		}
	}
	return hashes, nil
}

func encodeHashes(hashes []chainhash.Hash) []string {
	res := make([]string, len(hashes))
	for i := range res {
		res[i] = hashes[i].String()
	}
	return res
}

func jrpcClient(t testing.TB, w *dcrwtest.Wallet) *dcrwallet.Client {
	t.Helper()
	ctx := testctx.WithTimeout(t, 30*time.Second)
	jctl := w.JsonRPCCtl()
	c, err := jctl.C(ctx)
	if err != nil {
		t.Fatal("timeout waiting for JSON-RPC client")

	}

	// Convert to the current client type.
	return dcrwallet.NewClient(c, w.ChainParams())
}
