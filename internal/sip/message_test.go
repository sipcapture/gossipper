package sip

import (
	"testing"
)

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
