package integration

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/peer/v3"
	"github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/wire"
	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"
)

type syncerProxy interface {
	blockCFiltersAfter(height uint32)
	run(ctx context.Context) error
	blocked() <-chan struct{}
}

type spvSyncerProxy struct {
	mu            sync.Mutex
	inListener    net.Listener
	cfBlockHeight uint32
	dcrd          *rpcclient.Client
	blockedChan   chan struct{}
}

func newSPVSyncerProxy(t testing.TB, rpcCfg rpcclient.ConnConfig) *spvSyncerProxy {
	inListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	rpcClient, err := rpcclient.New(&rpcCfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	return &spvSyncerProxy{
		inListener:  inListener,
		dcrd:        rpcClient,
		blockedChan: make(chan struct{}),
	}
}

func (proxy *spvSyncerProxy) addr() string {
	return proxy.inListener.Addr().String()
}

func (proxy *spvSyncerProxy) blockCFiltersAfter(height uint32) {
	proxy.cfBlockHeight = height
}

func (proxy *spvSyncerProxy) blocked() <-chan struct{} {
	return proxy.blockedChan
}

func (proxy *spvSyncerProxy) run(ctx context.Context) error {
	dcrd := proxy.dcrd

	inPeerCfg := peer.Config{
		UserAgentName:    "peer",
		UserAgentVersion: "1.0.0",
		Net:              wire.SimNet,
		Services:         wire.SFNodeNetwork | wire.SFNodeCF,
		IdleTimeout:      time.Second * 120,
		NewestBlock: func() (hash *chainhash.Hash, height int64, err error) {
			return dcrd.GetBestBlock(ctx)
		},
		Listeners: peer.MessageListeners{
			OnGetInitState: func(p *peer.Peer, msg *wire.MsgGetInitState) {
				p.QueueMessage(&wire.MsgInitState{}, nil)
			},
			OnPing: func(p *peer.Peer, msg *wire.MsgPing) {
				p.QueueMessage(&wire.MsgPong{Nonce: msg.Nonce}, nil)
			},
			OnGetHeaders: func(p *peer.Peer, msg *wire.MsgGetHeaders) {
				res, err := dcrd.GetHeaders(ctx, msg.BlockLocatorHashes, &msg.HashStop)
				if err != nil {
					return
				}
				nbHeaders := 100
				if len(res.Headers) < nbHeaders {
					nbHeaders = len(res.Headers)
				}
				resMsg := &wire.MsgHeaders{
					Headers: make([]*wire.BlockHeader, nbHeaders),
				}
				for i := 0; i < nbHeaders; i++ {
					b, _ := hex.DecodeString(res.Headers[i])
					resMsg.Headers[i] = new(wire.BlockHeader)
					resMsg.Headers[i].Deserialize(bytes.NewBuffer(b))
				}
				p.QueueMessage(resMsg, nil)
			},
			OnGetCFilterV2: func(p *peer.Peer, msg *wire.MsgGetCFilterV2) {
				// Do not return cf if instructed to block.
				header, err := dcrd.GetBlockHeader(ctx, &msg.BlockHash)
				if err != nil {
					return
				}
				if proxy.cfBlockHeight > 0 && header.Height >= proxy.cfBlockHeight {
					proxy.mu.Lock()
					select {
					case <-proxy.blockedChan:
					default:
						close(proxy.blockedChan)
					}
					proxy.mu.Unlock()
					return
				}

				// Return cfilter.
				res, err := dcrd.GetCFilterV2(ctx, &msg.BlockHash)
				if err != nil {
					return
				}
				resMsg := &wire.MsgCFilterV2{
					BlockHash:   msg.BlockHash,
					Data:        res.Filter.Bytes(),
					ProofIndex:  res.ProofIndex,
					ProofHashes: res.ProofHashes,
				}
				p.QueueMessage(resMsg, nil)
			},
			OnGetData: func(p *peer.Peer, msg *wire.MsgGetData) {
				var resMsg wire.Message
				for _, inv := range msg.InvList {
					switch inv.Type {
					case wire.InvTypeBlock:
						res, err := dcrd.GetBlock(ctx, &inv.Hash)
						if err != nil {
							return
						}
						resMsg = res
					}
				}
				if resMsg != nil {
					p.QueueMessage(resMsg, nil)
				}
			},
		},
	}

	go func() {
		<-ctx.Done()
		proxy.inListener.Close()
	}()

	for ctx.Err() == nil {
		// Wait until the wallet connects.
		inConn, err := proxy.inListener.Accept()
		if err != nil {
			return err
		}
		inPeer := peer.NewInboundPeer(&inPeerCfg)
		inPeer.AssociateConnection(inConn)
	}
	return ctx.Err()
}

type rpcSyncerProxy struct {
	mu            sync.Mutex
	inListener    net.Listener
	cfBlockHeight uint32
	dcrd          *rpcclient.Client
	blockedChan   chan struct{}
}

func newRPCSyncerProxy(t testing.TB, rpcCfg rpcclient.ConnConfig) *rpcSyncerProxy {
	inListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	rpcClient, err := rpcclient.New(&rpcCfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	proxy := &rpcSyncerProxy{
		inListener:  inListener,
		dcrd:        rpcClient,
		blockedChan: make(chan struct{}),
	}

	return proxy
}

func (proxy *rpcSyncerProxy) connConfig() rpcclient.ConnConfig {
	return rpcclient.ConnConfig{
		Host:       proxy.inListener.Addr().String(),
		Endpoint:   "ws",
		DisableTLS: true,
		User:       "none",
		Pass:       "none",
	}
}

func (proxy *rpcSyncerProxy) blockCFiltersAfter(height uint32) {
	proxy.cfBlockHeight = height
}

func (proxy *rpcSyncerProxy) blocked() <-chan struct{} {
	return proxy.blockedChan
}

func (proxy *rpcSyncerProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ws" {
		return
	}

	upgrader := websocket.Upgrader{}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Upgrade err", err)
		return
	}
	ws.SetPingHandler(func(payload string) error {
		return ws.WriteControl(websocket.PongMessage, []byte(payload),
			time.Time{})
	})
	ws.SetReadDeadline(time.Time{})
	ctx := r.Context()

	var g errgroup.Group
	g.Go(func() error {
		for ctx.Err() == nil {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return fmt.Errorf("ReadMessage err %v", err)
			}

			var req dcrjson.Request
			var result any
			err = json.Unmarshal(msg, &req)
			if err != nil {
				return fmt.Errorf("Unmarshal err %v", err)
			}

			method := types.Method(req.Method)
			params, err := dcrjson.ParseParams(method, req.Params)
			if err != nil {
				fmt.Println("ParseParams err", err)
			}

			switch req.Method {
			case "getcurrentnet":
				result = wire.SimNet
			case "version":
				result = map[string]types.VersionResult{
					// This will require a bump every time the
					// major json rpc version changes.
					"dcrdjsonrpcapi": {Major: 8, Minor: 99999, Patch: 99999},
				}
			case "getblockchaininfo":
				result, err = proxy.dcrd.GetBlockChainInfo(ctx)
				if err != nil {
					return fmt.Errorf("GetBlockChainInfo err %v", err)
				}

			case "getinfo":
				result, err = proxy.dcrd.GetInfo(ctx)
				if err != nil {
					return err
				}

			case "getheaders":
				c := params.(*types.GetHeadersCmd)
				locators, err := decodeHashes(c.BlockLocators)
				if err != nil {
					return err
				}
				var hashStop chainhash.Hash
				if c.HashStop != "" {
					err := chainhash.Decode(&hashStop, c.HashStop)
					if err != nil {
						return err
					}
				}

				res, err := proxy.dcrd.GetHeaders(ctx, locators, &hashStop)
				if err != nil {
					return err
				}

				if len(res.Headers) > 100 {
					res.Headers = res.Headers[:100]
				}
				result = res

			case "getcfilterv2":
				c := params.(*types.GetCFilterV2Cmd)
				hash, err := chainhash.NewHashFromStr(c.BlockHash)
				if err != nil {
					return fmt.Errorf("GetCFilterV2 hash err %v", err)
				}

				header, err := proxy.dcrd.GetBlockHeader(ctx, hash)
				if err != nil {
					return err
				}
				if proxy.cfBlockHeight > 0 && header.Height >= proxy.cfBlockHeight {
					proxy.mu.Lock()
					select {
					case <-proxy.blockedChan:
					default:
						close(proxy.blockedChan)
					}
					proxy.mu.Unlock()
				} else {
					res, err := proxy.dcrd.GetCFilterV2(ctx, hash)
					if err != nil {
						return fmt.Errorf("GetCFilterV2 err %v", err)
					}
					result = &types.GetCFilterV2Result{
						BlockHash:   res.BlockHash.String(),
						Data:        hex.EncodeToString(res.Filter.Bytes()),
						ProofIndex:  res.ProofIndex,
						ProofHashes: encodeHashes(res.ProofHashes),
					}
				}

			default:
				return fmt.Errorf("unhandled method %s\n", req.Method)
			}

			if result == nil {
				continue
			}

			reply, err := dcrjson.MarshalResponse(req.Jsonrpc, req.ID, result, nil)
			if err != nil {
				return fmt.Errorf("MarhsalResponse err %v", err)
			}
			err = ws.WriteMessage(websocket.TextMessage, reply)
			if err != nil {
				return fmt.Errorf("WriteMessage err %v", err)
			}
		}
		return ctx.Err()
	})
	_ = g.Wait()
	_ = ws.Close()
}

func (proxy *rpcSyncerProxy) run(ctx context.Context) error {
	srv := &http.Server{Handler: proxy}
	srv.Serve(proxy.inListener)
	<-ctx.Done()
	srv.Close()
	return ctx.Err()
}
