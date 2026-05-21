package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sipcapture/gossipper/internal/media"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/sip"
	templ "github.com/sipcapture/gossipper/internal/template"
)

func (e *Engine) applyExecAction(ctx context.Context, action scenario.Action, renderCtx templ.Context, vars *varStore, callMedia *callMedia) error {
	mediaSession := callMedia.sessionOrNil()
	switch strings.ToLower(strings.TrimSpace(action.IntCmd)) {
	case "":
	case "stop_call":
		return errStopCall
	case "stop_now", "stop_gracefully":
		return errStopNow
	default:
		return fmt.Errorf("unsupported int_cmd %q", action.IntCmd)
	}

	if action.Command != "" {
		command, err := templ.RenderMessageStrict(action.Command, renderCtx)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	if action.RTPStream != "" {
		if callMedia == nil {
			return fmt.Errorf("rtp_stream is not available in this context")
		}
		streamSpec, err := templ.RenderMessageStrict(action.RTPStream, renderCtx)
		if err != nil {
			return err
		}
		command, cfg, err := media.ParseRTPStreamSpec(streamSpec, renderCtx.BasePath)
		if err != nil {
			return err
		}
		switch command {
		case "pause":
			if callMedia.usesWebRTC() {
				return media.ErrUnsupportedOverWebRTC
			}
			if e.cfg.MediaScale {
				if se := e.scaleMedia(); se != nil {
					se.PauseCall(renderCtx.CallID)
				}
			} else {
				mediaSession.Pause()
			}
		case "resume":
			if callMedia.usesWebRTC() {
				return media.ErrUnsupportedOverWebRTC
			}
			if e.cfg.MediaScale {
				if se := e.scaleMedia(); se != nil {
					se.ResumeCall(renderCtx.CallID)
				}
			} else {
				mediaSession.Resume()
			}
		case "stop":
			if callMedia.usesWebRTC() {
				callMedia.wrtc.stop()
				break
			}
			if e.cfg.MediaScale {
				if se := e.scaleMedia(); se != nil {
					_ = se.UnregisterCall(renderCtx.CallID)
				}
			} else {
				mediaSession.Stop()
			}
			mediaSession.ClearSDESSRTP()
		case "echo":
			if callMedia.usesWebRTC() {
				return media.ErrUnsupportedOverWebRTC
			}
			mediaSession.ClearSDESSRTP()
			if e.cfg.TraceMessages {
				fmt.Fprintf(os.Stdout, "rtp_stream echo listen on %s:%d\n", renderCtx.LocalIP, renderCtx.MediaPort)
			}
			if err := mediaSession.StartEcho(ctx, renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
				return err
			}
		case "mic":
			if callMedia.usesWebRTC() {
				return media.ErrUnsupportedOverWebRTC
			}
			last := mustParseLastMessage(renderCtx)
			if err := configureMediaSRTPForRTPStream(e, mediaSession, last, "mic"); err != nil {
				return err
			}
			endpoint, err := media.ParseAudioEndpoint(last, renderCtx.RemoteIP)
			if err != nil {
				return err
			}
			if e.cfg.TraceMessages {
				fmt.Fprintf(os.Stdout, "rtp_stream mic -> %s:%d\n", endpoint.IP, endpoint.Port)
			}
			if err := mediaSession.StartMicrophone(ctx, endpoint, renderCtx.LocalIP, renderCtx.MediaPort, cfg.MicInput); err != nil {
				return err
			}
		case "start":
			if callMedia.usesWebRTC() {
				if !cfg.Synthetic {
					return media.ErrUnsupportedOverWebRTC
				}
				if e.cfg.TraceMessages {
					fmt.Fprintf(os.Stdout, "rtp_stream start (webrtc) synthetic\n")
				}
				if err := callMedia.wrtc.startSynthetic(ctx, cfg); err != nil {
					return err
				}
				break
			}
			last := mustParseLastMessage(renderCtx)
			if e.cfg.MediaScale {
				if !cfg.Synthetic {
					return fmt.Errorf("media_scale requires rtp_stream synthetic")
				}
				endpoint, err := media.ParseAudioEndpoint(last, renderCtx.RemoteIP)
				if err != nil {
					return err
				}
				se := e.scaleMedia()
				if se == nil {
					return fmt.Errorf("scale media engine not running")
				}
				if e.cfg.TraceMessages {
					fmt.Fprintf(os.Stdout, "rtp_stream start (scale) synthetic -> %s:%d\n", endpoint.IP, endpoint.Port)
				}
				if err := se.RegisterStream(ctx, renderCtx.CallID, endpoint, cfg, renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
					return err
				}
				break
			}
			if err := configureMediaSRTPForRTPStream(e, mediaSession, last, "start"); err != nil {
				return err
			}
			endpoint, err := media.ParseAudioEndpoint(last, renderCtx.RemoteIP)
			if err != nil {
				return err
			}
			if e.cfg.TraceMessages {
				fmt.Fprintf(os.Stdout, "rtp_stream start %s -> %s:%d\n", cfg.Path, endpoint.IP, endpoint.Port)
			}
			if err := mediaSession.Start(ctx, endpoint, cfg, renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
				return err
			}
		}
	}

	if action.RTPRecord != "" {
		spec, err := templ.RenderMessageStrict(action.RTPRecord, renderCtx)
		if err != nil {
			return err
		}
		cmd, path, duplex, err := media.ParseRTPRecordSpec(spec)
		if err != nil {
			return err
		}
		switch cmd {
		case "stop":
			if err := callMedia.stopRecording(); err != nil {
				return err
			}
		case "start":
			if e.cfg.TraceMessages {
				fmt.Fprintf(os.Stdout, "rtp_record start duplex=%v -> %s\n", duplex, path)
			}
			if err := callMedia.startRecording(path, duplex, renderCtx.BasePath); err != nil {
				return err
			}
		}
	}

	if action.PlayPCAPAudio != "" {
		if callMedia.usesWebRTC() {
			return media.ErrUnsupportedOverWebRTC
		}
		if mediaSession == nil {
			return fmt.Errorf("play_pcap_audio is not available in this context")
		}
		endpoint, err := media.ParseMediaEndpoint(mustParseLastMessage(renderCtx), renderCtx.RemoteIP, "audio")
		if err != nil {
			return err
		}
		path, err := templ.RenderMessageStrict(action.PlayPCAPAudio, renderCtx)
		if err != nil {
			return err
		}
		mediaSession.ClearSDESSRTP()
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "play_pcap_audio start %s -> %s:%d\n", path, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.StartPCAPReplay(ctx, endpoint, media.ResolvePath(renderCtx.BasePath, path), renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}
	if action.RTPCheck != "" {
		if callMedia.usesWebRTC() {
			return media.ErrUnsupportedOverWebRTC
		}
		if mediaSession == nil {
			return fmt.Errorf("rtpcheck is not available in this context")
		}
		spec, err := parseRTPCheckSpec(action.RTPCheck, renderCtx)
		if err != nil {
			return err
		}
		checkCtx, cancel := context.WithTimeout(ctx, spec.timeout)
		defer cancel()
		if err := mediaSession.WaitForRTPActivity(checkCtx, spec.minPackets, spec.direction); err != nil {
			return err
		}
	}
	if action.PlayPCAPVideo != "" {
		if callMedia.usesWebRTC() {
			return media.ErrUnsupportedOverWebRTC
		}
		if mediaSession == nil {
			return fmt.Errorf("play_pcap_video is not available in this context")
		}
		endpoint, err := media.ParseMediaEndpoint(mustParseLastMessage(renderCtx), renderCtx.RemoteIP, "video")
		if err != nil {
			return err
		}
		path, err := templ.RenderMessageStrict(action.PlayPCAPVideo, renderCtx)
		if err != nil {
			return err
		}
		mediaSession.ClearSDESSRTP()
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "play_pcap_video start %s -> %s:%d\n", path, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.StartPCAPReplay(ctx, endpoint, media.ResolvePath(renderCtx.BasePath, path), renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}
	if action.PlayPCAPImage != "" {
		if callMedia.usesWebRTC() {
			return media.ErrUnsupportedOverWebRTC
		}
		if mediaSession == nil {
			return fmt.Errorf("play_pcap_image is not available in this context")
		}
		endpoint, err := media.ParseMediaEndpoint(mustParseLastMessage(renderCtx), renderCtx.RemoteIP, "image")
		if err != nil {
			return err
		}
		path, err := templ.RenderMessageStrict(action.PlayPCAPImage, renderCtx)
		if err != nil {
			return err
		}
		mediaSession.ClearSDESSRTP()
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "play_pcap_image start %s -> %s:%d\n", path, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.StartPCAPReplay(ctx, endpoint, media.ResolvePath(renderCtx.BasePath, path), renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}
	if action.SendDTMF != "" {
		if callMedia.usesWebRTC() {
			return media.ErrUnsupportedOverWebRTC
		}
		if mediaSession == nil {
			return fmt.Errorf("send_dtmf is not available in this context")
		}
		endpoint, err := media.ParseMediaEndpoint(mustParseLastMessage(renderCtx), renderCtx.RemoteIP, "audio")
		if err != nil {
			return err
		}
		digits, err := templ.RenderMessageStrict(action.SendDTMF, renderCtx)
		if err != nil {
			return err
		}
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "send_dtmf %q -> %s:%d\n", digits, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.SendDTMF(ctx, endpoint, digits, renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}

	_ = vars
	return nil
}

type rtpcheckSpec struct {
	minPackets uint32
	timeout    time.Duration
	direction  media.RTPCheckDirection
}

func parseRTPCheckSpec(raw string, renderCtx templ.Context) (rtpcheckSpec, error) {
	rendered, err := templ.RenderMessageStrict(raw, renderCtx)
	if err != nil {
		return rtpcheckSpec{}, err
	}
	spec := rtpcheckSpec{
		minPackets: 1,
		timeout:    time.Second,
		direction:  media.RTPCheckAny,
	}
	trimmed := strings.TrimSpace(rendered)
	if trimmed == "" {
		return spec, nil
	}
	if !strings.Contains(trimmed, "=") {
		value, err := strconv.Atoi(trimmed)
		if err != nil {
			return rtpcheckSpec{}, fmt.Errorf("rtpcheck value %q must be integer or key=value list", trimmed)
		}
		if value <= 0 {
			return rtpcheckSpec{}, fmt.Errorf("rtpcheck min packets must be > 0")
		}
		spec.minPackets = uint32(value)
		return spec, nil
	}
	for _, token := range strings.Fields(trimmed) {
		key, val, ok := strings.Cut(token, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch key {
		case "min_packets", "min", "packets":
			value, err := strconv.Atoi(val)
			if err != nil || value <= 0 {
				return rtpcheckSpec{}, fmt.Errorf("rtpcheck %s must be positive integer", key)
			}
			spec.minPackets = uint32(value)
		case "timeout_ms":
			value, err := strconv.Atoi(val)
			if err != nil || value <= 0 {
				return rtpcheckSpec{}, fmt.Errorf("rtpcheck timeout_ms must be positive integer")
			}
			spec.timeout = time.Duration(value) * time.Millisecond
		case "bidirectional":
			switch strings.ToLower(val) {
			case "1", "true", "yes":
				spec.direction = media.RTPCheckBoth
			case "0", "false", "no":
				spec.direction = media.RTPCheckAny
			default:
				return rtpcheckSpec{}, fmt.Errorf("rtpcheck bidirectional must be boolean")
			}
		case "direction":
			switch strings.ToLower(val) {
			case "any":
				spec.direction = media.RTPCheckAny
			case "send", "tx":
				spec.direction = media.RTPCheckSend
			case "recv", "rx", "receive":
				spec.direction = media.RTPCheckRecv
			case "both", "bidirectional":
				spec.direction = media.RTPCheckBoth
			default:
				return rtpcheckSpec{}, fmt.Errorf("rtpcheck direction must be one of any|send|recv|both")
			}
		}
	}
	return spec, nil
}

func configureMediaSRTPForRTPStream(e *Engine, mediaSession *media.Session, last sip.Message, verb string) error {
	sdpBody := media.EffectiveMediaSDPBody(last)
	hints := media.SDPHintsSRTP(sdpBody)
	iceOnly := !hints && media.SDPHasIceMediaAttributes(sdpBody)
	if !hints {
		if iceOnly {
			mediaSession.SetRtcpMuxFromSDP(sdpBody)
			mediaSession.SetRemoteIceFromSDP(sdpBody)
			return nil
		}
		mediaSession.ClearSDESSRTP()
		mediaSession.SetRtcpMuxFromSDP(sdpBody)
		return nil
	}
	mediaSession.ClearSDESSRTP()
	if e.cfg.MediaRejectSRTP {
		return fmt.Errorf("rtp_stream %s: remote SDP suggests SRTP (drop -media_reject_srtp to allow)", verb)
	}
	if !e.cfg.MediaSRTP {
		return fmt.Errorf("rtp_stream %s: remote SDP suggests SRTP; use -media_srtp for SDES (a=crypto inline), DTLS (a=fingerprint), or -media_reject_srtp to abort", verb)
	}
	if err := mediaSession.SetSDESSRTPFromSDP(sdpBody); err == nil {
		mediaSession.SetRtcpMuxFromSDP(sdpBody)
		mediaSession.SetRemoteIceFromSDP(sdpBody)
		return nil
	} else if !errors.Is(err, media.ErrNoAudioSDESCrypto) {
		return fmt.Errorf("rtp_stream %s: SRTP: %w", verb, err)
	}
	if err := mediaSession.SetDTLSFingerprintFromSDP(sdpBody); err != nil {
		if media.AudioSectionHasFingerprint(sdpBody) {
			return fmt.Errorf("rtp_stream %s: SRTP (DTLS fingerprint): %w", verb, err)
		}
		return fmt.Errorf("rtp_stream %s: SRTP: %w", verb, err)
	}
	mediaSession.SetRtcpMuxFromSDP(sdpBody)
	mediaSession.SetRemoteIceFromSDP(sdpBody)
	return nil
}

func mustParseLastMessage(ctx templ.Context) sip.Message {
	msg := sip.GetMessage()
	defer sip.PutMessage(msg)
	if err := sip.ParseInto(msg, []byte(ctx.LastMessage)); err != nil {
		return sip.Message{}
	}
	return msg.Copy()
}

func userID(callNumber, users int) int {
	if users <= 0 {
		return 0
	}
	return (callNumber - 1) % users
}
