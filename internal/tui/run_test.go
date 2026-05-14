package tui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
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
		false,
		true,
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

func TestBuildArgsUIInfWithoutIPFieldOmitsIPFieldFlag(t *testing.T) {
	t.Parallel()

	args, err := buildArgs(
		profile{Name: "builtin-uac", ScenarioName: "uac"},
		"client",
		"ui",
		"127.0.0.1:5060",
		"10.0.0.5",
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
		false,
		true,
		"/tmp/ui.csv",
		"",
		false,
		false,
		false,
		false,
	)
	if err != nil {
		t.Fatalf("buildArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-t ui") || !strings.Contains(joined, "-inf /tmp/ui.csv") {
		t.Fatalf("unexpected args: %v", args)
	}
	if strings.Contains(joined, "-ip_field") {
		t.Fatalf("expected no -ip_field when empty, got: %v", args)
	}
	if !strings.Contains(joined, "-i 10.0.0.5") {
		t.Fatalf("expected local bind -i, got: %v", args)
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
		false,
		true,
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
		false,
		true,
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

func TestBuildArgsServerMapsL1ToSLAlias(t *testing.T) {
	t.Parallel()

	args, err := buildArgs(
		profile{Name: "builtin-uas", ScenarioName: "uas"},
		"server",
		"l1",
		"",
		"127.0.0.1",
		"5061",
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
		false,
		true,
		"",
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
	if !strings.Contains(joined, "-t sl") {
		t.Fatalf("expected server l1 to emit -t sl, got: %v", args)
	}
}

func TestBuildArgsClientMapsTLSModesToCLAliases(t *testing.T) {
	t.Parallel()

	args, err := buildArgs(
		profile{Name: "builtin-uac", ScenarioName: "uac"},
		"client",
		"l1",
		"127.0.0.1:5061",
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
		false,
		true,
		"",
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
	if !strings.Contains(joined, "-t cl") {
		t.Fatalf("expected client l1 to emit -t cl, got: %v", args)
	}

	args, err = buildArgs(
		profile{Name: "builtin-uac", ScenarioName: "uac"},
		"client",
		"ln",
		"127.0.0.1:5061",
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
		false,
		true,
		"",
		"0",
		false,
		false,
		false,
		false,
	)
	if err != nil {
		t.Fatalf("buildArgs(ln) error = %v", err)
	}
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "-t cln") {
		t.Fatalf("expected client ln to emit -t cln, got: %v", args)
	}
}

func TestBuildArgsIncludesHEPMediaFlags(t *testing.T) {
	t.Parallel()

	args, err := buildArgs(
		profile{Name: "builtin-uac", ScenarioName: "uac"},
		"client",
		"u1",
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
		"127.0.0.1:9060",
		true,
		false,
		"",
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
	if !strings.Contains(joined, "-hep_addr 127.0.0.1:9060") {
		t.Fatalf("missing hep_addr: %v", args)
	}
	if !strings.Contains(joined, "-send_media_report") {
		t.Fatalf("missing send_media_report: %v", args)
	}
	if !strings.Contains(joined, "-hep_raw_rtcp=false") {
		t.Fatalf("missing hep_raw_rtcp=false: %v", args)
	}
}

func TestCurrentProfileUsesActiveOption(t *testing.T) {
	t.Parallel()

	profiles := []profile{
		{Name: "builtin-uac"},
		{Name: "builtin-uas"},
		{Name: "custom-xml"},
	}

	dropdown := tview.NewDropDown()
	dropdown.SetOptions([]string{"builtin-uac", "builtin-uas", "custom-xml"}, nil)
	dropdown.SetCurrentOption(2)

	selected, err := currentProfile(dropdown, profiles)
	if err != nil {
		t.Fatalf("currentProfile() error = %v", err)
	}
	if selected.Name != "custom-xml" {
		t.Fatalf("expected custom-xml, got %q", selected.Name)
	}
}

func TestCurrentProfileErrorsOnOutOfRange(t *testing.T) {
	t.Parallel()

	dropdown := tview.NewDropDown()
	dropdown.SetOptions([]string{"builtin-uac"}, nil)
	dropdown.SetCurrentOption(0)

	_, err := currentProfile(dropdown, []profile{})
	if err == nil {
		t.Fatal("expected error for empty profile list")
	}
}
