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

func TestSessionReadableDestinationSplit(t *testing.T) {
	t.Parallel()
	s := newSession()
	if err := s.Set("destination_host", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("destination_port", "5088"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.Argv(), " ")
	want := "-rsa 10.0.0.2:5088"
	if got != want {
		t.Fatalf("Argv() = %q want %q", got, want)
	}
}

func TestSessionDestinationHostDefaultPort(t *testing.T) {
	t.Parallel()
	s := newSession()
	if err := s.Set("destination_host", "10.0.0.3"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.Argv(), " ")
	want := "-rsa 10.0.0.3:5060"
	if got != want {
		t.Fatalf("Argv() = %q want %q", got, want)
	}
}

func TestSessionRSAOverridesSplitDestination(t *testing.T) {
	t.Parallel()
	s := newSession()
	_ = s.Set("destination_host", "10.0.0.2")
	_ = s.Set("destination_port", "5060")
	if err := s.Set("rsa", "192.0.2.1:5099"); err != nil {
		t.Fatal(err)
	}
	if s.destHost != "" || s.destPort != "" {
		t.Fatalf("expected split destination cleared, got host=%q port=%q", s.destHost, s.destPort)
	}
	if strings.Join(s.Argv(), " ") != "-rsa 192.0.2.1:5099" {
		t.Fatalf("Argv() = %q", strings.Join(s.Argv(), " "))
	}
}

func TestSessionReadableAliasesRoundTripArgv(t *testing.T) {
	t.Parallel()
	s := newSession()
	if err := s.Set("builtin_scenario", "uac"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("destination", "1.1.1.1:5060"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("local_bind_ip", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s.Argv(), " ")
	for _, frag := range []string{"-sn uac", "-rsa 1.1.1.1:5060", "-i 10.0.0.1"} {
		if !strings.Contains(got, frag) {
			t.Fatalf("Argv() = %q, missing %q", got, frag)
		}
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
