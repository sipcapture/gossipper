package sip

import (
	"testing"
)

func TestNormalizeCallID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "whitespace_only", in: "   ", want: ""},
		{name: "plain", in: "abc", want: "abc"},
		{name: "trims_whitespace", in: "  abc  ", want: "abc"},
		{name: "single_slash_passthrough", in: "abc/def", want: "abc/def"},
		{name: "double_slash_passthrough", in: "abc//def", want: "abc//def"},
		{name: "sipp_prefix_form", in: "ABCDEFGHIJ///[call_id]", want: "[call_id]"},
		{name: "real_world_prefix", in: "myprefix///dialog123", want: "dialog123"},
		{name: "multiple_triple_slashes_take_last", in: "x///y///z", want: "z"},
		{name: "leading_triple_slash", in: "///rest", want: "rest"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeCallID(tc.in); got != tc.want {
				t.Fatalf("NormalizeCallID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

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

func TestMatchRecvCSeqForBYE200(t *testing.T) {
	t.Parallel()

	bye := []byte(
		"BYE sip:peer@10.0.0.1:5060 SIP/2.0\r\n" +
			"Via: SIP/2.0/UDP 10.0.0.2:5080;branch=z9hG4bK-bye\r\n" +
			"Call-ID: dlg1\r\n" +
			"CSeq: 3 BYE\r\n" +
			"Content-Length: 0\r\n\r\n",
	)
	invite200 := []byte(
		"SIP/2.0 200 OK\r\n" +
			"Via: SIP/2.0/UDP 10.0.0.2:5080;branch=z9hG4bK-inv\r\n" +
			"Call-ID: dlg1\r\n" +
			"CSeq: 1 INVITE\r\n" +
			"Content-Length: 0\r\n\r\n",
	)
	bye200 := []byte(
		"SIP/2.0 200 OK\r\n" +
			"Via: SIP/2.0/UDP 10.0.0.2:5080;branch=z9hG4bK-bye\r\n" +
			"Call-ID: dlg1\r\n" +
			"CSeq: 3 BYE\r\n" +
			"Content-Length: 0\r\n\r\n",
	)

	invMsg, err := Parse(invite200)
	if err != nil {
		t.Fatalf("Parse invite 200: %v", err)
	}
	if MatchRecv(invMsg, "", "200", bye) {
		t.Fatal("late INVITE 200 must not match recv(200) with lastSent=BYE")
	}

	byeMsg, err := Parse(bye200)
	if err != nil {
		t.Fatalf("Parse bye 200: %v", err)
	}
	if !MatchRecv(byeMsg, "", "200", bye) {
		t.Fatal("200 OK to BYE must match recv(200) with lastSent=BYE")
	}
}

func TestResponseStatusMatches(t *testing.T) {
	t.Parallel()
	raw := []byte("SIP/2.0 200 OK\r\nCSeq: 1 INVITE\r\n\r\n")
	msg, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !ResponseStatusMatches(msg, "200") || !ResponseStatusMatches(msg, "200 OK") {
		t.Fatal("expected status match")
	}
	if ResponseStatusMatches(msg, "180") {
		t.Fatal("180 must not match")
	}
}

func TestViaSentBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		headers  map[string][]string
		wantHost string
		wantPort int
		wantOK   bool
	}{
		{
			name: "hostname with port",
			headers: map[string][]string{
				"Via": {"SIP/2.0/UDP foo.example.com:5061;branch=z9hG4bK-x"},
			},
			wantHost: "foo.example.com",
			wantPort: 5061,
			wantOK:   true,
		},
		{
			name: "IP with port",
			headers: map[string][]string{
				"Via": {"SIP/2.0/UDP 192.168.1.1:5060;branch=z9hG4bK-y"},
			},
			wantHost: "192.168.1.1",
			wantPort: 5060,
			wantOK:   true,
		},
		{
			name: "host only, default port",
			headers: map[string][]string{
				"Via": {"SIP/2.0/UDP hostonly;branch=z9hG4bK-z"},
			},
			wantHost: "hostonly",
			wantPort: 5060,
			wantOK:   true,
		},
		{
			name:     "no Via",
			headers:  map[string][]string{},
			wantHost: "",
			wantPort: 0,
			wantOK:   false,
		},
		{
			name: "TCP transport",
			headers: map[string][]string{
				"Via": {"SIP/2.0/TCP 10.0.0.5:5062;branch=z9hG4bK-a"},
			},
			wantHost: "10.0.0.5",
			wantPort: 5062,
			wantOK:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotPort, gotOK := ViaSentBy(tt.headers)
			if gotHost != tt.wantHost || gotPort != tt.wantPort || gotOK != tt.wantOK {
				t.Errorf("ViaSentBy() = (%q, %d, %v), want (%q, %d, %v)",
					gotHost, gotPort, gotOK, tt.wantHost, tt.wantPort, tt.wantOK)
			}
		})
	}
}

// ─── Benchmarks ──────────────────────────────────────────────────────────────

var benchInvite = []byte(
	"INVITE sip:echo@127.0.0.1:5060 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1:5080;branch=z9hG4bK-1\r\n" +
		"From: test <sip:test@127.0.0.1:5080>;tag=abc123\r\n" +
		"To: echo <sip:echo@127.0.0.1:5060>\r\n" +
		"Call-ID: bench-call-id-0001@127.0.0.1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Contact: <sip:test@127.0.0.1:5080>\r\n" +
		"Content-Length: 0\r\n\r\n",
)

// BenchmarkParse measures the old path that allocates a new Message every call.
func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		msg, err := Parse(benchInvite)
		if err != nil {
			b.Fatal(err)
		}
		_ = msg.Method
	}
}

// BenchmarkParseIntoPool measures GetMessage/ParseInto/PutMessage with sync.Pool.
func BenchmarkParseIntoPool(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		msg := GetMessage()
		if err := ParseInto(msg, benchInvite); err != nil {
			b.Fatal(err)
		}
		_ = msg.Method
		PutMessage(msg)
	}
}

// BenchmarkChanValue measures passing sip.Message by value through a buffered channel.
func BenchmarkChanValue(b *testing.B) {
	b.ReportAllocs()
	ch := make(chan Message, 1)
	for b.Loop() {
		msg, _ := Parse(benchInvite)
		ch <- msg
		<-ch
	}
}

// BenchmarkChanPointer measures passing *sip.Message by pointer through a buffered channel.
func BenchmarkChanPointer(b *testing.B) {
	b.ReportAllocs()
	ch := make(chan *Message, 1)
	for b.Loop() {
		msg := GetMessage()
		_ = ParseInto(msg, benchInvite)
		ch <- msg
		m := <-ch
		PutMessage(m)
	}
}

// BenchmarkCopyInto measures CopyInto (value → pooled pointer) used in dispatchMessages.
func BenchmarkCopyInto(b *testing.B) {
	b.ReportAllocs()
	src, _ := Parse(benchInvite)
	for b.Loop() {
		dst := GetMessage()
		CopyInto(dst, src)
		_ = dst.Method
		PutMessage(dst)
	}
}

// BenchmarkOldDispatchPath simulates the old TCP dispatch:
//
//	Parse(raw) → Message.Copy() to move headers across goroutine boundary.
func BenchmarkOldDispatchPath(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		msg, err := Parse(benchInvite)
		if err != nil {
			b.Fatal(err)
		}
		cpy := msg.Copy()
		_ = cpy.Method
	}
}

// BenchmarkNewDispatchPath simulates the new TCP dispatch:
//
//	Parse(raw) [shared transport] → GetMessage + CopyInto [per-call pool] → send *ptr.
func BenchmarkNewDispatchPath(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		src, err := Parse(benchInvite)
		if err != nil {
			b.Fatal(err)
		}
		dst := GetMessage()
		CopyInto(dst, src)
		_ = dst.Method
		PutMessage(dst)
	}
}

// BenchmarkNewUDPDispatchPath simulates the new UDP dispatch:
//
//	GetMessage + ParseInto → send *ptr → PutMessage (no intermediate Copy).
func BenchmarkNewUDPDispatchPath(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		msg := GetMessage()
		if err := ParseInto(msg, benchInvite); err != nil {
			b.Fatal(err)
		}
		_ = msg.Method
		PutMessage(msg)
	}
}

// BenchmarkOldDispatchParallel stresses the old path under concurrency to expose GC pressure.
func BenchmarkOldDispatchParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			msg, err := Parse(benchInvite)
			if err != nil {
				b.Fatal(err)
			}
			cpy := msg.Copy()
			_ = cpy.Method
		}
	})
}

// BenchmarkNewUDPDispatchParallel stresses the new UDP path under concurrency.
// The sync.Pool shines here: objects are returned by idle goroutines and reused.
func BenchmarkNewUDPDispatchParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			msg := GetMessage()
			if err := ParseInto(msg, benchInvite); err != nil {
				b.Fatal(err)
			}
			_ = msg.Method
			PutMessage(msg)
		}
	})
}

func TestResponseMatchesTransaction(t *testing.T) {
t.Parallel()

invite200 := []byte("SIP/2.0 200 OK\r\nVia: SIP/2.0/UDP 192.168.25.21:60470;received=192.168.25.21;branch=z9hG4bK-gossip-18-0;rport=60470\r\nFrom: test <sip:test@x>;tag=t1\r\nTo: callee <sip:callee@y>;tag=t2\r\nCall-ID: gossip-18-abc\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n")
byeMsg := []byte("BYE sip:callee@y SIP/2.0\r\nVia: SIP/2.0/UDP 192.168.25.21:60470;branch=z9hG4bK-gossip-18-5\r\nFrom: test <sip:test@x>;tag=t1\r\nTo: callee <sip:callee@y>;tag=t2\r\nCall-ID: gossip-18-abc\r\nCSeq: 2 BYE\r\nContent-Length: 0\r\n\r\n")
inviteMsg := []byte("INVITE sip:callee@y SIP/2.0\r\nVia: SIP/2.0/UDP 192.168.25.21:60470;branch=z9hG4bK-gossip-18-0\r\nFrom: test <sip:test@x>;tag=t1\r\nTo: callee <sip:callee@y>\r\nCall-ID: gossip-18-abc\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n")

msg := GetMessage()
defer PutMessage(msg)
if err := ParseInto(msg, invite200); err != nil {
t.Fatalf("ParseInto: %v", err)
}

// INVITE 200 should NOT match BYE transaction (different branch)
if ResponseMatchesTransaction(*msg, byeMsg) {
t.Error("INVITE 200 should not match BYE transaction")
}

// INVITE 200 should match INVITE transaction (same branch)
if !ResponseMatchesTransaction(*msg, inviteMsg) {
t.Error("INVITE 200 should match INVITE transaction")
}

// Empty lastSent → true (safe default)
if !ResponseMatchesTransaction(*msg, nil) {
t.Error("nil lastSent should return true")
}
}

func TestViaBranch(t *testing.T) {
t.Parallel()

cases := []struct {
name    string
headers map[string][]string
want    string
ok      bool
}{
{
name:    "standard via",
headers: map[string][]string{"Via": {"SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-abc123"}},
want:    "z9hG4bK-abc123",
ok:      true,
},
{
name:    "via with extra params",
headers: map[string][]string{"Via": {"SIP/2.0/UDP 10.0.0.1:5060;received=10.0.0.2;branch=z9hG4bK-xyz;rport=5060"}},
want:    "z9hG4bK-xyz",
ok:      true,
},
{
name:    "no branch",
headers: map[string][]string{"Via": {"SIP/2.0/UDP 10.0.0.1:5060"}},
want:    "",
ok:      false,
},
{
name:    "no via",
headers: map[string][]string{},
want:    "",
ok:      false,
},
}
for _, tc := range cases {
t.Run(tc.name, func(t *testing.T) {
got, ok := ViaBranch(tc.headers)
if got != tc.want || ok != tc.ok {
t.Errorf("ViaBranch() = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
}
})
}
}
