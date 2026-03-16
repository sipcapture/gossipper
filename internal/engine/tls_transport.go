package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/qxip/gossipper/internal/sip"
	"github.com/qxip/gossipper/internal/transport"
)

func (e *Engine) runClientSharedTLS(ctx context.Context) error {
	localAddr := fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort)
	remoteAddr := fmt.Sprintf("%s:%d", e.cfg.RemoteHost, e.cfg.RemotePort)
	tlsCfg, err := e.clientTLSConfig()
	if err != nil {
		return err
	}
	shared, err := transport.NewSharedTLSWithReconnect(localAddr, remoteAddr, tlsCfg, transport.ReconnectOptions{
		MaxAttempts:      e.cfg.MaxReconnect,
		Sleep:            e.cfg.ReconnectSleep,
		CloseOnReconnect: e.cfg.ReconnectClose,
	})
	if err != nil {
		return err
	}
	defer shared.Close()

	registry := newMailboxRegistry()
	go registry.dispatchMessages(shared.Receive())

	sem := make(chan struct{}, e.callConcurrencyLimit(false))
	ticker := e.sched.Interval(e.cfg.Rate)
	var wg sync.WaitGroup
	var once sync.Once
	var runErr error
	for i := 1; i <= e.cfg.TotalCalls; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer func() { <-sem }()
			callID := newCallID(callNumber)
			inbox := registry.register(callID)
			defer registry.unregister(callID)
			receive := func(waitCtx context.Context) (sip.Message, error) {
				select {
				case <-waitCtx.Done():
					return sip.Message{}, waitCtx.Err()
				case msg := <-inbox:
					return msg, nil
				}
			}
			send := func(payload []byte) error { return shared.Send(payload) }
			localIP := resolveLocalIP(shared.LocalPort(), e.cfg.LocalIP)
			send = e.wrapSIPSend(callNumber, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send, receive, nil)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}
	wg.Wait()
	return runErr
}

func (e *Engine) runClientPerCallTLS(ctx context.Context) error {
	remoteAddr := fmt.Sprintf("%s:%d", e.cfg.RemoteHost, e.cfg.RemotePort)
	tlsCfg, err := e.clientTLSConfig()
	if err != nil {
		return err
	}
	sem := make(chan struct{}, e.callConcurrencyLimit(true))
	ticker := e.sched.Interval(e.cfg.Rate)
	var wg sync.WaitGroup
	var once sync.Once
	var runErr error
	for i := 1; i <= e.cfg.TotalCalls; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer func() { <-sem }()
			dialog, err := transport.NewDialogTLS(fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort), remoteAddr, tlsCfg)
			if err != nil {
				once.Do(func() { runErr = err })
				return
			}
			defer dialog.Close()
			callID := newCallID(callNumber)
			localIP := resolveLocalIP(dialog.LocalPort(), e.cfg.LocalIP)
			send := func(payload []byte) error { return dialog.Send(payload) }
			receive := func(waitCtx context.Context) (sip.Message, error) { return dialog.Receive(waitCtx) }
			send = e.wrapSIPSend(callNumber, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send, receive, nil)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}
	wg.Wait()
	return runErr
}

func (e *Engine) runServerTLSShared(ctx context.Context) error {
	serverCfg, err := e.serverTLSConfig()
	if err != nil {
		return err
	}
	server, err := transport.NewTLSServer(fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort), serverCfg)
	if err != nil {
		return err
	}
	defer server.Close()
	conn, err := server.Accept(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	reader := transport.NewTLSConnReader(conn)
	var writeMu sync.Mutex
	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return fmt.Errorf("server scenario must start with a recv command")
	}

	type session struct{ inbox chan sip.Message }
	var (
		mu       sync.Mutex
		sessions = make(map[string]*session)
		wg       sync.WaitGroup
		done     = make(chan struct{})
	)
	go func() {
		defer close(done)
		for {
			msg, err := reader.Read(ctx)
			if err != nil {
				return
			}
			callID, ok := sip.Header(msg.Headers, "Call-ID")
			if !ok {
				continue
			}
			mu.Lock()
			sess, exists := sessions[callID]
			if !exists {
				firstCmd := e.cfg.Scenario.Commands[firstRecvIndex]
				if !sip.Match(msg, firstCmd.RecvReq, firstCmd.RecvResp) || len(sessions) >= e.cfg.TotalCalls {
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
					receive := func(waitCtx context.Context) (sip.Message, error) {
						select {
						case <-waitCtx.Done():
							return sip.Message{}, waitCtx.Err()
						case msg := <-inbox:
							return msg, nil
						}
					}
					send := func(payload []byte) error {
						writeMu.Lock()
						defer writeMu.Unlock()
						return reader.Write(payload)
					}
					remote := conn.RemoteAddr().(*net.TCPAddr)
					localIP := resolveLocalIP(reader.LocalPort(), e.cfg.LocalIP)
					send = e.wrapSIPSend(callNumber, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send)
					receive = e.wrapSIPReceive(callNumber, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, receive)
					_ = e.executeCall(ctx, callNumber, id, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send, receive, nil)
				}(callID, sess.inbox, callNumber)
			}
			mu.Unlock()
			select {
			case sess.inbox <- msg:
			default:
			}
		}
	}()
	<-done
	wg.Wait()
	return nil
}

func (e *Engine) runServerTLSPerConn(ctx context.Context) error {
	serverCfg, err := e.serverTLSConfig()
	if err != nil {
		return err
	}
	server, err := transport.NewTLSServer(fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort), serverCfg)
	if err != nil {
		return err
	}
	defer server.Close()
	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return fmt.Errorf("server scenario must start with a recv command")
	}
	var wg sync.WaitGroup
	for accepted := 0; accepted < e.cfg.TotalCalls; accepted++ {
		conn, err := server.Accept(ctx)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int, conn *tls.Conn) {
			defer wg.Done()
			defer conn.Close()
			reader := transport.NewTLSConnReader(conn)
			first, err := waitForFirstServerMessage(ctx, reader, firstRecvIndex, e.cfg.Scenario.Commands[firstRecvIndex])
			if err != nil {
				return
			}
			callID, ok := sip.Header(first.Headers, "Call-ID")
			if !ok {
				return
			}
			inbox := make(chan sip.Message, 8)
			inbox <- first
			receive := func(waitCtx context.Context) (sip.Message, error) {
				select {
				case <-waitCtx.Done():
					return sip.Message{}, waitCtx.Err()
				case msg := <-inbox:
					return msg, nil
				default:
					return reader.Read(waitCtx)
				}
			}
			send := func(payload []byte) error { return reader.Write(payload) }
			remote := conn.RemoteAddr().(*net.TCPAddr)
			localIP := resolveLocalIP(reader.LocalPort(), e.cfg.LocalIP)
			send = e.wrapSIPSend(callNumber, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send)
			receive = e.wrapSIPReceive(callNumber, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, receive)
			_ = e.executeCall(ctx, callNumber, callID, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send, receive, nil)
		}(accepted+1, conn)
	}
	wg.Wait()
	return nil
}
