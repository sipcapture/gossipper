package shell

import (
	"bytes"
	"strings"
	"testing"
)

func TestSessionSetArgv(t *testing.T) {
	t.Parallel()
	s := newSession()
	if err := s.Set("sn", "uac"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("rsa", "10.0.0.1:5060"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("trace_msg", "true"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.Argv(), " ")
	want := "-sn uac -rsa 10.0.0.1:5060 -trace_msg"
	if got != want {
		t.Fatalf("Argv() = %q want %q", got, want)
	}
}

func TestSessionBoolUnset(t *testing.T) {
	t.Parallel()
	s := newSession()
	if err := s.Set("trace_msg", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("trace_msg", "false"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.Argv(), " ")
	if got != "-trace_msg=false" {
		t.Fatalf("Argv() = %q", got)
	}
	s.Unset("trace_msg")
	if len(s.Argv()) != 0 {
		t.Fatalf("after unset: %v", s.Argv())
	}
}

func TestWriteHintsEmptySession(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteHints(&out, newSession())
	s := out.String()
	if !strings.Contains(s, "Session is empty") {
		t.Fatalf("expected empty-session hint, got: %s", s)
	}
}

func TestShellHelpQuit(t *testing.T) {
	t.Parallel()
	in := strings.NewReader("help\nquit\n")
	var out, errOut bytes.Buffer
	if err := Run(in, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "wizard") {
		t.Fatalf("help output: %s", out.String())
	}
	if !strings.Contains(out.String(), "bye") {
		t.Fatalf("expected bye: %s", out.String())
	}
}
