package uistore

import (
	"strings"
	"testing"
)

func TestPreprocessScenarioXML(t *testing.T) {
	s := newTestStore(t)
	in := `<scenario><play_pcap_audio>[[media:wav/ringback]]</play_pcap_audio></scenario>`
	out, errs := s.PreprocessScenarioXML(in)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !strings.Contains(out, "/media/wav/ringback") {
		t.Fatalf("substitution missing: %q", out)
	}
	if strings.Contains(out, "[[media:") {
		t.Fatalf("placeholder still present: %q", out)
	}
}

func TestPreprocessScenarioXMLNoop(t *testing.T) {
	s := newTestStore(t)
	in := `<scenario><send><![CDATA[INVITE]]></send></scenario>`
	out, errs := s.PreprocessScenarioXML(in)
	if len(errs) != 0 || out != in {
		t.Fatalf("unexpected change: out=%q errs=%v", out, errs)
	}
}

func TestPreprocessScenarioXMLInvalidName(t *testing.T) {
	s := newTestStore(t)
	in := `<x>[[media:wav/../etc/passwd]]</x>`
	out, errs := s.PreprocessScenarioXML(in)
	if strings.Contains(out, "..") && !strings.Contains(out, "[[media:wav/../etc/passwd]]") {
		t.Fatalf("path traversal should have been rejected, got %q", out)
	}
	_ = errs // we don't care whether it's an error or a no-op, only that traversal failed
}
