package transport

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestSharedTCPReconnectsAfterSocketLoss(t *testing.T) {
	t.Parallel()

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenTCP() error = %v", err)
	}
	addr := listener.Addr().String()
	defer listener.Close()

	firstPayloadCh := make(chan []byte, 1)
	go acceptAndReadPayload(t, listener, firstPayloadCh, true)

	client, err := NewSharedTCPWithReconnect("127.0.0.1:0", addr, ReconnectOptions{
		MaxAttempts: 50,
		Sleep:       20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSharedTCPWithReconnect() error = %v", err)
	}
	defer client.Close()

	firstPayload := []byte("first-payload")
	if err := client.Send(firstPayload); err != nil {
		t.Fatalf("Send(first) error = %v", err)
	}
	select {
	case got := <-firstPayloadCh:
		if !bytes.Equal(got, firstPayload) {
			t.Fatalf("unexpected first payload: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting first payload")
	}

	_ = listener.Close()
	time.Sleep(120 * time.Millisecond)

	listener2, err := net.ListenTCP("tcp", mustResolveTCPAddr(t, addr))
	if err != nil {
		t.Fatalf("ListenTCP(second) error = %v", err)
	}
	defer listener2.Close()

	secondPayloadCh := make(chan []byte, 1)
	go acceptAndReadPayload(t, listener2, secondPayloadCh, false)

	secondPayload := []byte("second-payload")
	if err := client.Send(secondPayload); err != nil {
		t.Fatalf("Send(second) error = %v", err)
	}
	select {
	case got := <-secondPayloadCh:
		if !bytes.Equal(got, secondPayload) {
			t.Fatalf("unexpected second payload: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting second payload")
	}
}

func TestSharedTCPCloseOnReconnectDisablesReconnectAttempts(t *testing.T) {
	t.Parallel()

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenTCP() error = %v", err)
	}
	addr := listener.Addr().String()
	defer listener.Close()

	firstPayloadCh := make(chan []byte, 1)
	go acceptAndReadPayload(t, listener, firstPayloadCh, true)

	client, err := NewSharedTCPWithReconnect("127.0.0.1:0", addr, ReconnectOptions{
		MaxAttempts:      20,
		Sleep:            20 * time.Millisecond,
		CloseOnReconnect: true,
	})
	if err != nil {
		t.Fatalf("NewSharedTCPWithReconnect() error = %v", err)
	}
	defer client.Close()

	if err := client.Send([]byte("first")); err != nil {
		t.Fatalf("Send(first) error = %v", err)
	}
	select {
	case <-firstPayloadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting first payload")
	}

	_ = listener.Close()
	time.Sleep(120 * time.Millisecond)

	if err := client.Send([]byte("second")); err == nil {
		t.Fatal("expected send to fail when reconnect_close is enabled")
	}
}

func acceptAndReadPayload(t *testing.T, listener *net.TCPListener, out chan<- []byte, closeWithRST bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = listener.SetDeadline(time.Now().Add(200 * time.Millisecond))
		conn, err := listener.AcceptTCP()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		if closeWithRST {
			_ = conn.SetLinger(0)
		}

		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		_ = conn.Close()
		if err != nil && err != io.EOF {
			continue
		}
		if n == 0 {
			continue
		}
		out <- append([]byte(nil), buf[:n]...)
		return
	}
}

func mustResolveTCPAddr(t *testing.T, addr string) *net.TCPAddr {
	t.Helper()
	resolved, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		t.Fatalf("ResolveTCPAddr(%q) error = %v", addr, err)
	}
	return resolved
}
