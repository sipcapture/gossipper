package engine

import (
	"context"
	"sync"
)

// dynSemaphore is a concurrency limiter whose capacity can be changed at runtime.
// Callers blocked in Acquire are re-evaluated whenever the limit is raised or a
// slot is released.
type dynSemaphore struct {
	mu     sync.Mutex
	cond   *sync.Cond
	active int
	limit  int
}

func newDynSemaphore(limit int) *dynSemaphore {
	if limit < 1 {
		limit = 1
	}
	s := &dynSemaphore{limit: limit}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire blocks until a slot is available or ctx is cancelled.
func (s *dynSemaphore) Acquire(ctx context.Context) error {
	// Wake all waiters when ctx is done so they can check ctx.Err().
	stop := context.AfterFunc(ctx, func() { s.cond.Broadcast() })
	defer stop()

	s.mu.Lock()
	defer s.mu.Unlock()
	for s.active >= s.limit {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.cond.Wait()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.active++
	return nil
}

// Release returns a slot and wakes one waiting caller.
func (s *dynSemaphore) Release() {
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	s.cond.Signal()
}

// Resize updates the concurrency limit. Blocked Acquire calls are re-evaluated
// so that newly available slots are filled immediately.
func (s *dynSemaphore) Resize(limit int) {
	if limit < 1 {
		limit = 1
	}
	s.mu.Lock()
	s.limit = limit
	s.mu.Unlock()
	s.cond.Broadcast()
}

// Limit returns the current concurrency limit.
func (s *dynSemaphore) Limit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}
