package eventlog

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRingBufferFIFOOrder(t *testing.T) {
	rb := newRingBuffer(8)
	for i := 0; i < 5; i++ {
		rb.push(Event{Msg: idMsg(i)})
	}
	out := make([]Event, 0, 8)
	go rb.close()
	out, _ = rb.drain(out)
	if len(out) != 5 {
		t.Fatalf("expected 5 events drained, got %d", len(out))
	}
	for i, ev := range out {
		if ev.Msg != idMsg(i) {
			t.Fatalf("position %d: expected %q, got %q", i, idMsg(i), ev.Msg)
		}
	}
	if got := rb.dropCount(); got != 0 {
		t.Fatalf("expected zero drops, got %d", got)
	}
}

func TestRingBufferOverwriteOldest(t *testing.T) {
	rb := newRingBuffer(4)
	for i := 0; i < 10; i++ {
		rb.push(Event{Msg: idMsg(i)})
	}
	if got := rb.dropCount(); got != 6 {
		t.Fatalf("expected 6 drops, got %d", got)
	}
	go rb.close()
	out := make([]Event, 0, 4)
	out, _ = rb.drain(out)
	if len(out) != 4 {
		t.Fatalf("expected 4 events drained, got %d", len(out))
	}
	for i, ev := range out {
		want := idMsg(i + 6)
		if ev.Msg != want {
			t.Fatalf("position %d: expected %q, got %q", i, want, ev.Msg)
		}
	}
}

func TestRingBufferConcurrentPushers(t *testing.T) {
	const writers = 8
	const perWriter = 200
	rb := newRingBuffer(writers * perWriter)
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				rb.push(Event{Msg: idMsg(id*perWriter + i)})
			}
		}(w)
	}
	wg.Wait()
	if got := rb.dropCount(); got != 0 {
		t.Fatalf("expected no drops in sized buffer, got %d", got)
	}
	if got := rb.length(); got != writers*perWriter {
		t.Fatalf("expected %d buffered events, got %d", writers*perWriter, got)
	}
	go rb.close()
	out := make([]Event, 0, writers*perWriter)
	got := 0
	for {
		out, ok := rb.drain(out[:0:writers*perWriter])
		got += len(out)
		if !ok {
			break
		}
	}
	if got != writers*perWriter {
		t.Fatalf("expected to drain %d events, got %d", writers*perWriter, got)
	}
}

func TestRingBufferDrainBlocksUntilEvent(t *testing.T) {
	rb := newRingBuffer(4)
	var drained atomic.Int32
	out := make([]Event, 0, 4)
	go func() {
		out, _ := rb.drain(out)
		drained.Store(int32(len(out)))
	}()
	time.Sleep(20 * time.Millisecond)
	if drained.Load() != 0 {
		t.Fatalf("drain returned before any push")
	}
	rb.push(Event{Msg: "hello"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if drained.Load() == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("drain did not unblock after push")
}

func TestRingBufferCloseDrainsRemaining(t *testing.T) {
	rb := newRingBuffer(8)
	for i := 0; i < 3; i++ {
		rb.push(Event{Msg: idMsg(i)})
	}
	rb.close()
	rb.push(Event{Msg: "after-close"})
	out := make([]Event, 0, 8)
	out, ok := rb.drain(out)
	if !ok {
		t.Fatalf("expected first drain to return ok=true (3 buffered events)")
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 buffered events, got %d", len(out))
	}
	out, ok = rb.drain(out[:0:8])
	if ok {
		t.Fatalf("expected closed+empty drain to return ok=false")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty drain after close, got %d events", len(out))
	}
}

func idMsg(n int) string {
	return "ev-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
