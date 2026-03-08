package template

import (
	"strings"
	"testing"
)

func TestRenderMessageComputesLengthAndHeaders(t *testing.T) {
	t.Parallel()

	ctx := Context{
		Service:      "echo",
		Transport:    "u1",
		RemoteIP:     "127.0.0.1",
		RemotePort:   5060,
		LocalIP:      "127.0.0.1",
		LocalIPType:  "4",
		LocalPort:    5080,
		CallID:       "abc",
		CSeq:         1,
		CallNumber:   3,
		MessageIndex: 1,
		PID:          1234,
		BranchBase:   "z9hG4bK-test",
		LastHeaders: map[string][]string{
			"Via":     {"Via: SIP/2.0/UDP 127.0.0.1:5060"},
			"To":      {"To: <sip:echo@127.0.0.1>;tag=peer"},
			"Call-ID": {"Call-ID: abc"},
			"From":    {"From: test"},
			"CSeq":    {"CSeq: 1 INVITE"},
			"Contact": {"Contact: <sip:test@127.0.0.1>"},
		},
	}

	raw := "SIP/2.0 200 OK\r\n[last_Via:]\r\n[last_To:][peer_tag_param]\r\nContent-Length: [len]\r\n\r\nhello"
	got := RenderMessage(raw, ctx)

	if !strings.Contains(got, "Content-Length: 5") {
		t.Fatalf("expected computed content length, got %q", got)
	}
	if !strings.Contains(got, ";tag=peer") {
		t.Fatalf("expected peer tag, got %q", got)
	}
}

func TestMissingLastHeaderDropsLine(t *testing.T) {
	t.Parallel()

	ctx := Context{}
	raw := "SIP/2.0 200 OK\r\n[last_Via:]\r\nContent-Length: 0\r\n\r\n"
	got := RenderMessage(raw, ctx)
	if strings.Contains(got, "Via:") {
		t.Fatalf("expected missing header line to be removed, got %q", got)
	}
}

func TestRenderLastHeaderReconstructsHeaderName(t *testing.T) {
	t.Parallel()

	ctx := Context{
		LastHeaders: map[string][]string{
			"Via": {"SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-1"},
		},
	}

	got := RenderMessage("SIP/2.0 200 OK\r\n[last_Via:]\r\nContent-Length: 0\r\n\r\n", ctx)
	if !strings.Contains(got, "Via: SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-1") {
		t.Fatalf("expected header name reconstruction, got %q", got)
	}
}

func TestRenderVariablesAndTCPTransport(t *testing.T) {
	t.Parallel()

	ctx := Context{
		Transport: "t1",
		Variables: map[string]string{"1": "alice", "user": "bob"},
	}

	raw := "Via: SIP/2.0/[transport] host\r\nX-One: [$1]\r\nX-User: [$user]\r\n\r\n"
	got := RenderMessage(raw, ctx)

	if !strings.Contains(got, "SIP/2.0/TCP") {
		t.Fatalf("expected TCP transport, got %q", got)
	}
	if !strings.Contains(got, "X-One: alice") || !strings.Contains(got, "X-User: bob") {
		t.Fatalf("expected variables to render, got %q", got)
	}
}

func TestRenderFileAndFieldTokens(t *testing.T) {
	t.Parallel()

	ctx := Context{
		BasePath:   "../../testdata/scenarios",
		CallNumber: 1,
	}

	raw := "X-File: [file name=../injection/message.txt]\r\nX-Field: [field2 file=../injection/inject.csv line=2]\r\n\r\n"
	got := RenderMessage(raw, ctx)
	if !strings.Contains(got, "X-File: hello-from-file") {
		t.Fatalf("expected file token to render, got %q", got)
	}
	if !strings.Contains(got, "X-Field: alice") {
		t.Fatalf("expected field token to render, got %q", got)
	}
}
