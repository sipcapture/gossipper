// Package webrtc wraps pion/webrtc/v4 in a thin SIP-friendly bridge.
//
// Status: experimental. The bridge is intentionally decoupled from the SIP
// engine so it can be developed and tested in isolation; the engine wiring
// (per-call PeerConnection bound to a MediaSession) is the next step.
//
// Design constraints:
//   - Single audio track per peer (G.711 PCMA at 8 kHz mono). Opus is plumbed
//     through the codec list but the engine produces μ-law / a-law today so
//     PCMA is what we exercise.
//   - DTLS-SRTP, ICE-lite-friendly defaults. ICE servers can be provided by
//     the caller (e.g. from a Server profile UI block).
//   - No SDP munging — pion produces a standards-compliant answer. The SIP
//     scenario must accept the answer verbatim.
//
// Typical use:
//
//	b, err := webrtc.NewBridge(webrtc.Options{ICEServers: []string{"stun:stun.l.google.com:19302"}})
//	if err != nil { return err }
//	defer b.Close()
//	answer, err := b.Answer(offerSDP)
//	... // ship answer over SIP / 200 OK
//	b.OnPCMA(func(payload []byte) { /* feed engine media session */ })
//	for _, frame := range outgoingPCMA { _ = b.WritePCMA(frame, 20*time.Millisecond) }
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
	// PrefersPCMA picks PCMA (G.711 a-law, payload 8) over PCMU when both
	// are offered. Default false → PCMU (payload 0).
	PrefersPCMA bool
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
	pc          *webrtc.PeerConnection
	outbound    *webrtc.TrackLocalStaticSample
	inboundOnce sync.Once
	inboundMu   sync.RWMutex
	inboundCB   func(payload []byte)
	codec       string // "PCMU" or "PCMA"
	closed      chan struct{}
	closeOnce   sync.Once
}

// NewBridge spins up a PeerConnection ready to answer an offer.
func NewBridge(opts Options) (*Bridge, error) {
	iceServers := []webrtc.ICEServer{}
	for _, u := range opts.ICEServers {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:       []string{u},
			Username:   opts.ICEUsername,
			Credential: opts.ICECredential,
		})
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
		pc:       pc,
		outbound: outbound,
		codec:    codec,
		closed:   make(chan struct{}),
	}

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if remote.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		go b.consumeRemoteTrack(remote)
	})

	return b, nil
}

func registerAudioCodecs(m *webrtc.MediaEngine) error {
	// Standard G.711 set; payload types follow IANA defaults that SIP scenarios
	// expect to see in the answer.
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

// Answer accepts an SDP offer and returns the corresponding answer (also
// gathered to completion so the returned SDP already contains every ICE
// candidate — trickle ICE is not used here to keep SIP integration simple).
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
	gather := webrtc.GatheringCompletePromise(b.pc)
	if err := b.pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("webrtc: set local: %w", err)
	}
	select {
	case <-gather:
	case <-time.After(5 * time.Second):
		return "", errors.New("webrtc: ICE gathering timed out")
	}
	final := b.pc.LocalDescription()
	if final == nil {
		return "", errors.New("webrtc: no local description")
	}
	return final.SDP, nil
}

// CreateOffer flips the role around: the bridge produces an offer instead of
// answering one. Useful when the SIP UAC drives the call.
func (b *Bridge) CreateOffer(ctx context.Context) (string, error) {
	offer, err := b.pc.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	gather := webrtc.GatheringCompletePromise(b.pc)
	if err := b.pc.SetLocalDescription(offer); err != nil {
		return "", err
	}
	select {
	case <-gather:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(5 * time.Second):
		return "", errors.New("webrtc: ICE gathering timed out")
	}
	return b.pc.LocalDescription().SDP, nil
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
		_ = pkt // shut go vet up about pkt being a *rtp.Packet
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

// avoid unused import warning when the pkg is built without tests
var _ = rtp.Packet{}
