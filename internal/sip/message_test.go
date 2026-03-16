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
