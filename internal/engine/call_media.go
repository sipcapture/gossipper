package engine

import (
	"context"
	"sync"
	"time"

	"github.com/sipcapture/gossipper/internal/media"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/webrtc"
)

// callMedia wraps either a classic UDP media.Session or a per-call WebRTC bridge.
type callMedia struct {
	session *media.Session
	wrtc    *webrtcCallMedia
}

type webrtcCallMedia struct {
	bridge          *webrtc.Bridge
	mu              sync.Mutex
	sent            uint32
	recv            uint32
	cancel          context.CancelFunc
	localOffer      string
	answerAccepted  bool
}

func newCallMedia(e *Engine, scen scenario.Scenario, callID string) (*callMedia, error) {
	cm := &callMedia{}
	useWebRTC := scen.WebRTC || e.cfg.WebRTCMedia
	if useWebRTC {
		br, err := e.NewWebRTCBridge()
		if err != nil {
			return nil, err
		}
		cm.wrtc = &webrtcCallMedia{bridge: br}
		br.OnPCMA(func(payload []byte) {
			cm.wrtc.mu.Lock()
			cm.wrtc.recv++
			cm.wrtc.mu.Unlock()
		})
		return cm, nil
	}
	s := media.NewSession()
	s.SetCallID(callID)
	cm.session = s
	return cm, nil
}

func (cm *callMedia) usesWebRTC() bool {
	return cm != nil && cm.wrtc != nil
}

func (cm *callMedia) sessionOrNil() *media.Session {
	if cm == nil {
		return nil
	}
	return cm.session
}

func (cm *callMedia) configure(e *Engine, callID string) {
	if cm.session == nil {
		return
	}
	cm.session.SetPCAPLinkLayer(e.cfg.PCAPLinkLayer)
	cm.session.SetTURN(e.cfg.TURNServer, e.cfg.TURNUser, e.cfg.TURNPass, e.cfg.TURNRealm)
	if e.hep != nil && !e.cfg.MediaScale {
		cm.session.SetHEPObserver(e.hep)
	}
	cm.session.SetCallID(callID)
	cm.session.SetAutoRecord(e.cfg.RecordWAVDir, e.cfg.RecordWAVDuplex)
	cm.session.EnsureLocalIceCredentials()
}

func (cm *callMedia) iceUfrag() string {
	if cm.session == nil {
		return ""
	}
	return cm.session.ICELocalUfrag()
}

func (cm *callMedia) icePwd() string {
	if cm.session == nil {
		return ""
	}
	return cm.session.ICELocalPwd()
}

func (cm *callMedia) snapshot() media.Stats {
	if cm.wrtc != nil {
		cm.wrtc.mu.Lock()
		defer cm.wrtc.mu.Unlock()
		return media.Stats{
			RTPPacketsSent:     cm.wrtc.sent,
			RTPPacketsReceived: cm.wrtc.recv,
		}
	}
	if cm.session != nil {
		return cm.session.Snapshot()
	}
	return media.Stats{}
}

func (cm *callMedia) stop() {
	if cm.wrtc != nil {
		cm.wrtc.stop()
	}
	if cm.session != nil {
		cm.session.Stop()
	}
}

func (w *webrtcCallMedia) createOffer(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.localOffer != "" {
		return w.localOffer, nil
	}
	offer, err := w.bridge.CreateOffer(ctx)
	if err != nil {
		return "", err
	}
	w.localOffer = offer
	return offer, nil
}

func (w *webrtcCallMedia) needsAcceptAnswer() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.localOffer != "" && !w.answerAccepted
}

func (w *webrtcCallMedia) acceptAnswer(answerSDP string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.answerAccepted {
		return nil
	}
	if err := w.bridge.AcceptAnswer(answerSDP); err != nil {
		return err
	}
	w.answerAccepted = true
	return nil
}

func (w *webrtcCallMedia) diagnostics() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]any{
		"codec":            w.bridge.Codec(),
		"ice_state":        w.bridge.ConnectionState().String(),
		"rtp_packets_sent": w.sent,
		"rtp_packets_recv": w.recv,
		"offer_created":    w.localOffer != "",
		"answer_accepted":  w.answerAccepted,
	}
	return out
}

func (w *webrtcCallMedia) answer(offerSDP string) (string, error) {
	return w.bridge.Answer(offerSDP)
}

func (w *webrtcCallMedia) startSynthetic(ctx context.Context, cfg media.StreamConfig) error {
	if w.cancel != nil {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	dur := cfg.PacketDuration
	if dur <= 0 {
		dur = 20 * time.Millisecond
	}
	frame := make([]byte, 160)
	go func() {
		ticker := time.NewTicker(dur)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				var err error
				if w.bridge.Codec() == "PCMA" {
					err = w.bridge.WritePCMA(frame, dur)
				} else {
					err = w.bridge.WritePCMU(frame, dur)
				}
				if err != nil {
					return
				}
				w.mu.Lock()
				w.sent++
				w.mu.Unlock()
			}
		}
	}()
	return nil
}

func (w *webrtcCallMedia) stop() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.bridge != nil {
		_ = w.bridge.Close()
	}
}
