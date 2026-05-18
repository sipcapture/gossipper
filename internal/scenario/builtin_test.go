package scenario

import "testing"

func TestListBuiltins(t *testing.T) {
	list := ListBuiltins()
	if len(list) < 3 {
		t.Fatalf("expected at least 3 builtins, got %d", len(list))
	}
	if list[0].Source != "builtin" {
		t.Fatalf("expected source=builtin, got %q", list[0].Source)
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
