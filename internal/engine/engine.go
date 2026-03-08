package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/adubovikov/gossipper/internal/hep"
	"github.com/adubovikov/gossipper/internal/media"
	"github.com/adubovikov/gossipper/internal/scenario"
	"github.com/adubovikov/gossipper/internal/scheduler"
	"github.com/adubovikov/gossipper/internal/sip"
	"github.com/adubovikov/gossipper/internal/stats"
	templ "github.com/adubovikov/gossipper/internal/template"
	"github.com/adubovikov/gossipper/internal/transport"
)

var (
	errStopCall = errors.New("stop current call")
	errStopNow  = errors.New("stop execution now")
)

type Config struct {
	Scenario        scenario.Scenario
	Transport       string
	LocalIP         string
	LocalPort       int
	RemoteHost      string
	RemotePort      int
	Service         string
	AuthUsername    string
	AuthPassword    string
	Rate            float64
	TotalCalls      int
	MaxConcurrent   int
	Users           int
	DefaultPause    time.Duration
	DefaultRecvTO   time.Duration
	TraceMessages   bool
	TraceShortMsg   bool
	MessageFile     string
	TraceErrors     bool
	ErrorFile       string
	TraceErrorCodes bool
	TraceLogs       bool
	LogFile         string
	TraceStats      bool
	TraceRTT        bool
	HEPAddr         string
	HEPCaptureID    uint32
	HEPPassword     string
	TLSCertFile     string
	TLSKeyFile      string
	TLSCAFile       string
	TLSSkipVerify   bool
	CommandName     string
	CommandPeers    map[string]string
}

type Engine struct {
	cfg      Config
	sched    scheduler.Scheduler
	stats    *stats.Collector
	randomMu sync.Mutex
	random   *mrand.Rand
	scopes   *scopedVars
	commands *commandBroker
	cmdNet   *commandNetwork
	trace    *traceLogger
	hep      *hep.Client
}

func New(cfg Config) *Engine {
	return &Engine{
		cfg:      cfg,
		sched:    scheduler.New(),
		stats:    stats.New(),
		random:   mrand.New(mrand.NewSource(time.Now().UnixNano())),
		scopes:   newScopedVars(),
		commands: newCommandBroker(),
	}
}

func (e *Engine) Stats() *stats.Collector {
	return e.stats
}

func (e *Engine) Run(ctx context.Context) (runErr error) {
	if err := e.startTrace(); err != nil {
		return err
	}
	defer e.stopTrace()
	if err := e.startHEP(); err != nil {
		return err
	}
	defer e.stopHEP()
	defer func() {
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			e.traceError("runtime-error", 0, runErr.Error())
		}
	}()
	if err := e.startCommandNetwork(ctx); err != nil {
		return err
	}
	defer e.stopCommandNetwork()
	if err := e.runInit(ctx); err != nil {
		return err
	}
	switch e.cfg.Scenario.Mode {
	case scenario.ModeServer:
		return e.runServer(ctx)
	default:
		return e.runClient(ctx)
	}
}

func (e *Engine) runClient(ctx context.Context) error {
	if !scenarioNeedsSIPTransport(e.cfg.Scenario) {
		return e.runClientCommandOnly(ctx)
	}
	if e.cfg.RemoteHost == "" || e.cfg.RemotePort == 0 {
		return errors.New("client mode requires a remote address")
	}

	switch e.cfg.Transport {
	case "u1":
		return e.runClientShared(ctx)
	case "un":
		return e.runClientPerCall(ctx)
	case "t1":
		return e.runClientSharedTCP(ctx)
	case "tn":
		return e.runClientPerCallTCP(ctx)
	case "l1":
		return e.runClientSharedTLS(ctx)
	case "ln":
		return e.runClientPerCallTLS(ctx)
	default:
		return fmt.Errorf("unsupported transport mode %q", e.cfg.Transport)
	}
}

func (e *Engine) runClientCommandOnly(ctx context.Context) error {
	sem := make(chan struct{}, e.cfg.MaxConcurrent)
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
			send := func(payload []byte) error {
				return fmt.Errorf("SIP send is not available in command-only scenario")
			}
			receive := func(waitCtx context.Context) (sip.Message, error) {
				return sip.Message{}, fmt.Errorf("SIP receive is not available in command-only scenario")
			}
			send = e.wrapSIPSend(callNumber, resolveLocalIP(0, e.cfg.LocalIP), e.cfg.LocalPort, e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, resolveLocalIP(0, e.cfg.LocalIP), e.cfg.LocalPort, e.cfg.RemoteHost, e.cfg.RemotePort, receive)

			runErrLocal := e.executeCall(
				ctx,
				callNumber,
				callID,
				resolveLocalIP(0, e.cfg.LocalIP),
				e.cfg.LocalPort,
				e.cfg.RemoteHost,
				e.cfg.RemotePort,
				send,
				receive,
			)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}

	wg.Wait()
	return runErr
}

func (e *Engine) runClientShared(ctx context.Context) error {
	localAddr := fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort)
	shared, err := transport.NewSharedUDP(localAddr)
	if err != nil {
		return err
	}
	defer shared.Close()

	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", e.cfg.RemoteHost, e.cfg.RemotePort))
	if err != nil {
		return err
	}

	registry := newMailboxRegistry()
	go registry.dispatch(shared.Receive())

	sem := make(chan struct{}, e.cfg.MaxConcurrent)
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

			send := func(payload []byte) error {
				return shared.Send(payload, remoteAddr)
			}

			localIP := resolveLocalIP(shared.LocalPort(), e.cfg.LocalIP)
			send = e.wrapSIPSend(callNumber, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send)
			receive = e.wrapSIPReceive(callNumber, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send, receive)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}

	wg.Wait()
	return runErr
}

func (e *Engine) runClientPerCall(ctx context.Context) error {
	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", e.cfg.RemoteHost, e.cfg.RemotePort))
	if err != nil {
		return err
	}

	sem := make(chan struct{}, e.cfg.MaxConcurrent)
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

			dialog, err := transport.NewDialogUDP(
				fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort),
				remoteAddr.String(),
			)
			if err != nil {
				once.Do(func() { runErr = err })
				return
			}
			defer dialog.Close()

			callID := newCallID(callNumber)
			receive := func(waitCtx context.Context) (sip.Message, error) {
				packet, err := dialog.Receive(waitCtx)
				if err != nil {
					return sip.Message{}, err
				}
				msg, err := sip.Parse(packet.Data)
				if err != nil {
					return sip.Message{}, err
				}
				return msg, nil
			}
			send := func(payload []byte) error {
				return dialog.Send(payload)
			}

			localIP := resolveLocalIP(dialog.LocalPort(), e.cfg.LocalIP)
			send = e.wrapSIPSend(callNumber, localIP, dialog.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send)
			receive = e.wrapSIPReceive(callNumber, localIP, dialog.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, dialog.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send, receive)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}

	wg.Wait()
	return runErr
}

func (e *Engine) runClientSharedTCP(ctx context.Context) error {
	localAddr := fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort)
	remoteAddr := fmt.Sprintf("%s:%d", e.cfg.RemoteHost, e.cfg.RemotePort)
	shared, err := transport.NewSharedTCP(localAddr, remoteAddr)
	if err != nil {
		return err
	}
	defer shared.Close()

	registry := newMailboxRegistry()
	go registry.dispatchMessages(shared.Receive())

	sem := make(chan struct{}, e.cfg.MaxConcurrent)
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
			send := func(payload []byte) error {
				return shared.Send(payload)
			}
			localIP := resolveLocalIP(shared.LocalPort(), e.cfg.LocalIP)
			send = e.wrapSIPSend(callNumber, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send, receive)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}

	wg.Wait()
	return runErr
}

func (e *Engine) runClientPerCallTCP(ctx context.Context) error {
	remoteAddr := fmt.Sprintf("%s:%d", e.cfg.RemoteHost, e.cfg.RemotePort)

	sem := make(chan struct{}, e.cfg.MaxConcurrent)
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

			dialog, err := transport.NewDialogTCP(
				fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort),
				remoteAddr,
			)
			if err != nil {
				once.Do(func() { runErr = err })
				return
			}
			defer dialog.Close()

			callID := newCallID(callNumber)
			receive := func(waitCtx context.Context) (sip.Message, error) {
				msg, err := dialog.Receive(waitCtx)
				if err != nil {
					return sip.Message{}, err
				}
				return msg, nil
			}
			send := func(payload []byte) error {
				return dialog.Send(payload)
			}

			localIP := resolveLocalIP(dialog.LocalPort(), e.cfg.LocalIP)
			send = e.wrapSIPSend(callNumber, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send, receive)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}

	wg.Wait()
	return runErr
}

func (e *Engine) runServer(ctx context.Context) error {
	switch e.cfg.Transport {
	case "u1", "un":
		return e.runServerUDP(ctx)
	case "t1":
		return e.runServerTCPShared(ctx)
	case "tn":
		return e.runServerTCPPerConn(ctx)
	case "l1":
		return e.runServerTLSShared(ctx)
	case "ln":
		return e.runServerTLSPerConn(ctx)
	default:
		return fmt.Errorf("unsupported server transport mode %q", e.cfg.Transport)
	}
}

func (e *Engine) runServerUDP(ctx context.Context) error {
	localAddr := fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort)
	shared, err := transport.NewSharedUDP(localAddr)
	if err != nil {
		return err
	}
	defer shared.Close()

	firstRecvIndex := -1
	for i, cmd := range e.cfg.Scenario.Commands {
		if cmd.Type == scenario.CommandRecv {
			firstRecvIndex = i
			break
		}
	}
	if firstRecvIndex == -1 {
		return errors.New("server scenario must start with a recv command")
	}

	type session struct {
		inbox  chan sip.Message
		remote *net.UDPAddr
	}

	var (
		mu       sync.Mutex
		sessions = make(map[string]*session)
		wg       sync.WaitGroup
	)

	finished := 0
	done := make(chan struct{})

	go func() {
		for packet := range shared.Receive() {
			msg, err := sip.Parse(packet.Data)
			if err != nil {
				continue
			}

			callID, ok := sip.Header(msg.Headers, "Call-ID")
			if !ok {
				continue
			}

			mu.Lock()
			sess, exists := sessions[callID]
			if !exists {
				firstCmd := e.cfg.Scenario.Commands[firstRecvIndex]
				if !sip.Match(msg, firstCmd.RecvReq, firstCmd.RecvResp) {
					mu.Unlock()
					continue
				}

				sess = &session{
					inbox:  make(chan sip.Message, 8),
					remote: packet.Addr,
				}
				sessions[callID] = sess
				wg.Add(1)
				go func(callNumber int, id string, startMsg sip.Message, sess *session) {
					defer wg.Done()
					defer func() {
						mu.Lock()
						delete(sessions, id)
						finished++
						if finished >= e.cfg.TotalCalls {
							close(done)
						}
						mu.Unlock()
					}()
					sess.inbox <- startMsg

					receive := func(waitCtx context.Context) (sip.Message, error) {
						select {
						case <-waitCtx.Done():
							return sip.Message{}, waitCtx.Err()
						case msg := <-sess.inbox:
							return msg, nil
						}
					}
					send := func(payload []byte) error {
						return shared.Send(payload, sess.remote)
					}

					localIP := resolveLocalIP(shared.LocalPort(), e.cfg.LocalIP)
					send = e.wrapSIPSend(callNumber, localIP, shared.LocalPort(), sess.remote.IP.String(), sess.remote.Port, send)
					receive = e.wrapSIPReceive(callNumber, localIP, shared.LocalPort(), sess.remote.IP.String(), sess.remote.Port, receive)
					_ = e.executeCall(ctx, callNumber, id, localIP, shared.LocalPort(), sess.remote.IP.String(), sess.remote.Port, send, receive)
				}(finished+1, callID, msg, sess)
				mu.Unlock()
				continue
			}
			mu.Unlock()

			select {
			case sess.inbox <- msg:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		wg.Wait()
		return nil
	}
}

func (e *Engine) runServerTCPShared(ctx context.Context) error {
	server, err := transport.NewTCPServer(fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort))
	if err != nil {
		return err
	}
	defer server.Close()

	conn, err := server.Accept(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	reader := transport.NewTCPConnReader(conn)
	var writeMu sync.Mutex
	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return errors.New("server scenario must start with a recv command")
	}

	type session struct {
		inbox chan sip.Message
	}

	var (
		mu       sync.Mutex
		sessions = make(map[string]*session)
		wg       sync.WaitGroup
		done     = make(chan struct{})
		finished int
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
						finished++
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
					_ = e.executeCall(ctx, callNumber, id, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send, receive)
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

func (e *Engine) runServerTCPPerConn(ctx context.Context) error {
	server, err := transport.NewTCPServer(fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort))
	if err != nil {
		return err
	}
	defer server.Close()

	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return errors.New("server scenario must start with a recv command")
	}

	var wg sync.WaitGroup
	for accepted := 0; accepted < e.cfg.TotalCalls; accepted++ {
		conn, err := server.Accept(ctx)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int, conn *net.TCPConn) {
			defer wg.Done()
			defer conn.Close()

			reader := transport.NewTCPConnReader(conn)
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
					msg, err := reader.Read(waitCtx)
					if err != nil {
						return sip.Message{}, err
					}
					return msg, nil
				}
			}
			send := func(payload []byte) error { return reader.Write(payload) }
			remote := conn.RemoteAddr().(*net.TCPAddr)
			localIP := resolveLocalIP(reader.LocalPort(), e.cfg.LocalIP)
			send = e.wrapSIPSend(callNumber, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send)
			receive = e.wrapSIPReceive(callNumber, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, receive)
			_ = e.executeCall(ctx, callNumber, callID, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send, receive)
		}(accepted+1, conn)
	}
	wg.Wait()
	return nil
}

func (e *Engine) executeCall(
	ctx context.Context,
	callNumber int,
	callID string,
	localIP string,
	localPort int,
	remoteHost string,
	remotePort int,
	send func([]byte) error,
	receive func(context.Context) (sip.Message, error),
) (runErr error) {
	startedAt := time.Now()
	e.stats.StartCall()
	success := false
	mediaSession := media.NewSession()
	sawUnexpectedSIP := false
	defer func() {
		if !success {
			e.stats.AddFailureClass(classifyCallFailure(runErr, sawUnexpectedSIP))
		}
		e.stats.AddMediaStats(mediaSession.Snapshot())
		mediaSession.Stop()
		e.stats.FinishCall(success, time.Since(startedAt))
	}()

	renderCtx := templ.Context{
		Service:     e.cfg.Service,
		Transport:   e.cfg.Transport,
		RemoteHost:  remoteHost,
		RemoteIP:    remoteHost,
		RemotePort:  remotePort,
		LocalIP:     localIP,
		LocalIPType: ipType(localIP),
		LocalPort:   localPort,
		MediaIP:     localIP,
		MediaIPType: ipType(localIP),
		MediaPort:   localPort + 2 + ((callNumber - 1) * 2),
		CallID:      callID,
		CSeq:        1,
		CallNumber:  callNumber,
		PID:         os.Getpid(),
		LastHeaders: make(map[string][]string),
		BasePath:    e.cfg.Scenario.BasePath,
	}

	var (
		lastSent         []byte
		lastRetrans      time.Duration
		inviteStartedAt  time.Time
		inviteLatencySet bool
		pending          []sip.Message
		commandCallKey   = renderCtx.CallID
		rtdStarts        = make(map[string]time.Time)
	)
	store := newVarStore(e.scopes, e.cfg.Scenario.GlobalVariables, e.cfg.Scenario.UserVariables, userID(callNumber, e.cfg.Users))
	renderCtx.Variables = store.Snapshot()
	finishRTD := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		startedAt, ok := rtdStarts[name]
		if !ok {
			return
		}
		value := time.Since(startedAt)
		e.stats.AddRTD(name, value)
		e.traceRTD(callNumber, name, value)
		delete(rtdStarts, name)
	}
	recordCommandStats := func(cmd scenario.Command) {
		e.stats.AddCounter(cmd.Counter)
		e.stats.AddDisplay(cmd.Display)
	}

	for index := 0; index < len(e.cfg.Scenario.Commands); {
		cmd := e.cfg.Scenario.Commands[index]

		renderCtx.Variables = store.Snapshot()
		if !shouldExecute(cmd, store) {
			index++
			continue
		}

		renderCtx.MessageIndex = cmd.Index
		if name := strings.TrimSpace(cmd.StartRTD); name != "" {
			rtdStarts[name] = time.Now()
		}

		switch cmd.Type {
		case scenario.CommandLabel:
		case scenario.CommandNop:
			if err := e.applyActions(ctx, cmd.Actions, renderCtx, store, mediaSession); err != nil {
				if errors.Is(err, errStopCall) {
					recordCommandStats(cmd)
					finishRTD(cmd.StopRTD)
					success = true
					return nil
				}
				return err
			}
		case scenario.CommandPause, scenario.CommandTimeWait:
			pause := cmd.Pause
			if pause <= 0 {
				pause = e.cfg.DefaultPause
			}
			if err := e.sched.Sleep(ctx, pause); err != nil {
				return err
			}
		case scenario.CommandSend:
			renderCtx.BranchBase = randomBranch(callNumber, cmd.Index)
			message, err := e.renderSIPMessage(cmd.SendText, renderCtx)
			if err != nil {
				return err
			}
			if err := send([]byte(message)); err != nil {
				return err
			}
			lastSent = []byte(message)
			lastRetrans = cmd.Retrans

			parsed, err := sip.Parse(lastSent)
			if err == nil && strings.EqualFold(parsed.Method, "INVITE") {
				inviteStartedAt = time.Now()
			}
		case scenario.CommandSendCmd:
			commandPayload := ensureMessageTerminator(templ.RenderMessage(cmd.SendText, renderCtx))
			if e.cfg.TraceMessages || e.cfg.TraceShortMsg {
				e.traceEvent("sendCmd", callNumber, commandPayload)
			}
			if err := e.sendCommand(cmd.CmdDest, commandPayload); err != nil {
				return err
			}
		case scenario.CommandRecv:
			recvTimeout := cmd.Timeout
			if recvTimeout <= 0 {
				if cmd.Optional {
					recvTimeout = 250 * time.Millisecond
				} else {
					recvTimeout = e.cfg.DefaultRecvTO
				}
			}

			retransmit := lastRetrans
			if !strings.HasPrefix(e.cfg.Transport, "u") {
				retransmit = 0
			}
			receiveWithPending := func(waitCtx context.Context) (sip.Message, error) {
				if len(pending) > 0 {
					msg := pending[0]
					pending = pending[1:]
					return msg, nil
				}
				return receive(waitCtx)
			}
			msg, err := e.waitForMatch(ctx, receiveWithPending, cmd, lastSent, send, retransmit, recvTimeout, func(msg sip.Message) {
				e.traceUnexpectedSIP(callNumber, cmd, msg)
				sawUnexpectedSIP = true
				pending = append(pending, msg)
			})
			if err != nil {
				if cmd.Optional {
					index = resolveNext(index, cmd, store, e.random)
					continue
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					e.stats.AddTimeout()
				}
				return err
			}

			renderCtx.LastMessage = msg.Raw
			renderCtx.LastHeaders = msg.Headers
			renderCtx.Variables = store.Snapshot()
			if err := e.applyActions(ctx, cmd.Actions, renderCtx, store, mediaSession); err != nil {
				if errors.Is(err, errStopCall) {
					recordCommandStats(cmd)
					finishRTD(cmd.StopRTD)
					success = true
					return nil
				}
				return err
			}
			if !inviteLatencySet && msg.StatusCode == 200 && !inviteStartedAt.IsZero() {
				e.stats.AddInviteLatency(time.Since(inviteStartedAt))
				inviteLatencySet = true
			}
		case scenario.CommandRecvCmd:
			recvTimeout := cmd.Timeout
			if recvTimeout <= 0 {
				if cmd.Optional {
					recvTimeout = 250 * time.Millisecond
				} else {
					recvTimeout = e.cfg.DefaultRecvTO
				}
			}
			waitKey := commandCallKey
			adoptCallID := e.cmdNet != nil && cmd.Index == 0 && len(lastSent) == 0 && renderCtx.LastMessage == ""
			if adoptCallID {
				waitKey = ""
			}
			callKey, msg, err := e.waitForCommand(ctx, waitKey, "", cmd.CmdSrc, recvTimeout)
			if err != nil {
				if cmd.Optional {
					index = resolveNext(index, cmd, store, e.random)
					continue
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					e.stats.AddTimeout()
				}
				return err
			}
			if callKey != "" {
				commandCallKey = callKey
				if adoptCallID {
					renderCtx.CallID = callKey
				}
			}
			renderCtx.LastMessage = msg.raw
			renderCtx.LastHeaders = parseCommandHeaders(msg.raw)
			if e.cfg.TraceMessages || e.cfg.TraceShortMsg {
				e.traceEvent("recvCmd", callNumber, msg.raw)
			}
			renderCtx.Variables = store.Snapshot()
			if err := e.applyActions(ctx, cmd.Actions, renderCtx, store, mediaSession); err != nil {
				if errors.Is(err, errStopCall) {
					recordCommandStats(cmd)
					finishRTD(cmd.StopRTD)
					success = true
					return nil
				}
				return err
			}
		}

		recordCommandStats(cmd)
		finishRTD(cmd.StopRTD)
		index = resolveNext(index, cmd, store, e.random)
	}

	success = true
	return nil
}

func classifyCallFailure(err error, sawUnexpectedSIP bool) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if sawUnexpectedSIP {
			return "unexpected_sip"
		}
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, io.EOF) {
		return "transport_error"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "transport_error"
	}

	errText := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errText, "empty sip message"),
		strings.Contains(errText, "invalid sip"),
		strings.Contains(errText, "malformed sip"):
		return "parse_error"
	case strings.Contains(errText, "missing call-id"),
		strings.Contains(errText, "malformed"):
		return "parse_error"
	default:
		return "scenario_error"
	}
}

func (e *Engine) waitForCommand(ctx context.Context, callID, channel, src string, timeout time.Duration) (string, commandMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		if callID == "" {
			if key, msg, ok := e.commands.dequeueAny(channel, src); ok {
				return key, msg, nil
			}
		} else if msg, ok := e.commands.dequeue(callID, channel, src); ok {
			return callID, msg, nil
		}
		if err := ctx.Err(); err != nil {
			return "", commandMessage{}, err
		}
		if time.Now().After(deadline) {
			return "", commandMessage{}, context.DeadlineExceeded
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *Engine) waitForMatch(
	ctx context.Context,
	receive func(context.Context) (sip.Message, error),
	cmd scenario.Command,
	lastSent []byte,
	send func([]byte) error,
	retrans time.Duration,
	timeout time.Duration,
	stash func(sip.Message),
) (sip.Message, error) {
	deadline := time.Now().Add(timeout)
	for {
		waitFor := time.Until(deadline)
		if waitFor <= 0 {
			return sip.Message{}, context.DeadlineExceeded
		}

		receiveCtx, cancel := context.WithTimeout(ctx, minDuration(waitFor, nextRetrans(retrans)))
		msg, err := receive(receiveCtx)
		cancel()
		if err == nil {
			if sip.Match(msg, cmd.RecvReq, cmd.RecvResp) {
				return msg, nil
			}
			if stash != nil {
				stash(msg)
			}
			continue
		}

		if errors.Is(err, context.DeadlineExceeded) && retrans > 0 && len(lastSent) > 0 && time.Now().Before(deadline) {
			if sendErr := send(lastSent); sendErr != nil {
				return sip.Message{}, sendErr
			}
			e.stats.AddRetransmit()
			continue
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return sip.Message{}, context.DeadlineExceeded
		}
		return sip.Message{}, err
	}
}

func shouldExecute(cmd scenario.Command, vars *varStore) bool {
	if cmd.CondExec == "" {
		return true
	}
	value := variableTruthy(vars.Get(cmd.CondExec))
	if cmd.CondExecInverse {
		return !value
	}
	return value
}

func resolveNext(index int, cmd scenario.Command, vars *varStore, rnd *mrand.Rand) int {
	next := index + 1
	if cmd.NextIndex < 0 {
		return next
	}
	if cmd.Test != "" && !variableTruthy(vars.Get(cmd.Test)) {
		return next
	}
	if cmd.Chance > 0 && cmd.Chance < 1.0 && rnd.Float64() > cmd.Chance {
		return next
	}
	return cmd.NextIndex
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func nextRetrans(value time.Duration) time.Duration {
	if value <= 0 {
		return 0
	}
	return value
}

func ipType(ip string) string {
	if strings.Contains(ip, ":") {
		return "6"
	}
	return "4"
}

func resolveLocalIP(port int, configured string) string {
	if configured != "" && configured != "0.0.0.0" && configured != "::" {
		return configured
	}
	if port == 0 {
		return "127.0.0.1"
	}
	return "127.0.0.1"
}

type mailboxRegistry struct {
	mu        sync.RWMutex
	mailboxes map[string]chan sip.Message
}

func newMailboxRegistry() *mailboxRegistry {
	return &mailboxRegistry{
		mailboxes: make(map[string]chan sip.Message),
	}
}

func (r *mailboxRegistry) register(callID string) chan sip.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan sip.Message, 8)
	r.mailboxes[callID] = ch
	return ch
}

func (r *mailboxRegistry) unregister(callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mailboxes, callID)
}

func (r *mailboxRegistry) dispatch(incoming <-chan transport.Packet) {
	for packet := range incoming {
		msg, err := sip.Parse(packet.Data)
		if err != nil {
			continue
		}
		callID, ok := sip.Header(msg.Headers, "Call-ID")
		if !ok {
			continue
		}
		r.mu.RLock()
		ch, exists := r.mailboxes[callID]
		r.mu.RUnlock()
		if !exists {
			continue
		}
		select {
		case ch <- msg:
		default:
		}
	}
}

func (r *mailboxRegistry) dispatchMessages(incoming <-chan sip.Message) {
	for msg := range incoming {
		r.dispatchMessage(msg)
	}
}

func (r *mailboxRegistry) dispatchMessage(msg sip.Message) {
	callID, ok := sip.Header(msg.Headers, "Call-ID")
	if !ok {
		return
	}
	r.mu.RLock()
	ch, exists := r.mailboxes[callID]
	r.mu.RUnlock()
	if !exists {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

func newCallID(callNumber int) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("gossip-%d-%d", callNumber, time.Now().UnixNano())
	}
	return fmt.Sprintf("gossip-%d-%s", callNumber, hex.EncodeToString(buf))
}

func randomBranch(callNumber, messageIndex int) string {
	return fmt.Sprintf("z9hG4bK-gossip-%d-%d", callNumber, messageIndex)
}

func ensureMessageTerminator(msg string) string {
	if strings.Contains(msg, "\r\n\r\n") {
		return msg
	}
	if strings.HasSuffix(msg, "\r\n") {
		return msg + "\r\n"
	}
	return msg + "\r\n\r\n"
}

func variableTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}

func (e *Engine) applyActions(ctx context.Context, actions []scenario.Action, renderCtx templ.Context, vars *varStore, mediaSession *media.Session) error {
	renderCtx.Variables = vars.Snapshot()
	for _, action := range actions {
		switch action.Type {
		case scenario.ActionAssignStr:
			if len(action.AssignTo) == 0 {
				continue
			}
			value := templ.RenderMessage(action.Value, renderCtx)
			for _, name := range action.AssignTo {
				vars.Set(name, value)
			}
		case scenario.ActionLog:
			if e.cfg.TraceLogs {
				e.traceActionLog(templ.RenderMessage(action.Message, renderCtx))
			}
		case scenario.ActionTest:
			if len(action.AssignTo) == 0 {
				continue
			}
			left := vars.Get(action.Variable)
			right := templ.RenderMessage(action.Value, renderCtx)
			result := compareValues(left, right, action.Compare)
			value := "0"
			if result {
				value = "1"
			}
			for _, name := range action.AssignTo {
				vars.Set(name, value)
			}
		case scenario.ActionEReg:
			if err := applyERegAction(action, renderCtx, vars); err != nil {
				return err
			}
		case scenario.ActionExec:
			if err := e.applyExecAction(ctx, action, renderCtx, vars, mediaSession); err != nil {
				return err
			}
		}
		renderCtx.Variables = vars.Snapshot()
	}
	return nil
}

func applyERegAction(action scenario.Action, renderCtx templ.Context, vars *varStore) error {
	source := renderCtx.LastMessage
	switch strings.ToLower(action.SearchIn) {
	case "", "msg":
	case "hdr":
		values, ok := lookupHeaderCI(renderCtx.LastHeaders, strings.TrimSuffix(action.Header, ":"))
		if !ok {
			source = ""
		} else {
			source = strings.Join(values, "\r\n")
		}
	case "body":
		source = extractBody(renderCtx.LastMessage)
	case "var":
		source = vars.Get(action.Variable)
	default:
		source = renderCtx.LastMessage
	}

	re, err := regexp.Compile(action.Regexp)
	if err != nil {
		return err
	}
	matches := re.FindStringSubmatch(source)
	matched := len(matches) > 0
	if action.CheckIt && !matched {
		return fmt.Errorf("ereg %q did not match", action.Regexp)
	}
	if action.CheckItInverse && matched {
		return fmt.Errorf("ereg %q matched unexpectedly", action.Regexp)
	}
	if !matched {
		return nil
	}
	for i, name := range action.AssignTo {
		if i < len(matches) {
			vars.Set(name, strings.TrimSpace(matches[i]))
		}
	}
	return nil
}

func compareValues(left, right, compare string) bool {
	switch strings.ToLower(strings.TrimSpace(compare)) {
	case "", "equal", "eq":
		return left == right
	case "not_equal", "ne":
		return left != right
	default:
		return left == right
	}
}

func lookupHeaderCI(headers map[string][]string, name string) ([]string, bool) {
	for key, values := range headers {
		if strings.EqualFold(key, name) {
			return values, true
		}
	}
	return nil, false
}

func extractBody(msg string) string {
	const sep = "\r\n\r\n"
	if idx := strings.Index(msg, sep); idx >= 0 {
		return msg[idx+len(sep):]
	}
	return ""
}

func parseCommandHeaders(msg string) map[string][]string {
	headers := make(map[string][]string)
	for _, line := range strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" {
			continue
		}
		headers[name] = append(headers[name], value)
	}
	return headers
}

func commandCallID(raw, fallback string) string {
	headers := parseCommandHeaders(raw)
	values, ok := lookupHeaderCI(headers, "Call-ID")
	if !ok || len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fallback
	}
	return strings.TrimSpace(values[0])
}

func commandSender(raw, fallback string) string {
	headers := parseCommandHeaders(raw)
	values, ok := lookupHeaderCI(headers, "From")
	if !ok || len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fallback
	}
	return strings.TrimSpace(values[0])
}

func firstReceiveIndex(sc scenario.Scenario) int {
	for i, cmd := range sc.Commands {
		if cmd.Type == scenario.CommandRecv {
			return i
		}
	}
	return -1
}

func scenarioNeedsSIPTransport(sc scenario.Scenario) bool {
	for _, cmd := range sc.Commands {
		switch cmd.Type {
		case scenario.CommandSend, scenario.CommandRecv:
			return true
		}
	}
	return false
}

type tcpReader interface {
	Read(context.Context) (sip.Message, error)
}

func waitForFirstServerMessage(ctx context.Context, reader tcpReader, firstRecvIndex int, firstCmd scenario.Command) (sip.Message, error) {
	for {
		msg, err := reader.Read(ctx)
		if err != nil {
			return sip.Message{}, err
		}
		if sip.Match(msg, firstCmd.RecvReq, firstCmd.RecvResp) {
			return msg, nil
		}
		_ = firstRecvIndex
	}
}
