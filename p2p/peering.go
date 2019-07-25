// Copyright (c) 2018-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package p2p

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/addrmgr"
	chaincfg1 "github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/connmgr"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/lru"
	"github.com/decred/dcrwallet/version"
	"golang.org/x/sync/errgroup"
)

// uaName is the LocalPeer useragent name.
const uaName = "dcrwallet"

// uaVersion is the LocalPeer useragent version.
var uaVersion = version.String()

// Pver is the maximum protocol version implemented by the LocalPeer.
const Pver = wire.NodeCFVersion

const maxOutboundConns = 8

// connectTimeout is the amount of time allowed before connecting, peering
// handshake, and protocol negotiation is aborted.
const connectTimeout = 30 * time.Second

// stallTimeout is the amount of time allowed before a request to receive data
// that is known to exist at the RemotePeer times out with no matching reply.
const stallTimeout = 30 * time.Second

const banThreshold = 100

const invLRUSize = 5000

type msgAck struct {
	msg wire.Message
	ack chan<- struct{}
}

// RemotePeer represents a remote peer that can send and receive wire protocol
// messages with the local peer.  RemotePeers must be created by dialing the
// peer's address with a LocalPeer.
type RemotePeer struct {
	// atomics
	atomicClosed uint64

	id         uint64
	lp         *LocalPeer
	ua         string
	services   wire.ServiceFlag
	pver       uint32
	initHeight int32
	raddr      net.Addr
	na         *wire.NetAddress

	// io
	c       net.Conn
	mr      msgReader
	out     chan *msgAck
	outPrio chan *msgAck
	pongs   chan *wire.MsgPong

	requestedBlocks   sync.Map // k=chainhash.Hash v=chan<- *wire.MsgBlock
	requestedCFilters sync.Map // k=chainhash.Hash v=chan<- *wire.MsgCFilter
	requestedTxs      map[chainhash.Hash]chan<- *wire.MsgTx
	requestedTxsMu    sync.Mutex

	// headers message management.  Headers can either be fetched synchronously
	// or used to push block notifications with sendheaders.
	requestedHeaders   chan<- *wire.MsgHeaders // non-nil result chan when synchronous getheaders in process
	sendheaders        bool                    // whether a sendheaders message was sent
	requestedHeadersMu sync.Mutex

	invsSent     lru.Cache // Hashes from sent inventory messages
	invsRecv     lru.Cache // Hashes of received inventory messages
	knownHeaders lru.Cache // Hashes of received headers
	banScore     connmgr.DynamicBanScore

	err  error         // Final error of disconnected peer
	errc chan struct{} // Closed after err is set
}

// LocalPeer represents the local peer that can send and receive wire protocol
// messages with remote peers on the network.
type LocalPeer struct {
	// atomics
	atomicMask          uint64
	atomicPeerIDCounter uint64

	dialer net.Dialer

	receivedGetData  chan *inMsg
	receivedHeaders  chan *inMsg
	receivedInv      chan *inMsg
	announcedHeaders chan *inMsg

	extaddr     net.Addr
	amgr        *addrmgr.AddrManager
	chainParams *chaincfg.Params

	rpByID map[uint64]*RemotePeer
	rpMu   sync.Mutex
}

// NewLocalPeer creates a LocalPeer that is externally reachable to remote peers
// through extaddr.
func NewLocalPeer(params *chaincfg.Params, extaddr *net.TCPAddr, amgr *addrmgr.AddrManager) *LocalPeer {
	lp := &LocalPeer{
		receivedGetData:  make(chan *inMsg),
		receivedHeaders:  make(chan *inMsg),
		receivedInv:      make(chan *inMsg),
		announcedHeaders: make(chan *inMsg),
		extaddr:          extaddr,
		amgr:             amgr,
		chainParams:      params,
		rpByID:           make(map[uint64]*RemotePeer),
	}
	return lp
}

func (lp *LocalPeer) newMsgVersion(pver uint32, extaddr net.Addr, c net.Conn) (*wire.MsgVersion, error) {
	la, err := wire.NewNetAddress(c.LocalAddr(), 0) // We provide no services
	if err != nil {
		return nil, err
	}
	ra, err := wire.NewNetAddress(c.RemoteAddr(), 0)
	if err != nil {
		return nil, err
	}
	nonce, err := wire.RandomUint64()
	if err != nil {
		return nil, err
	}
	v := wire.NewMsgVersion(la, ra, nonce, 0)
	v.AddUserAgent(uaName, uaVersion)
	return v, nil
}

// ConnectOutbound establishes a connection to a remote peer by their remote TCP
// address.  The peer is serviced in the background until the context is
// cancelled, the RemotePeer disconnects, times out, misbehaves, or the
// LocalPeer disconnects all peers.
func (lp *LocalPeer) ConnectOutbound(ctx context.Context, addr string, reqSvcs wire.ServiceFlag) (*RemotePeer, error) {
	const opf = "localpeer.ConnectOutbound(%v)"

	log.Debugf("Attempting connection to peer %v", addr)

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	// Generate a unique ID for this peer and add the initial connection state.
	id := atomic.AddUint64(&lp.atomicPeerIDCounter, 1)

	rp, err := lp.connectOutbound(connectCtx, id, addr)
	if err != nil {
		op := errors.Opf(opf, addr)
		return nil, errors.E(op, err)
	}

	go lp.serveUntilError(ctx, rp)

	var waitForAddrs <-chan time.Time
	if lp.amgr.NeedMoreAddresses() {
		waitForAddrs = time.After(stallTimeout)
		err = rp.Addrs(ctx)
		if err != nil {
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, err)
		}
	}

	// Disconnect from the peer if it does not specify all required services.
	if rp.services&reqSvcs != reqSvcs {
		op := errors.Opf(opf, rp.raddr)
		reason := errors.Errorf("missing required service flags %v", reqSvcs&^rp.services)
		err := errors.E(op, reason)
		go func() {
			if waitForAddrs != nil {
				<-waitForAddrs
			}
			reject := wire.NewMsgReject(wire.CmdVersion, wire.RejectNonstandard, reason.Error())
			rp.sendMessageAck(ctx, reject)
			rp.Disconnect(err)
		}()
		return nil, err
	}

	return rp, nil
}

// AddrManager returns the local peer's address manager.
func (lp *LocalPeer) AddrManager() *addrmgr.AddrManager { return lp.amgr }

// NA returns the remote peer's net address.
func (rp *RemotePeer) NA() *wire.NetAddress { return rp.na }

// UA returns the remote peer's user agent.
func (rp *RemotePeer) UA() string { return rp.ua }

// InitialHeight returns the current height the peer advertised in its version
// message.
func (rp *RemotePeer) InitialHeight() int32 { return rp.initHeight }

// Services returns the remote peer's advertised service flags.
func (rp *RemotePeer) Services() wire.ServiceFlag { return rp.services }

// InvsSent returns an LRU cache of inventory hashes sent to the remote peer.
func (rp *RemotePeer) InvsSent() *lru.Cache { return &rp.invsSent }

// InvsRecv returns an LRU cache of inventory hashes received by the remote
// peer.
func (rp *RemotePeer) InvsRecv() *lru.Cache { return &rp.invsRecv }

// KnownHeaders returns an LRU cache of block hashes from received headers messages.
func (rp *RemotePeer) KnownHeaders() *lru.Cache { return &rp.knownHeaders }

// DNSSeed uses DNS to seed the local peer with remote addresses matching the
// services.
func (lp *LocalPeer) DNSSeed(services wire.ServiceFlag) {
	params := &chaincfg1.MainNetParams
	switch lp.chainParams.Net {
	case wire.TestNet3:
		params = &chaincfg1.TestNet3Params
	case wire.SimNet:
		params = &chaincfg1.SimNetParams
	}
	connmgr.SeedFromDNS(params, services, net.LookupIP, func(addrs []*wire.NetAddress) {
		for _, a := range addrs {
			as := &net.TCPAddr{IP: a.IP, Port: int(a.Port)}
			log.Debugf("Discovered peer %v from seeder", as)
		}
		lp.amgr.AddAddresses(addrs, addrs[0])
	})
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

func handshake(ctx context.Context, lp *LocalPeer, id uint64, na *wire.NetAddress, c net.Conn) (*RemotePeer, error) {
	const op errors.Op = "p2p.handshake"

	rp := &RemotePeer{
		id:           id,
		lp:           lp,
		ua:           "",
		services:     0,
		pver:         Pver,
		raddr:        c.RemoteAddr(),
		na:           na,
		c:            c,
		mr:           msgReader{r: c, net: lp.chainParams.Net},
		out:          nil,
		outPrio:      nil,
		pongs:        make(chan *wire.MsgPong, 1),
		requestedTxs: make(map[chainhash.Hash]chan<- *wire.MsgTx),
		invsSent:     lru.NewCache(invLRUSize),
		invsRecv:     lru.NewCache(invLRUSize),
		knownHeaders: lru.NewCache(invLRUSize),
		errc:         make(chan struct{}),
	}

	mw := msgWriter{c, lp.chainParams.Net}

	// The first message sent must be the version message.
	lversion, err := lp.newMsgVersion(rp.pver, lp.extaddr, c)
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

	// Negotiate protocol down to compatible version
	if uint32(rversion.ProtocolVersion) < rp.pver {
		rp.pver = uint32(rversion.ProtocolVersion)
	}

	// Send the verack
	err = mw.write(ctx, wire.NewMsgVerAck(), rp.pver)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}

	// Wait until a verack is received
	err = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	msg, _, err = wire.ReadMessage(c, Pver, lp.chainParams.Net)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	_, ok = msg.(*wire.MsgVerAck)
	if !ok {
		return nil, errors.E(op, errors.Protocol, "did not receive verack")
	}
	c.SetReadDeadline(time.Time{})

	rp.out = make(chan *msgAck)
	rp.outPrio = make(chan *msgAck)

	return rp, nil
}

func (lp *LocalPeer) connectOutbound(ctx context.Context, id uint64, addr string) (*RemotePeer, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}

	// Create a net address with assumed services.
	na := wire.NewNetAddressTimestamp(time.Now(),
		wire.SFNodeNetwork|wire.SFNodeCF, tcpAddr.IP, uint16(tcpAddr.Port))

	var c net.Conn
	var retryDuration = 5 * time.Second
	timer := time.NewTimer(retryDuration)
	for {
		// Mark the connection attempt.
		lp.amgr.Attempt(na)

		// Dial with a timeout of 10 seconds.
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		c, err = lp.dialer.DialContext(dialCtx, "tcp", addr)
		cancel()
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
			if retryDuration < 200*time.Second {
				retryDuration += 5 * time.Second
				timer.Reset(retryDuration)
			}
		}
	}
	lp.amgr.Connected(na)

	rp, err := handshake(ctx, lp, id, na, c)
	if err != nil {
		return nil, err
	}

	// Associate connected rp with local peer.
	lp.rpMu.Lock()
	lp.rpByID[rp.id] = rp
	lp.rpMu.Unlock()

	// The real services of the net address are now known.
	na.Services = rp.services

	// Mark this as a good address.
	lp.amgr.Good(na)

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
		<-ctx.Done()
		rp.Disconnect(ctx.Err())
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
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Minute):
				ctx, cancel := context.WithDeadline(ctx, time.Now().Add(15*time.Second))
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
	if !atomic.CompareAndSwapUint64(&rp.atomicClosed, 0, 1) {
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

// RemoteAddr returns the remote address of the peer's TCP connection.
func (rp *RemotePeer) RemoteAddr() net.Addr {
	return rp.c.RemoteAddr()
}

func (rp *RemotePeer) String() string {
	return rp.raddr.String()
}

type inMsg struct {
	rp  *RemotePeer
	msg wire.Message
}

var inMsgPool = sync.Pool{
	New: func() interface{} { return new(inMsg) },
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
				rp.lp.amgr.AddAddresses(m.AddrList, rp.na)
			case *wire.MsgBlock:
				rp.receivedBlock(ctx, m)
			case *wire.MsgCFilter:
				rp.receivedCFilter(ctx, m)
			case *wire.MsgNotFound:
				rp.receivedNotFound(ctx, m)
			case *wire.MsgTx:
				rp.receivedTx(ctx, m)
			case *wire.MsgGetData:
				rp.receivedGetData(ctx, m)
			case *wire.MsgHeaders:
				rp.receivedHeaders(ctx, m)
			case *wire.MsgInv:
				if rp.lp.messageIsMasked(MaskInv) {
					rp.lp.receivedInv <- newInMsg(rp, msg)
				}
			case *wire.MsgReject:
				log.Warnf("%v reject(%v, %v, %v): %v", rp.raddr, m.Cmd, m.Code, &m.Hash, m.Reason)
			case *wire.MsgPing:
				pong(ctx, m, rp)
			case *wire.MsgPong:
				rp.receivedPong(ctx, m)
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
		p := atomic.LoadUint64(&lp.atomicMask)
		n := p | uint64(mask)
		if atomic.CompareAndSwapUint64(&lp.atomicMask, p, n) {
			return
		}
	}
}

// RemoveHandledMessages removes all messages defined by the bitmask.  This
// operation is concurrent safe.
func (lp *LocalPeer) RemoveHandledMessages(mask MessageMask) {
	for {
		p := atomic.LoadUint64(&lp.atomicMask)
		n := p &^ uint64(mask)
		if atomic.CompareAndSwapUint64(&lp.atomicMask, p, n) {
			return
		}
	}
}

func (lp *LocalPeer) messageIsMasked(m MessageMask) bool {
	return atomic.LoadUint64(&lp.atomicMask)&uint64(m) != 0
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

// addRequestBlock records the channel that a requested block is sent to when
// the block message is received.  If a block has already been requested, this
// returns false and the getdata request should not be queued.
func (rp *RemotePeer) addRequestedBlock(hash *chainhash.Hash, c chan<- *wire.MsgBlock) (newRequest bool) {
	_, loaded := rp.requestedBlocks.LoadOrStore(*hash, c)
	return !loaded
}

func (rp *RemotePeer) deleteRequestedBlock(hash *chainhash.Hash) {
	rp.requestedBlocks.Delete(*hash)
}

func (rp *RemotePeer) receivedBlock(ctx context.Context, msg *wire.MsgBlock) {
	const opf = "remotepeer(%v).receivedBlock(%v)"
	blockHash := msg.Header.BlockHash()
	var k interface{} = blockHash
	v, ok := rp.requestedBlocks.Load(k)
	if !ok {
		op := errors.Opf(opf, rp.raddr, &blockHash)
		err := errors.E(op, errors.Protocol, "received unrequested block")
		rp.Disconnect(err)
		return
	}
	rp.requestedBlocks.Delete(k)
	c := v.(chan<- *wire.MsgBlock)
	select {
	case <-ctx.Done():
	case c <- msg:
	}
}

func (rp *RemotePeer) addRequestedCFilter(hash *chainhash.Hash, c chan<- *wire.MsgCFilter) (newRequest bool) {
	_, loaded := rp.requestedCFilters.LoadOrStore(*hash, c)
	return !loaded
}

func (rp *RemotePeer) deleteRequestedCFilter(hash *chainhash.Hash) {
	rp.requestedCFilters.Delete(*hash)
}

func (rp *RemotePeer) receivedCFilter(ctx context.Context, msg *wire.MsgCFilter) {
	const opf = "remotepeer(%v).receivedCFilter(%v)"
	var k interface{} = msg.BlockHash
	v, ok := rp.requestedCFilters.Load(k)
	if !ok {
		op := errors.Opf(opf, rp.raddr, &msg.BlockHash)
		err := errors.E(op, errors.Protocol, "received unrequested cfilter")
		rp.Disconnect(err)
		return
	}
	rp.requestedCFilters.Delete(k)
	c := v.(chan<- *wire.MsgCFilter)
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
	for _, h := range msg.Headers {
		hash := h.BlockHash() // Must be type chainhash.Hash
		rp.knownHeaders.Add(hash)
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

// Block requests a block from a RemotePeer using getdata.  The same block can
// not be requested multiple times concurrently from the same peer.
func (rp *RemotePeer) Block(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	const opf = "remotepeer(%v).Block(%v)"

	m := wire.NewMsgGetDataSizeHint(1)
	err := m.AddInvVect(wire.NewInvVect(wire.InvTypeBlock, blockHash))
	if err != nil {
		op := errors.Opf(opf, rp.raddr, blockHash)
		return nil, errors.E(op, err)
	}
	c := make(chan *wire.MsgBlock, 1)
	if !rp.addRequestedBlock(blockHash, c) {
		op := errors.Opf(opf, rp.raddr, blockHash)
		return nil, errors.E(op, errors.Invalid, "block is already being requested from this peer")
	}

	stalled := time.NewTimer(stallTimeout)
	out := rp.out
	for {
		select {
		case <-ctx.Done():
			go func() {
				<-stalled.C
				rp.deleteRequestedBlock(blockHash)
			}()
			return nil, ctx.Err()
		case <-stalled.C:
			rp.deleteRequestedBlock(blockHash)
			op := errors.Opf(opf, rp.raddr, blockHash)
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
			return m, nil
		}
	}
}

// Blocks requests multiple blocks at a time from a RemotePeer using a single
// getdata message.  It returns when all of the blocks have been received.  The
// same block may not be requested multiple times concurrently from the same
// peer.
func (rp *RemotePeer) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	const opf = "remotepeer(%v).Blocks"

	m := wire.NewMsgGetDataSizeHint(uint(len(blockHashes)))
	cs := make([]chan *wire.MsgBlock, len(blockHashes))
	for i, h := range blockHashes {
		err := m.AddInvVect(wire.NewInvVect(wire.InvTypeBlock, h))
		if err != nil {
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, err)
		}
		cs[i] = make(chan *wire.MsgBlock, 1)
		if !rp.addRequestedBlock(h, cs[i]) {
			for _, h := range blockHashes[:i] {
				rp.deleteRequestedBlock(h)
			}
			op := errors.Opf(opf, rp.raddr)
			return nil, errors.E(op, errors.Errorf("block %v is already being requested from this peer", h))
		}
	}
	stalled := time.NewTimer(stallTimeout)
	select {
	case <-ctx.Done():
		go func() {
			<-stalled.C
			for _, h := range blockHashes {
				rp.deleteRequestedBlock(h)
			}
		}()
		return nil, ctx.Err()
	case <-stalled.C:
		op := errors.Opf(opf, rp.raddr)
		err := errors.E(op, errors.IO, "peer appears stalled")
		rp.Disconnect(err)
		for _, h := range blockHashes {
			rp.deleteRequestedBlock(h)
		}
		return nil, err
	case <-rp.errc:
		stalled.Stop()
		return nil, rp.err
	case rp.out <- &msgAck{m, nil}:
	}
	blocks := make([]*wire.MsgBlock, len(blockHashes))
	for i := 0; i < len(blockHashes); i++ {
		select {
		case <-ctx.Done():
			go func() {
				<-stalled.C
				for _, h := range blockHashes[i:] {
					rp.deleteRequestedBlock(h)
				}
			}()
			return nil, ctx.Err()
		case <-stalled.C:
			op := errors.Opf(opf, rp.raddr)
			err := errors.E(op, errors.IO, "peer appears stalled")
			rp.Disconnect(err)
			for _, h := range blockHashes[i:] {
				rp.deleteRequestedBlock(h)
			}
			return nil, err
		case <-rp.errc:
			stalled.Stop()
			return nil, rp.err
		case m := <-cs[i]:
			blocks[i] = m
			if !stalled.Stop() {
				<-stalled.C
			}
			stalled.Reset(stallTimeout)
		}
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

// CFilter requests a regular compact filter from a RemotePeer using getcfilter.
// The same block can not be requested concurrently from the same peer.
func (rp *RemotePeer) CFilter(ctx context.Context, blockHash *chainhash.Hash) (*gcs.Filter, error) {
	const opf = "remotepeer(%v).CFilter(%v)"

	m := wire.NewMsgGetCFilter(blockHash, wire.GCSFilterRegular)
	c := make(chan *wire.MsgCFilter, 1)
	if !rp.addRequestedCFilter(blockHash, c) {
		op := errors.Opf(opf, rp.raddr, blockHash)
		return nil, errors.E(op, errors.Invalid, "cfilter is already being requested from this peer for this block")
	}
	stalled := time.NewTimer(stallTimeout)
	out := rp.out
	for {
		select {
		case <-ctx.Done():
			go func() {
				<-stalled.C
				rp.deleteRequestedCFilter(blockHash)
			}()
			return nil, ctx.Err()
		case <-stalled.C:
			rp.deleteRequestedCFilter(blockHash)
			op := errors.Opf(opf, rp.raddr, blockHash)
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
			var f *gcs.Filter
			var err error
			if len(m.Data) == 0 {
				f, err = gcs.FromBytes(0, blockcf.P, nil)
			} else {
				f, err = gcs.FromNBytes(blockcf.P, m.Data)
			}
			if err != nil {
				op := errors.Opf(opf, rp.raddr, blockHash)
				return nil, errors.E(op, err)
			}
			return f, nil
		}
	}
}

// CFilters requests cfilters for all blocks described by blockHashes.  This
// is currently implemented by making many separate getcfilter requests
// concurrently and waiting on every result.
func (rp *RemotePeer) CFilters(ctx context.Context, blockHashes []*chainhash.Hash) ([]*gcs.Filter, error) {
	const opf = "remotepeer(%v).CFilters"

	// TODO: this is spammy and would be better implemented with a single
	// request/response.
	filters := make([]*gcs.Filter, len(blockHashes))
	g, ctx := errgroup.WithContext(ctx)
	for i := range blockHashes {
		i := i
		g.Go(func() error {
			f, err := rp.CFilter(ctx, blockHashes[i])
			filters[i] = f
			return err
		})
	}
	err := g.Wait()
	if err != nil {
		return nil, err
	}
	return filters, nil
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
			return m.Headers, nil
		}
	}
}

// PublishTransactions pushes an inventory message advertising transaction
// hashes of txs.
func (rp *RemotePeer) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	const opf = "remotepeer(%v).PublishTransactions"
	msg := wire.NewMsgInvSizeHint(uint(len(txs)))
	for i := range txs {
		txHash := txs[i].TxHash() // Must be type chainhash.Hash
		rp.invsSent.Add(txHash)
		err := msg.AddInvVect(wire.NewInvVect(wire.InvTypeTx, &txHash))
		if err != nil {
			op := errors.Opf(opf, rp.raddr)
			return errors.E(op, errors.Protocol, err)
		}
	}
	err := rp.SendMessage(ctx, msg)
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
