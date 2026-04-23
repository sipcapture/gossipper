package template

import (
	"os"
	"path/filepath"
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

func TestRenderTransportUIIsUDP(t *testing.T) {
	t.Parallel()

	got := RenderMessage("Via: SIP/2.0/[transport] host\r\n\r\n", Context{Transport: "ui"})
	if !strings.Contains(got, "SIP/2.0/UDP") {
		t.Fatalf("expected UDP transport for ui mode, got %q", got)
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

func TestGenerateCSVIndexAndLookupCSVLine(t *testing.T) {
	t.Parallel()

	basePath := t.TempDir()
	csvPath := filepath.Join(basePath, "users.csv")
	if err := os.WriteFile(csvPath, []byte("alice,pass_A\nbob,pass_B\ncarol,pass_C\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(csv) error = %v", err)
	}
	indexPath, entries, err := GenerateCSVIndex(basePath, "users.csv", 0)
	if err != nil {
		t.Fatalf("GenerateCSVIndex() error = %v", err)
	}
	if entries != 3 {
		t.Fatalf("expected 3 index entries, got %d", entries)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("expected generated index at %s: %v", indexPath, err)
	}

	line, found, err := LookupCSVLine(basePath, "users.csv", "bob")
	if err != nil {
		t.Fatalf("LookupCSVLine() error = %v", err)
	}
	if !found || line != 2 {
		t.Fatalf("expected bob on line 2, got line=%d found=%v", line, found)
	}
}

func TestLookupCSVLineUsesGeneratedIndex(t *testing.T) {
	t.Parallel()

	basePath := t.TempDir()
	csvPath := filepath.Join(basePath, "users.csv")
	if err := os.WriteFile(csvPath, []byte("alice,pass_A\nbob,pass_B\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(csv) error = %v", err)
	}
	if _, _, err := GenerateCSVIndex(basePath, "users.csv", 0); err != nil {
		t.Fatalf("GenerateCSVIndex() error = %v", err)
	}
	if err := os.WriteFile(csvPath, []byte("nobody,pass_N\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(csv overwrite) error = %v", err)
	}

	line, found, err := LookupCSVLine(basePath, "users.csv", "bob")
	if err != nil {
		t.Fatalf("LookupCSVLine() error = %v", err)
	}
	if !found || line != 2 {
		t.Fatalf("expected indexed lookup to return line 2, got line=%d found=%v", line, found)
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

func TestRenderMessageStrictSupportsM6P0Keywords(t *testing.T) {
	t.Parallel()

	ctx := Context{
		SIPpVersion: "Gossipper-0.6.0",
		ClockTick:   1200,
		DynamicID:   42,
	}

	got, err := RenderMessageStrict(
		"X-Version: [sipp_version]\r\nX-Tick: [clock_tick+2]\r\nX-Dynamic: [dynamic_id]\r\n\r\n",
		ctx,
	)
	if err != nil {
		t.Fatalf("RenderMessageStrict() error = %v", err)
	}
	if !strings.Contains(got, "X-Version: Gossipper-0.6.0") {
		t.Fatalf("expected sipp_version helper, got %q", got)
	}
	if !strings.Contains(got, "X-Tick: 1202") {
		t.Fatalf("expected clock_tick helper with arithmetic, got %q", got)
	}
	if !strings.Contains(got, "X-Dynamic: 42") {
		t.Fatalf("expected dynamic_id helper, got %q", got)
	}
}

func TestRenderMessageSupportsM6P0KeywordDefaults(t *testing.T) {
	t.Parallel()

	got := RenderMessage("X-Version: [sipp_version]\r\nX-Tick: [clock_tick]\r\nX-Dynamic: [dynamic_id]\r\n\r\n", Context{})
	if !strings.Contains(got, "X-Version: Gossipper") {
		t.Fatalf("expected default sipp_version helper, got %q", got)
	}
	if !strings.Contains(got, "X-Tick: 0") || !strings.Contains(got, "X-Dynamic: 0") {
		t.Fatalf("expected zero defaults for clock_tick/dynamic_id, got %q", got)
	}
}

func TestRenderMessageStrictSupportsFillKeyword(t *testing.T) {
	t.Parallel()

	ctx := Context{
		Variables: map[string]string{"pad": "5", "pad2": "7"},
	}
	got, err := RenderMessageStrict("X-Fill: [fill variable=$pad]\r\nX-Fill2: [fill variable=pad2 text=ab]\r\n\r\n", ctx)
	if err != nil {
		t.Fatalf("RenderMessageStrict() error = %v", err)
	}
	if !strings.Contains(got, "X-Fill: XXXXX") {
		t.Fatalf("expected default fill pattern, got %q", got)
	}
	if !strings.Contains(got, "X-Fill2: abababa") {
		t.Fatalf("expected custom fill pattern, got %q", got)
	}
}

func TestRenderMessageStrictRejectsInvalidFillKeyword(t *testing.T) {
	t.Parallel()

	_, err := RenderMessageStrict("X-Fill: [fill text=ab]\r\n\r\n", Context{Variables: map[string]string{"v": "2"}})
	if err == nil {
		t.Fatal("expected invalid fill token error")
	}
	if !strings.Contains(err.Error(), "fill token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

var benchCtx = Context{
	Service:    "echo",
	Transport:  "u1",
	LocalIP:    "127.0.0.1",
	LocalPort:  5080,
	RemoteHost: "127.0.0.1",
	RemoteIP:   "127.0.0.1",
	RemotePort: 5060,
	CallID:     "bench-call-id-0001",
	CSeq:       1,
	CallNumber: 1,
	PID:        12345,
	BranchBase: "z9hG4bK-bench",
}

// rawNoLen is a typical INVITE without [len] (common case: no body).
const rawNoLen = "INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0\r\n" +
	"Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]\r\n" +
	"From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]\r\n" +
	"To: [service] <sip:[service]@[remote_ip]:[remote_port]>\r\n" +
	"Call-ID: [call_id]\r\n" +
	"CSeq: [cseq] INVITE\r\n" +
	"Content-Length: 0\r\n\r\n"

// rawWithLen has a body and uses [len] — requires double render.
const rawWithLen = "INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0\r\n" +
	"Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]\r\n" +
	"From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]\r\n" +
	"To: [service] <sip:[service]@[remote_ip]:[remote_port]>\r\n" +
	"Call-ID: [call_id]\r\n" +
	"CSeq: [cseq] INVITE\r\n" +
	"Content-Length: [len]\r\n\r\n" +
	"v=0\r\no=test 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"

// BenchmarkRenderMessageNoLen measures the fast path (no [len] token).
func BenchmarkRenderMessageNoLen(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = RenderMessage(rawNoLen, benchCtx)
	}
}

// BenchmarkRenderMessageWithLen measures the double-render path ([len] present).
func BenchmarkRenderMessageWithLen(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = RenderMessage(rawWithLen, benchCtx)
	}
}

// BenchmarkRenderMessageStrictNoLen measures RenderMessageStrict fast path.
func BenchmarkRenderMessageStrictNoLen(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = RenderMessageStrict(rawNoLen, benchCtx)
	}
}

// BenchmarkRenderMessageStrictWithLen measures RenderMessageStrict double-render path.
func BenchmarkRenderMessageStrictWithLen(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = RenderMessageStrict(rawWithLen, benchCtx)
	}
}
