package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	recorder        media.G711WAVRecorder
	autoRecordDir   string
	autoRecordDuplex bool
	callID          string
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
			codec := br.Codec()
			cm.wrtc.recorder.AppendReceived(payload, codec)
			cm.wrtc.mu.Unlock()
			cm.wrtc.maybeAutoRecord()
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
	if cm.wrtc != nil {
		cm.wrtc.callID = callID
		cm.wrtc.autoRecordDir = strings.TrimSpace(e.cfg.RecordWAVDir)
		cm.wrtc.autoRecordDuplex = e.cfg.RecordWAVDuplex
	}
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
		rs := cm.wrtc.bridge.RTPStats()
		sent, recv := cm.wrtc.sent, cm.wrtc.recv
		if rs.PacketsSent > sent {
			sent = rs.PacketsSent
		}
		if rs.PacketsReceived > recv {
			recv = rs.PacketsReceived
		}
		st := media.Stats{
			RTPPacketsSent:     sent,
			RTPPacketsReceived: recv,
		}
		if rs.PacketsLost > 0 {
			st.RTPRecvMaxCumulativeLost = uint32(rs.PacketsLost)
		}
		if rs.JitterSeconds > 0 {
			jitterTS := uint32(rs.JitterSeconds * 8000)
			st.RTPRecvInterarrivalJitterPeak = jitterTS
			st.RTCPMaxJitter = jitterTS
		}
		if rs.RemoteFractionOK {
			st.RTCPReceiverReports = 1
			fl := rs.FractionLost * 255
			if fl > 255 {
				fl = 255
			}
			st.RTCPMaxFractionLost = uint8(fl)
		}
		return st
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

func (cm *callMedia) startRecording(path string, duplex bool, basePath string) error {
	if cm.wrtc != nil {
		return cm.wrtc.recorder.StartRecording(path, duplex, basePath)
	}
	if cm.session == nil {
		return fmt.Errorf("rtp_record: no active media session (start RTP first)")
	}
	return cm.session.StartRecording(path, duplex, basePath)
}

func (cm *callMedia) stopRecording() error {
	if cm.wrtc != nil {
		return cm.wrtc.recorder.StopRecording()
	}
	if cm.session == nil {
		return nil
	}
	return cm.session.StopRecording()
}

func (w *webrtcCallMedia) maybeAutoRecord() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maybeAutoRecordLocked()
}

func (w *webrtcCallMedia) maybeAutoRecordLocked() {
	if w.autoRecordDir == "" || w.callID == "" || w.recorder.Recording() {
		return
	}
	base := filepath.Join(w.autoRecordDir, media.SanitizeCallIDForFilename(w.callID)+".wav")
	_ = w.recorder.StartRecording(base, w.autoRecordDuplex, "")
}

func (w *webrtcCallMedia) appendSent(payload []byte) {
	codec := w.bridge.Codec()
	w.recorder.AppendSent(payload, codec)
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
	w.maybeAutoRecordLocked()
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
	rs := w.bridge.RTPStats()
	if rs.PacketsLost > 0 {
		out["rtp_packets_lost"] = rs.PacketsLost
	}
	if rs.JitterSeconds > 0 {
		out["jitter_ms"] = rs.JitterSeconds * 1000
	}
	if rs.RemoteFractionOK {
		out["fraction_lost"] = rs.FractionLost
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
				w.appendSent(frame)
				w.mu.Unlock()
				w.maybeAutoRecord()
			}
		}
	}()
	return nil
}

func (w *webrtcCallMedia) stop() {
	_ = w.recorder.StopRecording()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.bridge != nil {
		_ = w.bridge.Close()
	}
}
