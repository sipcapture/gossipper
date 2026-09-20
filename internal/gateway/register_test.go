package gateway

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/sip"
)

func TestRegistrarDigest401Then200(t *testing.T) {
	t.Parallel()
	stub := startStubRegistrar(t, stubMode{
		challengeFirst: true,
		expires:        60,
	})
	reg := NewRegistrar(Config{
		Domain:          "pbx.local",
		Addr:            stub.addr,
		Username:        "1001",
		Password:        "secret",
		Register:        true,
		AdvertisedIP:    "127.0.0.1",
		ContactPort:     5060,
		RegisterExpires: 60,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Loop(ctx)
	waitState(t, reg, StateRegistered, 3*time.Second)
	st := reg.Snapshot()
	if st.Code != 200 {
		t.Fatalf("code=%d reason=%q trace=%s", st.Code, st.Reason, st.Trace)
	}
	if !strings.Contains(st.Trace, "WWW-Authenticate") && !strings.Contains(st.Trace, "401") {
		t.Fatalf("expected digest challenge in trace: %s", st.Trace)
	}
	if !strings.Contains(strings.ToLower(stub.lastAuth()), "digest") {
		t.Fatalf("expected Digest Authorization, got %q", stub.lastAuth())
	}
}

func TestRegistrarRefreshAndDeregisterExpires0(t *testing.T) {
	prevRetry := registerRetry
	prevMin := registerMinRefresh
	registerRetry = 50 * time.Millisecond
	registerMinRefresh = 40 * time.Millisecond
	t.Cleanup(func() {
		registerRetry = prevRetry
		registerMinRefresh = prevMin
	})

	stub := startStubRegistrar(t, stubMode{expires: 1})
	reg := NewRegistrar(Config{
		Domain:          "pbx.local",
		Addr:            stub.addr,
		Username:        "1001",
		Password:        "secret",
		Register:        true,
		AdvertisedIP:    "192.168.1.20",
		ContactPort:     5060,
		RegisterExpires: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Loop(ctx)
	waitState(t, reg, StateRegistered, 3*time.Second)
	waitCount(t, stub, 2, 3*time.Second)
	cancel()
	waitExpires0(t, stub, 3*time.Second)
	ports := stub.portsSnapshot()
	if len(ports) < 2 {
		t.Fatalf("want multiple REGISTER, got ports %v", ports)
	}
	for i := 1; i < len(ports); i++ {
		if ports[i] != ports[0] {
			t.Fatalf("REGISTER source port changed %v (FreeSWITCH NAT needs a stable port)", ports)
		}
	}
	ids := stub.callIDsSnapshot()
	if len(ids) < 2 {
		t.Fatalf("want Call-IDs, got %v", ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[0] {
			t.Fatalf("REGISTER Call-ID changed %v", ids)
		}
	}
}

func TestRegistrarKeepaliveOPTIONSSamePort(t *testing.T) {
	t.Parallel()
	stub := startStubRegistrar(t, stubMode{expires: 60})
	reg := NewRegistrar(Config{
		Domain:           "pbx.local",
		Addr:             stub.addr,
		Username:         "1001",
		Password:         "secret",
		Register:         true,
		AdvertisedIP:     "192.168.1.20",
		ContactPort:      5060,
		RegisterExpires:  60,
		KeepaliveSeconds: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Loop(ctx)
	waitState(t, reg, StateRegistered, 3*time.Second)
	waitOptions(t, stub, 1, 3*time.Second)
	regPorts := stub.portsSnapshot()
	optPorts := stub.optionPortsSnapshot()
	if len(regPorts) == 0 || len(optPorts) == 0 {
		t.Fatalf("want REGISTER and OPTIONS ports, got %v / %v", regPorts, optPorts)
	}
	if optPorts[0] != regPorts[0] {
		t.Fatalf("OPTIONS source port %d != REGISTER %d (NAT mapping)", optPorts[0], regPorts[0])
	}
}

func TestRegistrarContactUsesSocketPort(t *testing.T) {
	t.Parallel()
	stub := startStubRegistrar(t, stubMode{expires: 60})
	reg := NewRegistrar(Config{
		Domain:          "pbx.local",
		Addr:            stub.addr,
		Username:        "1001",
		Password:        "secret",
		Register:        true,
		AdvertisedIP:    "203.0.113.9",
		ContactPort:     15069,
		RegisterExpires: 60,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Loop(ctx)
	waitState(t, reg, StateRegistered, 3*time.Second)
	ports := stub.portsSnapshot()
	if len(ports) == 0 {
		t.Fatal("no REGISTER source port")
	}
	contact := stub.lastContact()
	want := fmt.Sprintf("sip:1001@203.0.113.9:%d", ports[0])
	if !strings.Contains(contact, want) {
		t.Fatalf("Contact must use REGISTER socket port, want %s got %q", want, contact)
	}
	if strings.Contains(contact, ":15069") {
		t.Fatalf("Contact used stale contact_port, got %q", contact)
	}
}

func TestRegistrarViaUsesAdvertisedIP(t *testing.T) {
	t.Parallel()
	stub := startStubRegistrar(t, stubMode{expires: 60})
	reg := NewRegistrar(Config{
		Domain:          "pbx.local",
		Addr:            stub.addr,
		Username:        "1001",
		Password:        "secret",
		Register:        true,
		AdvertisedIP:    "203.0.113.9",
		ContactPort:     5060,
		RegisterExpires: 60,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Loop(ctx)
	waitState(t, reg, StateRegistered, 3*time.Second)
	via := stub.lastVia()
	if !strings.Contains(via, "203.0.113.9") {
		t.Fatalf("Via should use advertised_ip, got %q", via)
	}
}

func TestRegistrarFailThenRetry(t *testing.T) {
	prevRetry := registerRetry
	registerRetry = 40 * time.Millisecond
	t.Cleanup(func() { registerRetry = prevRetry })

	stub := startStubRegistrar(t, stubMode{failThenOK: true, expires: 60})
	reg := NewRegistrar(Config{
		Domain:          "pbx.local",
		Addr:            stub.addr,
		Username:        "1001",
		Password:        "secret",
		Register:        true,
		AdvertisedIP:    "127.0.0.1",
		ContactPort:     5060,
		RegisterExpires: 60,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Loop(ctx)
	waitState(t, reg, StateFailed, 3*time.Second)
	waitState(t, reg, StateRegistered, 3*time.Second)
}

type stubMode struct {
	challengeFirst bool
	failThenOK     bool
	expires        int
}

type stubRegistrar struct {
	addr     string
	mu       sync.Mutex
	n        int
	options  int
	auth     string
	exp0     bool
	ports    []int
	optPorts []int
	ids      []string
	via      string
	contact  string
}

func (s *stubRegistrar) lastVia() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.via
}

func (s *stubRegistrar) lastContact() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contact
}

func (s *stubRegistrar) portsSnapshot() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, len(s.ports))
	copy(out, s.ports)
	return out
}

func (s *stubRegistrar) callIDsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.ids))
	copy(out, s.ids)
	return out
}

func (s *stubRegistrar) optionPortsSnapshot() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, len(s.optPorts))
	copy(out, s.optPorts)
	return out
}

func (s *stubRegistrar) lastAuth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth
}

func (s *stubRegistrar) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func startStubRegistrar(t *testing.T, mode stubMode) *stubRegistrar {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	s := &stubRegistrar{addr: pc.LocalAddr().String()}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			raw := append([]byte(nil), buf[:n]...)
			msg, err := sip.Parse(raw)
			if err != nil {
				continue
			}
			s.mu.Lock()
			srcPort := 0
			if ua, ok := addr.(*net.UDPAddr); ok {
				srcPort = ua.Port
			}
			if strings.EqualFold(msg.Method, "OPTIONS") {
				s.options++
				s.optPorts = append(s.optPorts, srcPort)
				s.mu.Unlock()
				_, _ = pc.WriteTo(sipReply(msg, 200, "OK", ""), addr)
				continue
			}
			s.n++
			nSeen := s.n
			if srcPort > 0 {
				s.ports = append(s.ports, srcPort)
			}
			if v, ok := sip.Header(msg.Headers, "Call-ID"); ok {
				s.ids = append(s.ids, v)
			}
			if v, ok := sip.Header(msg.Headers, "Via"); ok {
				s.via = v
			}
			if v, ok := sip.Header(msg.Headers, "Contact"); ok {
				s.contact = v
			}
			if v, ok := sip.Header(msg.Headers, "Expires"); ok && strings.TrimSpace(v) == "0" {
				s.exp0 = true
			}
			if v, ok := sip.Header(msg.Headers, "Authorization"); ok {
				s.auth = v
			}
			s.mu.Unlock()

			extra := ""
			code, reason := 200, "OK"
			switch {
			case mode.failThenOK && nSeen == 1:
				code, reason = 403, "Forbidden"
			case mode.challengeFirst && nSeen == 1:
				code, reason = 401, "Unauthorized"
				extra = `WWW-Authenticate: Digest realm="pbx.local", nonce="n1", algorithm=MD5` + "\r\n"
			default:
				if mode.expires > 0 {
					extra = fmt.Sprintf("Expires: %d\r\n", mode.expires)
				}
			}
			_, _ = pc.WriteTo(sipReply(msg, code, reason, extra), addr)
		}
	}()
	return s
}

func sipReply(req sip.Message, code int, reason, extra string) []byte {
	via, _ := sip.Header(req.Headers, "Via")
	from, _ := sip.Header(req.Headers, "From")
	to, _ := sip.Header(req.Headers, "To")
	callID, _ := sip.Header(req.Headers, "Call-ID")
	cseq, _ := sip.Header(req.Headers, "CSeq")
	return []byte(fmt.Sprintf(
		"SIP/2.0 %d %s\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: 0\r\n\r\n",
		code, reason, via, from, to, callID, cseq, extra,
	))
}

func waitState(t *testing.T, r *Registrar, want string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if r.Snapshot().State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := r.Snapshot()
	t.Fatalf("want state %s, got %+v", want, st)
}

func waitCount(t *testing.T, s *stubRegistrar, min int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s.count() >= min {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want >= %d REGISTER, got %d", min, s.count())
}

func waitOptions(t *testing.T, s *stubRegistrar, min int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := s.options
		s.mu.Unlock()
		if n >= min {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	n := s.options
	s.mu.Unlock()
	t.Fatalf("want >= %d OPTIONS keep-alive, got %d", min, n)
}

func waitExpires0(t *testing.T, s *stubRegistrar, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ok := s.exp0
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected Expires: 0 deregister")
}
