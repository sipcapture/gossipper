package tui

import "testing"

func TestBumpRateUsesScaleAndSteps(t *testing.T) {
	t.Parallel()

	rate := bumpRate(10.0, 2.5, +1)
	if rate != 12.5 {
		t.Fatalf("expected 12.5, got %.2f", rate)
	}

	rate = bumpRate(10.0, 2.5, -10)
	if rate != 0.1 {
		t.Fatalf("expected floor 0.1, got %.2f", rate)
	}
}

func TestBumpRateDefaultsScaleToOne(t *testing.T) {
	t.Parallel()

	rate := bumpRate(5.0, 0, +1)
	if rate != 6.0 {
		t.Fatalf("expected 6.0, got %.2f", rate)
	}
}
