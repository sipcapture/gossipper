package webrtc

import (
	"strings"
	"testing"
	"time"
)

func TestParseICEServerLineEmbeddedCreds(t *testing.T) {
	t.Parallel()
	p, err := parseICEServerLine("turn:alice:secret@turn.example.com:3478?transport=udp")
	if err != nil {
		t.Fatal(err)
	}
	if p.URL != "turn:turn.example.com:3478?transport=udp" {
		t.Fatalf("url=%q", p.URL)
	}
	if p.Username != "alice" || p.Password != "secret" {
		t.Fatalf("creds=%q/%q", p.Username, p.Password)
	}
}

func TestParseICEServerLineSTUN(t *testing.T) {
	t.Parallel()
	p, err := parseICEServerLine("stun:stun.l.google.com:19302")
	if err != nil {
		t.Fatal(err)
	}
	if p.URL != "stun:stun.l.google.com:19302" {
		t.Fatalf("url=%q", p.URL)
	}
}

func TestTURNRESTCredentialFormat(t *testing.T) {
	t.Parallel()
	user, pass := TURNRESTCredential("testsecret", "alice", time.Hour)
	if !strings.Contains(user, ":alice") {
		t.Fatalf("username=%q", user)
	}
	if pass == "" {
		t.Fatal("expected password")
	}
}

func TestBuildICEServersREST(t *testing.T) {
	t.Parallel()
	servers, err := BuildICEServers(Options{
		ICEServers:    []string{"stun:stun.example.com:3478", "turn:turn.example.com:3478"},
		ICEAuthSecret: "s3cret",
		ICEUsername:   "bob",
		ICEAuthTTL:    time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("len=%d", len(servers))
	}
	if servers[0].Username != "" {
		t.Fatal("STUN should not get REST creds")
	}
	if servers[1].Username == "" || servers[1].Credential == "" {
		t.Fatal("TURN should get REST creds")
	}
}
