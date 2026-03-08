package transport

import (
	"bufio"
	"context"
	"net"
	"sync"
	"time"

	"github.com/adubovikov/gossip/internal/sip"
)

type SharedTCP struct {
	conn      *net.TCPConn
	reader    *bufio.Reader
	incoming  chan sip.Message
	closeOnce sync.Once
}

func NewSharedTCP(localAddr, remoteAddr string) (*SharedTCP, error) {
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
	s := &SharedTCP{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		incoming: make(chan sip.Message, 128),
	}
	go s.readLoop()
	return s, nil
}

func (s *SharedTCP) readLoop() {
	for {
		msg, err := sip.ReadMessage(s.reader)
		if err != nil {
			close(s.incoming)
			return
		}
		s.incoming <- msg
	}
}

func (s *SharedTCP) Send(payload []byte) error {
	_, err := s.conn.Write(payload)
	return err
}

func (s *SharedTCP) Receive() <-chan sip.Message {
	return s.incoming
}

func (s *SharedTCP) LocalPort() int {
	if addr, ok := s.conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

func (s *SharedTCP) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
	})
	return err
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
