package launcher

import (
	"testing"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/scenario"
)

func TestPrepareInviteMediaScaleEnablesMediaScale(t *testing.T) {
	t.Parallel()
	cfg := cli.Config{
		ScenarioName: scenario.BuiltinInviteMediaScale,
		Transport:    "u1",
		LocalIP:      "127.0.0.1",
		LocalPort:    5060,
		RemoteHost:   "127.0.0.1",
		RemotePort:   5060,
	}
	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !prepared.CLIConfig.MediaScale {
		t.Fatal("expected MediaScale=true for invite_media_scale")
	}
	if !prepared.EngineConfig.MediaScale {
		t.Fatal("expected EngineConfig.MediaScale=true")
	}
	if prepared.CLIConfig.Transport != "u1" {
		t.Fatalf("transport = %q, want u1", prepared.CLIConfig.Transport)
	}
}

func TestPrepareInviteMediaScaleDefaultTransportU1(t *testing.T) {
	t.Parallel()
	cfg := cli.Config{
		ScenarioName: scenario.BuiltinInviteMediaScale,
		LocalIP:      "127.0.0.1",
		LocalPort:    5060,
		RemoteHost:   "127.0.0.1",
		RemotePort:   5060,
	}
	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prepared.CLIConfig.Transport != "u1" {
		t.Fatalf("transport = %q, want u1", prepared.CLIConfig.Transport)
	}
}

func TestPreparePropagatesMediaIOUring(t *testing.T) {
	t.Parallel()
	cfg := cli.Config{
		ScenarioName: scenario.BuiltinInviteMediaScale,
		Transport:    "u1",
		LocalIP:      "127.0.0.1",
		LocalPort:    5060,
		RemoteHost:   "127.0.0.1",
		RemotePort:   5060,
		MediaScale:   true,
		MediaIOUring: true,
	}
	prepared, err := Prepare(cfg)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !prepared.EngineConfig.MediaIOUring {
		t.Fatal("expected EngineConfig.MediaIOUring=true")
	}

	off := cli.Config{
		ScenarioName: scenario.BuiltinInviteMediaScale,
		Transport:    "u1",
		LocalIP:      "127.0.0.1",
		LocalPort:    5061,
		RemoteHost:   "127.0.0.1",
		RemotePort:   5060,
		MediaScale:   true,
	}
	preparedOff, err := Prepare(off)
	if err != nil {
		t.Fatalf("Prepare off: %v", err)
	}
	if preparedOff.EngineConfig.MediaIOUring {
		t.Fatal("expected EngineConfig.MediaIOUring=false for a separate Prepare")
	}
}
