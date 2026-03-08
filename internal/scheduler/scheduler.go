package scheduler

import (
	"context"
	"time"
)

type Scheduler interface {
	Sleep(context.Context, time.Duration) error
	Interval(float64) <-chan time.Time
}

type Real struct{}

func New() Real {
	return Real{}
}

func (Real) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (Real) Interval(rate float64) <-chan time.Time {
	if rate <= 0 {
		ch := make(chan time.Time)
		close(ch)
		return ch
	}
	period := time.Duration(float64(time.Second) / rate)
	if period <= 0 {
		period = time.Nanosecond
	}
	ticker := time.NewTicker(period)
	return ticker.C
}
