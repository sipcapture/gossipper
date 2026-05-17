package webrtc

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestBridgeBackToBack drives two local bridges through an offer/answer cycle
// over an in-process channel, then verifies that audio written on one side
// arrives on the other.
func TestBridgeBackToBack(t *testing.T) {
	t.Parallel()

	offerer, err := NewBridge(Options{PrefersPCMA: true})
	if err != nil {
		t.Fatalf("NewBridge(offerer): %v", err)
	}
	defer offerer.Close()
	answerer, err := NewBridge(Options{PrefersPCMA: true})
	if err != nil {
		t.Fatalf("NewBridge(answerer): %v", err)
	}
	defer answerer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	offerSDP, err := offerer.CreateOffer(ctx)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	answerSDP, err := answerer.Answer(offerSDP)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if err := offerer.AcceptAnswer(answerSDP); err != nil {
		t.Fatalf("AcceptAnswer: %v", err)
	}

	var (
		mu       sync.Mutex
		received [][]byte
	)
	answerer.OnPCMA(func(payload []byte) {
		mu.Lock()
		defer mu.Unlock()
		buf := make([]byte, len(payload))
		copy(buf, payload)
		received = append(received, buf)
	})

	// Wait until ICE actually connects before writing samples; otherwise the
	// initial writes are dropped while DTLS-SRTP is still negotiating.
	if err := waitFor(ctx, 5*time.Second, func() bool {
		s := offerer.ConnectionState().String()
		return s == "connected" || s == "completed"
	}); err != nil {
		t.Fatalf("ICE never connected: state=%s", offerer.ConnectionState())
	}

	sample := make([]byte, 160) // 20ms of 8kHz 8-bit PCMA
	for i := range sample {
		sample[i] = byte(i)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := offerer.WritePCMA(sample, 20*time.Millisecond); err != nil {
			t.Fatalf("WritePCMA: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		got := len(received)
		mu.Unlock()
		if got > 0 {
			return
		}
	}
	t.Fatalf("expected at least one PCMA sample on the answer side, got none")
}

func waitFor(ctx context.Context, d time.Duration, pred func() bool) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if pred() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return context.DeadlineExceeded
}
