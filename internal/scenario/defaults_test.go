package scenario

import (
	"strings"
	"testing"
)

func TestLoadNamedUASHasRTPStream(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("uas")
	if err != nil {
		t.Fatalf("LoadNamed(uas): %v", err)
	}
	raw, err := BuiltinXML("uas")
	if err != nil {
		t.Fatalf("BuiltinXML(uas): %v", err)
	}
	if !strings.Contains(raw, `rtp_stream="synthetic,0,[audio_pt],[audio_codec],20"`) {
		t.Fatal("builtin uas must play synthetic RTP after ACK")
	}
	var start, stop bool
	for _, cmd := range sc.Commands {
		for _, a := range cmd.Actions {
			if strings.HasPrefix(a.RTPStream, "synthetic,") {
				start = true
			}
			if a.RTPStream == "stop" {
				stop = true
			}
		}
	}
	if !start || !stop {
		t.Fatalf("uas rtp_stream start=%v stop=%v", start, stop)
	}
}

func TestLoadNamedInviteMedia(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media): %v", err)
	}
	if sc.Name != "invite_media" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}

func TestLoadNamedInviteMediaScale(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_scale")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_scale): %v", err)
	}
	if sc.Name != "invite_media_scale" {
		t.Fatalf("scenario name = %q", sc.Name)
	}
}

func TestLoadNamedInviteMediaEarly(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_early")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_early): %v", err)
	}
	if sc.Name != "invite_media_early" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}

func TestLoadNamedInviteMediaEarly180(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_early_180")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_early_180): %v", err)
	}
	if sc.Name != "invite_media_early_180" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}

func TestLoadNamedInviteMediaSavpf(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_savpf")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_savpf): %v", err)
	}
	if sc.Name != "invite_media_savpf" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}

func TestLoadNamedManagement(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("management")
	if err != nil {
		t.Fatalf("LoadNamed(management): %v", err)
	}
	if sc.Name != "management" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
	if sc.Mode != ModeServer {
		t.Fatalf("mode: got %q want server", sc.Mode)
	}
}

func TestLoadNamedWebRTCScenarios(t *testing.T) {
	t.Parallel()
	uac, err := LoadNamed("webrtc_uac")
	if err != nil {
		t.Fatalf("LoadNamed(webrtc_uac): %v", err)
	}
	if !uac.WebRTC {
		t.Fatal("webrtc_uac: expected WebRTC=true")
	}
	uas, err := LoadNamed("webrtc_uas")
	if err != nil {
		t.Fatalf("LoadNamed(webrtc_uas): %v", err)
	}
	if !uas.WebRTC {
		t.Fatal("webrtc_uas: expected WebRTC=true")
	}
	xml, err := BuiltinXML("webrtc_uac")
	if err != nil || xml == "" {
		t.Fatalf("BuiltinXML(webrtc_uac): %v", err)
	}
}
