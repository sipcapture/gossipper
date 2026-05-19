package media

import "testing"

func TestSDPBodyFromRawMessage(t *testing.T) {
	raw := "INVITE sip:x SIP/2.0\r\nContent-Type: application/sdp\r\nContent-Length: 10\r\n\r\nv=0\r\no=-"
	got := SDPBodyFromRawMessage(raw)
	if got != "v=0\r\no=-" {
		t.Fatalf("got %q", got)
	}
}
