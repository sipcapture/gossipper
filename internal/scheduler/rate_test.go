package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRateControllerWaitRespectsRateUpdates(t *testing.T) {
	t.Parallel()

	controller := NewRateController(20)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	if err := controller.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	firstDelay := time.Since(start)
	if firstDelay < 30*time.Millisecond {
		t.Fatalf("expected initial wait to respect 20 cps, got %v", firstDelay)
	}

	controller.SetRate(200)
	start = time.Now()
	if err := controller.Wait(ctx); err != nil {
		t.Fatalf("Wait() after SetRate error = %v", err)
	}
	secondDelay := time.Since(start)
	if secondDelay > 40*time.Millisecond {
		t.Fatalf("expected updated wait to shorten after rate change, got %v", secondDelay)
	}
}

func TestRateControllerPauseResumeAndStop(t *testing.T) {
	t.Parallel()

	controller := NewRateController(100)
	controller.Pause()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	waitResult := make(chan error, 1)
	go func() {
		waitResult <- controller.Wait(ctx)
	}()

	select {
	case err := <-waitResult:
		t.Fatalf("Wait() returned while paused: %v", err)
	case <-time.After(60 * time.Millisecond):
	}

	controller.Resume()

	select {
	case err := <-waitResult:
		if err != nil {
			t.Fatalf("Wait() after Resume error = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Wait() did not resume after Resume()")
	}

	controller.Pause()
	stopResult := make(chan error, 1)
	go func() {
		stopResult <- controller.Wait(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	controller.Stop()

	select {
	case err := <-stopResult:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("Wait() after Stop error = %v, want %v", err, ErrStopped)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Wait() did not exit after Stop()")
	}
}
