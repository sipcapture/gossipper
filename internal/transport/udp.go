package transport

import (
	"context"
	"errors"
	"net"
	"sync"
)

type Packet struct {
	Data []byte
	Addr *net.UDPAddr
}

type SharedUDP struct {
	conn      *net.UDPConn
	incoming  chan Packet
	closeOnce sync.Once
}

func NewSharedUDP(localAddr string) (*SharedUDP, error) {
	addr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	s := &SharedUDP{
		conn:     conn,
		incoming: make(chan Packet, 128),
	}
	go s.readLoop()
	return s, nil
}

func (s *SharedUDP) readLoop() {
	buffer := make([]byte, 65535)
	for {
		n, addr, err := s.conn.ReadFromUDP(buffer)
		if err != nil {
			close(s.incoming)
			return
		}
		payload := make([]byte, n)
		copy(payload, buffer[:n])
		s.incoming <- Packet{Data: payload, Addr: addr}
	}
}

func (s *SharedUDP) Send(payload []byte, addr *net.UDPAddr) error {
	_, err := s.conn.WriteToUDP(payload, addr)
	return err
}

func (s *SharedUDP) Receive() <-chan Packet {
	return s.incoming
}

func (s *SharedUDP) LocalPort() int {
	if addr, ok := s.conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.Port
	}
	return 0
}

func (s *SharedUDP) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
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
