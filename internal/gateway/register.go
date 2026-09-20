package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sipcapture/gossipper/internal/sip"
)

var errUASUDPNotReady = errors.New("uas udp not ready")

var (
	registerRetry      = 15 * time.Second
	registerTimeout    = 8 * time.Second
	deregisterTO       = 5 * time.Second
	registerMinRefresh = 5 * time.Second
)

// State is the public REGISTER status (password never included).
type State struct {
	State      string `json:"state"`
	Registered bool   `json:"registered"`
	AOR        string `json:"aor,omitempty"`
	Code       int    `json:"code,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Error      string `json:"error,omitempty"`
	Trace      string `json:"trace,omitempty"`
}

type sipTrace struct {
	buf bytes.Buffer
}

func (t *sipTrace) add(label string, raw string) {
	if t.buf.Len() > 0 {
		t.buf.WriteString("\n---\n")
	}
	t.buf.WriteString(label)
	t.buf.WriteString("\n")
	t.buf.WriteString(raw)
}

func (t *sipTrace) addErr(err error) {
	if err == nil {
		return
	}
	if t.buf.Len() > 0 {
		t.buf.WriteString("\n---\n")
	}
	t.buf.WriteString("error: ")
	t.buf.WriteString(err.Error())
}

func (t *sipTrace) String() string { return t.buf.String() }

type registerResult struct {
	code    int
	reason  string
	expires int
}

// RegisterTransport sends REGISTER on the UAS UDP socket so FreeSWITCH NAT
// (received=/Route) delivers inbound INVITE to the same port Contact advertises.
type RegisterTransport interface {
	RegisterLocalAddr() *net.UDPAddr
	RegisterRoundTrip(ctx context.Context, callID string, payload []byte, remote *net.UDPAddr) ([]byte, error)
	RegisterSend(payload []byte, remote *net.UDPAddr) error
}

// Registrar runs the kefir-style REGISTER refresh loop.
type Registrar struct {
	mu    sync.Mutex
	cfg   Config
	state State
	trip  RegisterTransport

	cseq int
}

// NewRegistrar constructs a registrar with state off.
func NewRegistrar(cfg Config) *Registrar {
	cfg.Normalize()
	return &Registrar{
		cfg:   cfg,
		state: State{State: StateOff, AOR: cfg.AOR()},
		cseq:  1,
	}
}

// Snapshot returns a copy of the current status.
func (r *Registrar) Snapshot() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *Registrar) setState(st State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = st
}

func (r *Registrar) nextCSeq() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.cseq
	r.cseq++
	return n
}

// SetTransport uses the UAS listen socket for REGISTER (Contact port = source port).
func (r *Registrar) SetTransport(t RegisterTransport) {
	r.mu.Lock()
	r.trip = t
	r.mu.Unlock()
}

type registerSession struct {
	conn   *net.UDPConn
	trip   RegisterTransport
	local  *net.UDPAddr
	remote *net.UDPAddr
	callID string
	tag    string
}

func (s *registerSession) roundTrip(ctx context.Context, payload []byte) ([]byte, error) {
	if s != nil && s.trip != nil {
		return s.trip.RegisterRoundTrip(ctx, s.callID, payload, s.remote)
	}
	if s == nil || s.conn == nil {
		return nil, fmt.Errorf("register socket is not open")
	}
	if err := sendUDP(ctx, s.conn, s.remote, payload); err != nil {
		return nil, err
	}
	want := sip.NormalizeCallID(s.callID)
	for {
		raw, err := recvUDP(ctx, s.conn)
		if err != nil {
			return nil, err
		}
		msg, err := sip.Parse(raw)
		if err != nil {
			continue
		}
		got, _ := sip.Header(msg.Headers, "Call-ID")
		if sip.NormalizeCallID(got) == want {
			return raw, nil
		}
	}
}

func (s *registerSession) send(payload []byte) error {
	if s != nil && s.trip != nil {
		return s.trip.RegisterSend(payload, s.remote)
	}
	if s == nil || s.conn == nil {
		return fmt.Errorf("register socket is not open")
	}
	return sendUDP(context.Background(), s.conn, s.remote, payload)
}

// Loop sends REGISTER until ctx is cancelled, then deregisters (Expires: 0).
func (r *Registrar) Loop(ctx context.Context) {
	cfg := r.cfg
	aor := cfg.AOR()
	r.setState(State{State: StateRegistering, AOR: aor})

	r.mu.Lock()
	trip := r.trip
	r.mu.Unlock()

	var sess *registerSession
	for sess == nil {
		s, err := openRegisterSession(cfg, trip)
		if err != nil {
			if ctx.Err() != nil {
				r.setState(State{State: StateOff, AOR: aor})
				return
			}
			wait := registerRetry
			if errors.Is(err, errUASUDPNotReady) {
				wait = 200 * time.Millisecond
				r.setState(State{State: StateRegistering, AOR: aor})
			} else {
				r.setState(State{State: StateFailed, AOR: aor, Error: err.Error()})
			}
			select {
			case <-ctx.Done():
				r.setState(State{State: StateOff, AOR: aor})
				return
			case <-time.After(wait):
				r.setState(State{State: StateRegistering, AOR: aor})
			}
			continue
		}
		sess = s
	}
	if sess.conn != nil {
		defer sess.conn.Close()
	}

	for {
		res, trace, err := r.sendRegister(ctx, cfg, sess, false)
		if err != nil {
			if ctx.Err() != nil {
				r.deregister(cfg, sess)
				r.setState(State{State: StateOff, AOR: aor})
				return
			}
			st := State{
				State: StateFailed,
				AOR:   aor,
				Error: err.Error(),
				Trace: trace,
			}
			if res != nil {
				st.Code = res.code
				st.Reason = res.reason
			}
			r.setState(st)
			select {
			case <-ctx.Done():
				r.deregister(cfg, sess)
				r.setState(State{State: StateOff, AOR: aor, Trace: trace})
				return
			case <-time.After(registerRetry):
				r.setState(State{State: StateRegistering, AOR: aor, Trace: trace})
				continue
			}
		}
		ttl := cfg.Expires()
		if res != nil && res.expires > 0 {
			ttl = time.Duration(res.expires) * time.Second
		}
		refresh := time.Duration(float64(ttl) * 0.9)
		if refresh < registerMinRefresh {
			refresh = registerMinRefresh
		}
		st := State{
			State:      StateRegistered,
			Registered: true,
			AOR:        aor,
			Trace:      trace,
		}
		if res != nil {
			st.Code = res.code
			st.Reason = res.reason
		}
		r.setState(st)
		if r.waitUntilRefresh(ctx, cfg, sess, refresh) {
			r.deregister(cfg, sess)
			r.setState(State{State: StateOff, AOR: aor})
			return
		}
	}
}

// waitUntilRefresh sleeps until the next REGISTER. Returns true if ctx is done.
func (r *Registrar) waitUntilRefresh(ctx context.Context, cfg Config, sess *registerSession, refresh time.Duration) bool {
	refreshTimer := time.NewTimer(refresh)
	defer refreshTimer.Stop()
	ka := cfg.Keepalive()
	var kaC <-chan time.Time
	if ka > 0 {
		tick := time.NewTicker(ka)
		defer tick.Stop()
		kaC = tick.C
	}
	for {
		select {
		case <-ctx.Done():
			return true
		case <-kaC:
			r.sendKeepalive(cfg, sess)
		case <-refreshTimer.C:
			return false
		}
	}
}

func (r *Registrar) sendKeepalive(cfg Config, sess *registerSession) {
	if sess == nil {
		return
	}
	branch, err := sip.RandomHex(8)
	if err != nil {
		return
	}
	callID, err := sip.RandomHex(8)
	if err != nil {
		return
	}
	req := buildOPTIONS(cfg, sess.local, r.nextCSeq(), callID, sess.tag, branch)
	_ = sess.send([]byte(req))
}

func openRegisterSession(cfg Config, trip RegisterTransport) (*registerSession, error) {
	host, port, err := SplitAddr(cfg.Addr)
	if err != nil {
		return nil, err
	}
	remote, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	callID, err := sip.RandomHex(8)
	if err != nil {
		return nil, err
	}
	tag, err := sip.RandomHex(4)
	if err != nil {
		return nil, err
	}
	if trip != nil {
		local := trip.RegisterLocalAddr()
		if local == nil || local.Port <= 0 {
			return nil, errUASUDPNotReady
		}
		return &registerSession{trip: trip, local: local, remote: remote, callID: callID, tag: tag}, nil
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("register local addr is not UDP")
	}
	return &registerSession{conn: conn, local: local, remote: remote, callID: callID, tag: tag}, nil
}

func (r *Registrar) deregister(cfg Config, sess *registerSession) {
	if sess == nil {
		return
	}
	dctx, cancel := context.WithTimeout(context.Background(), deregisterTO)
	defer cancel()
	_, _, _ = r.sendRegister(dctx, cfg, sess, true)
}

func (r *Registrar) sendRegister(ctx context.Context, cfg Config, sess *registerSession, deregister bool) (*registerResult, string, error) {
	var tr sipTrace
	user := cfg.AORUser()
	if user == "" {
		err := fmt.Errorf("empty register user")
		tr.addErr(err)
		return nil, tr.String(), err
	}
	if sess == nil || (sess.conn == nil && sess.trip == nil) {
		err := fmt.Errorf("register socket is not open")
		tr.addErr(err)
		return nil, tr.String(), err
	}
	host, port, err := SplitAddr(cfg.Addr)
	if err != nil {
		tr.addErr(err)
		return nil, tr.String(), err
	}

	expires := int(cfg.Expires().Seconds())
	if deregister {
		expires = 0
	}

	attempt, cancel := context.WithTimeout(ctx, registerTimeout)
	defer cancel()

	cseq := r.nextCSeq()
	branch, err := sip.RandomHex(8)
	if err != nil {
		tr.addErr(err)
		return nil, tr.String(), err
	}

	req := buildREGISTER(cfg, sess.local, sess.remote, cseq, sess.callID, sess.tag, branch, expires, "")
	tr.add("REGISTER", req)
	raw, err := sess.roundTrip(attempt, []byte(req))
	if err != nil {
		tr.addErr(err)
		return nil, tr.String(), err
	}
	msg, err := sip.Parse(raw)
	if err != nil {
		tr.addErr(err)
		return nil, tr.String(), err
	}
	tr.add(fmt.Sprintf("%d %s", msg.StatusCode, msg.Reason), string(raw))

	switch msg.StatusCode {
	case 401, 407:
		if cfg.Password == "" && cfg.AuthUsername() == "" {
			err := fmt.Errorf("register challenge %d without credentials", msg.StatusCode)
			tr.addErr(err)
			return &registerResult{code: msg.StatusCode, reason: msg.Reason}, tr.String(), err
		}
		headerName := "Authorization"
		challengeName := "WWW-Authenticate"
		if msg.StatusCode == 407 {
			headerName = "Proxy-Authorization"
			challengeName = "Proxy-Authenticate"
		}
		challenge, ok := sip.Header(msg.Headers, challengeName)
		if !ok {
			err := fmt.Errorf("missing %s", challengeName)
			tr.addErr(err)
			return &registerResult{code: msg.StatusCode, reason: msg.Reason}, tr.String(), err
		}
		uri := fmt.Sprintf("sip:%s", net.JoinHostPort(host, strconv.Itoa(port)))
		authLine, err := sip.BuildDigestAuthHeader(headerName, challenge, "REGISTER", uri, "", cfg.AuthUsername(), cfg.Password)
		if err != nil {
			tr.addErr(err)
			return &registerResult{code: msg.StatusCode, reason: msg.Reason}, tr.String(), err
		}
		cseq = r.nextCSeq()
		branch, err = sip.RandomHex(8)
		if err != nil {
			tr.addErr(err)
			return nil, tr.String(), err
		}
		req = buildREGISTER(cfg, sess.local, sess.remote, cseq, sess.callID, sess.tag, branch, expires, authLine)
		tr.add("REGISTER (auth)", req)
		raw, err = sess.roundTrip(attempt, []byte(req))
		if err != nil {
			tr.addErr(err)
			return nil, tr.String(), err
		}
		msg, err = sip.Parse(raw)
		if err != nil {
			tr.addErr(err)
			return nil, tr.String(), err
		}
		tr.add(fmt.Sprintf("%d %s", msg.StatusCode, msg.Reason), string(raw))
	}

	if msg.StatusCode >= 300 {
		return &registerResult{code: msg.StatusCode, reason: msg.Reason}, tr.String(),
			fmt.Errorf("register status %d %s", msg.StatusCode, msg.Reason)
	}
	return &registerResult{code: msg.StatusCode, reason: msg.Reason, expires: grantedExpires(msg, expires)}, tr.String(), nil
}

func viaHost(cfg Config, local *net.UDPAddr) string {
	if ip := strings.TrimSpace(cfg.AdvertisedIP); ip != "" {
		return ip
	}
	if local == nil {
		return "127.0.0.1"
	}
	h := local.IP.String()
	if h == "0.0.0.0" || h == "::" {
		return "127.0.0.1"
	}
	return h
}

func grantedExpires(msg sip.Message, requested int) int {
	if v, ok := sip.Header(msg.Headers, "Expires"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	best := 0
	for k, vals := range msg.Headers {
		if !strings.EqualFold(k, "Contact") {
			continue
		}
		for _, v := range vals {
			if n := contactExpiresParam(v); n > best {
				best = n
			}
		}
	}
	if best > 0 {
		return best
	}
	return requested
}

func contactExpiresParam(contact string) int {
	best := 0
	s := contact
	for {
		lower := strings.ToLower(s)
		i := strings.Index(lower, "expires=")
		if i < 0 {
			return best
		}
		s = s[i+len("expires="):]
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > best {
			best = n
		}
	}
}

func buildREGISTER(cfg Config, local, remote *net.UDPAddr, cseq int, callID, tag, branch string, expires int, authLine string) string {
	host, port, _ := SplitAddr(cfg.Addr)
	requestURI := fmt.Sprintf("sip:%s", net.JoinHostPort(host, strconv.Itoa(port)))
	aorURI := fmt.Sprintf("sip:%s@%s", cfg.AORUser(), cfg.Domain)
	portNum := 0
	if local != nil {
		portNum = local.Port
	}
	var b strings.Builder
	fmt.Fprintf(&b, "REGISTER %s SIP/2.0\r\n", requestURI)
	fmt.Fprintf(&b, "Via: SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport\r\n", viaHost(cfg, local), portNum, branch)
	fmt.Fprintf(&b, "From: <%s>;tag=%s\r\n", aorURI, tag)
	fmt.Fprintf(&b, "To: <%s>\r\n", aorURI)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	fmt.Fprintf(&b, "CSeq: %d REGISTER\r\n", cseq)
	fmt.Fprintf(&b, "Contact: <%s>\r\n", cfg.ContactURIAt(portNum))
	fmt.Fprintf(&b, "Expires: %d\r\n", expires)
	fmt.Fprintf(&b, "Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "User-Agent: gossipper-gateway\r\n")
	if authLine != "" {
		b.WriteString(authLine)
		if !strings.HasSuffix(authLine, "\r\n") {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("Content-Length: 0\r\n\r\n")
	_ = remote
	return b.String()
}

func buildOPTIONS(cfg Config, local *net.UDPAddr, cseq int, callID, tag, branch string) string {
	host, port, _ := SplitAddr(cfg.Addr)
	requestURI := fmt.Sprintf("sip:%s", net.JoinHostPort(host, strconv.Itoa(port)))
	aorURI := fmt.Sprintf("sip:%s@%s", cfg.AORUser(), cfg.Domain)
	portNum := 0
	if local != nil {
		portNum = local.Port
	}
	var b strings.Builder
	fmt.Fprintf(&b, "OPTIONS %s SIP/2.0\r\n", requestURI)
	fmt.Fprintf(&b, "Via: SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport\r\n", viaHost(cfg, local), portNum, branch)
	fmt.Fprintf(&b, "From: <%s>;tag=%s\r\n", aorURI, tag)
	fmt.Fprintf(&b, "To: <%s>\r\n", aorURI)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	fmt.Fprintf(&b, "CSeq: %d OPTIONS\r\n", cseq)
	fmt.Fprintf(&b, "Contact: <%s>\r\n", cfg.ContactURIAt(portNum))
	fmt.Fprintf(&b, "Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "User-Agent: gossipper-gateway\r\n")
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

func sendUDP(ctx context.Context, conn *net.UDPConn, addr *net.UDPAddr, payload []byte) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
	}
	_, err := conn.WriteToUDP(payload, addr)
	return err
}

func recvUDP(ctx context.Context, conn *net.UDPConn) ([]byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	} else {
		_ = conn.SetReadDeadline(time.Now().Add(registerTimeout))
	}
	buf := make([]byte, 65535)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out, nil
}
