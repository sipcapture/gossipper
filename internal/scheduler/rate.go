package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrStopped = errors.New("rate controller stopped")

type RateController struct {
	mu       sync.RWMutex
	rate     float64
	paused   bool
	stopped  bool
	updateCh chan struct{}
	// nextFire tracks the absolute time the next call should fire.
	// This allows the controller to catch up if the caller was delayed
	// (e.g., blocked on a semaphore), maintaining accurate average rate.
	nextFire time.Time
}

func NewRateController(rate float64) *RateController {
	return &RateController{
		rate:     sanitizeRate(rate),
		updateCh: make(chan struct{}),
		nextFire: time.Now(),
	}
}

func (r *RateController) Wait(ctx context.Context) error {
	for {
		r.mu.RLock()
		rate := r.rate
		paused := r.paused
		stopped := r.stopped
		updateCh := r.updateCh
		r.mu.RUnlock()

		if stopped {
			return ErrStopped
		}
		if paused {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-updateCh:
				continue
			}
		}

		period := rateToPeriod(rate)
		r.mu.Lock()
		// Advance nextFire by one period. If we fell behind (nextFire is
		// in the past), fire immediately to catch up to the target rate.
		// Cap catch-up to maxBurst periods to prevent unbounded bursts
		// after long semaphore blocks.
		if r.nextFire.IsZero() {
			r.nextFire = time.Now()
		}
		const maxBurst = 10
		earliest := time.Now().Add(-time.Duration(maxBurst) * period)
		if r.nextFire.Before(earliest) {
			r.nextFire = earliest
		}
		r.nextFire = r.nextFire.Add(period)
		target := r.nextFire
		r.mu.Unlock()

		wait := time.Until(target)
		if wait <= 0 {
			// Already past target time — fire immediately (catch-up).
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-updateCh:
			if !timer.Stop() {
				<-timer.C
			}
			continue
		case <-timer.C:
			return nil
		}
	}
}

func (r *RateController) Rate() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rate
}

func (r *RateController) SetRate(rate float64) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rate = sanitizeRate(rate)
	r.nextFire = time.Now() // reset schedule on rate change
	r.broadcastLocked()
	return r.rate
}

func (r *RateController) AdjustRate(delta float64) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rate = sanitizeRate(r.rate + delta)
	r.nextFire = time.Now() // reset schedule on rate change
	r.broadcastLocked()
	return r.rate
}

func (r *RateController) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = true
	r.broadcastLocked()
}

func (r *RateController) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = false
	r.nextFire = time.Now() // reset schedule on resume to avoid catch-up burst
	r.broadcastLocked()
}

func (r *RateController) Paused() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.paused
}

func (r *RateController) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	r.broadcastLocked()
}

func (r *RateController) broadcastLocked() {
	close(r.updateCh)
	r.updateCh = make(chan struct{})
}

func sanitizeRate(rate float64) float64 {
	if rate <= 0 {
		return 1.0
	}
	return rate
}

func rateToPeriod(rate float64) time.Duration {
	period := time.Duration(float64(time.Second) / sanitizeRate(rate))
	if period <= 0 {
		return time.Nanosecond
	}
	return period
}
