package webrtc

import (
	"testing"
	"time"
)

func TestTURNRESTExpiryUnix(t *testing.T) {
	t.Parallel()
	user, _ := TURNRESTCredential("sec", "id", time.Hour)
	got, ok := TURNRESTExpiryUnix(user)
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Before(time.Now()) {
		t.Fatalf("expiry in past: %v", got)
	}
}

func TestTurnRefreshSleep(t *testing.T) {
	t.Parallel()
	expiry := time.Now().Add(2 * time.Hour)
	d := turnRefreshSleep(expiry, time.Hour)
	if d < time.Minute || d > 2*time.Hour {
		t.Fatalf("sleep=%v", d)
	}
}

func TestRefreshTURNCredentials(t *testing.T) {
	t.Parallel()
	b, err := NewBridge(Options{
		ICEServers:    []string{"turn:turn.example.com:3478"},
		ICEAuthSecret: "s3cret",
		ICEAuthTTL:    2 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	b.stateMu.RLock()
	before := b.turnCredExpires
	b.stateMu.RUnlock()
	if before == 0 {
		t.Fatal("expected initial turn expiry")
	}

	if err := b.refreshTURNCredentials(); err != nil {
		t.Fatal(err)
	}
	b.stateMu.RLock()
	after := b.turnCredExpires
	count := b.turnRefreshCount
	b.stateMu.RUnlock()
	if count != 1 {
		t.Fatalf("refresh count=%d", count)
	}
	if after < before {
		t.Fatalf("expiry went backwards: before=%d after=%d", before, after)
	}
}
