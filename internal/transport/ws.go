package transport

// WebSocket transport for SIP (RFC 7118): SIP messages are carried as text
// frames over a single ws:// or wss:// connection. The implementation lives
// in its own file (ws.go) and reuses internal/sip parsing so that callers
// can swap between SharedTCP and SharedWS with minimal effort.
//
// Engine wiring (`w1`/`wn`/`ws1`/`wsn` transport codes) lands in Phase 4.1 —
// for now this module is consumed by integration tests and surfaced in the UI
// transport selector as "beta".

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sipcapture/gossipper/internal/sip"
)

// SharedWS is the client end of a single websocket connection to a SIP-over-WS
// peer. Behaviour mirrors SharedTCP but with text frames instead of CRLF
// framing.
type SharedWS struct {
	conn     *websocket.Conn
	incoming chan sip.Message
	closed   chan struct{}
	once     sync.Once
	writeMu  sync.Mutex
}

// DialWS opens a single ws:// or wss:// connection to the given URL and
// returns a SharedWS that reads incoming SIP messages in the background.
//
// tlsConfig is used for wss:// only and may be nil for default behaviour.
func DialWS(ctx context.Context, rawURL string, tlsConfig *tls.Config) (*SharedWS, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ws: parse url: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("ws: unsupported scheme %q", u.Scheme)
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		Subprotocols:     []string{"sip"},
		TLSClientConfig:  tlsConfig,
	}
	conn, _, err := dialer.DialContext(ctx, rawURL, http.Header{
		"User-Agent": []string{"gossipper"},
	})
	if err != nil {
		return nil, fmt.Errorf("ws: dial %s: %w", rawURL, err)
	}
	s := &SharedWS{
		conn:     conn,
		incoming: make(chan sip.Message, 128),
		closed:   make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

// Send transmits raw SIP wire bytes as a single text frame.
func (s *SharedWS) Send(buf []byte) error {
	if s.isClosed() {
		return errors.New("ws: connection closed")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteMessage(websocket.TextMessage, buf)
}

// Incoming returns the channel of inbound SIP messages. It is closed when the
// connection closes.
func (s *SharedWS) Incoming() <-chan sip.Message { return s.incoming }

// Receive is an alias for Incoming() so SharedWS matches the SharedTLS API
// used by the engine.
func (s *SharedWS) Receive() <-chan sip.Message { return s.incoming }

// LocalPort returns the TCP port of the local socket end of this websocket.
// Returns 0 when the local addr is not a *net.TCPAddr (should not happen).
func (s *SharedWS) LocalPort() int {
	if addr, ok := s.conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// RemoteAddr returns the remote peer (server side: the SIP client).
func (s *SharedWS) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }

// Close shuts down the connection idempotently.
func (s *SharedWS) Close() error {
	s.once.Do(func() {
		close(s.closed)
		_ = s.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"),
			time.Now().Add(time.Second))
		_ = s.conn.Close()
	})
	return nil
}

// LocalAddr exposes the underlying socket's local address.
func (s *SharedWS) LocalAddr() net.Addr { return s.conn.LocalAddr() }

func (s *SharedWS) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *SharedWS) readLoop() {
	defer close(s.incoming)
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		// RFC 7118 §5: each WS frame contains exactly one SIP message; use the
		// SIP parser to keep behaviour identical to SharedTCP.
		msg, perr := sip.Parse(data)
		if perr != nil {
			continue
		}
		select {
		case s.incoming <- msg:
		case <-s.closed:
			return
		}
	}
}

// ----------- server side -----------

// WSServer is the UAS-side accept loop. Plug it into an http.ServeMux at the
// chosen path; each upgraded connection becomes a SharedWS published via
// Accept(). Use NewWSServer for plain ws:// and TLS-aware http.Server for
// wss://.
type WSServer struct {
	upgrader  websocket.Upgrader
	accepted  chan *SharedWS
	closeOnce sync.Once
	closed    chan struct{}
	// httpSrv is populated by ListenAndServe / ListenAndServeTLS so Close
	// shuts the embedded HTTP server down too.
	httpSrv *http.Server
	// boundPort is the resolved TCP port chosen by the OS (works with :0).
	boundPort int
}

// NewWSServer returns a server-side handler that accepts SIP-over-WS clients
// from any origin and forwards them via Accept().
func NewWSServer() *WSServer {
	return &WSServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			Subprotocols:    []string{"sip"},
			CheckOrigin:     func(_ *http.Request) bool { return true },
		},
		accepted: make(chan *SharedWS, 64),
		closed:   make(chan struct{}),
	}
}

// Handler returns the http.Handler that should be mounted on the chosen path.
func (s *WSServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isClosed() {
			http.Error(w, "ws server closed", http.StatusServiceUnavailable)
			return
		}
		// Reject non-WS clients early with a 400 so curl-style probes don't
		// hang the connection.
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "WebSocket upgrade required", http.StatusBadRequest)
			return
		}
		conn, err := s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := &SharedWS{
			conn:     conn,
			incoming: make(chan sip.Message, 128),
			closed:   make(chan struct{}),
		}
		go c.readLoop()
		select {
		case s.accepted <- c:
		case <-s.closed:
			_ = c.Close()
		}
	})
}

// Accept returns the channel of newly upgraded SIP-over-WS connections.
func (s *WSServer) Accept() <-chan *SharedWS { return s.accepted }

// Close stops accepting new clients (existing SharedWS connections survive
// long enough to drain inbound queues) and shuts down the embedded HTTP
// server if ListenAndServe / ListenAndServeTLS was used.
func (s *WSServer) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		close(s.accepted)
		if s.httpSrv != nil {
			_ = s.httpSrv.Close()
		}
	})
	return nil
}

// BoundPort returns the port the embedded HTTP listener resolved to. Useful
// when callers passed ":0" so the kernel picked a port.
func (s *WSServer) BoundPort() int { return s.boundPort }

// ListenAndServe starts an HTTP server on bindAddr at path (defaults to "/"
// when empty) and serves the WSServer handler. Runs in a background
// goroutine; the returned chan reports the final ListenAndServe error.
func (s *WSServer) ListenAndServe(bindAddr, path string) (<-chan error, error) {
	return s.listenAndServe(bindAddr, path, nil)
}

// ListenAndServeTLS is the wss:// counterpart — same semantics but the HTTP
// server is HTTPS, terminating TLS with the provided cert / key files.
func (s *WSServer) ListenAndServeTLS(bindAddr, path, certFile, keyFile string) (<-chan error, error) {
	return s.listenAndServe(bindAddr, path, &tlsFiles{certFile: certFile, keyFile: keyFile})
}

type tlsFiles struct{ certFile, keyFile string }

func (s *WSServer) listenAndServe(bindAddr, path string, tls *tlsFiles) (<-chan error, error) {
	if path == "" {
		path = "/"
	}
	mux := http.NewServeMux()
	mux.Handle(path, s.Handler())
	srv := &http.Server{
		Addr:    bindAddr,
		Handler: mux,
	}
	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, err
	}
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		s.boundPort = addr.Port
	}
	s.httpSrv = srv
	errCh := make(chan error, 1)
	go func() {
		var serveErr error
		if tls != nil {
			serveErr = srv.ServeTLS(ln, tls.certFile, tls.keyFile)
		} else {
			serveErr = srv.Serve(ln)
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- serveErr
		}
		close(errCh)
	}()
	return errCh, nil
}

func (s *WSServer) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}
