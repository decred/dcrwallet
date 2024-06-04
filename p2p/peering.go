// Copyright (c) 2018-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/lru"
	"decred.org/dcrwallet/v4/version"
	"github.com/decred/dcrd/addrmgr/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/connmgr/v3"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/gcs/v4"
	blockcf "github.com/decred/dcrd/gcs/v4/blockcf2"
	"github.com/decred/dcrd/mixing"
	"github.com/decred/dcrd/wire"
	"github.com/decred/go-socks/socks"
	"golang.org/x/sync/errgroup"
)

// uaName is the LocalPeer useragent name.
const uaName = "dcrwallet"

// uaVersion is the LocalPeer useragent version.
var uaVersion = version.String()

// minPver is the minimum protocol version we require remote peers to
// implement.
const minPver = wire.RemoveRejectVersion

// Pver is the maximum protocol version implemented by the LocalPeer.
const Pver = wire.BatchedCFiltersV2Version

// stallTimeout is the amount of time allowed before a request to receive data
// that is known to exist at the RemotePeer times out with no matching reply.
const stallTimeout = 30 * time.Second

const banThreshold = 100

const invLRUSize = 5000

type msgAck struct {
	msg wire.Message
	ack chan<- struct{}
}

type blockRequest struct {
	hash  *chainhash.Hash
	ready chan struct{}
	block *wire.MsgBlock
	err   error
}

// RemotePeer represents a remote peer that can send and receive wire protocol
// messages with the local peer.  RemotePeers must be created by dialing the
// peer's address with a LocalPeer.
type RemotePeer struct {
	atomicClosed atomic.Uint64

	id         uint64
	lp         *LocalPeer
	ua         string
	services   wire.ServiceFlag
	pver       uint32
	initHeight int32
	raddr      net.Addr
	na         *addrmgr.NetAddress

	// io
	c       net.Conn
	mr      msgReader
	out     chan *msgAck
	outPrio chan *msgAck
	pongs   chan *wire.MsgPong

	// requestedBlocksMu controls access to both the requestedBlocks map
	// *and* its contents. Changes to individual *blockRequests MUST only
	// be made under a locked requestedBlocksMu.
	requestedBlocksMu sync.Mutex
	requestedBlocks   map[chainhash.Hash]*blockRequest

	requestedCFiltersV2     sync.Map // k=chainhash.Hash v=chan<- *wire.MsgCFilterV2
	requestedManyCFiltersV2 sync.Map // k=chainhash.Hash v=chan<- *wire.MsgCFiltersV2

	requestedTxs   map[chainhash.Hash]chan<- *wire.MsgTx
	requestedTxsMu sync.Mutex

	requestedMixMsgs   map[chainhash.Hash]chan<- mixing.Message
	requestedMixMsgsMu sync.Mutex

	// headers message management.  Headers can either be fetched synchronously
	// or used to push block notifications with sendheaders.
	requestedHeaders   chan<- *wire.MsgHeaders // non-nil result chan when synchronous getheaders in process
	sendheaders        bool                    // whether a sendheaders message was sent
	requestedHeadersMu sync.Mutex

	// init state message management.
	requestedInitState   chan<- *wire.MsgInitState // non-nil result chan when synchronous getinitstate in process
	requestedInitStateMu sync.Mutex

	invsSent lru.Cache[chainhash.Hash] // Hashes from sent inventory messages
	invsRecv lru.Cache[chainhash.Hash] // Hashes of received inventory messages
	banScore connmgr.DynamicBanScore

	// Height of the last header received via getheaders or header ann.
	lastHeight   int32
	lastHeightMu sync.Mutex

	err  error         // Final error of disconnected peer
	errc chan struct{} // Closed after err is set
}

// LocalPeer represents the local peer that can send and receive wire protocol
// messages with remote peers on the network.
type LocalPeer struct {
	atomicMask          atomic.Uint64
	atomicPeerIDCounter atomic.Uint64
	atomicRequireHeight atomic.Int32

	dial DialFunc

	receivedGetData   chan *inMsg
	receivedHeaders   chan *inMsg
	receivedInv       chan *inMsg
	announcedHeaders  chan *inMsg
	receivedInitState chan *inMsg
	receivedMixMsg    chan *inMsg

	extaddr        net.Addr
	amgr           *addrmgr.AddrManager
	chainParams    *chaincfg.Params
	disableRelayTx bool

	rpByID map[uint64]*RemotePeer
	rpMu   sync.Mutex
}

// NewLocalPeer creates a LocalPeer that is externally reachable to remote peers
// through extaddr.
func NewLocalPeer(params *chaincfg.Params, extaddr *net.TCPAddr, amgr *addrmgr.AddrManager) *LocalPeer {
	var dialer net.Dialer
	lp := &LocalPeer{
		dial:              dialer.DialContext,
		receivedGetData:   make(chan *inMsg),
		receivedHeaders:   make(chan *inMsg),
		receivedInv:       make(chan *inMsg),
		announcedHeaders:  make(chan *inMsg),
		receivedInitState: make(chan *inMsg),
		receivedMixMsg:    make(chan *inMsg),
		extaddr:           extaddr,
		amgr:              amgr,
		chainParams:       params,
		rpByID:            make(map[uint64]*RemotePeer),
	}
	return lp
}

// DialFunc provides a method to dial a network connection.
type DialFunc func(ctx context.Context, net, addr string) (net.Conn, error)

// SetDialFunc sets the function used to dial peer and seeder connections.
func (lp *LocalPeer) SetDialFunc(dial DialFunc) {
	lp.dial = dial
}

// SetDisableRelayTx sets whether remote peers will be asked to relay
// transactions to the local peer. This must be called before the local peer
// runs.
func (lp *LocalPeer) SetDisableRelayTx(disableRelayTx bool) {
	lp.disableRelayTx = disableRelayTx
}

func isCGNAT(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 100 && ip4[1]&0xc0 == 64 // 100.64.0.0/10
	}
	return false
}

func newNetAddress(addr net.Addr, services wire.ServiceFlag) (*wire.NetAddress, error) {
	var ip net.IP
	var port uint16
	switch a := addr.(type) {
	case *net.TCPAddr:
		ip = a.IP
		port = uint16(a.Port)
	case *socks.ProxiedAddr:
		ip = net.ParseIP(a.Host)
		port = uint16(a.Port)
	default:
		return nil, fmt.Errorf("newNetAddress: unsupported address "+
			"type %T", addr)
	}
	switch {
	case ip.IsLoopback(), ip.IsPrivate(), !ip.IsGlobalUnicast(), isCGNAT(ip):
		ip = nil
		port = 0
	}
	return wire.NewNetAddressIPPort(ip, port, services), nil
}

func (lp *LocalPeer) newMsgVersion(pver uint32, c net.Conn) (*wire.MsgVersion, error) {
	la := new(wire.NetAddress)
	ra, err := newNetAddress(c.RemoteAddr(), 0)
	if err != nil {
		return nil, err
	}
	nonce, err := wire.RandomUint64()
	if err != nil {
		return nil, err
	}
	v := wire.NewMsgVersion(la, ra, nonce, 0)
	v.AddUserAgent(uaName, uaVersion)
	v.ProtocolVersion = int32(pver)
	v.DisableRelayTx = lp.disableRelayTx
	return v, nil
}

// RequirePeerHeight sets the minimum height a peer must advertise during its
// handshake.  Peers advertising below this height will error during the
// handshake, and will not be marked as good peers in the address manager.
func (lp *LocalPeer) RequirePeerHeight(requiredHeight int32) {
	lp.atomicRequireHeight.Store(requiredHeight)
}

// ConnectOutbound establishes a connection to a remote peer by their remote TCP
// address.  The peer is serviced in the background until the context is
// cancelled, the RemotePeer disconnects, times out, misbehaves, or the
// LocalPeer disconnects all peers.
func (lp *LocalPeer) ConnectOutbound(ctx context.Context, addr string, reqSvcs wire.ServiceFlag) (*RemotePeer, error) {
	const opf = "localpeer.ConnectOutbound(%v)"

	log.Debugf("Attempting connection to peer %v", addr)

	// Generate a unique ID for this peer and add the initial connection state.
	id := lp.atomicPeerIDCounter.Add(1)

	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}

	// Create a net address with assumed services.
	na := addrmgr.NewNetAddressIPPort(tcpAddr.IP, uint16(tcpAddr.Port), wire.SFNodeNetwork)
	na.Timestamp = time.Now()

	rp, err := lp.connectOutbound(ctx, id, addr, na)
	if err != nil {
		op := errors.Opf(opf, addr)
		return nil, errors.E(op, err)
	}

	go lp.serveUntilError(ctx, rp)

	// Disconnect from the peer if it does not specify all required services.
	if rp.services&reqSvcs != reqSvcs {
		op := errors.Opf(opf, rp.raddr)
		err := errors.E(op, errors.Errorf("missing required service flags %v",
			reqSvcs&^rp.services))
		rp.Disconnect(err)
		return nil, err
	}

	// Disconnect from the peer if its advertised last height is below our
	// required minimum.
	reqHeight := lp.atomicRequireHeight.Load()
	if rp.initHeight < reqHeight {
		op := errors.Opf(opf, rp.raddr)
		err := errors.E(op, "peer is not synced")
		rp.Disconnect(err)
		return nil, err
	}

	// Mark this as a good address.
	lp.amgr.Good(na)

	if lp.amgr.NeedMoreAddresses() {
		err = rp.Addrs(ctx)
		if err != nil {
			rp.Disconnect(err)
			return nil, err
		}
	}

	return rp, nil
}

// AddrManager returns the local peer's address manager.
func (lp *LocalPeer) AddrManager() *addrmgr.AddrManager { return lp.amgr }

// NA returns the remote peer's net address.
func (rp *RemotePeer) NA() *addrmgr.NetAddress { return rp.na }

// UA returns the remote peer's user agent.
func (rp *RemotePeer) UA() string { return rp.ua }

// ID returns the remote ID.
func (rp *RemotePeer) ID() uint64 { return rp.id }

// InitialHeight returns the current height the peer advertised in its version
// message.
func (rp *RemotePeer) InitialHeight() int32 { return rp.initHeight }

// LastHeight returns the height of the last header the peer sent through a
// headers message.
func (rp *RemotePeer) LastHeight() int32 {
	rp.lastHeightMu.Lock()
	h := rp.lastHeight
	rp.lastHeightMu.Unlock()
	return h
}

// Services returns the remote peer's advertised service flags.
func (rp *RemotePeer) Services() wire.ServiceFlag { return rp.services }

// InvsSent returns an LRU cache of inventory hashes sent to the remote peer.
func (rp *RemotePeer) InvsSent() *lru.Cache[chainhash.Hash] { return &rp.invsSent }

// InvsRecv returns an LRU cache of inventory hashes received by the remote
// peer.
func (rp *RemotePeer) InvsRecv() *lru.Cache[chainhash.Hash] { return &rp.invsRecv }

// SeedPeers seeds the local peer with remote addresses matching the
// services.
func (lp *LocalPeer) SeedPeers(ctx context.Context, services wire.ServiceFlag) {
	seeders := lp.chainParams.Seeders()
	url := &url.URL{
		Scheme:   "https",
		Path:     "/api/addrs",
		RawQuery: fmt.Sprintf("services=%d", services),
	}
	resps := make(chan *http.Response)
	client := http.Client{
		Transport: &http.Transport{
			DialContext: lp.dial,
		},
	}
	cancels := make([]func(), 0, len(seeders))
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	for _, host := range seeders {
		host := host
		url.Host = host
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		cancels = append(cancels, cancel)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
		if err != nil {
			log.Errorf("Bad seeder request: %v", err)
			continue
		}
		go func() {
			resp, err := client.Do(req)
			if err != nil {
				log.Warnf("Failed to seed addresses from %s: %v", host, err)
				resp = nil
			}
			resps <- resp
		}()
	}
	var na []*addrmgr.NetAddress
	for range seeders {
		resp := <-resps
		if resp == nil {
			continue
		}
		seeder := resp.Request.Host
		var apiResponse struct {
			Host     string `json:"host"`
			Services uint64 `json:"services"`
		}
		dec := json.NewDecoder(io.LimitReader(resp.Body, 4096))
		na = na[:0]
		// Read at most 16 entries from each seeder, discard rest
		for i := 0; i < 16; i++ {
			err := dec.Decode(&apiResponse)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				log.Warnf("Invalid seeder %v API response: %v", seeder, err)
				break
			}
			host, port, err := net.SplitHostPort(apiResponse.Host)
			if err != nil {
				log.Warnf("Invalid host in seeder %v API: %v", seeder, err)
				continue
			}
			ip := net.ParseIP(host)
			if ip == nil {
				log.Warnf("Invalid IP address %q in seeder %v API host field",
					host, seeder)
				continue
			}
			portNum, err := strconv.ParseUint(port, 10, 16)
			if err != nil {
				log.Warnf("Invalid port %q in seeder %v API host field", port,
					seeder)
				continue
			}
			log.Debugf("Discovered peer %v from seeder %v", apiResponse.Host,
				seeder)
			na = append(na, &addrmgr.NetAddress{
				IP:        ip,
				Port:      uint16(portNum),
				Timestamp: time.Now(),
				Services:  wire.ServiceFlag(apiResponse.Services),
			})
		}
		resp.Body.Close()
		if len(na) > 0 {
			lp.amgr.AddAddresses(na, na[0])
		}
	}
}

type msgReader struct {
	r      io.Reader
	net    wire.CurrencyNet
	msg    wire.Message
	rawMsg []byte
	err    error
}

func (mr *msgReader) next(pver uint32) bool {
	mr.msg, mr.rawMsg, mr.err = wire.ReadMessage(mr.r, pver, mr.net)
	return mr.err == nil
}

func (rp *RemotePeer) writeMessages(ctx context.Context) error {
	e := make(chan error, 1)
	go func() {
		c := rp.c
		pver := rp.pver
		cnet := rp.lp.chainParams.Net
		for {
			var m *msgAck
			select {
			case m = <-rp.outPrio:
			default:
				select {
				case m = <-rp.outPrio:
				case m = <-rp.out:
				}
			}
			log.Debugf("%v -> %v", m.msg.Command(), rp.raddr)
			err := wire.WriteMessage(c, m.msg, pver, cnet)
			if m.ack != nil {
				m.ack <- struct{}{}
			}
			if err != nil {
				e <- err
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-e:
		return err
	}
}

type msgWriter struct {
	w   io.Writer
	net wire.CurrencyNet
}

func (mw *msgWriter) write(ctx context.Context, msg wire.Message, pver uint32) error {
	e := make(chan error, 1)
	go func() {
		e <- wire.WriteMessage(mw.w, msg, pver, mw.net)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-e:
		return err
	}
}

func handshake(ctx context.Context, lp *LocalPeer, id uint64, na *addrmgr.NetAddress, c net.Conn) (*RemotePeer, error) {
	const op errors.Op = "p2p.handshake"

	rp := &RemotePeer{
		id:               id,
		lp:               lp,
		ua:               "",
		services:         0,
		pver:             Pver,
		raddr:            c.RemoteAddr(),
		na:               na,
		c:                c,
		mr:               msgReader{r: c, net: lp.chainParams.Net},
		out:              nil,
		outPrio:          nil,
		pongs:            make(chan *wire.MsgPong, 1),
		requestedBlocks:  make(map[chainhash.Hash]*blockRequest),
		requestedTxs:     make(map[chainhash.Hash]chan<- *wire.MsgTx),
		requestedMixMsgs: make(map[chainhash.Hash]chan<- mixing.Message),
		invsSent:         lru.NewCache[chainhash.Hash](invLRUSize),
		invsRecv:         lru.NewCache[chainhash.Hash](invLRUSize),
		errc:             make(chan struct{}),
	}

	mw := msgWriter{c, lp.chainParams.Net}

	// The first message sent must be the version message.
	lversion, err := lp.newMsgVersion(rp.pver, c)
	if err != nil {
		return nil, errors.E(op, err)
	}
	err = mw.write(ctx, lversion, rp.pver)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}

	// The first message received must also be a version message.
	err = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	msg, _, err := wire.ReadMessage(c, Pver, lp.chainParams.Net)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	rversion, ok := msg.(*wire.MsgVersion)
	if !ok {
		return nil, errors.E(op, errors.Protocol, "first received message was not the version message")
	}
	rp.initHeight = rversion.LastBlock
	rp.services = rversion.Services
	rp.ua = rversion.UserAgent
	c.SetReadDeadline(time.Time{})

	// Negotiate protocol down to compatible version
	if uint32(rversion.ProtocolVersion) < minPver {
		return nil, errors.E(op, errors.Protocol, "remote peer has pver lower than minimum required")
	}
	if uint32(rversion.ProtocolVersion) < rp.pver {
		rp.pver = uint32(rversion.ProtocolVersion)
	}

	// Send the verack.  The received verack is ignored.
	err = mw.write(ctx, wire.NewMsgVerAck(), rp.pver)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}

	rp.out = make(chan *msgAck)
	rp.outPrio = make(chan *msgAck)

	return rp, nil
}

func (lp *LocalPeer) connectOutbound(ctx context.Context, id uint64, addr string,
	na *addrmgr.NetAddress) (*RemotePeer, error) {

	// Mark the connection attempt.
	lp.amgr.Attempt(na)

	// Dial with a timeout of 10 seconds.
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	c, err := lp.dial(dialCtx, "tcp", addr)
	cancel()
	if err != nil {
		return nil, err
	}

	lp.amgr.Connected(na)

	// Handshake with a timeout of 5s.
	handshakeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	rp, err := handshake(handshakeCtx, lp, id, na, c)
	cancel()
	if err != nil {
		return nil, err
	}

	// Associate connected rp with local peer.
	lp.rpMu.Lock()
	lp.rpByID[rp.id] = rp
	lp.rpMu.Unlock()

	// The real services of the net address are now known.
	na.Services = rp.services

	return rp, nil
}

func (lp *LocalPeer) serveUntilError(ctx context.Context, rp *RemotePeer) {
	defer func() {
		// Remove from local peer
		log.Debugf("Disconnected from outbound peer %v", rp.raddr)
		lp.rpMu.Lock()
		delete(lp.rpByID, rp.id)
		lp.rpMu.Unlock()
	}()

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		<-gctx.Done()
		rp.Disconnect(gctx.Err())
		rp.c.Close()
		return nil
	})
	g.Go(func() (err error) {
		defer func() {
			if err != nil && gctx.Err() == nil {
				log.Debugf("remotepeer(%v).readMessages: %v", rp.raddr, err)
			}
		}()
		return rp.readMessages(gctx)
	})
	g.Go(func() (err error) {
		defer func() {
			if err != nil && gctx.Err() == nil {
				log.Debugf("syncWriter(%v).write: %v", rp.raddr, err)
			}
		}()
		return rp.writeMessages(gctx)
	})
	g.Go(func() error {
		for {
			select {
			case <-gctx.Done():
				return gctx.Err()
			case <-time.After(2 * time.Minute):
				ctx, cancel := context.WithDeadline(gctx, time.Now().Add(15*time.Second))
				rp.pingPong(ctx)
				cancel()
			}
		}
	})
	err := g.Wait()
	if err != nil {
		rp.Disconnect(err)
	}
}

// ErrDisconnected describes the error of a remote peer being disconnected by
// the local peer.  While the disconnection may be clean, other methods
// currently being called on the peer must return this as a non-nil error.
var ErrDisconnected = errors.New("peer has been disconnected")

// Disconnect closes the underlying TCP connection to a RemotePeer.  A nil
// reason is replaced with ErrDisconnected.
func (rp *RemotePeer) Disconnect(reason error) {
	if !rp.atomicClosed.CompareAndSwap(0, 1) {
		// Already disconnected
		return
	}
	log.Debugf("Disconnecting %v", rp.raddr)
	rp.c.Close()
	if reason == nil {
		reason = ErrDisconnected
	}
	rp.err = reason
	close(rp.errc)
}

// Err blocks until the RemotePeer disconnects, returning the reason for
// disconnection.
func (rp *RemotePeer) Err() error {
	<-rp.errc
	return rp.err
}

// Done returns a channel that is closed once the peer disconnects.
func (rp *RemotePeer) Done() <-chan struct{} {
	return rp.errc
}

// RemoteAddr returns the remote address of the peer's TCP connection.
func (rp *RemotePeer) RemoteAddr() net.Addr {
	return rp.c.RemoteAddr()
}

// LocalAddr returns the local address of the peer's TCP connection.
func (rp *RemotePeer) LocalAddr() net.Addr {
	return rp.c.LocalAddr()
}

// BanScore returns the banScore of the peer's.
func (rp *RemotePeer) BanScore() uint32 {
	return rp.banScore.Int()
}

// Pver returns the negotiated protocol version.
func (rp *RemotePeer) Pver() uint32 { return rp.pver }

func (rp *RemotePeer) String() string {
	return rp.raddr.String()
}

type inMsg struct {
	rp  *RemotePeer
	msg wire.Message
}

var inMsgPool = sync.Pool{
	New: func() any { return new(inMsg) },
}

func newInMsg(rp *RemotePeer, msg wire.Message) *inMsg {
	m := inMsgPool.Get().(*inMsg)
	m.rp = rp
	m.msg = msg
	return m
}

func recycleInMsg(m *inMsg) {
	*m = inMsg{}
	inMsgPool.Put(m)
}

func (rp *RemotePeer) readMessages(ctx context.Context) error {
	for rp.mr.next(rp.pver) {
		msg := rp.mr.msg
		log.Debugf("%v <- %v", msg.Command(), rp.raddr)
		if _, ok := msg.(*wire.MsgVersion); ok {
			// TODO: reject duplicate version message
			return errors.E(errors.Protocol, "received unexpected version message")
		}
		go func() {
			switch m := msg.(type) {
			case *wire.MsgAddr:
				rp.receivedAddr(ctx, m)
			case *wire.MsgBlock:
				rp.receivedBlock(ctx, m)
			case *wire.MsgCFilterV2:
				rp.receivedCFilterV2(ctx, m)
			case *wire.MsgCFiltersV2:
				rp.receivedManyCFilterV2(ctx, m)
			case *wire.MsgNotFound:
				rp.receivedNotFound(ctx, m)
			case *wire.MsgTx:
				rp.receivedTx(ctx, m)
			case *wire.MsgGetData:
				rp.receivedGetData(ctx, m)
			case *wire.MsgHeaders:
				rp.receivedHeaders(ctx, m)
			case *wire.MsgInv:
				rp.receivedInv(ctx, m)
			case *wire.MsgGetMiningState:
				rp.receivedGetMiningState(ctx)
			case *wire.MsgGetInitState:
				rp.receivedGetInitState(ctx)
			case *wire.MsgInitState:
				rp.receivedInitState(ctx, m)
			case *wire.MsgPing:
				pong(ctx, m, rp)
			case *wire.MsgPong:
				rp.receivedPong(ctx, m)

			case *wire.MsgMixPairReq:
				rp.receivedMixMsg(ctx, m)
			case *wire.MsgMixKeyExchange:
				rp.receivedMixMsg(ctx, m)
			case *wire.MsgMixCiphertexts:
				rp.receivedMixMsg(ctx, m)
			case *wire.MsgMixSlotReserve:
				rp.receivedMixMsg(ctx, m)
			case *wire.MsgMixDCNet:
				rp.receivedMixMsg(ctx, m)
			case *wire.MsgMixConfirm:
				rp.receivedMixMsg(ctx, m)
			case *wire.MsgMixFactoredPoly:
				rp.receivedMixMsg(ctx, m)
			case *wire.MsgMixSecrets:
				rp.receivedMixMsg(ctx, m)
			}
		}()
	}
	return rp.mr.err
}

func pong(ctx context.Context, ping *wire.MsgPing, rp *RemotePeer) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	select {
	case <-ctx.Done():
	case rp.outPrio <- &msgAck{wire.NewMsgPong(ping.Nonce), nil}:
	}
}

// MessageMask is a bitmask of message types that can be received and handled by
// consumers of this package by calling various Receive* methods on a LocalPeer.
// Received messages not in the mask are ignored and not receiving messages in
// the mask will leak goroutines.  Handled messages can be added and removed by
// using the AddHandledMessages and RemoveHandledMessages methods of a
// LocalPeer.
type MessageMask uint64

// Message mask constants
const (
	MaskGetData MessageMask = 1 << iota
	MaskInv
)

// AddHandledMessages adds all messages defined by the bitmask.  This operation
// is concurrent-safe.
func (lp *LocalPeer) AddHandledMessages(mask MessageMask) {
	for {
		p := lp.atomicMask.Load()
		n := p | uint64(mask)
		if lp.atomicMask.CompareAndSwap(p, n) {
			return
		}
	}
}

// RemoveHandledMessages removes all messages defined by the bitmask.  This
// operation is concurrent safe.
func (lp *LocalPeer) RemoveHandledMessages(mask MessageMask) {
	for {
		p := lp.atomicMask.Load()
		n := p &^ uint64(mask)
		if lp.atomicMask.CompareAndSwap(p, n) {
			return
		}
	}
}

func (lp *LocalPeer) messageIsMasked(m MessageMask) bool {
	return lp.atomicMask.Load()&uint64(m) != 0
}

// ReceiveGetData waits for a getdata message from a remote peer, returning the
// peer that sent the message, and the message itself.
func (lp *LocalPeer) ReceiveGetData(ctx context.Context) (*RemotePeer, *wire.MsgGetData, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case r := <-lp.receivedGetData:
		rp, msg := r.rp, r.msg.(*wire.MsgGetData)
		recycleInMsg(r)
		return rp, msg, nil
	}
}

// ReceiveInv waits for an inventory message from a remote peer, returning the
// peer that sent the message, and the message itself.
func (lp *LocalPeer) ReceiveInv(ctx context.Context) (*RemotePeer, *wire.MsgInv, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case r := <-lp.receivedInv:
		rp, msg := r.rp, r.msg.(*wire.MsgInv)
		recycleInMsg(r)
		return rp, msg, nil
	}
}

// ReceiveHeadersAnnouncement returns any unrequested headers that were
// announced without an inventory message due to a previous sendheaders request.
func (lp *LocalPeer) ReceiveHeadersAnnouncement(ctx context.Context) (*RemotePeer, []*wire.BlockHeader, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case r := <-lp.announcedHeaders:
		rp, msg := r.rp, r.msg.(*wire.MsgHeaders)
		recycleInMsg(r)
		return rp, msg.Headers, nil
	}
}

// ReceiveMixMessage waits for a mixing message from a remote peer, returning
// the peer that sent the message, and the message itself.
func (lp *LocalPeer) ReceiveMixMessage(ctx context.Context) (*RemotePeer, mixing.Message, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case r := <-lp.receivedMixMsg:
		rp, msg := r.rp, r.msg.(mixing.Message)
		recycleInMsg(r)
		return rp, msg, nil
	}
}

func (rp *RemotePeer) pingPong(ctx context.Context) {
	nonce, err := wire.RandomUint64()
	if err != nil {
		log.Errorf("Failed to generate random ping nonce: %v", err)
		return
	}
	select {
	case <-ctx.Done():
		return
	case rp.outPrio <- &msgAck{wire.NewMsgPing(nonce), nil}:
	}
	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			err := errors.E(errors.IO, "ping timeout")
			rp.Disconnect(err)
		}
	case pong := <-rp.pongs:
		if pong.Nonce != nonce {
			err := errors.E(errors.Protocol, "pong contains nonmatching nonce")
			rp.Disconnect(err)
		}
	}
}

func (rp *RemotePeer) receivedPong(ctx context.Context, msg *wire.MsgPong) {
	select {
	case <-ctx.Done():
	case rp.pongs <- msg:
	}
}

func (rp *RemotePeer) receivedAddr(ctx context.Context, msg *wire.MsgAddr) {
	addrs := make([]*addrmgr.NetAddress, len(msg.AddrList))
	for i, a := range msg.AddrList {
		addrs[i] = &addrmgr.NetAddress{
			IP:        a.IP,
			Port:      a.Port,
			Services:  a.Services,
			Timestamp: a.Timestamp,
		}
	}
	rp.lp.amgr.AddAddresses(addrs, rp.na)
}

func (rp *RemotePeer) receivedBlock(ctx context.Context, msg *wire.MsgBlock) {
	const opf = "remotepeer(%v).receivedBlock(%v)"
	blockHash := msg.Header.BlockHash()

	// Acquire the lock so we can work with the relevant blockRequest.
	rp.requestedBlocksMu.Lock()
	req := rp.requestedBlocks[blockHash]
	if req == nil {
		rp.requestedBlocksMu.Unlock()
		op := errors.Opf(opf, rp.raddr, &blockHash)
		err := errors.E(op, errors.Protocol, "received unrequested block")
		rp.Disconnect(err)
		return
	}
	select {
	case <-req.ready:
		// Already have a resolution for this block.
	default:
		req.block = msg
		close(req.ready)
	}
	rp.requestedBlocksMu.Unlock()
}

func (rp *RemotePeer) addRequestedManyCFilterV2(hash *chainhash.Hash, c chan<- *wire.MsgCFiltersV2) (newRequest bool) {
	_, loaded := rp.requestedManyCFiltersV2.LoadOrStore(*hash, c)
	return !loaded
}

func (rp *RemotePeer) deleteRequestedManyCFilterV2(hash *chainhash.Hash) {
	rp.requestedManyCFiltersV2.Delete(*hash)
}

func (rp *RemotePeer) receivedManyCFilterV2(ctx context.Context, msg *wire.MsgCFiltersV2) {
	const opf = "remotepeer(%v).receivedCFilterV2(%v)"
	if len(msg.CFilters) == 0 {
		op := errors.Opf(opf, rp.raddr, chainhash.Hash{})
		err := errors.E(op, errors.Protocol, "received empty cfiltersv2 message")
		rp.Disconnect(err)
		return

	}

	var k any = msg.CFilters[0].BlockHash
	v, ok := rp.requestedManyCFiltersV2.Load(k)
	if !ok {
		op := errors.Opf(opf, rp.raddr, k)
		err := errors.E(op, errors.Protocol, "received unrequested many cfilter")
		rp.Disconnect(err)
		return
	}

	rp.requestedManyCFiltersV2.Delete(k)
	c := v.(chan<- *wire.MsgCFiltersV2)
	select {
	case <-ctx.Done():
	case c <- msg:
	}
}

func (rp *RemotePeer) addRequestedCFilterV2(hash *chainhash.Hash, c chan<- *wire.MsgCFilterV2) (newRequest bool) {
	_, loaded := rp.requestedCFiltersV2.LoadOrStore(*hash, c)
	return !loaded
}

func (rp *RemotePeer) deleteRequestedCFilterV2(hash *chainhash.Hash) {
	rp.requestedCFiltersV2.Delete(*hash)
}

func (rp *RemotePeer) receivedCFilterV2(ctx context.Context, msg *wire.MsgCFilterV2) {
	log.Debugf("received cfilter for block %v", &msg.BlockHash)
	const opf = "remotepeer(%v).receivedCFilterV2(%v)"
	var k any = msg.BlockHash
	v, ok := rp.requestedCFiltersV2.Load(k)
	if !ok {
		op := errors.Opf(opf, rp.raddr, &msg.BlockHash)
		err := errors.E(op, errors.Protocol, "received unrequested cfilter")
		rp.Disconnect(err)
		return
	}

	rp.requestedCFiltersV2.Delete(k)
	c := v.(chan<- *wire.MsgCFilterV2)
	select {
	case <-ctx.Done():
	case c <- msg:
	}
}

func (rp *RemotePeer) addRequestedHeaders(c chan<- *wire.MsgHeaders) (sendheaders, newRequest bool) {
	rp.requestedHeadersMu.Lock()
	if rp.sendheaders {
		rp.requestedHeadersMu.Unlock()
		return true, false
	}
	if rp.requestedHeaders != nil {
		rp.requestedHeadersMu.Unlock()
		return false, false
	}
	rp.requestedHeaders = c
	rp.requestedHeadersMu.Unlock()
	return false, true
}

func (rp *RemotePeer) deleteRequestedHeaders() {
	rp.requestedHeadersMu.Lock()
	rp.requestedHeaders = nil
	rp.requestedHeadersMu.Unlock()
}

func (rp *RemotePeer) receivedHeaders(ctx context.Context, msg *wire.MsgHeaders) {
	const opf = "remotepeer(%v).receivedHeaders"
	rp.requestedHeadersMu.Lock()
	var prevHash chainhash.Hash
	var prevHeight uint32
	for i, h := range msg.Headers {
		hash := h.BlockHash()

		// Sanity check the headers connect to each other in sequence.
		if i > 0 && (!prevHash.IsEqual(&h.PrevBlock) || h.Height != prevHeight+1) {
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.Protocol, "received out-of-sequence headers")
			rp.Disconnect(err)
			rp.requestedHeadersMu.Unlock()
			return
		}

		prevHash = hash
		prevHeight = h.Height
	}

	if prevHeight > 0 {
		rp.lastHeightMu.Lock()
		if int32(prevHeight) > rp.lastHeight {
			rp.lastHeight = int32(prevHeight)
		}
		rp.lastHeightMu.Unlock()
	}

	if rp.sendheaders {
		rp.requestedHeadersMu.Unlock()
		select {
		case <-ctx.Done():
		case rp.lp.announcedHeaders <- newInMsg(rp, msg):
		}
		return
	}
	if rp.requestedHeaders == nil {
		op := errors.Opf(opf, rp.raddr)
		err := errors.E(op, errors.Protocol, "received unrequested headers")
		rp.Disconnect(err)
		rp.requestedHeadersMu.Unlock()
		return
	}
	c := rp.requestedHeaders
	rp.requestedHeaders = nil
	rp.requestedHeadersMu.Unlock()
	select {
	case <-ctx.Done():
	case c <- msg:
	}
}

func (rp *RemotePeer) receivedNotFound(ctx context.Context, msg *wire.MsgNotFound) {
	const opf = "remotepeer(%v).receivedNotFound(%v)"
	var err error
	for _, inv := range msg.InvList {
		rp.requestedTxsMu.Lock()
		c, ok := rp.requestedTxs[inv.Hash]
		delete(rp.requestedTxs, inv.Hash)
		rp.requestedTxsMu.Unlock()
		if ok {
			close(c)
			continue
		}

		// Blocks that were requested but that the remote peer does not
		// have end up also falling through to this conditional.
		if err == nil {
			op := errors.Errorf(opf, rp.raddr, &inv.Hash)
			err = errors.E(op, errors.Protocol, "received notfound for unrequested hash")
		}
	}
	if err != nil {
		rp.Disconnect(err)
	}
}

func (rp *RemotePeer) addRequestedTx(hash *chainhash.Hash, c chan<- *wire.MsgTx) (newRequest bool) {
	rp.requestedTxsMu.Lock()
	_, ok := rp.requestedTxs[*hash]
	if !ok {
		rp.requestedTxs[*hash] = c
	}
	rp.requestedTxsMu.Unlock()
	return !ok
}

func (rp *RemotePeer) deleteRequestedTx(hash *chainhash.Hash) {
	rp.requestedTxsMu.Lock()
	delete(rp.requestedTxs, *hash)
	rp.requestedTxsMu.Unlock()
}

func (rp *RemotePeer) receivedTx(ctx context.Context, msg *wire.MsgTx) {
	const opf = "remotepeer(%v).receivedTx(%v)"
	txHash := msg.TxHash()
	rp.requestedTxsMu.Lock()
	c, ok := rp.requestedTxs[txHash]
	delete(rp.requestedTxs, txHash)
	rp.requestedTxsMu.Unlock()
	if !ok {
		op := errors.Opf(opf, rp.raddr, &txHash)
		err := errors.E(op, errors.Protocol, "received unrequested tx")
		rp.Disconnect(err)
		return
	}
	select {
	case <-ctx.Done():
	case c <- msg:
	}
}

var (
	blake256Hasher = blake256.New()
	blake256Mu     sync.Mutex
)

func writeMixMsgHash(msg mixing.Message) chainhash.Hash {
	blake256Mu.Lock()
	defer blake256Mu.Unlock()

	msg.WriteHash(blake256Hasher)
	return msg.Hash()
}

func (rp *RemotePeer) addRequestedMixMsg(hash *chainhash.Hash, c chan<- mixing.Message) (newRequest bool) {
	rp.requestedMixMsgsMu.Lock()
	_, ok := rp.requestedMixMsgs[*hash]
	if !ok {
		rp.requestedMixMsgs[*hash] = c
	}
	rp.requestedMixMsgsMu.Unlock()
	return !ok
}

func (rp *RemotePeer) deleteRequestedMixMsg(hash *chainhash.Hash) {
	rp.requestedMixMsgsMu.Lock()
	delete(rp.requestedMixMsgs, *hash)
	rp.requestedMixMsgsMu.Unlock()
}

func (rp *RemotePeer) receivedMixMsg(ctx context.Context, msg mixing.Message) {
	const opf = "remotepeer(%v).receivedMixMsg(%v)"
	mixHash := writeMixMsgHash(msg)
	rp.requestedMixMsgsMu.Lock()
	c, ok := rp.requestedMixMsgs[mixHash]
	delete(rp.requestedMixMsgs, mixHash)
	rp.requestedMixMsgsMu.Unlock()
	if !ok {
		op := errors.Opf(opf, rp.raddr, &mixHash)
		err := errors.E(op, errors.Protocol, "received unrequested mix msg")
		rp.Disconnect(err)
		return
	}
	select {
	case <-ctx.Done():
	case c <- msg:
	}
}

func (rp *RemotePeer) receivedGetData(ctx context.Context, msg *wire.MsgGetData) {
	if rp.banScore.Increase(0, uint32(len(msg.InvList))*banThreshold/wire.MaxInvPerMsg) > banThreshold {
		log.Warnf("%v: ban score reached threshold", rp.RemoteAddr())
		rp.Disconnect(errors.E(errors.Protocol, "ban score reached"))
		return
	}

	if rp.lp.messageIsMasked(MaskGetData) {
		rp.lp.receivedGetData <- newInMsg(rp, msg)
	}
}

func (rp *RemotePeer) addRequestedInitState(c chan<- *wire.MsgInitState) (newRequest bool) {
	rp.requestedInitStateMu.Lock()
	if rp.requestedInitState != nil {
		rp.requestedInitStateMu.Unlock()
		return false
	}
	rp.requestedInitState = c
	rp.requestedInitStateMu.Unlock()
	return true
}

func (rp *RemotePeer) deleteRequestedInitState() {
	rp.requestedInitStateMu.Lock()
	rp.requestedInitState = nil
	rp.requestedInitStateMu.Unlock()
}

func (rp *RemotePeer) receivedInitState(ctx context.Context, msg *wire.MsgInitState) {
	const opf = "remotepeer(%v).receivedInitState"
	rp.requestedInitStateMu.Lock()
	c := rp.requestedInitState
	rp.requestedInitState = nil
	rp.requestedInitStateMu.Unlock()

	if c == nil {
		op := errors.Opf(opf, rp.raddr)
		err := errors.E(op, errors.Protocol, "received unrequested init state")
		rp.Disconnect(err)
		return
	}

	select {
	case <-ctx.Done():
	case c <- msg:
	}
}

func (rp *RemotePeer) receivedGetMiningState(ctx context.Context) {
	// Send an empty miningstate reply.
	m := wire.NewMsgMiningState()
	select {
	case <-ctx.Done():
	case <-rp.errc:
	case rp.out <- &msgAck{m, nil}:
	}
}

func (rp *RemotePeer) receivedGetInitState(ctx context.Context) {
	// Send an empty initstate reply.
	m := wire.NewMsgInitState()
	select {
	case <-ctx.Done():
	case <-rp.errc:
	case rp.out <- &msgAck{m, nil}:
	}
}

// Addrs requests a list of known active peers from a RemotePeer using getaddr.
// As many addr responses may be received for a single getaddr request, received
// address messages are handled asynchronously by the local peer and at least
// the stall timeout should be waited before disconnecting a remote peer while
// waiting for addr messages.
func (rp *RemotePeer) Addrs(ctx context.Context) error {
	const opf = "remotepeer(%v).Addrs"
	ctx, cancel := context.WithTimeout(ctx, stallTimeout)
	defer cancel()

	m := wire.NewMsgGetAddr()
	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return err
		}
		return ctx.Err()
	case <-rp.errc:
		return rp.err
	case rp.out <- &msgAck{m, nil}:
		return nil
	}
}

// Block requests a block from a RemotePeer using getdata.
func (rp *RemotePeer) Block(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	const opf = "remotepeer(%v).Block(%v)"

	blocks, err := rp.Blocks(ctx, []*chainhash.Hash{blockHash})
	if err != nil {
		op := errors.Opf(opf, rp.raddr, blockHash)
		return nil, errors.E(op, err)
	}

	return blocks[0], nil
}

// requestBlocks sends a getdata wire message and waits for all the specified
// blocks to be received. This blocks so it should be called from a goroutine.
func (rp *RemotePeer) requestBlocks(reqs []*blockRequest) {
	// Aux func to fulfill requests. It signals any outstanding requests of
	// the given error and removes all from the requestedBlocks map.
	fulfill := func(err error) {
		rp.requestedBlocksMu.Lock()
		for _, req := range reqs {
			select {
			case <-req.ready:
				// Already fulfilled.
			default:
				req.err = err
				close(req.ready)
			}
			delete(rp.requestedBlocks, *req.hash)
		}
		rp.requestedBlocksMu.Unlock()
	}

	// Build the message.
	//
	// TODO: split into batches when len(blockHashes) > wire.MaxInvPerMsg
	// so AddInvVect() can't error.
	m := wire.NewMsgGetDataSizeHint(uint(len(reqs)))
	for _, req := range reqs {
		err := m.AddInvVect(wire.NewInvVect(wire.InvTypeBlock, req.hash))
		if err != nil {
			fulfill(err)
			return
		}
	}

	// Send the message.
	stalled := time.NewTimer(stallTimeout)
	select {
	case <-stalled.C:
		err := errors.E(errors.IO, "peer appears stalled")
		rp.Disconnect(err)
		fulfill(err)
		return

	case <-rp.errc:
		if !stalled.Stop() {
			<-stalled.C
		}
		fulfill(rp.err)
		return

	case rp.out <- &msgAck{m, nil}:
	}

	// Receive responses.
	for i := 0; i < len(reqs); i++ {
		select {
		case <-stalled.C:
			err := errors.E(errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			fulfill(err)
			return

		case <-rp.errc:
			if !stalled.Stop() {
				<-stalled.C
			}
			fulfill(rp.err)
			return

		case <-reqs[i].ready:
			if !stalled.Stop() {
				<-stalled.C
			}
			stalled.Reset(stallTimeout)
		}
	}

	// Remove all requests that were just completed from the
	// `requestedBlocks` map.
	fulfill(nil)
}

// Blocks requests multiple blocks at a time from a RemotePeer using a single
// getdata message.  It returns when all of the blocks have been received.
func (rp *RemotePeer) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	const opf = "remotepeer(%v).Blocks"

	// Determine which blocks don't have an in-flight request yet so we can
	// dispatch a new one for them.
	reqs := make([]*blockRequest, len(blockHashes))
	newReqs := make([]*blockRequest, 0, len(blockHashes))
	rp.requestedBlocksMu.Lock()
	for i, h := range blockHashes {
		if req, ok := rp.requestedBlocks[*h]; ok {
			// Already requesting this block.
			reqs[i] = req
			continue
		}

		// Not requesting this block yet.
		req := &blockRequest{
			ready: make(chan struct{}),
			hash:  h,
		}
		reqs[i] = req
		rp.requestedBlocks[*h] = req
		newReqs = append(newReqs, req)
	}
	rp.requestedBlocksMu.Unlock()

	// Request any blocks which have not yet been requested.
	var doneRequests chan struct{}
	if len(newReqs) > 0 {
		doneRequests = make(chan struct{}, 1)
		go func() {
			rp.requestBlocks(newReqs)
			doneRequests <- struct{}{}
		}()
	}

	// Wait for all blocks to be received or to error out.
	blocks := make([]*wire.MsgBlock, len(blockHashes))
	for i, req := range reqs {
		select {
		case <-req.ready:
			if req.err != nil {
				op := errors.Opf(opf, rp.raddr)
				return nil, errors.E(op, req.err)
			}
			blocks[i] = req.block

		case <-ctx.Done():
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, ctx.Err())
		}
	}

	if doneRequests != nil {
		<-doneRequests
	}
	return blocks, nil
}

// ErrNotFound describes one or more transactions not being returned by a remote
// peer, indicated with notfound.
var ErrNotFound = errors.E(errors.NotExist, "transaction not found")

// Transactions requests multiple transactions at a time from a RemotePeer
// using a single getdata message.  It returns when all of the transactions
// and/or notfound messages have been received.  The same transaction may not be
// requested multiple times concurrently from the same peer.  Returns
// ErrNotFound with a slice of one or more nil transactions if any notfound
// messages are received for requested transactions.
func (rp *RemotePeer) Transactions(ctx context.Context, hashes []*chainhash.Hash) ([]*wire.MsgTx, error) {
	const opf = "remotepeer(%v).Transactions"

	m := wire.NewMsgGetDataSizeHint(uint(len(hashes)))
	cs := make([]chan *wire.MsgTx, len(hashes))
	for i, h := range hashes {
		err := m.AddInvVect(wire.NewInvVect(wire.InvTypeTx, h))
		if err != nil {
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, err)
		}
		cs[i] = make(chan *wire.MsgTx, 1)
		if !rp.addRequestedTx(h, cs[i]) {
			for _, h := range hashes[:i] {
				rp.deleteRequestedTx(h)
			}
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, errors.Errorf("tx %v is already being requested from this peer", h))
		}
	}
	select {
	case <-ctx.Done():
		for _, h := range hashes {
			rp.deleteRequestedTx(h)
		}
		return nil, ctx.Err()
	case rp.out <- &msgAck{m, nil}:
	}
	txs := make([]*wire.MsgTx, len(hashes))
	var notfound bool
	stalled := time.NewTimer(stallTimeout)
	for i := 0; i < len(hashes); i++ {
		select {
		case <-ctx.Done():
			go func() {
				<-stalled.C
				for _, h := range hashes[i:] {
					rp.deleteRequestedTx(h)
				}
			}()
			return nil, ctx.Err()
		case <-stalled.C:
			for _, h := range hashes[i:] {
				rp.deleteRequestedTx(h)
			}
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return nil, err
		case <-rp.errc:
			stalled.Stop()
			return nil, rp.err
		case m, ok := <-cs[i]:
			txs[i] = m
			notfound = notfound || !ok
		}
	}
	stalled.Stop()
	if notfound {
		return txs, ErrNotFound
	}
	return txs, nil
}

// MixMessages requests multiple mixing messages at a time from a RemotePeer
// using a single getdata message.  It returns when all of the messages
// and/or notfound messages have been received.  The same message may not be
// requested multiple times concurrently from the same peer.  Returns
// ErrNotFound with a slice of one or more nil messages if any notfound
// messages are received for requested mix messages.
func (rp *RemotePeer) MixMessages(ctx context.Context, hashes []*chainhash.Hash) ([]mixing.Message, error) {
	const opf = "remotepeer(%v).MixMessages"

	m := wire.NewMsgGetDataSizeHint(uint(len(hashes)))
	cs := make([]chan mixing.Message, len(hashes))
	for i, h := range hashes {
		err := m.AddInvVect(wire.NewInvVect(wire.InvTypeMix, h))
		if err != nil {
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, err)
		}
		cs[i] = make(chan mixing.Message, 1)
		if !rp.addRequestedMixMsg(h, cs[i]) {
			for _, h := range hashes[:i] {
				rp.deleteRequestedMixMsg(h)
			}
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, errors.Errorf("mix msg %v is already being requested from this peer", h))
		}
	}
	select {
	case <-ctx.Done():
		for _, h := range hashes {
			rp.deleteRequestedMixMsg(h)
		}
		return nil, ctx.Err()
	case rp.out <- &msgAck{m, nil}:
	}
	msgs := make([]mixing.Message, len(hashes))
	var notfound bool
	stalled := time.NewTimer(stallTimeout)
	for i := 0; i < len(hashes); i++ {
		select {
		case <-ctx.Done():
			go func() {
				<-stalled.C
				for _, h := range hashes[i:] {
					rp.deleteRequestedMixMsg(h)
				}
			}()
			return nil, ctx.Err()
		case <-stalled.C:
			for _, h := range hashes[i:] {
				rp.deleteRequestedMixMsg(h)
			}
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return nil, err
		case <-rp.errc:
			stalled.Stop()
			return nil, rp.err
		case m, ok := <-cs[i]:
			msgs[i] = m
			notfound = notfound || !ok
		}
	}
	stalled.Stop()
	if notfound {
		return msgs, ErrNotFound
	}
	return msgs, nil
}

// CFilterV2 requests a version 2 regular compact filter from a RemotePeer
// using getcfilterv2.  The same block can not be requested concurrently from
// the same peer.
//
// The inclusion proof data that ensures the cfilter is committed to in the
// header is returned as well.
func (rp *RemotePeer) CFilterV2(ctx context.Context, blockHash *chainhash.Hash) (*gcs.FilterV2, uint32, []chainhash.Hash, error) {
	const opf = "remotepeer(%v).CFilterV2(%v)"

	if rp.pver < wire.CFilterV2Version {
		op := errors.Opf(opf, rp.raddr, blockHash)
		err := errors.Errorf("protocol version %v is too low to fetch cfiltersv2 from this peer", rp.pver)
		return nil, 0, nil, errors.E(op, errors.Protocol, err)
	}

	m := wire.NewMsgGetCFilterV2(blockHash)
	c := make(chan *wire.MsgCFilterV2, 1)
	if !rp.addRequestedCFilterV2(blockHash, c) {
		op := errors.Opf(opf, rp.raddr, blockHash)
		return nil, 0, nil, errors.E(op, errors.Invalid, "cfilterv2 is already being requested from this peer for this block")
	}
	stalled := time.NewTimer(stallTimeout)
	out := rp.out
	for {
		select {
		case <-ctx.Done():
			go func() {
				<-stalled.C
				rp.deleteRequestedCFilterV2(blockHash)
			}()
			return nil, 0, nil, ctx.Err()
		case <-stalled.C:
			rp.deleteRequestedCFilterV2(blockHash)
			op := errors.Opf(opf, rp.raddr, blockHash)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return nil, 0, nil, err
		case <-rp.errc:
			stalled.Stop()
			return nil, 0, nil, rp.err
		case out <- &msgAck{m, nil}:
			out = nil
		case m := <-c:
			stalled.Stop()
			var f *gcs.FilterV2
			var err error
			f, err = gcs.FromBytesV2(blockcf.B, blockcf.M, m.Data)
			if err != nil {
				op := errors.Opf(opf, rp.raddr, blockHash)
				return nil, 0, nil, errors.E(op, err)
			}
			return f, m.ProofIndex, m.ProofHashes, nil
		}
	}
}

// filterProof is an alias to the same anonymous struct as wallet package's
// FilterProof struct.
type filterProof = struct {
	Filter     *gcs.FilterV2
	ProofIndex uint32
	Proof      []chainhash.Hash
}

// CFiltersV2 requests version 2 cfilters for all blocks described by
// blockHashes.  This is currently implemented by making many separate
// getcfilter requests concurrently and waiting on every result.
func (rp *RemotePeer) CFiltersV2(ctx context.Context, blockHashes []*chainhash.Hash) ([]filterProof, error) {
	const opf = "remotepeer(%v).CFiltersV2(%v)"

	ctxSend, cancelSend := context.WithCancel(ctx)
	defer cancelSend()

	type request struct {
		t time.Time
		c chan *wire.MsgCFilterV2
	}

	// Send the requests on a separate goroutine, as fast as the network
	// accepts them.
	errChan := make(chan error, 1)
	requests := make(chan request, len(blockHashes))
	go func() {
		defer close(requests)
		for _, blockHash := range blockHashes {
			m := wire.NewMsgGetCFilterV2(blockHash)
			c := make(chan *wire.MsgCFilterV2, 1)
			if !rp.addRequestedCFilterV2(blockHash, c) {
				op := errors.Opf(opf, rp.raddr, blockHash)
				errChan <- errors.E(op, errors.Invalid, "cfilterv2 is already being requested from this peer for this block")
				return
			}
			now := time.Now()
			select {
			case rp.out <- &msgAck{m, nil}:
				requests <- request{t: now, c: c}
			case <-ctxSend.Done():
				return
			case <-rp.errc:
				return
			}
		}
	}()

	stalled := time.NewTimer(stallTimeout)

	// Helper func that stops the sending goroutine and removes all requests
	// made starting at index `start`.
	cleanup := func(start int, stopStalled bool) {
		cancelSend()
		for range requests { // Drain until it signals closed.
		}
		for i := start; i < len(blockHashes); i++ {
			rp.deleteRequestedCFilterV2(blockHashes[i])
		}
		if stopStalled && !stalled.Stop() {
			<-stalled.C
		}
	}

	// Receive the responses.
	filters := make([]filterProof, len(blockHashes))
	for i := range blockHashes {
		// Alternate between waiting for the next request to be sent
		// and waiting for its response by switching which of req.c
		// and q channels are not nil.
		var req request
		q := requests
		for req.c != nil || q != nil {
			select {
			case <-ctx.Done():
				cleanup(i, true)
				return nil, ctx.Err()
			case <-stalled.C:
				cleanup(i, false)
				op := errors.Opf(opf, rp.raddr, blockHashes[i])
				err := errors.E(op, errors.IO, "peer appears stalled")
				rp.Disconnect(err)
				return nil, err
			case <-rp.errc:
				cleanup(i, true)
				return nil, rp.err
			case err := <-errChan:
				cleanup(i, true)
				return nil, err
			case req = <-q:
				q = nil

				// Request was sent. Reset the stall timer to
				// be relative to the sending time.
				if !stalled.Stop() {
					<-stalled.C
				}
				stalled.Reset(stallTimeout - time.Now().Sub(req.t))
			case m := <-req.c:
				var f *gcs.FilterV2
				var err error
				f, err = gcs.FromBytesV2(blockcf.B, blockcf.M, m.Data)
				if err != nil {
					cleanup(i, true)
					op := errors.Opf(opf, rp.raddr, blockHashes[i])
					return nil, errors.E(op, err)
				}
				filters[i] = filterProof{
					Filter:     f,
					ProofIndex: m.ProofIndex,
					Proof:      m.ProofHashes,
				}
				req = request{}
			}
		}
	}
	return filters, nil
}

// BatchedCFiltersV2 fetches all cfilters between the passed start and end
// blocks.  The first block MUST be an ancestor of the final block.
func (rp *RemotePeer) BatchedCFiltersV2(ctx context.Context, startHash, endHash *chainhash.Hash) ([]filterProof, error) {
	const opf = "remotepeer(%v).CFilterV2(%v)"

	if rp.pver < wire.CFilterV2Version {
		op := errors.Opf(opf, rp.raddr, startHash)
		err := errors.Errorf("protocol version %v is too low to fetch cfiltersv2 from this peer", rp.pver)
		return nil, errors.E(op, errors.Protocol, err)
	}

	m := wire.NewMsgGetCFsV2(startHash, endHash)
	c := make(chan *wire.MsgCFiltersV2, 1)
	if !rp.addRequestedManyCFilterV2(startHash, c) {
		op := errors.Opf(opf, rp.raddr, startHash)
		return nil, errors.E(op, errors.Invalid, "cfilterv2 is already being requested from this peer for this block")
	}
	stalled := time.NewTimer(stallTimeout)
	out := rp.out
	for {
		select {
		case <-ctx.Done():
			rp.deleteRequestedManyCFilterV2(startHash)
			go func() {
				<-stalled.C
			}()
			return nil, ctx.Err()
		case <-stalled.C:
			rp.deleteRequestedManyCFilterV2(startHash)
			op := errors.Opf(opf, rp.raddr, startHash)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return nil, err
		case <-rp.errc:
			stalled.Stop()
			return nil, rp.err
		case out <- &msgAck{m, nil}:
			out = nil
		case m := <-c:
			stalled.Stop()
			res := make([]filterProof, len(m.CFilters))
			for i := 0; i < len(m.CFilters); i++ {
				var f *gcs.FilterV2
				var err error
				f, err = gcs.FromBytesV2(blockcf.B, blockcf.M, m.CFilters[i].Data)
				if err != nil {
					op := errors.Opf(opf, rp.raddr, startHash)
					return nil, errors.E(op, err)
				}
				res[i].Filter = f
				res[i].Proof = m.CFilters[i].ProofHashes
				res[i].ProofIndex = m.CFilters[i].ProofIndex
			}
			return res, nil
		}
	}
}

// SendHeaders sends the remote peer a sendheaders message.  This informs the
// peer to announce new blocks by immediately sending them in a headers message
// rather than sending an inv message containing the block hash.
//
// Once this is called, it is no longer permitted to use the synchronous
// GetHeaders method, as there is no guarantee that the next received headers
// message corresponds with any getheaders request.
func (rp *RemotePeer) SendHeaders(ctx context.Context) error {
	const opf = "remotepeer(%v).SendHeaders"

	// If negotiated protocol version allows it, and the option is set, request
	// blocks to be announced by pushing headers messages.
	if rp.pver < wire.SendHeadersVersion {
		op := errors.Opf(opf, rp.raddr)
		err := errors.Errorf("protocol version %v is too low to receive block header announcements", rp.pver)
		return errors.E(op, errors.Protocol, err)
	}

	rp.requestedHeadersMu.Lock()
	old := rp.sendheaders
	rp.sendheaders = true
	rp.requestedHeadersMu.Unlock()
	if old {
		return nil
	}

	stalled := time.NewTimer(stallTimeout)
	defer stalled.Stop()
	select {
	case <-ctx.Done():
		rp.requestedHeadersMu.Lock()
		rp.sendheaders = false
		rp.requestedHeadersMu.Unlock()
		return ctx.Err()
	case <-stalled.C:
		op := errors.Opf(opf, rp.raddr)
		err := errors.E(op, errors.IO, "peer appears stalled")
		rp.Disconnect(err)
		return err
	case <-rp.errc:
		return rp.err
	case rp.out <- &msgAck{wire.NewMsgSendHeaders(), nil}:
		return nil
	}
}

// Headers requests block headers from the RemotePeer with getheaders.  Block
// headers can not be requested concurrently from the same peer.  Sending a
// getheaders message and synchronously waiting for the result is not possible
// if a sendheaders message has been sent to the remote peer.
func (rp *RemotePeer) Headers(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error) {
	const opf = "remotepeer(%v).Headers"

	m := &wire.MsgGetHeaders{
		ProtocolVersion:    rp.pver,
		BlockLocatorHashes: blockLocators,
		HashStop:           *hashStop,
	}
	c := make(chan *wire.MsgHeaders, 1)
	sendheaders, newRequest := rp.addRequestedHeaders(c)
	if sendheaders {
		op := errors.Opf(opf, rp.raddr)
		return nil, errors.E(op, errors.Invalid, "synchronous getheaders after sendheaders is unsupported")
	}
	if !newRequest {
		op := errors.Opf(opf, rp.raddr)
		return nil, errors.E(op, errors.Invalid, "headers are already being requested from this peer")
	}
	stalled := time.NewTimer(stallTimeout)
	out := rp.out
	for {
		select {
		case <-ctx.Done():
			go func() {
				<-stalled.C
				rp.deleteRequestedHeaders()
			}()
			return nil, ctx.Err()
		case <-stalled.C:
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return nil, err
		case <-rp.errc:
			stalled.Stop()
			return nil, rp.err
		case out <- &msgAck{m, nil}:
			out = nil
		case m := <-c:
			stalled.Stop()

			// The parent of the first header (if there is one) MUST
			// be one of the block locators we used to request
			// headers from the peer.
			if len(m.Headers) > 0 {
				wantParent := m.Headers[0].PrevBlock
				contains := false
				for _, loc := range blockLocators {
					if *loc == wantParent {
						contains = true
						break
					}
				}
				if !contains {
					op := errors.Opf(opf, rp.raddr)
					err := errors.E(op, errors.Protocol,
						"peer sent headers that do not connect "+
							"to block locators")
					rp.Disconnect(err)
					return nil, err
				}
			}

			return m.Headers, nil
		}
	}
}

// HeadersAsync requests block headers from the RemotePeer with getheaders.
// Block headers can not be requested concurrently from the same peer.  This
// can only be used _after_ the sendheaders msg was sent and does _not_ wait
// for a reply from the remote peer. Headers will be delivered via the
// ReceiveHeaderAnnouncements call.
func (rp *RemotePeer) HeadersAsync(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) error {
	const opf = "remotepeer(%v).Headers"

	rp.requestedHeadersMu.Lock()
	sendheaders := rp.sendheaders
	rp.requestedHeadersMu.Unlock()
	if !sendheaders {
		op := errors.Opf(opf, rp.raddr)
		return errors.E(op, errors.Invalid, "asynchronous getheaders before sendheaders is unsupported")
	}

	m := &wire.MsgGetHeaders{
		ProtocolVersion:    rp.pver,
		BlockLocatorHashes: blockLocators,
		HashStop:           *hashStop,
	}
	stalled := time.NewTimer(stallTimeout)
	out := rp.out
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stalled.C:
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return err
		case <-rp.errc:
			stalled.Stop()
			return rp.err
		case out <- &msgAck{m, nil}:
			return nil
		}
	}
}

// PublishTransactions pushes an inventory message advertising transaction
// hashes of txs.
func (rp *RemotePeer) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	const opf = "remotepeer(%v).PublishTransactions"
	inv := wire.NewMsgInvSizeHint(uint(len(txs)))
	for i := range txs {
		txHash := txs[i].TxHash()
		rp.invsSent.Add(txHash)
		err := inv.AddInvVect(wire.NewInvVect(wire.InvTypeTx, &txHash))
		if err != nil {
			op := errors.Opf(opf, rp.raddr)
			return errors.E(op, errors.Protocol, err)
		}
	}
	err := rp.SendMessage(ctx, inv)
	if err != nil {
		op := errors.Opf(opf, rp.raddr)
		return errors.E(op, err)
	}
	return nil
}

// PublishTransactions pushes an inventory message advertising transaction
// hashes of txs.
func (rp *RemotePeer) PublishMixMessages(ctx context.Context, msgs ...mixing.Message) error {
	const opf = "remotepeer(%v).PublishMixMessages"

	if rp.pver < wire.MixVersion {
		op := errors.Opf(opf, rp.raddr)
		err := errors.Errorf("protocol version %v is too low to publish mix messages",
			rp.pver)
		return errors.E(op, errors.Protocol, err)
	}

	inv := wire.NewMsgInvSizeHint(uint(len(msgs)))
	for _, msg := range msgs {
		msgHash := msg.Hash() // Must be type chainhash.Hash
		log.Debugf("Publishing inv for mix message %v", msgHash)
		rp.invsSent.Add(msgHash)
		err := inv.AddInvVect(wire.NewInvVect(wire.InvTypeMix, &msgHash))
		if err != nil {
			op := errors.Opf(opf, rp.raddr)
			return errors.E(op, errors.Protocol, err)
		}
	}
	err := rp.SendMessage(ctx, inv)
	if err != nil {
		op := errors.Opf(opf, rp.raddr)
		return errors.E(op, err)
	}
	return nil
}

// SendMessage sends an message to the remote peer.  Use this method carefully,
// as calling this with an unexpected message that changes the protocol state
// may cause problems with the convenience methods implemented by this package.
func (rp *RemotePeer) SendMessage(ctx context.Context, msg wire.Message) error {
	ctx, cancel := context.WithTimeout(ctx, stallTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case rp.out <- &msgAck{msg, nil}:
		return nil
	}
}

// sendMessageAck sends a message to a remote peer, waiting until the write
// finishes before returning.
func (rp *RemotePeer) sendMessageAck(ctx context.Context, msg wire.Message) error {
	ctx, cancel := context.WithTimeout(ctx, stallTimeout)
	defer cancel()
	ack := make(chan struct{}, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case rp.out <- &msgAck{msg, ack}:
		<-ack
		return nil
	}
}

// ReceivedOrphanHeader increases the banscore for a peer due to them sending
// an orphan header. Returns an error if the banscore has been breached.
func (rp *RemotePeer) ReceivedOrphanHeader() error {
	// Allow up to 10 orphan header chain announcements.
	delta := uint32(banThreshold / 10)
	bs := rp.banScore.Increase(0, delta)
	if bs > banThreshold {
		return errors.E(errors.Protocol, "ban score reached due to orphan header")
	}
	return nil
}

// SendHeadersSent returns whether this peer was already instructed to send new
// headers via the sendheaders message.
func (rp *RemotePeer) SendHeadersSent() bool {
	rp.requestedHeadersMu.Lock()
	sent := rp.sendheaders
	rp.requestedHeadersMu.Unlock()
	return sent
}

// GetInitState attempts to get initial state by sending a GetInitState message.
func (rp *RemotePeer) GetInitState(ctx context.Context, msg *wire.MsgGetInitState) (*wire.MsgInitState, error) {
	const opf = "remotepeer(%v).GetInitState"

	c := make(chan *wire.MsgInitState, 1)
	newRequest := rp.addRequestedInitState(c)
	if !newRequest {
		op := errors.Opf(opf, rp.raddr)
		return nil, errors.E(op, errors.Invalid, "init state is already being requested from this peer")
	}

	stalled := time.NewTimer(stallTimeout)
	out := rp.out
	for {
		select {
		case <-ctx.Done():
			rp.deleteRequestedInitState()
			return nil, ctx.Err()
		case <-stalled.C:
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			return nil, err
		case <-rp.errc:
			if !stalled.Stop() {
				<-stalled.C
			}
			return nil, rp.err
		case out <- &msgAck{msg, nil}:
			out = nil
		case msg := <-c:
			if !stalled.Stop() {
				<-stalled.C
			}
			return msg, nil
		}
	}
}

// invVecContainsTxOrMix returns true if at least one inv vector is of type
// transaction or mix message.
func invVecContainsTxOrMix(inv []*wire.InvVect) bool {
	for i := range inv {
		if inv[i].Type == wire.InvTypeTx {
			return true
		}
		if inv[i].Type == wire.InvTypeMix {
			return true
		}
	}
	return false
}

// receivedInv is called when an inv message is received from the remote peer.
func (rp *RemotePeer) receivedInv(ctx context.Context, inv *wire.MsgInv) {
	const opf = "remotepeer(%v).receivedInv"

	// When tx relay is disabled, we don't expect transactions on invs.
	if rp.lp.disableRelayTx && invVecContainsTxOrMix(inv.InvList) {
		op := errors.Opf(opf, rp.raddr)
		err := errors.E(op, errors.Protocol, "received tx in msginv when tx relaying is disabled")
		rp.Disconnect(err)
		return
	}

	// Ignore if the user is not interested in invs.
	if !rp.lp.messageIsMasked(MaskInv) {
		return
	}

	select {
	case rp.lp.receivedInv <- newInMsg(rp, inv):
	case <-ctx.Done():
	}
}
