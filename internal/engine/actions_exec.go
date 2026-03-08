package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
		command := templ.RenderMessage(action.Command, renderCtx)
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
		command, cfg, err := media.ParseRTPStreamSpec(templ.RenderMessage(action.RTPStream, renderCtx), renderCtx.BasePath)
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
		endpoint, err := media.ParseAudioEndpoint(mustParseLastMessage(renderCtx), renderCtx.RemoteIP)
		if err != nil {
			return err
		}
		path := templ.RenderMessage(action.PlayPCAPAudio, renderCtx)
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "play_pcap_audio start %s -> %s:%d\n", path, endpoint.IP, endpoint.Port)
		}
		if err := mediaSession.StartPCAPReplay(ctx, endpoint, media.ResolvePath(renderCtx.BasePath, path), renderCtx.LocalIP, renderCtx.MediaPort); err != nil {
			return err
		}
	}

	_ = vars
	return nil
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
