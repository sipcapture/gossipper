package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/media"
	"github.com/sipcapture/gossipper/internal/scenario"
	templ "github.com/sipcapture/gossipper/internal/template"
)

func TestApplyExecRTPStreamScaleSynthetic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		Scenario:   scenario.Scenario{Name: "test", Mode: scenario.ModeClient},
		MediaScale: true,
	}
	eng := New(cfg)
	eng.startScaleEngine(ctx)
	defer eng.stopScaleEngine()

	mediaSession := media.NewSession()
	defer mediaSession.Stop()

	sdp := "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 18000 RTP/AVP 0\r\n"
	renderCtx := templ.Context{
		CallID:      "scale-call-1",
		LocalIP:     "127.0.0.1",
		MediaPort:   17000,
		RemoteIP:    "127.0.0.1",
		LastMessage: "SIP/2.0 200 OK\r\nContent-Type: application/sdp\r\nContent-Length: " + fmt.Sprintf("%d", len(sdp)) + "\r\n\r\n" + sdp,
	}

	action := scenario.Action{RTPStream: "synthetic,0,0,PCMU/8000,20"}
	if err := eng.applyExecAction(ctx, action, renderCtx, nil, mediaSession); err != nil {
		t.Fatalf("applyExecAction: %v", err)
	}
	if eng.scaleMedia().StreamCount() != 1 {
		t.Fatalf("StreamCount=%d want 1", eng.scaleMedia().StreamCount())
	}
	time.Sleep(80 * time.Millisecond)
	st := eng.scaleUnregisterCall("scale-call-1")
	if st.RTPPacketsSent < 2 {
		t.Fatalf("RTPPacketsSent=%d want >= 2", st.RTPPacketsSent)
	}
}
