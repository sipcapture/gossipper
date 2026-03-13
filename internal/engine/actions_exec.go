package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/adubovikov/gossipper/internal/media"
	"github.com/adubovikov/gossipper/internal/scenario"
	"github.com/adubovikov/gossipper/internal/sip"
	templ "github.com/adubovikov/gossipper/internal/template"
)

func (e *Engine) applyExecAction(ctx context.Context, action scenario.Action, renderCtx templ.Context, vars *varStore, mediaSession *media.Session) error {
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
		if mediaSession == nil {
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
			mediaSession.Pause()
		case "resume":
			mediaSession.Resume()
		case "stop":
			mediaSession.Stop()
		case "echo":
			if e.cfg.TraceMessages {
				fmt.Fprintf(os.Stdout, "rtp_stream echo listen on %s:%d\n", renderCtx.LocalIP, renderCtx.MediaPort)
			}
			if err := mediaSession.StartEcho(ctx, renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
				return err
			}
		case "start":
			endpoint, err := media.ParseAudioEndpoint(mustParseLastMessage(renderCtx), renderCtx.RemoteIP)
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

	if action.PlayPCAPAudio != "" {
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
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "play_pcap_audio start %s -> %s:%d\n", path, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.StartPCAPReplay(ctx, endpoint, media.ResolvePath(renderCtx.BasePath, path), renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}
	if action.RTPCheck != "" {
		if mediaSession == nil {
			return fmt.Errorf("rtpcheck is not available in this context")
		}
		spec, err := parseRTPCheckSpec(action.RTPCheck, renderCtx)
		if err != nil {
			return err
		}
		checkCtx, cancel := context.WithTimeout(ctx, spec.timeout)
		defer cancel()
		if err := mediaSession.WaitForRTPActivity(checkCtx, spec.minPackets, spec.bidirectional); err != nil {
			return err
		}
	}
	if action.PlayPCAPVideo != "" {
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
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "play_pcap_video start %s -> %s:%d\n", path, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.StartPCAPReplay(ctx, endpoint, media.ResolvePath(renderCtx.BasePath, path), renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}
	if action.PlayPCAPImage != "" {
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
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "play_pcap_image start %s -> %s:%d\n", path, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.StartPCAPReplay(ctx, endpoint, media.ResolvePath(renderCtx.BasePath, path), renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}

	_ = vars
	return nil
}

type rtpcheckSpec struct {
	minPackets    uint32
	timeout       time.Duration
	bidirectional bool
}

func parseRTPCheckSpec(raw string, renderCtx templ.Context) (rtpcheckSpec, error) {
	rendered, err := templ.RenderMessageStrict(raw, renderCtx)
	if err != nil {
		return rtpcheckSpec{}, err
	}
	spec := rtpcheckSpec{
		minPackets:    1,
		timeout:       time.Second,
		bidirectional: false,
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
				spec.bidirectional = true
			case "0", "false", "no":
				spec.bidirectional = false
			default:
				return rtpcheckSpec{}, fmt.Errorf("rtpcheck bidirectional must be boolean")
			}
		}
	}
	return spec, nil
}

func mustParseLastMessage(ctx templ.Context) sip.Message {
	msg, _ := sip.Parse([]byte(ctx.LastMessage))
	return msg
}

func userID(callNumber, users int) int {
	if users <= 0 {
		return 0
	}
	return (callNumber - 1) % users
}
