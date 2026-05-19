package webrtc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExtractCandidateInits(t *testing.T) {
	t.Parallel()
	body := "a=candidate:1 1 udp 2130706431 192.0.2.1 1234 typ host\r\n" +
		"a=candidate:2 1 udp 1694498815 203.0.113.1 54321 typ srflx raddr 192.0.2.1 rport 1234\r\n"
	inits := extractCandidateInits(body)
	if len(inits) != 2 {
		t.Fatalf("len=%d", len(inits))
	}
	if !strings.HasPrefix(inits[0].Candidate, "candidate:") {
		t.Fatalf("candidate=%q", inits[0].Candidate)
	}
}

func TestIsTrickleICEFragment(t *testing.T) {
	t.Parallel()
	if !IsTrickleICEFragment(`{"candidate":"candidate:1 1 udp 2130706431 192.0.2.1 1234 typ host"}`) {
		t.Fatal("json trickle should match")
	}
	if !IsTrickleICEFragment("a=candidate:1 1 udp 2130706431 192.0.2.1 1234 typ host\r\n") {
		t.Fatal("candidate-only fragment should match")
	}
	if IsTrickleICEFragment("v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\nm=audio 9 UDP/TLS/RTP/SAVPF 0\r\n") {
		t.Fatal("full sdp should not match")
	}
}

func TestBridgeTrickleOfferAnswer(t *testing.T) {
	t.Parallel()
	offerer, err := NewBridge(Options{PrefersPCMA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer offerer.Close()
	answerer, err := NewBridge(Options{PrefersPCMA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer answerer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	offerSDP, err := offerer.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(offerSDP, "a=candidate:") {
		t.Fatalf("expected trickle offer to include at least one candidate, got %q", offerSDP[:min(120, len(offerSDP))])
	}
	answerSDP, err := answerer.Answer(offerSDP)
	if err != nil {
		t.Fatal(err)
	}
	if err := offerer.AcceptAnswer(answerSDP); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeAddRemoteTrickleFragment(t *testing.T) {
	t.Parallel()
	offerer, err := NewBridge(Options{PrefersPCMA: true, ICETrickleFullGather: true})
	if err != nil {
		t.Fatal(err)
	}
	defer offerer.Close()
	answerer, err := NewBridge(Options{PrefersPCMA: true, ICETrickleFullGather: true})
	if err != nil {
		t.Fatal(err)
	}
	defer answerer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	offerSDP, err := offerer.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answerSDP, err := answerer.Answer(offerSDP)
	if err != nil {
		t.Fatal(err)
	}
	if err := offerer.AcceptAnswer(answerSDP); err != nil {
		t.Fatal(err)
	}
	frag := "a=candidate:999 1 udp 2130706431 192.0.2.99 9999 typ host\r\n"
	added, err := offerer.AddRemoteICECandidatesFromBody(frag)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("added=%d", added)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
