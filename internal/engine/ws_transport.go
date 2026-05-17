package engine

// SIP-over-WebSocket transports (RFC 7118). Mirrors the layout of
// tls_transport.go: each transport code (w1/wn for ws://, ws1/wsn for wss://)
// gets a client and a server entry point plus a multi-listener variant. The
// underlying transport is implemented in internal/transport/ws.go.
//
// Behaviour notes:
//   - w1/ws1 = shared connection (multiplexes multiple calls over one socket),
//     mirrors t1/l1.
//   - wn/wsn = per-call connection, mirrors tn/ln.
//   - WSPath in engine.Config controls the HTTP path; defaults to "/".
//   - TLSCertFile/TLSKeyFile must be present for wss servers; the same
//     clientTLSConfig helper used by l1/ln is reused here.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/sipcapture/gossipper/internal/scheduler"
	"github.com/sipcapture/gossipper/internal/sip"
	"github.com/sipcapture/gossipper/internal/transport"
)

func (e *Engine) wsPath() string {
	if p := e.cfg.WSPath; p != "" {
		return p
	}
	return "/"
}

func (e *Engine) wsClientURL(scheme string) string {
	return fmt.Sprintf("%s://%s:%d%s", scheme, e.cfg.RemoteHost, e.cfg.RemotePort, e.wsPath())
}

func (e *Engine) runClientSharedWS(ctx context.Context) error {
	return e.runClientSharedWSCommon(ctx, "ws", nil)
}

func (e *Engine) runClientSharedWSS(ctx context.Context) error {
	tlsCfg, err := e.clientTLSConfig()
	if err != nil {
		return err
	}
	return e.runClientSharedWSCommon(ctx, "wss", tlsCfg)
}

func (e *Engine) runClientSharedWSCommon(ctx context.Context, scheme string, tlsCfg *tls.Config) error {
	shared, err := transport.DialWS(ctx, e.wsClientURL(scheme), tlsCfg)
	if err != nil {
		return err
	}
	defer shared.Close()

	registry := newMailboxRegistry(e.log)
	go registry.dispatchMessages(shared.Receive())

	e.perSocketMode.Store(false)
	e.sem.Resize(e.callConcurrencyLimit(false))
	var wg sync.WaitGroup
	var once sync.Once
	var runErr error
	for i := 1; e.clientShouldSpawnAnother(i); i++ {
		if err := e.waitForNextCall(ctx); err != nil {
			if errors.Is(err, scheduler.ErrStopped) {
				break
			}
			return err
		}
		if err := e.sem.Acquire(ctx); err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer e.sem.Release()
			callID := newCallID(callNumber)
			inbox := registry.register(callID)
			defer registry.unregister(callID)
			receive := func(waitCtx context.Context) (*sip.Message, error) {
				select {
				case msg := <-inbox:
					return msg, nil
				default:
				}
				select {
				case msg := <-inbox:
					return msg, nil
				case <-waitCtx.Done():
					return nil, waitCtx.Err()
				}
			}
			send := func(payload []byte) error { return shared.Send(payload) }
			localPort := shared.LocalPort()
			localIP := resolveLocalIP(localPort, e.cfg.LocalIP, e.cfg.RemoteHost, e.cfg.RemotePort)
			send = e.wrapSIPSend(callNumber, callID, localIP, localPort, e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, callID, localIP, localPort, e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, e.cfg.Transport, callNumber, callID, localIP, localPort, e.cfg.RemoteHost, e.cfg.RemotePort, send, receive, nil)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}
	wg.Wait()
	return runErr
}

func (e *Engine) runClientPerCallWS(ctx context.Context) error {
	return e.runClientPerCallWSCommon(ctx, "ws", nil)
}

func (e *Engine) runClientPerCallWSS(ctx context.Context) error {
	tlsCfg, err := e.clientTLSConfig()
	if err != nil {
		return err
	}
	return e.runClientPerCallWSCommon(ctx, "wss", tlsCfg)
}

func (e *Engine) runClientPerCallWSCommon(ctx context.Context, scheme string, tlsCfg *tls.Config) error {
	url := e.wsClientURL(scheme)
	e.perSocketMode.Store(true)
	e.sem.Resize(e.callConcurrencyLimit(true))
	var wg sync.WaitGroup
	var once sync.Once
	var runErr error
	for i := 1; e.clientShouldSpawnAnother(i); i++ {
		if err := e.waitForNextCall(ctx); err != nil {
			if errors.Is(err, scheduler.ErrStopped) {
				break
			}
			return err
		}
		if err := e.sem.Acquire(ctx); err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer e.sem.Release()
			conn, err := transport.DialWS(ctx, url, tlsCfg)
			if err != nil {
				once.Do(func() { runErr = err })
				return
			}
			defer conn.Close()
			callID := newCallID(callNumber)
			localPort := conn.LocalPort()
			localIP := resolveLocalIP(localPort, e.cfg.LocalIP, e.cfg.RemoteHost, e.cfg.RemotePort)
			inboxCh := conn.Receive()
			receive := func(waitCtx context.Context) (*sip.Message, error) {
				select {
				case msg, ok := <-inboxCh:
					if !ok {
						return nil, errors.New("ws connection closed")
					}
					cp := msg
					return &cp, nil
				case <-waitCtx.Done():
					return nil, waitCtx.Err()
				}
			}
			send := func(payload []byte) error { return conn.Send(payload) }
			send = e.wrapSIPSend(callNumber, callID, localIP, localPort, e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, callID, localIP, localPort, e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, e.cfg.Transport, callNumber, callID, localIP, localPort, e.cfg.RemoteHost, e.cfg.RemotePort, send, receive, nil)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}
	wg.Wait()
	return runErr
}

func (e *Engine) runServerWSShared(ctx context.Context) error {
	return e.runServerWSSharedOnAddr(ctx, fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort), e.cfg.LocalIP, e.cfg.Transport, nil, 0)
}

func (e *Engine) runServerWSSShared(ctx context.Context) error {
	if e.cfg.TLSCertFile == "" || e.cfg.TLSKeyFile == "" {
		return fmt.Errorf("wss server mode requires tls_cert and tls_key")
	}
	return e.runServerWSSharedOnAddr(ctx, fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort), e.cfg.LocalIP, e.cfg.Transport, &tlsFiles{cert: e.cfg.TLSCertFile, key: e.cfg.TLSKeyFile}, 0)
}

func (e *Engine) runServerWSPerConn(ctx context.Context) error {
	// In the WS world "shared vs per-call" only changes how the engine
	// multiplexes calls onto sockets — the accept loop itself is identical.
	// The per-call variant tracks each upgraded connection 1:1 with a call.
	return e.runServerWSPerConnOnAddr(ctx, fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort), e.cfg.LocalIP, e.cfg.Transport, nil, 0)
}

func (e *Engine) runServerWSSPerConn(ctx context.Context) error {
	if e.cfg.TLSCertFile == "" || e.cfg.TLSKeyFile == "" {
		return fmt.Errorf("wss server mode requires tls_cert and tls_key")
	}
	return e.runServerWSPerConnOnAddr(ctx, fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort), e.cfg.LocalIP, e.cfg.Transport, &tlsFiles{cert: e.cfg.TLSCertFile, key: e.cfg.TLSKeyFile}, 0)
}

type tlsFiles struct{ cert, key string }

func (e *Engine) runServerWSSharedOn(ctx context.Context, co *serverMultiCoordinator, bindAddr, bindIP, sipTransport string, _ int, listenerIdx int) error {
	var tlsf *tlsFiles
	if sipTransport == "ws1" || sipTransport == "wsn" {
		if e.cfg.TLSCertFile == "" || e.cfg.TLSKeyFile == "" {
			return fmt.Errorf("wss listener %s: tls_cert/tls_key required", bindAddr)
		}
		tlsf = &tlsFiles{cert: e.cfg.TLSCertFile, key: e.cfg.TLSKeyFile}
	}
	_ = co // shared accept uses per-call slot reservation through e.serverRejectNew below
	return e.runServerWSSharedOnAddr(ctx, bindAddr, bindIP, sipTransport, tlsf, listenerIdx)
}

func (e *Engine) runServerWSPerConnOn(ctx context.Context, co *serverMultiCoordinator, bindAddr, bindIP, sipTransport string, _ int, listenerIdx int) error {
	var tlsf *tlsFiles
	if sipTransport == "ws1" || sipTransport == "wsn" {
		if e.cfg.TLSCertFile == "" || e.cfg.TLSKeyFile == "" {
			return fmt.Errorf("wss listener %s: tls_cert/tls_key required", bindAddr)
		}
		tlsf = &tlsFiles{cert: e.cfg.TLSCertFile, key: e.cfg.TLSKeyFile}
	}
	_ = co
	return e.runServerWSPerConnOnAddr(ctx, bindAddr, bindIP, sipTransport, tlsf, listenerIdx)
}

func (e *Engine) runServerWSSharedOnAddr(ctx context.Context, bindAddr, bindIP, sipTransport string, tlsf *tlsFiles, listenerIdx int) error {
	srv := transport.NewWSServer()
	errCh, err := wsListen(srv, bindAddr, e.wsPath(), tlsf)
	if err != nil {
		return err
	}
	defer srv.Close()

	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return fmt.Errorf("server scenario must start with a recv command")
	}

	var wg sync.WaitGroup
	acceptCtx, acceptCancel := context.WithCancel(ctx)
	defer acceptCancel()
	go func() {
		<-acceptCtx.Done()
		srv.Close()
	}()

	for shared := range srv.Accept() {
		shared := shared
		if !e.listenerAcceptNew(listenerIdx) {
			shared.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer shared.Close()
			e.serveWSConnection(ctx, shared, bindIP, sipTransport, false)
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (e *Engine) runServerWSPerConnOnAddr(ctx context.Context, bindAddr, bindIP, sipTransport string, tlsf *tlsFiles, listenerIdx int) error {
	srv := transport.NewWSServer()
	errCh, err := wsListen(srv, bindAddr, e.wsPath(), tlsf)
	if err != nil {
		return err
	}
	defer srv.Close()

	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return fmt.Errorf("server scenario must start with a recv command")
	}

	var wg sync.WaitGroup
	acceptCtx, acceptCancel := context.WithCancel(ctx)
	defer acceptCancel()
	go func() {
		<-acceptCtx.Done()
		srv.Close()
	}()
	for shared := range srv.Accept() {
		shared := shared
		if !e.listenerAcceptNew(listenerIdx) {
			shared.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer shared.Close()
			e.serveWSConnection(ctx, shared, bindIP, sipTransport, true)
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func wsListen(srv *transport.WSServer, bindAddr, path string, tlsf *tlsFiles) (<-chan error, error) {
	if tlsf != nil {
		return srv.ListenAndServeTLS(bindAddr, path, tlsf.cert, tlsf.key)
	}
	return srv.ListenAndServe(bindAddr, path)
}

// serveWSConnection multiplexes inbound SIP messages from a single upgraded
// connection across one (per-call) or many (shared) calls. perCall=true makes
// the connection terminate after the first call finishes; perCall=false keeps
// reading until the WS closes.
func (e *Engine) serveWSConnection(ctx context.Context, shared *transport.SharedWS, bindIP, sipTransport string, perCall bool) {
	type session struct{ inbox chan sip.Message }
	var (
		mu       sync.Mutex
		sessions = make(map[string]*session)
		wg       sync.WaitGroup
	)

	send := func(payload []byte) error { return shared.Send(payload) }

	remote, _ := shared.RemoteAddr().(*net.TCPAddr)
	var (
		remoteIP   string
		remotePort int
	)
	if remote != nil {
		remoteIP = remote.IP.String()
		remotePort = remote.Port
	}

	for msg := range shared.Receive() {
		callID, ok := sip.Header(msg.Headers, "Call-ID")
		if !ok {
			continue
		}
		callID = sip.NormalizeCallID(callID)
		mu.Lock()
		sess, exists := sessions[callID]
		if !exists {
			firstCmd, ok := e.snapshotLiveFirstRecvCommand()
			if !ok {
				mu.Unlock()
				continue
			}
			if !sip.Match(msg, firstCmd.RecvReq, firstCmd.RecvResp) {
				mu.Unlock()
				continue
			}
			if perCall && len(sessions) > 0 {
				mu.Unlock()
				continue
			}
			sess = &session{inbox: make(chan sip.Message, 8)}
			sessions[callID] = sess
			callNumber := len(sessions)
			wg.Add(1)
			go func(id string, inbox chan sip.Message, callNumber int) {
				defer wg.Done()
				defer func() {
					mu.Lock()
					delete(sessions, id)
					mu.Unlock()
				}()
				receive := adaptReceiveToPtr(func(waitCtx context.Context) (sip.Message, error) {
					select {
					case <-waitCtx.Done():
						return sip.Message{}, waitCtx.Err()
					case m := <-inbox:
						return m, nil
					}
				})
				localPort := shared.LocalPort()
				localIP := resolveLocalIP(localPort, bindIP, remoteIP, remotePort)
				wSend := e.wrapSIPSend(callNumber, id, localIP, localPort, remoteIP, remotePort, send)
				receive = e.wrapSIPReceive(callNumber, id, localIP, localPort, remoteIP, remotePort, receive)
				_ = e.executeCall(ctx, sipTransport, callNumber, id, localIP, localPort, remoteIP, remotePort, wSend, receive, nil)
			}(callID, sess.inbox, callNumber)
		}
		mu.Unlock()
		select {
		case sess.inbox <- msg:
		default:
		}
	}
	wg.Wait()
}
