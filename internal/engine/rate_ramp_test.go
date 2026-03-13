package engine

import (
	"context"
	"testing"
	"time"

	"github.com/adubovikov/gossipper/internal/scenario"
)

func TestStartRateRampLoopIncreasesRateUpToMax(t *testing.T) {
	t.Parallel()

	e := New(Config{
		Scenario:         scenario.Scenario{Mode: scenario.ModeClient},
		Rate:             10,
		RateIncrease:     2,
		RateIncreaseStep: 5 * time.Millisecond,
		RateMax:          13,
	})
	ctx, cancel := context.WithCancel(context.Background())
	stop := e.startRateRampLoop(ctx)
	t.Cleanup(func() {
		cancel()
		stop()
	})

	time.Sleep(40 * time.Millisecond)
	if got := e.Rate(); got != 13 {
		t.Fatalf("expected capped rate 13, got %f", got)
	}
}

func TestStartRateRampLoopDisabledWhenIncreaseIsZero(t *testing.T) {
	t.Parallel()

	e := New(Config{
		Scenario:         scenario.Scenario{Mode: scenario.ModeClient},
		Rate:             10,
		RateIncrease:     0,
		RateIncreaseStep: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	stop := e.startRateRampLoop(ctx)
	t.Cleanup(func() {
		cancel()
		stop()
	})

	time.Sleep(20 * time.Millisecond)
	if got := e.Rate(); got != 10 {
		t.Fatalf("expected unchanged rate 10, got %f", got)
	}
}
