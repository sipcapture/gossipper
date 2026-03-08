package transport

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"sync"

	"github.com/adubovikov/gossipper/internal/sip"
)

type SharedTLS struct {
	conn      *tls.Conn
	reader    *bufio.Reader
	incoming  chan sip.Message
	closeOnce sync.Once
}

func NewSharedTLS(localAddr, remoteAddr string, cfg *tls.Config) (*SharedTLS, error) {
	local, err := net.ResolveTCPAddr("tcp", localAddr)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{LocalAddr: local}
	conn, err := tls.DialWithDialer(dialer, "tcp", remoteAddr, cfg)
	if err != nil {
		return nil, err
	}
	s := &SharedTLS{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		incoming: make(chan sip.Message, 128),
	}
	go s.readLoop()
	return s, nil
}

func (s *SharedTLS) readLoop() {
	for {
		msg, err := sip.ReadMessage(s.reader)
		if err != nil {
			close(s.incoming)
			return
		}
		s.incoming <- msg
	}
}

func (s *SharedTLS) Send(payload []byte) error {
	_, err := s.conn.Write(payload)
	return err
}

func (s *SharedTLS) Receive() <-chan sip.Message {
	return s.incoming
}

func (s *SharedTLS) LocalPort() int {
	if addr, ok := s.conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (s *SharedTLS) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
	})
	return err
}

type DialogTLS struct {
	conn   *tls.Conn
	reader *bufio.Reader
}

func NewDialogTLS(localAddr, remoteAddr string, cfg *tls.Config) (*DialogTLS, error) {
	local, err := net.ResolveTCPAddr("tcp", localAddr)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{LocalAddr: local}
	conn, err := tls.DialWithDialer(dialer, "tcp", remoteAddr, cfg)
	if err != nil {
		return nil, err
	}
	return &DialogTLS{conn: conn, reader: bufio.NewReader(conn)}, nil
}

func (d *DialogTLS) Send(payload []byte) error {
	_, err := d.conn.Write(payload)
	return err
}

func (d *DialogTLS) Receive(ctx context.Context) (sip.Message, error) {
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

func (d *DialogTLS) LocalPort() int {
	if addr, ok := d.conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (d *DialogTLS) Close() error {
	return d.conn.Close()
}

type TLSServer struct {
	listener net.Listener
}

func NewTLSServer(localAddr string, cfg *tls.Config) (*TLSServer, error) {
	listener, err := tls.Listen("tcp", localAddr, cfg)
	if err != nil {
		return nil, err
	}
	return &TLSServer{listener: listener}, nil
}

func (s *TLSServer) Accept(ctx context.Context) (*tls.Conn, error) {
	type acceptResult struct {
		conn *tls.Conn
		err  error
	}
	resultCh := make(chan acceptResult, 1)
	go func() {
		conn, err := s.listener.Accept()
		if err != nil {
			resultCh <- acceptResult{err: err}
			return
		}
		tlsConn, _ := conn.(*tls.Conn)
		resultCh <- acceptResult{conn: tlsConn}
	}()
	select {
	case <-ctx.Done():
		_ = s.listener.Close()
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.conn, result.err
	}
}

func (s *TLSServer) LocalPort() int {
	if addr, ok := s.listener.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (s *TLSServer) Close() error {
	return s.listener.Close()
}

type TLSConnReader struct {
	conn   *tls.Conn
	reader *bufio.Reader
}

func NewTLSConnReader(conn *tls.Conn) *TLSConnReader {
	return &TLSConnReader{conn: conn, reader: bufio.NewReader(conn)}
}

func (r *TLSConnReader) Read(ctx context.Context) (sip.Message, error) {
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

func (r *TLSConnReader) Write(payload []byte) error {
	_, err := r.conn.Write(payload)
	return err
}

func (r *TLSConnReader) LocalPort() int {
	if addr, ok := r.conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (r *TLSConnReader) RemoteAddr() *net.TCPAddr {
	addr, _ := r.conn.RemoteAddr().(*net.TCPAddr)
	return addr
}

func (r *TLSConnReader) Close() error {
	return r.conn.Close()
}
