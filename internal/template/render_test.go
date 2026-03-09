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

func TestRenderFieldTokenWithVariableLine(t *testing.T) {
	t.Parallel()

	ctx := Context{
		BasePath:   "../../testdata/injection",
		CallNumber: 1,
		Variables:  map[string]string{"line": "3"},
	}

	raw := "X-Field: [field2 file=inject.csv line=$line]\r\n\r\n"
	got := RenderMessage(raw, ctx)
	if !strings.Contains(got, "X-Field: bob") {
		t.Fatalf("expected variable-based line lookup, got %q", got)
	}
}

func TestLookupCSVLine(t *testing.T) {
	t.Parallel()

	line, found, err := LookupCSVLine("../../testdata/injection", "inject.csv", "2")
	if err != nil {
		t.Fatalf("LookupCSVLine() error = %v", err)
	}
	if !found {
		t.Fatal("expected key to be found")
	}
	if line != 3 {
		t.Fatalf("expected line 3, got %d", line)
	}
}

func TestRenderMessageStrictRejectsUnsupportedKeyword(t *testing.T) {
	t.Parallel()

	_, err := RenderMessageStrict("X-Test: [unsupported_helper]\r\n\r\n", Context{})
	if err == nil {
		t.Fatal("expected unsupported keyword error")
	}
}

func TestRenderMessageStrictSupportsAdditionalHelpers(t *testing.T) {
	t.Parallel()

	ctx := Context{
		LocalIP:      "127.0.0.10",
		ServerIP:     "127.0.0.20",
		Users:        7,
		UserID:       3,
		LastMessage:  "INVITE sip:alice@example.com SIP/2.0\r\nTo: <sip:bob@example.com>\r\n\r\n",
		LastHeaders:  map[string][]string{"To": {"<sip:bob@example.com>"}},
		CallNumber:   1,
		MessageIndex: 2,
	}

	got, err := RenderMessageStrict("X-Server-IP: [server_ip]\r\nX-Users: [users]\r\nX-UserID: [userid]\r\nX-URI: [last_Request_URI]\r\n\r\n", ctx)
	if err != nil {
		t.Fatalf("RenderMessageStrict() error = %v", err)
	}
	if !strings.Contains(got, "X-Server-IP: 127.0.0.20") {
		t.Fatalf("expected server_ip helper, got %q", got)
	}
	if !strings.Contains(got, "X-Users: 7") || !strings.Contains(got, "X-UserID: 3") {
		t.Fatalf("expected user helpers, got %q", got)
	}
	if !strings.Contains(got, "X-URI: sip:alice@example.com") {
		t.Fatalf("expected last_Request_URI helper, got %q", got)
	}
}
