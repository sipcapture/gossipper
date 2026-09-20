package transport

import (
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestSharedUDPOpenClose(t *testing.T) {
	s, err := NewSharedUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedUDPParallelReceivers(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("SO_REUSEPORT parallel listeners not used on this platform")
	}
	s, err := NewSharedUDPWithReceivers("127.0.0.1:0", 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if n := s.ReceiverCount(); n < 2 {
		t.Fatalf("expected at least 2 parallel receivers on unix, got %d", n)
	}
	la := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.LocalPort()))
	remote, err := net.ResolveUDPAddr("udp", la)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("ping")
	if err := s.Send(payload, remote); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-s.Receive():
		if string(p.Data) != "ping" {
			t.Fatalf("payload = %q", p.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for self-sent UDP packet")
	}
}

func TestSharedUDPTap(t *testing.T) {
	s, err := NewSharedUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	got := make(chan string, 8)
	s.SetTap(func(dir string, payload []byte, _ *net.UDPAddr) {
		got <- dir + ":" + string(payload)
	})
	la := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.LocalPort()))
	remote, err := net.ResolveUDPAddr("udp", la)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send([]byte("OPTIONS sip:x SIP/2.0"), remote); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case v := <-got:
			seen[v] = true
		case <-deadline:
			t.Fatalf("taps=%v", seen)
		}
	}
	if !seen["send:OPTIONS sip:x SIP/2.0"] || !seen["recv:OPTIONS sip:x SIP/2.0"] {
		t.Fatalf("taps=%v", seen)
	}
	select {
	case p := <-s.Receive():
		p.Release()
	case <-time.After(500 * time.Millisecond):
	}
}
