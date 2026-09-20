package scenario

import (
	"strings"
	"testing"
)

func TestListBuiltins(t *testing.T) {
	list := ListBuiltins()
	if len(list) < 3 {
		t.Fatalf("expected at least 3 builtins, got %d", len(list))
	}
	if list[0].Source != "builtin" {
		t.Fatalf("expected source=builtin, got %q", list[0].Source)
	}
	var labs int
	seen := map[string]bool{}
	for _, b := range list {
		if seen[b.ID] {
			t.Fatalf("duplicate builtin id %q", b.ID)
		}
		seen[b.ID] = true
		if b.Source == "lab" {
			labs++
		}
	}
	if labs != len(labCatalog) {
		t.Fatalf("lab builtins: got %d want %d", labs, len(labCatalog))
	}
}

func TestBuiltinXML(t *testing.T) {
	xml, err := BuiltinXML("uac")
	if err != nil {
		t.Fatalf("BuiltinXML(uac): %v", err)
	}
	if !contains(xml, "<scenario") {
		t.Fatalf("expected scenario xml, got %q", xml[:min(40, len(xml))])
	}
	_, err = BuiltinXML("no-such-scenario")
	if err == nil {
		t.Fatal("expected error for unknown builtin")
	}
}

func TestLabBuiltinXMLParses(t *testing.T) {
	if len(labCatalog) != 26 {
		t.Fatalf("expected 26 kefir lab ids, got %d", len(labCatalog))
	}
	entries, err := labFS.ReadDir("lab")
	if err != nil {
		t.Fatalf("labFS.ReadDir: %v", err)
	}
	var xmlFiles int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".xml") {
			xmlFiles++
		}
	}
	if xmlFiles != len(labCatalog) {
		t.Fatalf("lab xml files: got %d want %d", xmlFiles, len(labCatalog))
	}
	for _, info := range labCatalog {
		raw, err := BuiltinXML(info.ID)
		if err != nil {
			t.Fatalf("BuiltinXML(%s): %v", info.ID, err)
		}
		sc, err := ParseString(raw)
		if err != nil {
			t.Fatalf("ParseString(%s): %v", info.ID, err)
		}
		if sc.Name == "" {
			t.Fatalf("%s: empty scenario name", info.ID)
		}
	}
	loaded, err := LoadNamed("one_way")
	if err != nil {
		t.Fatalf("LoadNamed(one_way): %v", err)
	}
	if loaded.Name != "one_way" {
		t.Fatalf("LoadNamed(one_way) name=%q", loaded.Name)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
