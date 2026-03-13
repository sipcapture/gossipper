package tui

import (
	"strings"
	"testing"
)

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

func TestBuildArgsIncludesUIInfSettings(t *testing.T) {
	t.Parallel()

	args, err := buildArgs(
		profile{Name: "builtin-uac", ScenarioName: "uac"},
		"client",
		"ui",
		"127.0.0.1:5060",
		"127.0.0.1",
		"0",
		"service",
		"1",
		"1",
		"1",
		"1",
		"1",
		"",
		"",
		"",
		"",
		"/tmp/ui.csv",
		"2",
		false,
		false,
		false,
		false,
	)
	if err != nil {
		t.Fatalf("buildArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-t ui") || !strings.Contains(joined, "-inf /tmp/ui.csv") || !strings.Contains(joined, "-ip_field 2") {
		t.Fatalf("unexpected args for ui mode: %v", args)
	}
}

func TestBuildArgsRejectsUIWithoutInf(t *testing.T) {
	t.Parallel()

	_, err := buildArgs(
		profile{Name: "builtin-uac", ScenarioName: "uac"},
		"client",
		"ui",
		"127.0.0.1:5060",
		"127.0.0.1",
		"0",
		"service",
		"1",
		"1",
		"1",
		"1",
		"1",
		"",
		"",
		"",
		"",
		"",
		"0",
		false,
		false,
		false,
		false,
	)
	if err == nil {
		t.Fatal("expected error when ui transport has no inf path")
	}
}

func TestBuildArgsServerKeepsUITransport(t *testing.T) {
	t.Parallel()

	args, err := buildArgs(
		profile{Name: "builtin-uas", ScenarioName: "uas"},
		"server",
		"ui",
		"",
		"127.0.0.1",
		"0",
		"service",
		"1",
		"1",
		"1",
		"1",
		"1",
		"",
		"",
		"",
		"",
		"/tmp/ui.csv",
		"0",
		false,
		false,
		false,
		false,
	)
	if err != nil {
		t.Fatalf("buildArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-t ui") {
		t.Fatalf("expected server ui transport to remain ui, got args: %v", args)
	}
}
