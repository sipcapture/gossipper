package scenario

import "testing"

func TestParseScenarioWebRTCAttr(t *testing.T) {
	sc, err := ParseString(`<?xml version="1.0"?><scenario name="w" webrtc="true"><send>OK</send></scenario>`)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.WebRTC {
		t.Fatal("expected WebRTC=true")
	}
}
