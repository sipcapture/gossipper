package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/qxip/gossipper/internal/scenario"
	templ "github.com/qxip/gossipper/internal/template"
)

func TestParseSampleSpec(t *testing.T) {
	t.Parallel()

	spec, err := parseSampleSpec("min=10 max=20 step=5 seed=42", templ.Context{})
	if err != nil {
		t.Fatalf("parseSampleSpec() error = %v", err)
	}
	if spec.min != 10 || spec.max != 20 || spec.step != 5 || spec.seed == nil || *spec.seed != 42 {
		t.Fatalf("unexpected sample spec: %+v", spec)
	}
}

func TestParseSampleSpecRejectsInvalidRange(t *testing.T) {
	t.Parallel()

	_, err := parseSampleSpec("min=20 max=10", templ.Context{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "max") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyActionsSampleDeterministicWithSeed(t *testing.T) {
	t.Parallel()

	engine := New(Config{})
	vars := newVarStore(newScopedVars(), nil, nil, 0)

	actions := []scenario.Action{
		{
			Type:     scenario.ActionSample,
			AssignTo: []string{"picked"},
			Value:    "min=10 max=20 step=5 seed=17",
		},
	}

	_, err := engine.applyActions(context.Background(), 1, actions, templ.Context{}, vars, nil)
	if err != nil {
		t.Fatalf("applyActions(sample) error = %v", err)
	}
	first := vars.Get("picked")
	if first != "10" && first != "15" && first != "20" {
		t.Fatalf("unexpected sampled value %q", first)
	}

	vars2 := newVarStore(newScopedVars(), nil, nil, 0)
	_, err = engine.applyActions(context.Background(), 1, actions, templ.Context{}, vars2, nil)
	if err != nil {
		t.Fatalf("applyActions(sample) second run error = %v", err)
	}
	if got := vars2.Get("picked"); got != first {
		t.Fatalf("expected deterministic seed result, first=%q second=%q", first, got)
	}
}

func TestApplyActionsSampleSupportsTemplatedSpec(t *testing.T) {
	t.Parallel()

	engine := New(Config{})
	vars := newVarStore(newScopedVars(), nil, nil, 0)
	vars.Set("min", "1")
	vars.Set("max", "5")
	vars.Set("step", "2")
	vars.Set("seed", "99")

	actions := []scenario.Action{
		{
			Type:     scenario.ActionSample,
			AssignTo: []string{"picked"},
			Value:    "min=[$min] max=[$max] step=[$step] seed=[$seed]",
		},
	}
	_, err := engine.applyActions(context.Background(), 1, actions, templ.Context{}, vars, nil)
	if err != nil {
		t.Fatalf("applyActions(sample templated) error = %v", err)
	}
	got := vars.Get("picked")
	if got != "1" && got != "3" && got != "5" {
		t.Fatalf("unexpected templated sampled value %q", got)
	}
}
