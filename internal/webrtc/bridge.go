package webrtc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// Options configure a Bridge. Zero values are sensible (no STUN, default UDP
// MUX port range).
type Options struct {
	// ICEServers are STUN/TURN URLs (e.g. "stun:stun.l.google.com:19302").
	ICEServers []string
	// ICEUsername / ICECredential are forwarded to every server that needs
	// auth. Most STUN servers ignore them.
	ICEUsername   string
	ICECredential string
	// ICEAuthSecret enables coturn REST-style ephemeral credentials ( --use-auth-secret ).
	// When set, a fresh username/password is minted per Bridge for TURN URLs.
	ICEAuthSecret string
	// ICEAuthTTL bounds REST credential lifetime (default 24h).
	ICEAuthTTL time.Duration
	// PrefersPCMA picks PCMA (G.711 a-law, payload 8) over PCMU when both
	// are offered. Default false → PCMU (payload 0).
	PrefersPCMA bool
	// ICEGatherTimeout bounds how long Answer/CreateOffer wait for ICE
	// gathering when ICETrickleFullGather is true. Zero defaults to 5s.
	ICEGatherTimeout time.Duration
	// ICETrickleFullGather waits for full ICE gathering before returning SDP
	// (legacy behaviour). Default false → trickle: return after first local
	// candidates or a short grace window.
	ICETrickleFullGather bool
	// Logger is a *log/slog.Logger-like hook; nil means silent.
	Logger Logger
}

// Logger is a tiny interface so we don't pull in log/slog directly.
type Logger interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
}

// Bridge owns a single pion PeerConnection plus its outbound audio track.
type Bridge struct {
	pc               *webrtc.PeerConnection
	outbound         *webrtc.TrackLocalStaticSample
	inboundOnce      sync.Once
	inboundMu        sync.RWMutex
	inboundCB        func(payload []byte)
	codec            string // "PCMU" or "PCMA"
	opts             Options
	iceGatherTimeout time.Duration
	trickleFullGather bool
	iceServers       []string
	iceAuthMode      string
	closed           chan struct{}
	closeOnce        sync.Once
	stateMu          sync.RWMutex
	gatheringState   string
	connectionState  string
	selectedLocal    string
	selectedRemote   string
	localCandidateCount int
	localCandidates     []*webrtc.ICECandidate
	remoteTrickleAdded  int
	turnRefreshCount    int
	turnCredExpires     int64
}

// NewBridge spins up a PeerConnection ready to answer an offer.
func NewBridge(opts Options) (*Bridge, error) {
	iceServers, err := BuildICEServers(opts)
	if err != nil {
		return nil, err
	}
	cfg := webrtc.Configuration{ICEServers: iceServers}

	m := &webrtc.MediaEngine{}
	if err := registerAudioCodecs(m); err != nil {
		return nil, fmt.Errorf("webrtc: register codecs: %w", err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return nil, fmt.Errorf("webrtc: new peer connection: %w", err)
	}

	codec := "PCMU"
	mime := webrtc.MimeTypePCMU
	if opts.PrefersPCMA {
		codec = "PCMA"
		mime = webrtc.MimeTypePCMA
	}
	outbound, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: mime, ClockRate: 8000, Channels: 1},
		"audio", "gossipper",
	)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("webrtc: new track: %w", err)
	}
	if _, err := pc.AddTrack(outbound); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("webrtc: add track: %w", err)
	}

	b := &Bridge{
		pc:                pc,
		outbound:          outbound,
		codec:             codec,
		opts:              opts,
		iceGatherTimeout:  gatherTimeout(opts),
		trickleFullGather: opts.ICETrickleFullGather,
		iceServers:        append([]string(nil), opts.ICEServers...),
		iceAuthMode:       authMode(opts),
		closed:            make(chan struct{}),
		gatheringState:    pc.ICEGatheringState().String(),
		connectionState:   pc.ICEConnectionState().String(),
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		cCopy := c
		b.stateMu.Lock()
		b.localCandidateCount++
		b.localCandidates = append(b.localCandidates, cCopy)
		b.stateMu.Unlock()
	})
	pc.OnICEGatheringStateChange(func(s webrtc.ICEGatheringState) {
		b.stateMu.Lock()
		b.gatheringState = s.String()
		b.stateMu.Unlock()
	})
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		b.stateMu.Lock()
		b.connectionState = s.String()
		b.stateMu.Unlock()
		b.refreshSelectedCandidatePair()
	})

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if remote.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		go b.consumeRemoteTrack(remote)
	})

	b.startTURNRefreshLoop()
	return b, nil
}

func registerAudioCodecs(m *webrtc.MediaEngine) error {
	codecs := []webrtc.RTPCodecParameters{
		{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000, Channels: 1},
			PayloadType:        0,
		},
		{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMA, ClockRate: 8000, Channels: 1},
			PayloadType:        8,
		},
	}
	for _, c := range codecs {
		if err := m.RegisterCodec(c, webrtc.RTPCodecTypeAudio); err != nil {
			return err
		}
	}
	return nil
}

func gatherTimeout(opts Options) time.Duration {
	if opts.ICEGatherTimeout > 0 {
		return opts.ICEGatherTimeout
	}
	return 5 * time.Second
}

// Answer accepts an SDP offer and returns the corresponding answer.
func (b *Bridge) Answer(offerSDP string) (string, error) {
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		return "", fmt.Errorf("webrtc: set remote: %w", err)
	}
	answer, err := b.pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("webrtc: create answer: %w", err)
	}
	return b.finishLocalSDP(context.Background(), answer)
}

// CreateOffer flips the role around: the bridge produces an offer instead of
// answering one. Useful when the SIP UAC drives the call.
func (b *Bridge) CreateOffer(ctx context.Context) (string, error) {
	offer, err := b.pc.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	return b.finishLocalSDP(ctx, offer)
}

func (b *Bridge) finishLocalSDP(ctx context.Context, desc webrtc.SessionDescription) (string, error) {
	if err := b.pc.SetLocalDescription(desc); err != nil {
		return "", fmt.Errorf("webrtc: set local: %w", err)
	}
	if b.trickleFullGather {
		gather := webrtc.GatheringCompletePromise(b.pc)
		select {
		case <-gather:
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(b.iceGatherTimeout):
			return "", errors.New("webrtc: ICE gathering timed out")
		}
	} else {
		deadline := time.Now().Add(trickleFirstCandidateWait)
		for {
			b.stateMu.RLock()
			n := b.localCandidateCount
			gs := b.gatheringState
			b.stateMu.RUnlock()
			if n > 0 || gs == "complete" {
				break
			}
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	final := b.pc.LocalDescription()
	if final == nil {
		return "", errors.New("webrtc: no local description")
	}
	b.stateMu.RLock()
	pending := append([]*webrtc.ICECandidate(nil), b.localCandidates...)
	b.stateMu.RUnlock()
	return mergeCandidatesIntoSDP(final.SDP, pending), nil
}

// AcceptAnswer completes the offer-side handshake.
func (b *Bridge) AcceptAnswer(answerSDP string) error {
	return b.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	})
}

// WritePCMA pushes a raw G.711 PCMA payload (no RTP header) as a media sample
// of the given duration. Use 20 ms for typical SIP timing.
func (b *Bridge) WritePCMA(payload []byte, dur time.Duration) error {
	return b.outbound.WriteSample(media.Sample{Data: payload, Duration: dur})
}

// WritePCMU is the μ-law variant; only meaningful when the bridge was
// constructed without PrefersPCMA.
func (b *Bridge) WritePCMU(payload []byte, dur time.Duration) error {
	return b.outbound.WriteSample(media.Sample{Data: payload, Duration: dur})
}

// OnPCMA sets the callback invoked for each inbound RTP audio payload (codec
// payload bytes, no RTP header). Calling more than once replaces the previous
// callback. Safe to call before Answer().
func (b *Bridge) OnPCMA(cb func(payload []byte)) {
	b.inboundMu.Lock()
	b.inboundCB = cb
	b.inboundMu.Unlock()
}

func (b *Bridge) consumeRemoteTrack(remote *webrtc.TrackRemote) {
	for {
		select {
		case <-b.closed:
			return
		default:
		}
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		b.inboundMu.RLock()
		cb := b.inboundCB
		b.inboundMu.RUnlock()
		if cb != nil && len(pkt.Payload) > 0 {
			cb(pkt.Payload)
		}
		_ = pkt
	}
}

// Close tears down the PeerConnection.
func (b *Bridge) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return b.pc.Close()
}

// Codec reports the outbound codec name ("PCMA" or "PCMU").
func (b *Bridge) Codec() string { return b.codec }

// ConnectionState exposes the underlying ICE connection state for diagnostics.
func (b *Bridge) ConnectionState() webrtc.ICEConnectionState { return b.pc.ICEConnectionState() }

// ICEDiagnostics returns ICE/TURN runtime hints for logging and call records.
func (b *Bridge) ICEDiagnostics() map[string]any {
	b.refreshSelectedCandidatePair()
	b.stateMu.RLock()
	defer b.stateMu.RUnlock()
	out := map[string]any{
		"ice_gathering":       b.gatheringState,
		"ice_connection":      b.connectionState,
		"ice_servers":         len(b.iceServers),
		"turn_auth":           b.iceAuthMode,
		"ice_trickle":         !b.trickleFullGather,
		"local_candidates":    b.localCandidateCount,
		"remote_trickle_added": b.remoteTrickleAdded,
	}
	if b.turnRefreshCount > 0 {
		out["turn_refresh_count"] = b.turnRefreshCount
	}
	if b.turnCredExpires > 0 {
		out["turn_cred_expires"] = b.turnCredExpires
	}
	if b.selectedLocal != "" {
		out["selected_local"] = b.selectedLocal
	}
	if b.selectedRemote != "" {
		out["selected_remote"] = b.selectedRemote
	}
	return out
}

func (b *Bridge) refreshSelectedCandidatePair() {
	if b == nil || b.pc == nil {
		return
	}
	for _, s := range b.pc.GetStats() {
		pair, ok := s.(webrtc.ICECandidatePairStats)
		if !ok || !pair.Nominated || pair.State != webrtc.StatsICECandidatePairStateSucceeded {
			continue
		}
		local, remote := pair.LocalCandidateID, pair.RemoteCandidateID
		b.stateMu.Lock()
		b.selectedLocal, b.selectedRemote = local, remote
		b.stateMu.Unlock()
		return
	}
}

var _ = rtp.Packet{}
