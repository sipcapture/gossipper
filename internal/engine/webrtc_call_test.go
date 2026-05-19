package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/sipcapture/gossipper/internal/media"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/webrtc"
	templ "github.com/sipcapture/gossipper/internal/template"
)

func TestSDPBodyExtractionForWebRTC(t *testing.T) {
	offerer, err := webrtc.NewBridge(webrtc.Options{PrefersPCMA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer offerer.Close()
	offerSDP, err := offerer.CreateOffer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lastMsg := "INVITE sip:x SIP/2.0\r\nContent-Type: application/sdp\r\nContent-Length: " +
		strconv.Itoa(len(offerSDP)) + "\r\n\r\n" + offerSDP
	body := media.SDPBodyFromRawMessage(lastMsg)
	if body == "" {
		t.Fatal("empty sdp body")
	}
	answerer, err := webrtc.NewBridge(webrtc.Options{PrefersPCMA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer answerer.Close()
	if _, err := answerer.Answer(body); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareWebRTCAnswerKeyword(t *testing.T) {
	offerer, err := webrtc.NewBridge(webrtc.Options{PrefersPCMA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer offerer.Close()
	answerer, err := webrtc.NewBridge(webrtc.Options{PrefersPCMA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer answerer.Close()

	offerSDP, err := offerer.CreateOffer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(offerSDP) == "" {
		t.Fatal("empty offer sdp")
	}

	cm := &callMedia{wrtc: &webrtcCallMedia{bridge: answerer}}
	lastMsg := "INVITE sip:x SIP/2.0\r\nContent-Type: application/sdp\r\nContent-Length: " +
		strconv.Itoa(len(offerSDP)) + "\r\n\r\n" + offerSDP
	renderCtx := templ.Context{
		LastMessage:   lastMsg,
		ExtraKeywords: map[string]string{},
	}
	if err := prepareWebRTCAnswerKeyword(cm, &renderCtx, "v=0\r\n[webrtc_answer]"); err != nil {
		t.Fatal(err)
	}
	ans := renderCtx.ExtraKeywords["webrtc_answer"]
	if ans == "" || !strings.Contains(ans, "m=audio") {
		t.Fatalf("missing answer sdp: %q", ans)
	}
}

func TestApplyExecActionWebRTCRejectsPCAP(t *testing.T) {
	engine := New(Config{})
	br, err := webrtc.NewBridge(webrtc.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cm := &callMedia{wrtc: &webrtcCallMedia{bridge: br}}
	err = engine.applyExecAction(
		context.Background(),
		scenario.Action{Type: scenario.ActionExec, PlayPCAPAudio: "x.pcap"},
		templ.Context{LastMessage: "OK"},
		newVarStore(nil, nil, nil, 0),
		cm,
	)
	if err == nil {
		t.Fatal("expected error")
	}
}
