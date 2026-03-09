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
}

func NewRateController(rate float64) *RateController {
	return &RateController{
		rate:     sanitizeRate(rate),
		updateCh: make(chan struct{}),
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

		timer := time.NewTimer(rateToPeriod(rate))
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
	r.broadcastLocked()
	return r.rate
}

func (r *RateController) AdjustRate(delta float64) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rate = sanitizeRate(r.rate + delta)
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
