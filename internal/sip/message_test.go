package sip

import "testing"

func TestParseRequestAndCallID(t *testing.T) {
	t.Parallel()

	raw := []byte("INVITE sip:echo@example.com SIP/2.0\r\nCall-ID: abc\r\nCSeq: 1 INVITE\r\n\r\n")
	msg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if msg.Method != "INVITE" {
		t.Fatalf("expected INVITE, got %q", msg.Method)
	}

	callID, err := ExtractCallID(raw)
	if err != nil {
		t.Fatalf("ExtractCallID() error = %v", err)
	}
	if callID != "abc" {
		t.Fatalf("expected abc, got %q", callID)
	}
}
