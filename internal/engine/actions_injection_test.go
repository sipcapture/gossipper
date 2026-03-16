package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qxip/gossipper/internal/scenario"
	templ "github.com/qxip/gossipper/internal/template"
)

func TestParseCSVMutationSpec(t *testing.T) {
	t.Parallel()

	spec, err := parseCSVMutationSpec("line=2 field=1 text=hello position=prefix", templ.Context{})
	if err != nil {
		t.Fatalf("parseCSVMutationSpec() error = %v", err)
	}
	if spec.line != 2 || spec.field != 1 || spec.text != "hello" || spec.position != "prefix" {
		t.Fatalf("unexpected csv mutation spec: %+v", spec)
	}
}

func TestParseCSVMutationSpecRejectsMissingLine(t *testing.T) {
	t.Parallel()

	_, err := parseCSVMutationSpec("field=1 text=hello", templ.Context{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyActionsInsertReplaceMutateFieldLookup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	csvPath := filepath.Join(tempDir, "inject.csv")
	if err := os.WriteFile(csvPath, []byte("id,val\n1,abc\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	engine := New(Config{})
	store := newVarStore(newScopedVars(), nil, nil, 0)
	renderCtx := templ.Context{
		BasePath:          tempDir,
		CallNumber:        1,
		CSVFieldOverrides: make(map[string]map[int]map[int]string),
		Variables:         map[string]string{},
	}

	actions := []scenario.Action{
		{Type: scenario.ActionInsert, File: "inject.csv", Value: "line=2 field=1 text=_z"},
		{Type: scenario.ActionReplace, File: "inject.csv", Value: "line=2 field=1 text=done"},
	}
	if _, err := engine.applyActions(context.Background(), 1, actions, renderCtx, store, nil); err != nil {
		t.Fatalf("applyActions() error = %v", err)
	}

	got, err := templ.RenderMessageStrict("X: [field1 file=inject.csv line=2]\r\n\r\n", renderCtx)
	if err != nil {
		t.Fatalf("RenderMessageStrict() error = %v", err)
	}
	if !strings.Contains(got, "X: done") {
		t.Fatalf("expected replaced CSV field value, got %q", got)
	}
}
