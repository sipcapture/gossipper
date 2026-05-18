package transport

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	// maxUDPSIPDatagram is the maximum expected SIP message size over UDP.
	// SIP messages rarely exceed MTU (1500 bytes); 4096 covers jumbo or
	// multi-line SDP while using 16x less memory than the theoretical UDP max.
	maxUDPSIPDatagram = 4096
	// inboundChanDepth is the base depth of SharedUDP Receive channel per receiver slot (scaled below).
	inboundChanDepth = 512
	maxInboundChan   = 8192
	udpSocketRecvBuf = 8 * 1024 * 1024
	udpSocketSendBuf = 1024 * 1024
	// maxUDPReceivers caps parallel SO_REUSEPORT listeners (kernel spreads ingress across them).
	maxUDPReceivers = 16
	envUDPReceivers = "GOSSIPPER_UDP_RECEIVERS"
)

// Linux tuning hints for lab/production high ingress:
//   sysctl net.core.rmem_max, net.core.netdev_max_backlog; optionally busy polling on NIC path.
// Parallel listeners: set GOSSIPPER_UDP_RECEIVERS to match core count (capped at maxUDPReceivers).

type Packet struct {
	Data []byte
	Addr *net.UDPAddr
	pool *sync.Pool // non-nil when Data is from a pool; call Release() after use
}

// Release returns the underlying buffer to the pool. Safe to call on zero-value.
func (p *Packet) Release() {
	if p.pool != nil && p.Data != nil {
		p.pool.Put(p.Data[:cap(p.Data)])
		p.Data = nil
		p.pool = nil
	}
}

// udpBufPool reuses read buffers to reduce GC pressure under high packet rates.
var udpBufPool = sync.Pool{
	New: func() any { return make([]byte, maxUDPSIPDatagram) },
}

// SharedUDP is a UDP socket shared by multiple logical SIP flows (gossipper UAC/UAS).
// On supported platforms it may use several listeners with SO_REUSEPORT and one merged Receive channel.
type SharedUDP struct {
	conns         []*net.UDPConn
	incoming      chan Packet
	closeOnce     sync.Once
	incomingOnce  sync.Once
	closed        atomic.Bool
	receiverCount int
	sendIdx       atomic.Uint64 // round-robin index for send distribution
}

// NewSharedUDP binds localAddr and starts ingress goroutines. Parallelism is controlled by
// environment variable GOSSIPPER_UDP_RECEIVERS (integer): 0 or unset means auto (see effectiveReceivers).
func NewSharedUDP(localAddr string) (*SharedUDP, error) {
	return NewSharedUDPWithReceivers(localAddr, receiversFromEnv())
}

// NewSharedUDPWithReceivers is like NewSharedUDP but takes an explicit receiver count (0 = auto).
// Intended for tests; production tuning uses GOSSIPPER_UDP_RECEIVERS.
func NewSharedUDPWithReceivers(localAddr string, receivers int) (*SharedUDP, error) {
	addr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return nil, err
	}
	n := effectiveReceivers(receivers)
	conns, err := listenParallelUDP(addr, n)
	if err != nil {
		return nil, err
	}
	chDepth := inboundChanDepthFor(n)
	s := &SharedUDP{
		conns:         conns,
		incoming:      make(chan Packet, chDepth),
		receiverCount: len(conns),
	}
	for _, c := range conns {
		go s.readLoop(c)
	}
	return s, nil
}

// ReceiverCount returns how many parallel UDP listeners were started (SO_REUSEPORT fan-in).
func (s *SharedUDP) ReceiverCount() int {
	return s.receiverCount
}

func receiversFromEnv() int {
	v := strings.TrimSpace(os.Getenv(envUDPReceivers))
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func effectiveReceivers(requested int) int {
	if requested <= 0 {
		requested = autoReceiverCount()
	}
	return clampReceivers(requested)
}

func autoReceiverCount() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return clampReceivers(n)
}

func clampReceivers(n int) int {
	if n < 1 {
		n = 1
	}
	if n > maxUDPReceivers {
		n = maxUDPReceivers
	}
	return maybeSingleReceiverOS(n)
}

func inboundChanDepthFor(receivers int) int {
	d := inboundChanDepth * receivers
	if d > maxInboundChan {
		d = maxInboundChan
	}
	if d < inboundChanDepth {
		d = inboundChanDepth
	}
	return d
}

func tuneUDPConn(c *net.UDPConn) {
	_ = c.SetReadBuffer(udpSocketRecvBuf)
	_ = c.SetWriteBuffer(udpSocketSendBuf)
}

func (s *SharedUDP) readLoop(conn *net.UDPConn) {
	for {
		buffer := udpBufPool.Get().([]byte)
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			udpBufPool.Put(buffer)
			s.shutdownIncoming()
			return
		}
		if n <= 0 || n > maxUDPSIPDatagram {
			udpBufPool.Put(buffer)
			continue
		}
		if addr == nil {
			udpBufPool.Put(buffer)
			continue
		}
		p := Packet{Data: buffer[:n], Addr: addr, pool: &udpBufPool}
		select {
		case s.incoming <- p:
		default:
			select {
			case s.incoming <- p:
			default:
				udpBufPool.Put(buffer)
			}
		}
	}
}

func (s *SharedUDP) shutdownIncoming() {
	s.incomingOnce.Do(func() {
		close(s.incoming)
	})
}

func (s *SharedUDP) Send(payload []byte, addr *net.UDPAddr) error {
	if s.closed.Load() {
		return net.ErrClosed
	}
	if len(s.conns) == 0 {
		return errors.New("transport: UDP not initialized")
	}
	// Round-robin across sockets to reduce kernel lock contention.
	idx := s.sendIdx.Add(1) % uint64(len(s.conns))
	_, err := s.conns[idx].WriteToUDP(payload, addr)
	return err
}

func (s *SharedUDP) Receive() <-chan Packet {
	return s.incoming
}

func (s *SharedUDP) LocalPort() int {
	if len(s.conns) == 0 {
		return 0
	}
	if addr, ok := s.conns[0].LocalAddr().(*net.UDPAddr); ok && addr != nil {
		return addr.Port
	}
	return 0
}

func (s *SharedUDP) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		for _, c := range s.conns {
			if e := c.Close(); e != nil && err == nil {
				err = e
			}
		}
		s.shutdownIncoming()
	})
	return err
}

type DialogUDP struct {
	conn   *net.UDPConn
	remote *net.UDPAddr
}

func NewDialogUDP(localAddr, remoteAddr string) (*DialogUDP, error) {
	local, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return nil, err
	}
	remote, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", local)
	if err != nil {
		return nil, err
	}
	return &DialogUDP{conn: conn, remote: remote}, nil
}

func (d *DialogUDP) Send(payload []byte) error {
	_, err := d.conn.WriteToUDP(payload, d.remote)
	return err
}

func (d *DialogUDP) Receive(ctx context.Context) (Packet, error) {
	buffer := make([]byte, 65535)
	go func() {
		<-ctx.Done()
		_ = d.conn.SetReadDeadline(deadlineFromContext(ctx))
	}()
	n, addr, err := d.conn.ReadFromUDP(buffer)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return Packet{}, ctx.Err()
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return Packet{}, ctx.Err()
		}
		return Packet{}, err
	}
	payload := make([]byte, n)
	copy(payload, buffer[:n])
	return Packet{Data: payload, Addr: addr}, nil
}

func (d *DialogUDP) LocalPort() int {
	if addr, ok := d.conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.Port
	}
	return 0
}

func (d *DialogUDP) Close() error {
	return d.conn.Close()
}
