package engine

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/media"
	"github.com/sipcapture/gossipper/internal/scenario"
	templ "github.com/sipcapture/gossipper/internal/template"
)

func TestParseRTPCheckSpecDefaults(t *testing.T) {
	t.Parallel()

	spec, err := parseRTPCheckSpec("", templ.Context{})
	if err != nil {
		t.Fatalf("parseRTPCheckSpec() error = %v", err)
	}
	if spec.minPackets != 1 || spec.timeout != time.Second || spec.direction != media.RTPCheckAny {
		t.Fatalf("unexpected defaults: %+v", spec)
	}
}

func TestParseRTPCheckSpecKeyValue(t *testing.T) {
	t.Parallel()

	spec, err := parseRTPCheckSpec("min_packets=2 timeout_ms=350 direction=both", templ.Context{})
	if err != nil {
		t.Fatalf("parseRTPCheckSpec() error = %v", err)
	}
	if spec.minPackets != 2 {
		t.Fatalf("expected minPackets=2, got %d", spec.minPackets)
	}
	if spec.timeout != 350*time.Millisecond {
		t.Fatalf("expected timeout=350ms, got %v", spec.timeout)
	}
	if spec.direction != media.RTPCheckBoth {
		t.Fatalf("expected direction=both, got %q", spec.direction)
	}
}

func TestParseRTPCheckSpecRejectsInvalidTimeout(t *testing.T) {
	t.Parallel()

	_, err := parseRTPCheckSpec("timeout_ms=nope", templ.Context{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "timeout_ms") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRTPCheckSpecBidirectionalCompatibility(t *testing.T) {
	t.Parallel()

	spec, err := parseRTPCheckSpec("bidirectional=1", templ.Context{})
	if err != nil {
		t.Fatalf("parseRTPCheckSpec() error = %v", err)
	}
	if spec.direction != media.RTPCheckBoth {
		t.Fatalf("expected direction=both, got %q", spec.direction)
	}
}

func TestParseRTPCheckSpecRejectsInvalidDirection(t *testing.T) {
	t.Parallel()

	_, err := parseRTPCheckSpec("direction=sideways", templ.Context{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "direction") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyExecActionRTPCheck(t *testing.T) {
	t.Parallel()

	engine := New(Config{})
	mediaSession := media.NewSession()
	defer mediaSession.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := mediaSession.StartEcho(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("StartEcho() error = %v", err)
	}
	rtpPort, _ := mediaSession.Ports()
	clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(client) error = %v", err)
	}
	defer clientConn.Close()
	payload := make([]byte, 12)
	if _, err := clientConn.WriteToUDP(payload, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpPort}); err != nil {
		t.Fatalf("WriteToUDP() error = %v", err)
	}

	err = engine.applyExecAction(
		ctx,
		scenario.Action{Type: scenario.ActionExec, RTPCheck: "min_packets=1 timeout_ms=300 direction=send"},
		templ.Context{},
		newVarStore(nil, nil, nil, 0),
		&callMedia{session: mediaSession},
	)
	if err != nil {
		t.Fatalf("applyExecAction(rtpcheck) error = %v", err)
	}
}

func TestApplyExecActionRTPCheckTimeout(t *testing.T) {
	t.Parallel()

	engine := New(Config{})
	mediaSession := media.NewSession()
	defer mediaSession.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := engine.applyExecAction(
		ctx,
		scenario.Action{Type: scenario.ActionExec, RTPCheck: "min_packets=1 timeout_ms=50 direction=recv"},
		templ.Context{},
		newVarStore(nil, nil, nil, 0),
		&callMedia{session: mediaSession},
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "rtpcheck timeout") {
		t.Fatalf("unexpected error: %v", err)
	}
}
