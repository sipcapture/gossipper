package engine

import "testing"

func TestCallConcurrencyLimitPerCallUsesMaxSocketCap(t *testing.T) {
	t.Parallel()

	e := New(Config{
		MaxConcurrent: 10,
		MaxSockets:    4,
	})
	if got := e.callConcurrencyLimit(true); got != 4 {
		t.Fatalf("expected per-call limit 4, got %d", got)
	}
}

func TestCallConcurrencyLimitSharedIgnoresMaxSocketCap(t *testing.T) {
	t.Parallel()

	e := New(Config{
		MaxConcurrent: 10,
		MaxSockets:    4,
	})
	if got := e.callConcurrencyLimit(false); got != 10 {
		t.Fatalf("expected shared limit 10, got %d", got)
	}
}
