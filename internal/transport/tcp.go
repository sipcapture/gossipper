package transport

import (
	"bufio"
	"context"
	"net"
	"sync"
	"time"

	"github.com/qxip/gossipper/internal/sip"
)

type ReconnectOptions struct {
	MaxAttempts      int
	Sleep            time.Duration
	CloseOnReconnect bool
}

type SharedTCP struct {
	localAddr  string
	remoteAddr string
	reconnect  ReconnectOptions

	connMu      sync.RWMutex
	reconnectMu sync.Mutex
	conn        *net.TCPConn
	reader      *bufio.Reader
	incoming    chan sip.Message
	closeOnce   sync.Once
	closed      chan struct{}
}

func NewSharedTCP(localAddr, remoteAddr string) (*SharedTCP, error) {
	return NewSharedTCPWithReconnect(localAddr, remoteAddr, ReconnectOptions{})
}

func NewSharedTCPWithReconnect(localAddr, remoteAddr string, reconnect ReconnectOptions) (*SharedTCP, error) {
	conn, err := dialSharedTCP(localAddr, remoteAddr)
	if err != nil {
		return nil, err
	}
	s := &SharedTCP{
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		reconnect:  reconnect,
		conn:       conn,
		reader:     bufio.NewReader(conn),
		incoming:   make(chan sip.Message, 128),
		closed:     make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

func (s *SharedTCP) readLoop() {
	for {
		if s.isClosed() {
			close(s.incoming)
			return
		}
		s.connMu.RLock()
		reader := s.reader
		s.connMu.RUnlock()
		if reader == nil {
			close(s.incoming)
			return
		}
		msg, err := sip.ReadMessage(reader)
		if err != nil {
			if s.tryReconnect() {
				continue
			}
			close(s.incoming)
			return
		}
		select {
		case <-s.closed:
			close(s.incoming)
			return
		case s.incoming <- msg:
		}
	}
}

func (s *SharedTCP) Send(payload []byte) error {
	s.connMu.RLock()
	conn := s.conn
	s.connMu.RUnlock()
	if conn == nil {
		return net.ErrClosed
	}
	_, err := conn.Write(payload)
	if err == nil {
		return nil
	}
	if !s.tryReconnect() {
		return err
	}
	s.connMu.RLock()
	conn = s.conn
	s.connMu.RUnlock()
	if conn == nil {
		return net.ErrClosed
	}
	_, err = conn.Write(payload)
	return err
}

func (s *SharedTCP) Receive() <-chan sip.Message {
	return s.incoming
}

func (s *SharedTCP) LocalPort() int {
	s.connMu.RLock()
	conn := s.conn
	s.connMu.RUnlock()
	if conn == nil {
		return 0
	}
	if addr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (s *SharedTCP) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		s.connMu.RLock()
		conn := s.conn
		s.connMu.RUnlock()
		if conn != nil {
			err = conn.Close()
		}
	})
	return err
}

func (s *SharedTCP) tryReconnect() bool {
	if s.reconnect.CloseOnReconnect || s.reconnect.MaxAttempts <= 0 || s.isClosed() {
		return false
	}
	s.reconnectMu.Lock()
	defer s.reconnectMu.Unlock()
	if s.isClosed() {
		return false
	}
	for attempt := 0; attempt < s.reconnect.MaxAttempts; attempt++ {
		if attempt > 0 && s.reconnect.Sleep > 0 {
			if !s.sleepUnlessClosed(s.reconnect.Sleep) {
				return false
			}
		}
		conn, err := dialSharedTCP(s.localAddr, s.remoteAddr)
		if err != nil {
			continue
		}
		s.connMu.Lock()
		old := s.conn
		s.conn = conn
		s.reader = bufio.NewReader(conn)
		s.connMu.Unlock()
		if old != nil {
			_ = old.Close()
		}
		return true
	}
	return false
}

func (s *SharedTCP) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *SharedTCP) sleepUnlessClosed(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.closed:
		return false
	case <-timer.C:
		return true
	}
}

func dialSharedTCP(localAddr, remoteAddr string) (*net.TCPConn, error) {
	local, err := net.ResolveTCPAddr("tcp", localAddr)
	if err != nil {
		return nil, err
	}
	remote, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		return nil, err
	}
	return net.DialTCP("tcp", local, remote)
}

type DialogTCP struct {
	conn   *net.TCPConn
	reader *bufio.Reader
}

func NewDialogTCP(localAddr, remoteAddr string) (*DialogTCP, error) {
	local, err := net.ResolveTCPAddr("tcp", localAddr)
	if err != nil {
		return nil, err
	}
	remote, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTCP("tcp", local, remote)
	if err != nil {
		return nil, err
	}
	return &DialogTCP{conn: conn, reader: bufio.NewReader(conn)}, nil
}

func (d *DialogTCP) Send(payload []byte) error {
	_, err := d.conn.Write(payload)
	return err
}

func (d *DialogTCP) Receive(ctx context.Context) (sip.Message, error) {
	go func() {
		<-ctx.Done()
		_ = d.conn.SetReadDeadline(deadlineFromContext(ctx))
	}()
	msg, err := sip.ReadMessage(d.reader)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return sip.Message{}, ctx.Err()
		}
		return sip.Message{}, err
	}
	return msg, nil
}

func (d *DialogTCP) LocalPort() int {
	if addr, ok := d.conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (d *DialogTCP) Close() error {
	return d.conn.Close()
}

type TCPServer struct {
	listener *net.TCPListener
}

func NewTCPServer(localAddr string) (*TCPServer, error) {
	addr, err := net.ResolveTCPAddr("tcp", localAddr)
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &TCPServer{listener: listener}, nil
}

func (s *TCPServer) Accept(ctx context.Context) (*net.TCPConn, error) {
	go func() {
		<-ctx.Done()
		_ = s.listener.SetDeadline(deadlineFromContext(ctx))
	}()
	return s.listener.AcceptTCP()
}

func (s *TCPServer) LocalPort() int {
	if addr, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (s *TCPServer) Close() error {
	return s.listener.Close()
}

type TCPConnReader struct {
	conn   *net.TCPConn
	reader *bufio.Reader
}

func NewTCPConnReader(conn *net.TCPConn) *TCPConnReader {
	return &TCPConnReader{conn: conn, reader: bufio.NewReader(conn)}
}

func (r *TCPConnReader) Read(ctx context.Context) (sip.Message, error) {
	go func() {
		<-ctx.Done()
		_ = r.conn.SetReadDeadline(deadlineFromContext(ctx))
	}()
	msg, err := sip.ReadMessage(r.reader)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return sip.Message{}, ctx.Err()
		}
		return sip.Message{}, err
	}
	return msg, nil
}

func (r *TCPConnReader) Write(payload []byte) error {
	_, err := r.conn.Write(payload)
	return err
}

func (r *TCPConnReader) LocalPort() int {
	if addr, ok := r.conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (r *TCPConnReader) RemoteAddr() *net.TCPAddr {
	addr, _ := r.conn.RemoteAddr().(*net.TCPAddr)
	return addr
}

func (r *TCPConnReader) Close() error {
	return r.conn.Close()
}

func deadlineFromContext(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now()
}
