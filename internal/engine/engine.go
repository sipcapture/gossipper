package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipcapture/gossipper/internal/eventlog"
	"github.com/sipcapture/gossipper/internal/hep"
	"github.com/sipcapture/gossipper/internal/media"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/scheduler"
	"github.com/sipcapture/gossipper/internal/sip"
	"github.com/sipcapture/gossipper/internal/stats"
	templ "github.com/sipcapture/gossipper/internal/template"
	"github.com/sipcapture/gossipper/internal/transport"
)

var (
	regexpCache             sync.Map // map[string]*regexp.Regexp
	errStopCall             = errors.New("stop current call")
	errStopNow              = errors.New("stop execution now")
	errUnexpectedToMain     = errors.New("unexpected SIP routed to _unexp.main")
	errOptionalRecvMismatch = errors.New("optional recv mismatched with incoming SIP")
	errSIPMailboxClosed     = errors.New("sip mailbox closed (transport ended)")
	parseHeadersLinesPool   = sync.Pool{New: func() interface{} { return new([]string) }}
)

// sipTimerB is RFC 3261 Timer B: the INVITE client transaction timeout
// (64 * T1 = 64 * 500ms = 32s). Used as a floor for optional recv timeouts
// so INVITE response chains don't abort before the transaction can complete.
const sipTimerB = 64 * 500 * time.Millisecond

type Config struct {
	Scenario         scenario.Scenario
	Transport        string
	LocalIP          string
	LocalPort        int
	RemoteHost       string
	RemotePort       int
	Service          string
	AuthUsername     string
	AuthPassword     string
	Rate             float64
	RateScale        float64
	RateIncrease     float64
	RateIncreaseStep time.Duration
	RateMax          float64
	MaxReconnect     int
	ReconnectSleep   time.Duration
	ReconnectClose   bool
	BaseCSeq         int
	TotalCalls       int
	UnlimitedCalls   bool // if true, ignore TotalCalls as a cap (stress until ctx cancel)
	MaxConcurrent    int
	MaxSockets       int
	Users            int
	DefaultPause     time.Duration
	DefaultRecvTO    time.Duration
	// RecvBYEFloorTO is the minimum wait for mandatory <recv request="BYE"/> when
	// the scenario omits recv timeout (zero uses DefaultRecvTO). Prevents UAS
	// from failing while the UAC is still in a long media pause before BYE.
	// Zero disables (BYE uses only DefaultRecvTO).
	RecvBYEFloorTO   time.Duration
	TraceMessages    bool
	TraceShortMsg    bool
	TraceCounts      bool
	MessageFile      string
	TraceErrors      bool
	ErrorFile        string
	TraceErrorCodes  bool
	TraceLogs        bool
	LogFile          string
	TraceStats       bool
	TraceRTT         bool
	TraceScreen      bool
	StatsDumpPeriod  time.Duration
	RTTDumpFrequency int
	ScreenFile       string
	HEPAddr          string
	HEPCaptureID     uint32
	HEPPassword      string
	HEPRawRTCP       bool
	HEPHomerLakeRTCP bool
	SendMediaReport  bool
	TLSCertFile      string
	TLSKeyFile       string
	TLSCAFile        string
	TLSSkipVerify    bool
	CommandName      string
	CommandPeers     map[string]string
	UISourceIPs      []string
	// InjectionFile is the CLI -inf CSV path; used as default for [fieldN] without file=.
	InjectionFile string

	// Log is the structured event logger used for SIP/call/auth events.
	// nil is treated as eventlog.Noop().
	Log  eventlog.Logger
	Role string // gossipper.role attribute: "client" or "server"

	// PCAPLinkLayer selects the PCAP datalink decoder for play_pcap_* replay (and mirrors CLI -pcap-link).
	// Empty means auto (uses file DLT; LINUX_SLL2 is detected from the global header).
	PCAPLinkLayer string
}

type Engine struct {
	cfg      Config
	sched    scheduler.Scheduler
	rate     *scheduler.RateController
	stats    *stats.Collector
	randomMu sync.Mutex
	random   *mrand.Rand
	scopes   *scopedVars
	commands *commandBroker
	cmdNet   *commandNetwork
	trace    *traceLogger
	hep      *hep.Client
	log      eventlog.Logger

	// liveScenario is the SIP scenario executed by new and running calls (executeCall).
	// It starts as a copy of cfg.Scenario and can be replaced at runtime via TryReplaceLiveScenario
	// when no calls are active and the mode matches the original scenario.
	liveScMu     sync.RWMutex
	liveScenario scenario.Scenario

	// startTime is captured at engine construction; [clock_tick] renders milliseconds since this point.
	startTime time.Time
	// dynamicID is incremented per rendered SIP message; wraps at math.MaxInt32 to mirror SIPp.
	dynamicID atomic.Int64
}

func New(cfg Config) *Engine {
	log := cfg.Log
	if log == nil {
		log = eventlog.Noop()
	}
	return &Engine{
		cfg:          cfg,
		sched:        scheduler.New(),
		rate:         scheduler.NewRateController(cfg.Rate),
		stats:        stats.New(),
		random:       mrand.New(mrand.NewSource(time.Now().UnixNano())),
		scopes:       newScopedVars(),
		commands:     newCommandBroker(),
		log:          log,
		liveScenario: cfg.Scenario,
		startTime:    time.Now(),
	}
}

// nextDynamicID returns the next unique [dynamic_id] value with INT32 wraparound, matching SIPp.
func (e *Engine) nextDynamicID() int64 {
	id := e.dynamicID.Add(1)
	if id > math.MaxInt32 {
		e.dynamicID.Store(0)
		return 0
	}
	return id
}

// clockTick returns milliseconds elapsed since engine start, matching SIPp [clock_tick] semantics.
func (e *Engine) clockTick() int64 {
	if e.startTime.IsZero() {
		return 0
	}
	return time.Since(e.startTime).Milliseconds()
}

// snapshotLiveScenario returns the scenario used for the current and next calls.
func (e *Engine) snapshotLiveScenario() scenario.Scenario {
	e.liveScMu.RLock()
	defer e.liveScMu.RUnlock()
	return e.liveScenario
}

// TryReplaceLiveScenario swaps the live SIP scenario for subsequent calls.
// Init commands are not re-run; trace count specs stay based on the original scenario.
// It fails if any call is active or if the new scenario mode differs from the startup scenario.
func (e *Engine) TryReplaceLiveScenario(next scenario.Scenario) error {
	if next.Mode != e.cfg.Scenario.Mode {
		return fmt.Errorf("scenario mode %q does not match startup mode %q (restart required)", next.Mode, e.cfg.Scenario.Mode)
	}
	snap := e.Stats().Snapshot()
	if snap.ActiveCalls > 0 {
		return fmt.Errorf("cannot replace scenario while %d calls are active", snap.ActiveCalls)
	}
	e.liveScMu.Lock()
	defer e.liveScMu.Unlock()
	e.liveScenario = next
	return nil
}

// LiveScenario returns the SIP scenario used for current and new calls (after optional hot reload).
func (e *Engine) LiveScenario() scenario.Scenario {
	return e.snapshotLiveScenario()
}

func (e *Engine) Stats() *stats.Collector {
	return e.stats
}

func (e *Engine) DumpScreenSnapshot() {
	if e.trace == nil || e.stats == nil {
		return
	}
	e.trace.writeScreenSnapshot(e.stats.Snapshot())
}

func (e *Engine) Rate() float64 {
	return e.rate.Rate()
}

func (e *Engine) SetRate(rate float64) float64 {
	return e.rate.SetRate(rate)
}

func (e *Engine) AdjustRate(delta float64) float64 {
	return e.rate.AdjustRate(delta)
}

func (e *Engine) Pause() {
	e.rate.Pause()
}

func (e *Engine) Resume() {
	e.rate.Resume()
}

func (e *Engine) Paused() bool {
	return e.rate.Paused()
}

func (e *Engine) StopScheduling() {
	e.rate.Stop()
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
	stopRateRamp := e.startRateRampLoop(ctx)
	defer stopRateRamp()
	switch e.cfg.Scenario.Mode {
	case scenario.ModeServer:
		return e.runServer(ctx)
	default:
		return e.runClient(ctx)
	}
}

func (e *Engine) startRateRampLoop(ctx context.Context) func() {
	if e.cfg.Scenario.Mode == scenario.ModeServer || e.cfg.RateIncrease == 0 {
		return func() {}
	}
	interval := e.cfg.RateIncreaseStep
	if interval <= 0 {
		interval = time.Second
	}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				next := e.rate.AdjustRate(e.cfg.RateIncrease)
				if e.cfg.RateMax > 0 && next > e.cfg.RateMax {
					e.rate.SetRate(e.cfg.RateMax)
					return
				}
			}
		}
	}()

	return func() {
		close(stopCh)
		<-doneCh
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
	case "ui":
		return e.runClientPerSourceIP(ctx)
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
	sem := make(chan struct{}, e.callConcurrencyLimit(false))
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

		if err := acquireCallSemaphore(ctx, sem); err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer func() { <-sem }()

			callID := newCallID(callNumber)
			send := func(payload []byte) error {
				return fmt.Errorf("SIP send is not available in command-only scenario")
			}
			receive := adaptReceiveToPtr(func(waitCtx context.Context) (sip.Message, error) {
				return sip.Message{}, fmt.Errorf("SIP receive is not available in command-only scenario")
			})
			send = e.wrapSIPSend(callNumber, callID, resolveLocalIP(0, e.cfg.LocalIP, e.cfg.RemoteHost, e.cfg.RemotePort), e.cfg.LocalPort, e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, callID, resolveLocalIP(0, e.cfg.LocalIP, e.cfg.RemoteHost, e.cfg.RemotePort), e.cfg.LocalPort, e.cfg.RemoteHost, e.cfg.RemotePort, receive)

			runErrLocal := e.executeCall(
				ctx,
				callNumber,
				callID,
				resolveLocalIP(0, e.cfg.LocalIP, e.cfg.RemoteHost, e.cfg.RemotePort),
				e.cfg.LocalPort,
				e.cfg.RemoteHost,
				e.cfg.RemotePort,
				send,
				receive,
				nil,
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

	registry := newMailboxRegistry(e.log)
	go registry.dispatch(shared.Receive())

	sem := make(chan struct{}, e.callConcurrencyLimit(true))
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

		if err := acquireCallSemaphore(ctx, sem); err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer func() { <-sem }()

			callID := newCallID(callNumber)
			inbox := registry.register(callID)
			defer registry.unregister(callID)

			receive := func(waitCtx context.Context) (*sip.Message, error) {
				return recvPooledFromMailboxWaitFirst(waitCtx, inbox)
			}

			send := func(payload []byte) error {
				return shared.Send(payload, remoteAddr)
			}
			setDestination := func(host string, port int) (string, error) {
				resolved, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
				if err != nil {
					return "", fmt.Errorf("setdest failed to resolve %s:%d: %w", host, port, err)
				}
				remoteAddr = resolved
				return resolved.IP.String(), nil
			}

			localIP := resolveLocalIP(shared.LocalPort(), e.cfg.LocalIP, remoteAddr.IP.String(), remoteAddr.Port)
			send = e.wrapSIPSend(callNumber, callID, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send)
			receive = e.wrapSIPReceive(callNumber, callID, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send, receive, setDestination)
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

	sem := make(chan struct{}, e.callConcurrencyLimit(false))
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

		if err := acquireCallSemaphore(ctx, sem); err != nil {
			return err
		}
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
			receive := adaptReceiveToPtr(func(waitCtx context.Context) (sip.Message, error) {
				packet, err := dialog.Receive(waitCtx)
				if err != nil {
					return sip.Message{}, err
				}
				msg := sip.GetMessage()
				defer sip.PutMessage(msg)
				if err := sip.ParseInto(msg, packet.Data); err != nil {
					return sip.Message{}, err
				}
				return msg.Copy(), nil
			})
			send := func(payload []byte) error {
				return dialog.Send(payload)
			}

			localIP := resolveLocalIP(dialog.LocalPort(), e.cfg.LocalIP, remoteAddr.IP.String(), remoteAddr.Port)
			send = e.wrapSIPSend(callNumber, callID, localIP, dialog.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send)
			receive = e.wrapSIPReceive(callNumber, callID, localIP, dialog.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, dialog.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send, receive, nil)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}

	wg.Wait()
	return runErr
}

func (e *Engine) runClientPerSourceIP(ctx context.Context) error {
	if len(e.cfg.UISourceIPs) == 0 {
		return errors.New("transport ui requires at least one source IP")
	}

	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", e.cfg.RemoteHost, e.cfg.RemotePort))
	if err != nil {
		return err
	}

	registry := newMailboxRegistry(e.log)
	sharedByIP := make(map[string]*transport.SharedUDP)
	for _, sourceIP := range e.cfg.UISourceIPs {
		if _, exists := sharedByIP[sourceIP]; exists {
			continue
		}
		bindAddr := fmt.Sprintf("%s:%d", sourceIP, e.cfg.LocalPort)
		shared, err := transport.NewSharedUDP(bindAddr)
		if err != nil {
			closeSharedSocketPool(sharedByIP)
			return fmt.Errorf("transport ui failed to bind client socket on %s: %w", bindAddr, err)
		}
		sharedByIP[sourceIP] = shared
		go registry.dispatch(shared.Receive())
	}
	defer closeSharedSocketPool(sharedByIP)

	sem := make(chan struct{}, e.callConcurrencyLimit(false))
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

		if err := acquireCallSemaphore(ctx, sem); err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer func() { <-sem }()

			sourceIP := e.sourceIPForCall(callNumber)
			shared, ok := sharedByIP[sourceIP]
			if !ok {
				once.Do(func() { runErr = fmt.Errorf("no shared socket for source IP %q", sourceIP) })
				return
			}
			callID := newCallID(callNumber)
			inbox := registry.register(callID)
			defer registry.unregister(callID)

			receive := func(waitCtx context.Context) (*sip.Message, error) {
				return recvPooledFromMailboxWaitFirst(waitCtx, inbox)
			}

			send := func(payload []byte) error {
				return shared.Send(payload, remoteAddr)
			}
			setDestination := func(host string, port int) (string, error) {
				resolved, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
				if err != nil {
					return "", fmt.Errorf("setdest failed to resolve %s:%d: %w", host, port, err)
				}
				remoteAddr = resolved
				return resolved.IP.String(), nil
			}

			localIP := resolveLocalIP(shared.LocalPort(), sourceIP, remoteAddr.IP.String(), remoteAddr.Port)
			send = e.wrapSIPSend(callNumber, callID, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send)
			receive = e.wrapSIPReceive(callNumber, callID, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, shared.LocalPort(), remoteAddr.IP.String(), remoteAddr.Port, send, receive, setDestination)
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
	shared, err := transport.NewSharedTCPWithReconnect(localAddr, remoteAddr, transport.ReconnectOptions{
		MaxAttempts:      e.cfg.MaxReconnect,
		Sleep:            e.cfg.ReconnectSleep,
		CloseOnReconnect: e.cfg.ReconnectClose,
	})
	if err != nil {
		return err
	}
	defer shared.Close()

	registry := newMailboxRegistry(e.log)
	go registry.dispatchMessages(shared.Receive())

	sem := make(chan struct{}, e.callConcurrencyLimit(true))
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

		if err := acquireCallSemaphore(ctx, sem); err != nil {
			return err
		}
		wg.Add(1)
		go func(callNumber int) {
			defer wg.Done()
			defer func() { <-sem }()

			callID := newCallID(callNumber)
			inbox := registry.register(callID)
			defer registry.unregister(callID)

			receive := func(waitCtx context.Context) (*sip.Message, error) {
				return recvPooledFromMailboxTryBuffer(waitCtx, inbox)
			}
			send := func(payload []byte) error {
				return shared.Send(payload)
			}
			localIP := resolveLocalIP(shared.LocalPort(), e.cfg.LocalIP, e.cfg.RemoteHost, e.cfg.RemotePort)
			send = e.wrapSIPSend(callNumber, callID, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, callID, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, shared.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send, receive, nil)
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

		if err := acquireCallSemaphore(ctx, sem); err != nil {
			return err
		}
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
			receive := adaptReceiveToPtr(func(waitCtx context.Context) (sip.Message, error) {
				return dialog.Receive(waitCtx)
			})
			send := func(payload []byte) error {
				return dialog.Send(payload)
			}

			localIP := resolveLocalIP(dialog.LocalPort(), e.cfg.LocalIP, e.cfg.RemoteHost, e.cfg.RemotePort)
			send = e.wrapSIPSend(callNumber, callID, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send)
			receive = e.wrapSIPReceive(callNumber, callID, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, receive)
			runErrLocal := e.executeCall(ctx, callNumber, callID, localIP, dialog.LocalPort(), e.cfg.RemoteHost, e.cfg.RemotePort, send, receive, nil)
			if runErrLocal != nil {
				once.Do(func() { runErr = runErrLocal })
			}
		}(i)
	}

	wg.Wait()
	return runErr
}

func (e *Engine) waitForNextCall(ctx context.Context) error {
	return e.rate.Wait(ctx)
}

// acquireCallSemaphore takes one slot from a buffered semaphore or returns ctx.Err()
// when the parent context is cancelled (so SIGINT cannot wedge the scheduler).
func acquireCallSemaphore(ctx context.Context, sem chan struct{}) error {
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// recvPooledFromMailboxWaitFirst waits for waitCtx or the next *sip.Message from inbox.
// A closed inbox yields errSIPMailboxClosed instead of (nil, nil).
func recvPooledFromMailboxWaitFirst(waitCtx context.Context, inbox <-chan *sip.Message) (*sip.Message, error) {
	select {
	case <-waitCtx.Done():
		return nil, waitCtx.Err()
	case msg, ok := <-inbox:
		if !ok || msg == nil {
			return nil, errSIPMailboxClosed
		}
		return msg, nil
	}
}

// recvPooledFromMailboxTryBuffer drains a buffered message without blocking, then waits.
func recvPooledFromMailboxTryBuffer(waitCtx context.Context, inbox <-chan *sip.Message) (*sip.Message, error) {
	select {
	case msg, ok := <-inbox:
		if !ok || msg == nil {
			return nil, errSIPMailboxClosed
		}
		return msg, nil
	default:
	}
	select {
	case <-waitCtx.Done():
		return nil, waitCtx.Err()
	case msg, ok := <-inbox:
		if !ok || msg == nil {
			return nil, errSIPMailboxClosed
		}
		return msg, nil
	}
}

// recvSIPValueFromMailboxWaitFirst reads a sip.Message value from a server-side inbox.
func recvSIPValueFromMailboxWaitFirst(waitCtx context.Context, inbox <-chan sip.Message) (sip.Message, error) {
	select {
	case <-waitCtx.Done():
		return sip.Message{}, waitCtx.Err()
	case msg, ok := <-inbox:
		if !ok {
			return sip.Message{}, errSIPMailboxClosed
		}
		return msg, nil
	}
}

func (e *Engine) callConcurrencyLimit(perCallSocket bool) int {
	limit := e.cfg.MaxConcurrent
	if limit <= 0 {
		limit = 1
	}
	if perCallSocket && e.cfg.MaxSockets > 0 && e.cfg.MaxSockets < limit {
		return e.cfg.MaxSockets
	}
	return limit
}

// clientShouldSpawnAnother is true while the UAC side should schedule another call (1-based index).
func (e *Engine) clientShouldSpawnAnother(callIndex int) bool {
	if e.cfg.UnlimitedCalls {
		return true
	}
	return callIndex <= e.cfg.TotalCalls
}

// serverRejectNew returns true when no more incoming calls should be accepted (capacity reached).
func (e *Engine) serverRejectNew(currentCount int) bool {
	if e.cfg.UnlimitedCalls {
		return false
	}
	return currentCount >= e.cfg.TotalCalls
}

// serverFinishedAll returns true when exactly TotalCalls sessions have completed (UAS shutdown).
func (e *Engine) serverFinishedAll(finished int) bool {
	if e.cfg.UnlimitedCalls {
		return false
	}
	return finished >= e.cfg.TotalCalls
}

func (e *Engine) runServer(ctx context.Context) error {
	switch e.cfg.Transport {
	case "u1", "un":
		return e.runServerUDP(ctx)
	case "ui":
		return e.runServerPerSourceIP(ctx)
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

type serverUDPSession struct {
	inbox     chan *sip.Message
	remote    *net.UDPAddr
	shared    *transport.SharedUDP
	localIP   string
	localPort int
}

type udpServerInbound struct {
	msg       *sip.Message
	callID    string
	remote    *net.UDPAddr
	shared    *transport.SharedUDP
	localIP   string
	localPort int
}

// resolveResponseAddr checks Via sent-by: if it is a hostname, resolves via DNS A-record.
// If the resolved (or already-IP) Via address differs from packet source, returns that address for responses.
// Returns nil to keep using packet.Addr.
func resolveResponseAddr(msg sip.Message, packetAddr *net.UDPAddr) *net.UDPAddr {
	host, port, ok := sip.ViaSentBy(msg.Headers)
	if !ok {
		return nil
	}
	var viaIP net.IP
	if ip := net.ParseIP(host); ip != nil {
		viaIP = ip
	} else {
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return nil
		}
		viaIP = net.ParseIP(addrs[0])
		if viaIP == nil {
			return nil
		}
	}
	if viaIP.Equal(packetAddr.IP) {
		return nil
	}
	return &net.UDPAddr{IP: viaIP, Port: port}
}

func (e *Engine) runServerUDP(ctx context.Context) error {
	localAddr := fmt.Sprintf("%s:%d", e.cfg.LocalIP, e.cfg.LocalPort)
	shared, err := transport.NewSharedUDP(localAddr)
	if err != nil {
		return err
	}
	defer shared.Close()

	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return errors.New("server scenario must start with a recv command")
	}

	var (
		mu       sync.Mutex
		sessions = make(map[string]*serverUDPSession)
		wg       sync.WaitGroup
		doneOnce sync.Once
	)

	accepted := 0
	finished := 0
	done := make(chan struct{})

	go func() {
		for packet := range shared.Receive() {
			msg := sip.GetMessage()
			if err := sip.ParseInto(msg, packet.Data); err != nil {
				sip.PutMessage(msg)
				continue
			}

			callID, ok := sip.Header(msg.Headers, "Call-ID")
			if !ok {
				sip.PutMessage(msg)
				continue
			}
			callID = sip.NormalizeCallID(callID)

			mu.Lock()
			sess, exists := sessions[callID]
			if !exists {
				firstCmd := e.cfg.Scenario.Commands[firstRecvIndex]
				if !sip.Match(*msg, firstCmd.RecvReq, firstCmd.RecvResp) || e.serverRejectNew(accepted) {
					mu.Unlock()
					sip.PutMessage(msg)
					continue
				}
				accepted++
				callNumber := accepted

				remote := packet.Addr
				if viaAddr := resolveResponseAddr(*msg, packet.Addr); viaAddr != nil {
					remote = viaAddr
				}
				sess = &serverUDPSession{
					inbox:     make(chan *sip.Message, sipMailboxCap),
					remote:    remote,
					shared:    shared,
					localIP:   resolveLocalIP(shared.LocalPort(), e.cfg.LocalIP, remote.IP.String(), remote.Port),
					localPort: shared.LocalPort(),
				}
				sessions[callID] = sess
				wg.Add(1)
				go func(callNumber int, id string, startMsg *sip.Message, sess *serverUDPSession) {
					defer wg.Done()
					defer func() {
						mu.Lock()
						delete(sessions, id)
						finished++
						if e.serverFinishedAll(finished) {
							doneOnce.Do(func() { close(done) })
						}
						mu.Unlock()
					}()
					sess.inbox <- startMsg

					receive := func(waitCtx context.Context) (*sip.Message, error) {
						return recvPooledFromMailboxWaitFirst(waitCtx, sess.inbox)
					}
					send := func(payload []byte) error {
						return sess.shared.Send(payload, sess.remote)
					}

					send = e.wrapSIPSend(callNumber, id, sess.localIP, sess.localPort, sess.remote.IP.String(), sess.remote.Port, send)
					receive = e.wrapSIPReceive(callNumber, id, sess.localIP, sess.localPort, sess.remote.IP.String(), sess.remote.Port, receive)
					_ = e.executeCall(ctx, callNumber, id, sess.localIP, sess.localPort, sess.remote.IP.String(), sess.remote.Port, send, receive, func(host string, port int) (string, error) {
						resolved, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
						if err != nil {
							return "", fmt.Errorf("setdest failed to resolve %s:%d: %w", host, port, err)
						}
						sess.remote = resolved
						return resolved.IP.String(), nil
					})
				}(callNumber, callID, msg, sess)
				mu.Unlock()
				continue
			}
			mu.Unlock()

			select {
			case sess.inbox <- msg:
			default:
				e.log.Emit(eventlog.Event{
					Time:  time.Now(),
					Level: eventlog.LevelWarn,
					Kind:  eventlog.KindSIPMailboxDrop,
					Msg:   "SIP message dropped (server UDP session mailbox full)",
					Attrs: map[string]any{"call_id": callID},
				})
				sip.PutMessage(msg)
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

func (e *Engine) runServerPerSourceIP(ctx context.Context) error {
	if len(e.cfg.UISourceIPs) == 0 {
		return errors.New("transport ui requires at least one source IP")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	firstRecvIndex := firstReceiveIndex(e.cfg.Scenario)
	if firstRecvIndex == -1 {
		return errors.New("server scenario must start with a recv command")
	}

	incoming := make(chan udpServerInbound, 256)
	pool := make(map[string]*transport.SharedUDP)
	for _, sourceIP := range e.cfg.UISourceIPs {
		if _, exists := pool[sourceIP]; exists {
			continue
		}
		bindAddr := fmt.Sprintf("%s:%d", sourceIP, e.cfg.LocalPort)
		shared, err := transport.NewSharedUDP(bindAddr)
		if err != nil {
			closeSharedSocketPool(pool)
			return fmt.Errorf("transport ui failed to bind server listener on %s: %w", bindAddr, err)
		}
		pool[sourceIP] = shared
		go func(localIP string, socket *transport.SharedUDP) {
			for packet := range socket.Receive() {
				msg := sip.GetMessage()
				if err := sip.ParseInto(msg, packet.Data); err != nil {
					sip.PutMessage(msg)
					continue
				}
				callID, ok := sip.Header(msg.Headers, "Call-ID")
				if !ok {
					sip.PutMessage(msg)
					continue
				}
				callID = sip.NormalizeCallID(callID)
				ib := udpServerInbound{
					msg:       msg,
					callID:    callID,
					remote:    packet.Addr,
					shared:    socket,
					localIP:   localIP,
					localPort: socket.LocalPort(),
				}
				select {
				case <-runCtx.Done():
					return
				case incoming <- ib:
				}
			}
		}(sourceIP, shared)
	}
	defer closeSharedSocketPool(pool)

	var (
		mu       sync.Mutex
		sessions = make(map[string]*serverUDPSession)
		wg       sync.WaitGroup
		doneOnce sync.Once
	)
	accepted := 0
	finished := 0
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-runCtx.Done():
				return
			case packet := <-incoming:
				mu.Lock()
				sess, exists := sessions[packet.callID]
				if !exists {
					firstCmd := e.cfg.Scenario.Commands[firstRecvIndex]
					if !sip.Match(*packet.msg, firstCmd.RecvReq, firstCmd.RecvResp) || e.serverRejectNew(accepted) {
						mu.Unlock()
						sip.PutMessage(packet.msg)
						continue
					}
					accepted++
					callNumber := accepted

					remote := packet.remote
					if viaAddr := resolveResponseAddr(*packet.msg, packet.remote); viaAddr != nil {
						remote = viaAddr
					}
					sess = &serverUDPSession{
						inbox:     make(chan *sip.Message, sipMailboxCap),
						remote:    remote,
						shared:    packet.shared,
						localIP:   packet.localIP,
						localPort: packet.localPort,
					}
					sessions[packet.callID] = sess
					wg.Add(1)
					go func(id string, startMsg *sip.Message, callNumber int, sess *serverUDPSession) {
						defer wg.Done()
						defer func() {
							mu.Lock()
							delete(sessions, id)
							finished++
							if e.serverFinishedAll(finished) {
								doneOnce.Do(func() { close(done) })
							}
							mu.Unlock()
						}()
						sess.inbox <- startMsg
						receive := func(waitCtx context.Context) (*sip.Message, error) {
							return recvPooledFromMailboxWaitFirst(waitCtx, sess.inbox)
						}
						send := func(payload []byte) error {
							return sess.shared.Send(payload, sess.remote)
						}
						send = e.wrapSIPSend(callNumber, id, sess.localIP, sess.localPort, sess.remote.IP.String(), sess.remote.Port, send)
						receive = e.wrapSIPReceive(callNumber, id, sess.localIP, sess.localPort, sess.remote.IP.String(), sess.remote.Port, receive)
						_ = e.executeCall(ctx, callNumber, id, sess.localIP, sess.localPort, sess.remote.IP.String(), sess.remote.Port, send, receive, func(host string, port int) (string, error) {
							resolved, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
							if err != nil {
								return "", fmt.Errorf("setdest failed to resolve %s:%d: %w", host, port, err)
							}
							sess.remote = resolved
							return resolved.IP.String(), nil
						})
					}(packet.callID, packet.msg, callNumber, sess)
					mu.Unlock()
					continue
				}
				if sess.shared != packet.shared {
					mu.Unlock()
					sip.PutMessage(packet.msg)
					continue
				}
				mu.Unlock()

				select {
				case sess.inbox <- packet.msg:
				default:
					e.log.Emit(eventlog.Event{
						Time:  time.Now(),
						Level: eventlog.LevelWarn,
						Kind:  eventlog.KindSIPMailboxDrop,
						Msg:   "SIP message dropped (server UDP session mailbox full)",
						Attrs: map[string]any{"call_id": packet.callID},
					})
					sip.PutMessage(packet.msg)
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	case <-done:
		cancel()
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
			callID = sip.NormalizeCallID(callID)
			mu.Lock()
			sess, exists := sessions[callID]
			if !exists {
				firstCmd := e.cfg.Scenario.Commands[firstRecvIndex]
				if !sip.Match(msg, firstCmd.RecvReq, firstCmd.RecvResp) || e.serverRejectNew(len(sessions)) {
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
						return recvSIPValueFromMailboxWaitFirst(waitCtx, inbox)
					}
					send := func(payload []byte) error {
						writeMu.Lock()
						defer writeMu.Unlock()
						return reader.Write(payload)
					}
					remote := conn.RemoteAddr().(*net.TCPAddr)
					localIP := resolveLocalIP(reader.LocalPort(), e.cfg.LocalIP, remote.IP.String(), remote.Port)
					send = e.wrapSIPSend(callNumber, id, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send)
					recv := adaptReceiveToPtr(receive)
					recv = e.wrapSIPReceive(callNumber, id, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, recv)
					_ = e.executeCall(ctx, callNumber, id, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send, recv, nil)
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
	for accepted := 0; e.cfg.UnlimitedCalls || accepted < e.cfg.TotalCalls; accepted++ {
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
			callID = sip.NormalizeCallID(callID)
			inbox := make(chan sip.Message, 8)
			inbox <- first
			receive := func(waitCtx context.Context) (sip.Message, error) {
				select {
				case <-waitCtx.Done():
					return sip.Message{}, waitCtx.Err()
				case msg, ok := <-inbox:
					if !ok {
						return sip.Message{}, errSIPMailboxClosed
					}
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
			localIP := resolveLocalIP(reader.LocalPort(), e.cfg.LocalIP, remote.IP.String(), remote.Port)
			send = e.wrapSIPSend(callNumber, callID, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send)
			recv := adaptReceiveToPtr(receive)
			recv = e.wrapSIPReceive(callNumber, callID, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, recv)
			_ = e.executeCall(ctx, callNumber, callID, localIP, reader.LocalPort(), remote.IP.String(), remote.Port, send, recv, nil)
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
	receive func(context.Context) (*sip.Message, error),
	setDestination func(host string, port int) (string, error),
) (runErr error) {
	startedAt := time.Now()
	e.stats.StartCall()
	success := false
	mediaSession := media.NewSession()
	mediaSession.SetPCAPLinkLayer(e.cfg.PCAPLinkLayer)
	if e.hep != nil {
		mediaSession.SetHEPObserver(e.hep)
	}
	mediaSession.SetCallID(callID)
	sawUnexpectedSIP := false
	e.log.Emit(eventlog.Event{
		Time:  startedAt,
		Level: eventlog.LevelInfo,
		Kind:  eventlog.KindCallStart,
		Msg:   "call started",
		Attrs: map[string]any{
			"call_id":     callID,
			"call_num":    callNumber,
			"local_ip":    localIP,
			"local_port":  localPort,
			"remote_ip":   remoteHost,
			"remote_port": remotePort,
			"transport":   e.cfg.Transport,
		},
	})
	defer func() {
		if !success {
			e.stats.AddFailureClass(classifyCallFailure(runErr, sawUnexpectedSIP))
		}
		e.stats.AddMediaStats(mediaSession.Snapshot())
		if e.hep != nil {
			e.hep.SendFinalReports(callID)
		}
		mediaSession.Stop()
		duration := time.Since(startedAt)
		e.stats.FinishCall(success, duration)
		e.traceCallCompleted()
		result := "success"
		if !success {
			result = classifyCallFailure(runErr, sawUnexpectedSIP)
			if result == "" {
				result = "failed"
			}
		}
		endAttrs := map[string]any{
			"call_id":     callID,
			"call_num":    callNumber,
			"result":      result,
			"duration_ms": duration.Milliseconds(),
		}
		if runErr != nil {
			endAttrs["error"] = runErr.Error()
		}
		level := eventlog.LevelInfo
		if !success {
			level = eventlog.LevelWarn
		}
		e.log.Emit(eventlog.Event{
			Time:  time.Now(),
			Level: level,
			Kind:  eventlog.KindCallEnd,
			Msg:   "call ended",
			Attrs: endAttrs,
		})
	}()

	scen := e.snapshotLiveScenario()
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
		CSeq:        e.cfg.BaseCSeq,
		CallNumber:  callNumber,
		PID:         os.Getpid(),
		LastHeaders: make(map[string][]string),
		ExtraKeywords: map[string]string{
			"routes": "",
		},
		CSVFieldOverrides: make(map[string]map[int]map[int]string),
		BasePath:          scen.BasePath,
		InjectionFile:     e.cfg.InjectionFile,
	}
	currentRemoteHost := remoteHost
	currentRemoteIP := remoteHost
	currentRemotePort := remotePort

	var (
		lastSent         []byte
		lastRetrans      time.Duration
		inviteStartedAt  time.Time
		inviteLatencySet bool
		pending          []*sip.Message
		commandCallKey   = renderCtx.CallID
		rtdStarts        = make(map[string]time.Time)
	)
	currentUserID := userID(callNumber, e.cfg.Users)
	renderCtx.Users = e.cfg.Users
	renderCtx.UserID = currentUserID
	renderCtx.ServerIP = localIP
	store := newVarStore(e.scopes, scen.GlobalVariables, scen.UserVariables, currentUserID)
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
	applySetDestination := func(result actionResult) error {
		if !result.hasSetDestination {
			return nil
		}
		if setDestination == nil {
			return errors.New("setdest is not supported for this transport mode")
		}
		if result.setDestinationProto != "" {
			requested := normalizeTransportForSetDest(result.setDestinationProto)
			current := normalizeTransportForSetDest(renderCtx.Transport)
			if requested != current {
				return fmt.Errorf("setdest protocol %q is incompatible with current transport %q", result.setDestinationProto, current)
			}
		}
		resolvedIP, err := setDestination(result.setDestinationHost, result.setDestinationPort)
		if err != nil {
			return err
		}
		currentRemoteHost = result.setDestinationHost
		currentRemotePort = result.setDestinationPort
		if strings.TrimSpace(resolvedIP) != "" {
			currentRemoteIP = resolvedIP
		} else {
			currentRemoteIP = currentRemoteHost
		}
		renderCtx.RemoteHost = currentRemoteHost
		renderCtx.RemoteIP = currentRemoteIP
		renderCtx.RemotePort = currentRemotePort
		return nil
	}

	for index := 0; index < len(scen.Commands); {
		cmd := scen.Commands[index]

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
			actionResult, err := e.applyActions(ctx, callNumber, cmd.Actions, renderCtx, store, mediaSession)
			if err != nil {
				if errors.Is(err, errStopCall) {
					recordCommandStats(cmd)
					finishRTD(cmd.StopRTD)
					success = true
					return nil
				}
				return err
			}
			if err := applySetDestination(actionResult); err != nil {
				return err
			}
			if actionResult.hasJump {
				recordCommandStats(cmd)
				finishRTD(cmd.StopRTD)
				index = actionResult.jumpIndex
				continue
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
			e.traceCountSent(cmd.Index)
			lastSent = []byte(message)
			lastRetrans = cmd.Retrans

			parsed := sip.GetMessage()
			defer sip.PutMessage(parsed)
			if err := sip.ParseInto(parsed, lastSent); err == nil && strings.EqualFold(parsed.Method, "INVITE") {
				inviteStartedAt = time.Now()
			}
		case scenario.CommandSendCmd:
			rawCmd := normalizeSIPScenarioLineIndent(cmd.SendText)
			cmdRenderCtx := renderCtx
			cmdRenderCtx.ClockTick = e.clockTick()
			cmdRenderCtx.DynamicID = e.nextDynamicID()
			commandPayload, err := templ.RenderMessageStrict(rawCmd, cmdRenderCtx)
			if err != nil {
				return err
			}
			commandPayload = ensureMessageTerminator(commandPayload)
			if e.cfg.TraceMessages || e.cfg.TraceShortMsg {
				e.traceEvent("sendCmd", callNumber, commandPayload)
			}
			if err := e.sendCommand(cmd.CmdDest, commandPayload); err != nil {
				return err
			}
		case scenario.CommandRecv:
			recvTimeout := effectiveSIPRecvTimeout(cmd, e.cfg.DefaultRecvTO, e.cfg.RecvBYEFloorTO)

			retransmit := lastRetrans
			if !strings.HasPrefix(e.cfg.Transport, "u") {
				retransmit = 0
			}
			receiveWithPending := func(waitCtx context.Context) (*sip.Message, bool, error) {
				if len(pending) > 0 {
					msg := pending[0]
					pending = pending[1:]
					return msg, true, nil
				}
				msg, err := receive(waitCtx)
				return msg, false, err
			}
			unexpMainIndex := -1
			if idx, ok := scen.Labels["_unexp.main"]; ok {
				unexpMainIndex = idx
			}
			var unexpectedForMain *sip.Message
			msg, err := e.waitForMatch(ctx, receiveWithPending, cmd, lastSent, send, retransmit, recvTimeout, func(m *sip.Message, fromPending bool) bool {
				if fromPending {
					if cmd.Optional {
						// Re-queue back so subsequent optional recvs can try it.
						pending = append(pending, m)
					}
					return false
				}
				if cmd.RecvResp != "" && sip.ResponseStatusMatches(*m, cmd.RecvResp) && !sip.MatchRecv(*m, cmd.RecvReq, cmd.RecvResp, lastSent) {
					e.emitRecvCSeqReject(callNumber, renderCtx.CallID, cmd, m, lastSent)
				}
				if !cmd.Optional {
					// For optional recvs, a mismatched message is just being
					// deferred to a later recv — not truly unexpected.
					e.traceUnexpectedSIP(callNumber, cmd, *m)
					e.traceCountUnexpected(cmd.Index)
					sawUnexpectedSIP = true
				}
				if unexpMainIndex >= 0 && unexpectedForMain == nil {
					cpy := m.Copy()
					unexpectedForMain = &cpy
					return true
				}
				pending = append(pending, m)
				return false
			})
			if err != nil {
				if errors.Is(err, errUnexpectedToMain) && unexpectedForMain != nil && unexpMainIndex >= 0 {
					renderCtx.LastMessage = unexpectedForMain.Raw
					renderCtx.LastHeaders = copyHeaders(unexpectedForMain.Headers)
					store.Set("_unexp.retaddr", strconv.Itoa(index+1))
					index = unexpMainIndex
					continue
				}
				if cmd.Optional {
					index = index + 1
					continue
				}
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					e.stats.AddTimeout()
					e.emitTimeoutEvent(callNumber, renderCtx.CallID, cmd, recvTimeout, "sip.recv")
				}
				return err
			}
			defer sip.PutMessage(msg)
			e.traceCountRecv(cmd.Index)

			renderCtx.LastMessage = msg.Raw
			renderCtx.LastHeaders = copyHeaders(msg.Headers)
			if cmd.RRS {
				renderCtx.ExtraKeywords["routes"] = buildRouteHeaders(msg.Headers)
			}
			renderCtx.Variables = store.Snapshot()
			actionResult, err := e.applyActions(ctx, callNumber, cmd.Actions, renderCtx, store, mediaSession)
			if err != nil {
				if errors.Is(err, errStopCall) {
					recordCommandStats(cmd)
					finishRTD(cmd.StopRTD)
					success = true
					return nil
				}
				return err
			}
			if err := applySetDestination(actionResult); err != nil {
				return err
			}
			if actionResult.hasJump {
				recordCommandStats(cmd)
				finishRTD(cmd.StopRTD)
				index = actionResult.jumpIndex
				continue
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
					e.emitTimeoutEvent(callNumber, renderCtx.CallID, cmd, recvTimeout, "cmd.recv")
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
			actionResult, err := e.applyActions(ctx, callNumber, cmd.Actions, renderCtx, store, mediaSession)
			if err != nil {
				if errors.Is(err, errStopCall) {
					recordCommandStats(cmd)
					finishRTD(cmd.StopRTD)
					success = true
					return nil
				}
				return err
			}
			if err := applySetDestination(actionResult); err != nil {
				return err
			}
			if actionResult.hasJump {
				recordCommandStats(cmd)
				finishRTD(cmd.StopRTD)
				index = actionResult.jumpIndex
				continue
			}
		}

		recordCommandStats(cmd)
		finishRTD(cmd.StopRTD)
		index = resolveNext(index, cmd, store, e.random)
	}

	success = true
	return nil
}

// effectiveSIPRecvTimeout chooses the wait duration for a SIP <recv> when the
// scenario leaves cmd.Timeout at zero (meaning: use engine defaults).
func effectiveSIPRecvTimeout(cmd scenario.Command, defaultRecv, recvBYEFloor time.Duration) time.Duration {
	if cmd.Timeout > 0 {
		return cmd.Timeout
	}
	out := defaultRecv
	if cmd.Optional && out < sipTimerB {
		// Optional recvs must wait at least RFC 3261 Timer B so that slow
		// INVITE transactions (TLS + media processing) have time to deliver
		// their 200 OK before the scenario gives up.
		out = sipTimerB
	}
	if !cmd.Optional && recvBYEFloor > 0 && strings.EqualFold(strings.TrimSpace(cmd.RecvReq), "BYE") && out < recvBYEFloor {
		out = recvBYEFloor
	}
	return out
}

// emitTimeoutEvent surfaces a structured timeout for the SIP recv hot path so
// users see why a call was abandoned without re-parsing trace files.
func (e *Engine) emitTimeoutEvent(callNumber int, callID string, cmd scenario.Command, recvTimeout time.Duration, source string) {
	if e == nil || e.log == nil {
		return
	}
	expected := cmd.RecvResp
	if cmd.RecvReq != "" {
		expected = cmd.RecvReq
	}
	attrs := map[string]any{
		"call_num":      callNumber,
		"timeout_ms":    recvTimeout.Milliseconds(),
		"timeout_ns":    recvTimeout.Nanoseconds(),
		"command.index": cmd.Index,
		"source":        source,
	}
	if callID != "" {
		attrs["call_id"] = callID
	}
	if expected != "" {
		attrs["expected"] = expected
	}
	msg := "recv timeout"
	if expected != "" {
		msg = "recv timeout waiting for " + expected
	}
	e.log.Emit(eventlog.Event{
		Level: eventlog.LevelWarn,
		Kind:  eventlog.KindTimeout,
		Msg:   msg,
		Attrs: attrs,
	})
}

// emitRecvCSeqReject logs when a response matches the recv status filter but is
// rejected by MatchRecv (typically wrong CSeq while waiting for 200 to BYE).
func (e *Engine) emitRecvCSeqReject(callNumber int, callID string, cmd scenario.Command, m *sip.Message, lastSent []byte) {
	if e == nil || e.log == nil || m == nil || cmd.RecvResp == "" {
		return
	}
	if !sip.ResponseStatusMatches(*m, cmd.RecvResp) || sip.MatchRecv(*m, cmd.RecvReq, cmd.RecvResp, lastSent) {
		return
	}
	attrs := map[string]any{
		"call_num":      callNumber,
		"command.index": cmd.Index,
		"recv_response": cmd.RecvResp,
		"sip.status":    m.StatusCode,
	}
	if callID != "" {
		attrs["call_id"] = callID
	}
	if v, ok := sip.Header(m.Headers, "CSeq"); ok {
		attrs["in_cseq"] = v
	}
	if len(lastSent) > 0 {
		req := sip.GetMessage()
		defer sip.PutMessage(req)
		if err := sip.ParseInto(req, lastSent); err == nil && req.Method != "" && req.StatusCode == 0 {
			attrs["last_request_method"] = req.Method
			if v, ok := sip.Header(req.Headers, "CSeq"); ok {
				attrs["expected_cseq"] = v
			}
		}
	}
	e.log.Emit(eventlog.Event{
		Time:  time.Now(),
		Level: eventlog.LevelWarn,
		Kind:  eventlog.KindSIPRecvCSeq,
		Msg:   "SIP response matches recv status but not CSeq of last sent request (stashed as unexpected)",
		Attrs: attrs,
	})
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
	if errors.Is(err, errSIPMailboxClosed) {
		return "transport_error"
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
	if callID != "" {
		callID = sip.NormalizeCallID(callID)
	}
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
	receive func(context.Context) (*sip.Message, bool, error),
	cmd scenario.Command,
	lastSent []byte,
	send func([]byte) error,
	retrans time.Duration,
	timeout time.Duration,
	stash func(*sip.Message, bool) bool,
) (*sip.Message, error) {
	deadline := time.Now().Add(timeout)
	for {
		waitFor := time.Until(deadline)
		if waitFor <= 0 {
			return nil, fmt.Errorf("sip recv: exceeded total wait %v (request=%q response=%q): %w",
				timeout, strings.TrimSpace(cmd.RecvReq), strings.TrimSpace(cmd.RecvResp), context.DeadlineExceeded)
		}

		receiveCtx, cancel := context.WithTimeout(ctx, minDuration(waitFor, nextRetrans(retrans)))
		msg, fromPending, err := receive(receiveCtx)
		cancel()
		if err == nil {
			if sip.MatchRecv(*msg, cmd.RecvReq, cmd.RecvResp, lastSent) {
				return msg, nil
			}
			if stash != nil {
				if routeToMain := stash(msg, fromPending); routeToMain {
					sip.PutMessage(msg)
					return nil, errUnexpectedToMain
				}
			}
			if cmd.Optional {
				// Free msg only if stash did NOT take ownership.
				// When stash is present it re-queues the message into pending
				// (even when fromPending is true) so the next recv can try it.
				if stash == nil {
					sip.PutMessage(msg)
				}
				return nil, errOptionalRecvMismatch
			}
			if fromPending {
				sip.PutMessage(msg)
			}
			continue
		}

		if errors.Is(err, context.DeadlineExceeded) && retrans > 0 && len(lastSent) > 0 && time.Now().Before(deadline) && ctx.Err() == nil {
			if sendErr := send(lastSent); sendErr != nil {
				return nil, sendErr
			}
			e.stats.AddRetransmit()
			continue
		}

		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("sip recv: timed out after %v (request=%q response=%q): %w",
				timeout, strings.TrimSpace(cmd.RecvReq), strings.TrimSpace(cmd.RecvResp), err)
		}
		return nil, err
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

// isWildcardLocalIP reports addresses that are valid for bind but must not
// appear verbatim in SIP Via/Contact (aligned with SIPp treating INADDR_ANY).
func isWildcardLocalIP(configured string) bool {
	s := strings.TrimSpace(configured)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" || s == "0.0.0.0" || s == "::" {
		return true
	}
	if ip := net.ParseIP(s); ip != nil && ip.IsUnspecified() {
		return true
	}
	return false
}

// localIPToward returns the local IP the kernel would use to reach remoteHost:remotePort,
// using a connected UDP socket (same idea as SIPp src/socket.cpp).
func localIPToward(remoteHost string, remotePort int) (string, error) {
	if strings.TrimSpace(remoteHost) == "" || remotePort <= 0 {
		return "", errors.New("local IP discovery requires remote host:port")
	}
	addr := net.JoinHostPort(remoteHost, strconv.Itoa(remotePort))
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return "", err
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udpAddr == nil || udpAddr.IP == nil {
		return "", errors.New("unexpected UDP local address")
	}
	ip := udpAddr.IP
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String(), nil
	}
	return ip.String(), nil
}

func resolveLocalIP(port int, configured string, towardHost string, towardPort int) string {
	if !isWildcardLocalIP(configured) {
		return configured
	}
	if ip, err := localIPToward(towardHost, towardPort); err == nil && ip != "" {
		return ip
	}
	_ = port
	return "127.0.0.1"
}

func closeSharedSocketPool(pool map[string]*transport.SharedUDP) {
	for _, shared := range pool {
		_ = shared.Close()
	}
}

func (e *Engine) sourceIPForCall(callNumber int) string {
	if len(e.cfg.UISourceIPs) == 0 {
		return e.cfg.LocalIP
	}
	index := (callNumber - 1) % len(e.cfg.UISourceIPs)
	return e.cfg.UISourceIPs[index]
}

// sipMailboxCap is the per-call buffer for demuxed UDP/TCP SIP messages. Too
// small a value combined with non-blocking enqueue caused silent drops of
// in-dialog responses (e.g. 200 OK to BYE) under provisional bursts.
const sipMailboxCap = 128

type mailboxRegistry struct {
	mu        sync.RWMutex
	mailboxes map[string]chan *sip.Message
	log       eventlog.Logger
}

func newMailboxRegistry(log eventlog.Logger) *mailboxRegistry {
	if log == nil {
		log = eventlog.Noop()
	}
	return &mailboxRegistry{
		mailboxes: make(map[string]chan *sip.Message),
		log:       log,
	}
}

func (r *mailboxRegistry) register(callID string) chan *sip.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan *sip.Message, sipMailboxCap)
	r.mailboxes[sip.NormalizeCallID(callID)] = ch
	return ch
}

func (r *mailboxRegistry) unregister(callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mailboxes, sip.NormalizeCallID(callID))
}

func (r *mailboxRegistry) dispatch(incoming <-chan transport.Packet) {
	for packet := range incoming {
		msg := sip.GetMessage()
		if err := sip.ParseInto(msg, packet.Data); err != nil {
			sip.PutMessage(msg)
			continue
		}
		callID, ok := sip.Header(msg.Headers, "Call-ID")
		if !ok {
			sip.PutMessage(msg)
			continue
		}
		callID = sip.NormalizeCallID(callID)
		r.mu.RLock()
		ch, exists := r.mailboxes[callID]
		r.mu.RUnlock()
		if !exists {
			sip.PutMessage(msg)
			continue
		}
		select {
		case ch <- msg:
		default:
			r.log.Emit(eventlog.Event{
				Time:  time.Now(),
				Level: eventlog.LevelWarn,
				Kind:  eventlog.KindSIPMailboxDrop,
				Msg:   "SIP message dropped (per-call mailbox full)",
				Attrs: map[string]any{"call_id": callID},
			})
			sip.PutMessage(msg)
		}
	}
}

func (r *mailboxRegistry) dispatchMessages(incoming <-chan sip.Message) {
	for msg := range incoming {
		m := sip.GetMessage()
		sip.CopyInto(m, msg)
		r.dispatchMessagePtr(m)
	}
}

func (r *mailboxRegistry) dispatchMessagePtr(m *sip.Message) {
	callID, ok := sip.Header(m.Headers, "Call-ID")
	if !ok {
		sip.PutMessage(m)
		return
	}
	callID = sip.NormalizeCallID(callID)
	r.mu.RLock()
	ch, exists := r.mailboxes[callID]
	r.mu.RUnlock()
	if !exists {
		sip.PutMessage(m)
		return
	}
	select {
	case ch <- m:
	default:
		r.log.Emit(eventlog.Event{
			Time:  time.Now(),
			Level: eventlog.LevelWarn,
			Kind:  eventlog.KindSIPMailboxDrop,
			Msg:   "SIP message dropped (per-call mailbox full)",
			Attrs: map[string]any{"call_id": callID},
		})
		sip.PutMessage(m)
	}
}

func (r *mailboxRegistry) dispatchMessage(msg sip.Message) {
	m := sip.GetMessage()
	sip.CopyInto(m, msg)
	r.dispatchMessagePtr(m)
}

// adaptReceiveToPtr wraps a receive that returns (sip.Message, error) to return (*sip.Message, error)
// by copying into a pooled message. Caller (executeCall) must PutMessage when done.
func adaptReceiveToPtr(receive func(context.Context) (sip.Message, error)) func(context.Context) (*sip.Message, error) {
	return func(ctx context.Context) (*sip.Message, error) {
		msg, err := receive(ctx)
		if err != nil {
			return nil, err
		}
		if msg.Raw == "" && msg.StatusCode == 0 && msg.Method == "" {
			return nil, errSIPMailboxClosed
		}
		m := sip.GetMessage()
		sip.CopyInto(m, msg)
		return m, nil
	}
}

func (r *mailboxRegistry) _dispatchMessageOld(msg sip.Message) {
	m := sip.GetMessage()
	sip.CopyInto(m, msg)
	callID, ok := sip.Header(m.Headers, "Call-ID")
	if !ok {
		sip.PutMessage(m)
		return
	}
	r.mu.RLock()
	ch, exists := r.mailboxes[callID]
	r.mu.RUnlock()
	if !exists {
		sip.PutMessage(m)
		return
	}
	select {
	case ch <- m:
	default:
		sip.PutMessage(m)
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

// normalizeSIPScenarioLineIndent strips a common leading space/tab prefix from
// every line in the message (both SIP headers and body), matching typical
// SIPp/XML CDATA indentation so headers and SDP body are not sent with leading
// spaces.
func normalizeSIPScenarioLineIndent(msg string) string {
	if msg == "" {
		return msg
	}
	const sep = "\r\n\r\n"
	idx := strings.Index(msg, sep)
	var head, body string
	foundSep := false
	if idx >= 0 {
		foundSep = true
		head = msg[:idx]
		body = msg[idx+len(sep):]
	} else {
		head = msg
	}
	head, minStripped := dedentSIPHeaderLines(head)
	if foundSep {
		if minStripped > 0 {
			body = dedentLines(body, minStripped)
		}
		return head + sep + body
	}
	return head
}

// dedentSIPHeaderLines removes the common leading whitespace indent from SIP
// header lines and returns the dedented string together with the number of
// characters that were stripped per line.
func dedentSIPHeaderLines(head string) (string, int) {
	if head == "" {
		return head, 0
	}
	lines := strings.Split(head, "\r\n")
	// If the start line (request or status) is flush-left but following header
	// lines are XML-indented, global min would be 0 and nothing would strip —
	// handle that by skipping the first line when computing the minimum.
	skipFirstForMin := false
	if len(lines) > 0 {
		first := lines[0]
		if strings.TrimSpace(first) != "" && countLeadingSpaceTab(first) == 0 {
			skipFirstForMin = true
		}
	}
	min := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := countLeadingSpaceTab(line)
		if skipFirstForMin && i == 0 {
			continue
		}
		if min < 0 || n < min {
			min = n
		}
	}
	if min <= 0 {
		return head, 0
	}
	for i := range lines {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := countLeadingSpaceTab(line)
		if n >= min {
			lines[i] = line[min:]
		}
	}
	return strings.Join(lines, "\r\n"), min
}

// dedentLines removes up to n leading space/tab characters from each non-blank
// line in s (which uses \r\n line endings).
func dedentLines(s string, n int) string {
	if s == "" || n <= 0 {
		return s
	}
	lines := strings.Split(s, "\r\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		leading := countLeadingSpaceTab(line)
		strip := n
		if leading < strip {
			strip = leading
		}
		lines[i] = line[strip:]
	}
	return strings.Join(lines, "\r\n")
}

func countLeadingSpaceTab(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			n++
		} else {
			break
		}
	}
	return n
}

func ensureMessageTerminator(msg string) string {
	if strings.HasSuffix(msg, "\r\n\r\n") {
		return msg
	}
	hasBody := strings.Contains(msg, "\r\n\r\n")
	if strings.HasSuffix(msg, "\r\n") {
		if hasBody {
			// Body already ends with \r\n — correct.
			return msg
		}
		// No body: headers end with a single \r\n, need one more to close them.
		return msg + "\r\n"
	}
	if hasBody {
		// Body is missing its trailing \r\n.
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

type actionResult struct {
	jumpIndex           int
	hasJump             bool
	setDestinationHost  string
	setDestinationPort  int
	setDestinationProto string
	hasSetDestination   bool
}

func (e *Engine) applyActions(ctx context.Context, callNumber int, actions []scenario.Action, renderCtx templ.Context, vars *varStore, mediaSession *media.Session) (actionResult, error) {
	renderCtx.Variables = vars.Snapshot()
	result := actionResult{}
	for _, action := range actions {
		switch action.Type {
		case scenario.ActionAssignStr:
			if len(action.AssignTo) == 0 {
				continue
			}
			value, err := templ.RenderMessageStrict(action.Value, renderCtx)
			if err != nil {
				return actionResult{}, err
			}
			for _, name := range action.AssignTo {
				vars.Set(name, value)
			}
		case scenario.ActionAssign:
			value, err := renderNumericActionValue(action, renderCtx, vars)
			if err != nil {
				return actionResult{}, err
			}
			assignActionValue(action.AssignTo, value, vars)
		case scenario.ActionToDouble:
			value, err := parseActionFloat(vars.Get(action.Variable))
			if err != nil {
				return actionResult{}, err
			}
			assignActionValue(action.AssignTo, formatActionFloat(value), vars)
		case scenario.ActionAdd, scenario.ActionSubtract, scenario.ActionMultiply, scenario.ActionDivide:
			current, err := parseActionFloat(vars.Get(firstAssignTarget(action.AssignTo)))
			if err != nil {
				return actionResult{}, err
			}
			operand, err := actionNumericOperand(action, renderCtx, vars)
			if err != nil {
				return actionResult{}, err
			}
			value, err := applyArithmeticAction(action.Type, current, operand)
			if err != nil {
				return actionResult{}, err
			}
			assignActionValue(action.AssignTo, formatActionFloat(value), vars)
		case scenario.ActionLog:
			if e.cfg.TraceLogs {
				message, err := templ.RenderMessageStrict(action.Message, renderCtx)
				if err != nil {
					return actionResult{}, err
				}
				e.traceActionLog(message)
			}
		case scenario.ActionWarning:
			if e.cfg.TraceErrors {
				message, err := templ.RenderMessageStrict(action.Message, renderCtx)
				if err != nil {
					return actionResult{}, err
				}
				e.traceError("warning", callNumber, message)
			}
		case scenario.ActionLookup:
			if len(action.AssignTo) == 0 {
				continue
			}
			if strings.TrimSpace(action.File) == "" {
				return actionResult{}, errors.New("lookup action requires file")
			}
			fileName, err := templ.RenderMessageStrict(action.File, renderCtx)
			if err != nil {
				return actionResult{}, err
			}
			key, err := templ.RenderMessageStrict(action.Key, renderCtx)
			if err != nil {
				return actionResult{}, err
			}
			line, found, err := templ.LookupCSVLine(renderCtx.BasePath, fileName, key)
			if err != nil {
				return actionResult{}, err
			}
			value := "0"
			if found {
				value = strconv.Itoa(line)
			}
			for _, name := range action.AssignTo {
				vars.Set(name, value)
			}
		case scenario.ActionSample:
			value, err := e.applySampleAction(action, renderCtx)
			if err != nil {
				return actionResult{}, err
			}
			assignActionValue(action.AssignTo, value, vars)
		case scenario.ActionInsert, scenario.ActionReplace:
			if err := applyCSVMutationAction(action, renderCtx); err != nil {
				return actionResult{}, err
			}
		case scenario.ActionStrCmp:
			if len(action.AssignTo) == 0 {
				continue
			}
			left := vars.Get(action.Variable)
			right, err := resolveActionOperand(action, renderCtx, vars)
			if err != nil {
				return actionResult{}, err
			}
			result := strings.Compare(left, right)
			value := strconv.Itoa(result)
			for _, name := range action.AssignTo {
				vars.Set(name, value)
			}
		case scenario.ActionTest:
			if len(action.AssignTo) == 0 {
				continue
			}
			left := vars.Get(action.Variable)
			right, err := resolveActionOperand(action, renderCtx, vars)
			if err != nil {
				return actionResult{}, err
			}
			result := compareValues(left, right, action.Compare)
			value := "0"
			if result {
				value = "1"
			}
			for _, name := range action.AssignTo {
				vars.Set(name, value)
			}
		case scenario.ActionJump:
			jumpIndex, err := resolveJumpTarget(action, renderCtx, vars)
			if err != nil {
				return actionResult{}, err
			}
			result.jumpIndex = jumpIndex
			result.hasJump = true
		case scenario.ActionGetTimeOfDay:
			seconds, micros := currentEpochParts()
			if len(action.AssignTo) > 0 {
				vars.Set(action.AssignTo[0], strconv.FormatInt(seconds, 10))
			}
			if len(action.AssignTo) > 1 {
				vars.Set(action.AssignTo[1], strconv.FormatInt(micros, 10))
			}
		case scenario.ActionURLEncode:
			vars.Set(action.Variable, url.QueryEscape(vars.Get(action.Variable)))
		case scenario.ActionURLDecode:
			decoded, err := url.QueryUnescape(vars.Get(action.Variable))
			if err != nil {
				return actionResult{}, err
			}
			vars.Set(action.Variable, decoded)
		case scenario.ActionVerifyAuth:
			if len(action.AssignTo) == 0 {
				continue
			}
			username, err := templ.RenderMessageStrict(action.Username, renderCtx)
			if err != nil {
				return actionResult{}, err
			}
			password, err := templ.RenderMessageStrict(action.Password, renderCtx)
			if err != nil {
				return actionResult{}, err
			}
			valid, err := verifyAuthHeader(renderCtx.LastMessage, username, password)
			if err != nil {
				return actionResult{}, err
			}
			value := "0"
			if valid {
				value = "1"
			}
			assignActionValue(action.AssignTo, value, vars)
		case scenario.ActionSetDest:
			host, port, protocol, err := resolveSetDestAction(action, renderCtx)
			if err != nil {
				return actionResult{}, err
			}
			result.setDestinationHost = host
			result.setDestinationPort = port
			result.setDestinationProto = protocol
			result.hasSetDestination = true
		case scenario.ActionEReg:
			if err := applyERegAction(action, renderCtx, vars); err != nil {
				return actionResult{}, err
			}
		case scenario.ActionExec:
			if err := e.applyExecAction(ctx, action, renderCtx, vars, mediaSession); err != nil {
				return actionResult{}, err
			}
		}
		renderCtx.Variables = vars.Snapshot()
	}
	return result, nil
}

func getCachedRegexp(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexpCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexpCache.Store(pattern, re)
	return re, nil
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

	re, err := getCachedRegexp(action.Regexp)
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

func resolveSetDestAction(action scenario.Action, renderCtx templ.Context) (string, int, string, error) {
	if strings.TrimSpace(action.Host) == "" {
		return "", 0, "", errors.New("setdest action requires host")
	}
	if strings.TrimSpace(action.Port) == "" {
		return "", 0, "", errors.New("setdest action requires port")
	}
	host, err := templ.RenderMessageStrict(action.Host, renderCtx)
	if err != nil {
		return "", 0, "", err
	}
	portRaw, err := templ.RenderMessageStrict(action.Port, renderCtx)
	if err != nil {
		return "", 0, "", err
	}
	port, err := strconv.Atoi(strings.TrimSpace(portRaw))
	if err != nil {
		return "", 0, "", fmt.Errorf("setdest action invalid port %q", portRaw)
	}
	if port <= 0 || port > 65535 {
		return "", 0, "", fmt.Errorf("setdest action port %d is out of range", port)
	}
	protocol := ""
	if strings.TrimSpace(action.Protocol) != "" {
		protocol, err = templ.RenderMessageStrict(action.Protocol, renderCtx)
		if err != nil {
			return "", 0, "", err
		}
	}
	return strings.TrimSpace(host), port, strings.TrimSpace(protocol), nil
}

type sampleSpec struct {
	min  int64
	max  int64
	step int64
	seed *int64
}

func (e *Engine) applySampleAction(action scenario.Action, renderCtx templ.Context) (string, error) {
	if len(action.AssignTo) == 0 {
		return "", nil
	}
	spec, err := parseSampleSpec(action.Value, renderCtx)
	if err != nil {
		return "", err
	}
	values, err := buildSampleValues(spec)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", errors.New("sample action produced no values")
	}
	idx, err := e.sampleIndex(len(values), spec.seed)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(values[idx], 10), nil
}

func (e *Engine) sampleIndex(size int, seed *int64) (int, error) {
	if size <= 0 {
		return 0, errors.New("sample action requires non-empty value set")
	}
	if seed != nil {
		rnd := mrand.New(mrand.NewSource(*seed))
		return rnd.Intn(size), nil
	}
	e.randomMu.Lock()
	defer e.randomMu.Unlock()
	return e.random.Intn(size), nil
}

func parseSampleSpec(raw string, renderCtx templ.Context) (sampleSpec, error) {
	rendered, err := templ.RenderMessageStrict(raw, renderCtx)
	if err != nil {
		return sampleSpec{}, err
	}
	trimmed := strings.TrimSpace(rendered)
	if trimmed == "" {
		return sampleSpec{}, errors.New("sample action requires value spec")
	}
	spec := sampleSpec{
		step: 1,
	}
	var hasMin, hasMax bool
	for _, field := range strings.Fields(trimmed) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return sampleSpec{}, fmt.Errorf("sample action invalid token %q", field)
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "min":
			v, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return sampleSpec{}, fmt.Errorf("sample action invalid min %q", val)
			}
			spec.min = v
			hasMin = true
		case "max":
			v, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return sampleSpec{}, fmt.Errorf("sample action invalid max %q", val)
			}
			spec.max = v
			hasMax = true
		case "step":
			v, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return sampleSpec{}, fmt.Errorf("sample action invalid step %q", val)
			}
			spec.step = v
		case "seed":
			v, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return sampleSpec{}, fmt.Errorf("sample action invalid seed %q", val)
			}
			spec.seed = &v
		default:
			return sampleSpec{}, fmt.Errorf("sample action unsupported key %q", key)
		}
	}
	if !hasMin || !hasMax {
		return sampleSpec{}, errors.New("sample action requires min and max")
	}
	if spec.step <= 0 {
		return sampleSpec{}, errors.New("sample action step must be > 0")
	}
	if spec.max < spec.min {
		return sampleSpec{}, errors.New("sample action max must be >= min")
	}
	return spec, nil
}

func buildSampleValues(spec sampleSpec) ([]int64, error) {
	const maxValues = 1_000_000
	values := make([]int64, 0, 16)
	for v := spec.min; v <= spec.max; v += spec.step {
		values = append(values, v)
		if len(values) > maxValues {
			return nil, errors.New("sample action value set is too large")
		}
		if spec.step > 0 && v > spec.max-spec.step {
			break
		}
	}
	return values, nil
}

type csvMutationSpec struct {
	line     int
	field    int
	text     string
	position string
}

func applyCSVMutationAction(action scenario.Action, renderCtx templ.Context) error {
	if strings.TrimSpace(action.File) == "" {
		return fmt.Errorf("%s action requires file", action.Type)
	}
	fileName, err := templ.RenderMessageStrict(action.File, renderCtx)
	if err != nil {
		return err
	}
	spec, err := parseCSVMutationSpec(action.Value, renderCtx)
	if err != nil {
		return err
	}
	mode := "replace"
	if action.Type == scenario.ActionInsert {
		mode = "insert"
	}
	return templ.ApplyCSVMutation(
		renderCtx.BasePath,
		fileName,
		spec.line,
		spec.field,
		mode,
		spec.text,
		spec.position,
		renderCtx.CSVFieldOverrides,
	)
}

func parseCSVMutationSpec(raw string, renderCtx templ.Context) (csvMutationSpec, error) {
	rendered, err := templ.RenderMessageStrict(raw, renderCtx)
	if err != nil {
		return csvMutationSpec{}, err
	}
	trimmed := strings.TrimSpace(rendered)
	if trimmed == "" {
		return csvMutationSpec{}, errors.New("csv mutation action requires value spec")
	}
	params := make(map[string]string)
	for _, part := range strings.Fields(trimmed) {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return csvMutationSpec{}, fmt.Errorf("csv mutation action invalid token %q", part)
		}
		params[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(val)
	}
	rawLine, ok := params["line"]
	if !ok {
		return csvMutationSpec{}, errors.New("csv mutation action requires line")
	}
	line, err := strconv.Atoi(rawLine)
	if err != nil || line <= 0 {
		return csvMutationSpec{}, fmt.Errorf("csv mutation action invalid line %q", rawLine)
	}
	rawField, ok := params["field"]
	if !ok {
		return csvMutationSpec{}, errors.New("csv mutation action requires field")
	}
	field, err := strconv.Atoi(rawField)
	if err != nil || field < 0 {
		return csvMutationSpec{}, fmt.Errorf("csv mutation action invalid field %q", rawField)
	}
	text, ok := params["text"]
	if !ok {
		return csvMutationSpec{}, errors.New("csv mutation action requires text")
	}
	return csvMutationSpec{
		line:     line,
		field:    field,
		text:     text,
		position: params["position"],
	}, nil
}

func normalizeTransportForSetDest(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "u1", "un", "ui", "udp":
		return "udp"
	case "t1", "tn", "tcp":
		return "tcp"
	case "l1", "ln", "tls":
		return "tls"
	default:
		return strings.ToLower(strings.TrimSpace(transport))
	}
}

func compareValues(left, right, compare string) bool {
	if leftNum, rightNum, ok := parseComparableNumbers(left, right); ok {
		switch strings.ToLower(strings.TrimSpace(compare)) {
		case "", "equal", "eq":
			return leftNum == rightNum
		case "not_equal", "ne":
			return leftNum != rightNum
		case "greater_than", "gt":
			return leftNum > rightNum
		case "less_than", "lt":
			return leftNum < rightNum
		case "greater_than_equal", "greater_than_or_equal", "ge", "gte":
			return leftNum >= rightNum
		case "less_than_equal", "less_than_or_equal", "le", "lte":
			return leftNum <= rightNum
		default:
			return leftNum == rightNum
		}
	}

	order := strings.Compare(left, right)
	switch strings.ToLower(strings.TrimSpace(compare)) {
	case "", "equal", "eq":
		return order == 0
	case "not_equal", "ne":
		return order != 0
	case "greater_than", "gt":
		return order > 0
	case "less_than", "lt":
		return order < 0
	case "greater_than_equal", "greater_than_or_equal", "ge", "gte":
		return order >= 0
	case "less_than_equal", "less_than_or_equal", "le", "lte":
		return order <= 0
	default:
		return order == 0
	}
}

func resolveActionOperand(action scenario.Action, renderCtx templ.Context, vars *varStore) (string, error) {
	if strings.TrimSpace(action.Variable2) != "" {
		return vars.Get(action.Variable2), nil
	}
	value, err := templ.RenderMessageStrict(action.Value, renderCtx)
	if err != nil {
		return "", err
	}
	return value, nil
}

func parseComparableNumbers(left, right string) (float64, float64, bool) {
	leftNum, err := strconv.ParseFloat(strings.TrimSpace(left), 64)
	if err != nil {
		return 0, 0, false
	}
	rightNum, err := strconv.ParseFloat(strings.TrimSpace(right), 64)
	if err != nil {
		return 0, 0, false
	}
	return leftNum, rightNum, true
}

func firstAssignTarget(assignTo []string) string {
	if len(assignTo) == 0 {
		return ""
	}
	return assignTo[0]
}

func assignActionValue(assignTo []string, value string, vars *varStore) {
	for _, name := range assignTo {
		vars.Set(name, value)
	}
}

func renderNumericActionValue(action scenario.Action, renderCtx templ.Context, vars *varStore) (string, error) {
	if strings.TrimSpace(action.Variable) != "" {
		return vars.Get(action.Variable), nil
	}
	return templ.RenderMessageStrict(action.Value, renderCtx)
}

func actionNumericOperand(action scenario.Action, renderCtx templ.Context, vars *varStore) (float64, error) {
	operandValue, err := renderNumericActionValue(action, renderCtx, vars)
	if err != nil {
		return 0, err
	}
	return parseActionFloat(operandValue)
}

func parseActionFloat(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value %q", value)
	}
	return parsed, nil
}

func formatActionFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func applyArithmeticAction(actionType scenario.ActionType, left, right float64) (float64, error) {
	switch actionType {
	case scenario.ActionAdd:
		return left + right, nil
	case scenario.ActionSubtract:
		return left - right, nil
	case scenario.ActionMultiply:
		return left * right, nil
	case scenario.ActionDivide:
		if right == 0 {
			return 0, errors.New("divide by zero")
		}
		return left / right, nil
	default:
		return 0, fmt.Errorf("unsupported arithmetic action %q", actionType)
	}
}

func resolveJumpTarget(action scenario.Action, renderCtx templ.Context, vars *varStore) (int, error) {
	target := strings.TrimSpace(action.Value)
	if target == "" && strings.TrimSpace(action.Variable) != "" {
		target = strings.TrimSpace(vars.Get(action.Variable))
	}
	if target == "" {
		return 0, errors.New("jump action requires value or variable")
	}
	value, err := strconv.Atoi(target)
	if err != nil {
		return 0, fmt.Errorf("invalid jump target %q", target)
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid jump target %q", target)
	}
	_ = renderCtx
	return value, nil
}

func currentEpochParts() (int64, int64) {
	now := time.Now()
	return now.Unix(), int64(now.Nanosecond() / 1000)
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
	ptr := parseHeadersLinesPool.Get().(*[]string)
	templ.SplitLinesTo(ptr, msg)
	for _, line := range *ptr {
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
	parseHeadersLinesPool.Put(ptr)
	return headers
}

func copyHeaders(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func buildRouteHeaders(headers map[string][]string) string {
	values, ok := lookupHeaderCI(headers, "Record-Route")
	if !ok || len(values) == 0 {
		return ""
	}
	routeLines := make([]string, 0, len(values))
	for i := len(values) - 1; i >= 0; i-- {
		value := strings.TrimSpace(values[i])
		if value == "" {
			continue
		}
		routeLines = append(routeLines, "Route: "+value)
	}
	return strings.Join(routeLines, "\r\n")
}

func commandCallID(raw, fallback string) string {
	headers := parseCommandHeaders(raw)
	values, ok := lookupHeaderCI(headers, "Call-ID")
	if !ok || len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fallback
	}
	return sip.NormalizeCallID(values[0])
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
