package sipp_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/sipcapture/gossipper/internal/sipp"
)

func TestPrintUsage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sipp.PrintUsage(&buf)
	text := buf.String()
	if buf.Len() < 100 {
		t.Fatalf("usage too short: %d bytes", buf.Len())
	}
	if !strings.Contains(text, "SIPp-style") {
		t.Fatalf("usage should describe SIPp-style entry: %q", text)
	}
}

func TestRunForwardsWhenArgsPresent(t *testing.T) {
	t.Parallel()
	var got []string
	err := sipp.Run([]string{"-sn", "uac"}, func(argv []string) error {
		got = append([]string(nil), argv...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "-sn" || got[1] != "uac" {
		t.Fatalf("forwarded argv = %#v", got)
	}
}

func TestRunHelpForwardsToFullHelpArgv(t *testing.T) {
	t.Parallel()
	for _, helpTok := range []string{"help", "-h", "--help"} {
		helpTok := helpTok
		t.Run(helpTok, func(t *testing.T) {
			t.Parallel()
			var got []string
			err := sipp.Run([]string{helpTok, "-sn", "uac"}, func(argv []string) error {
				got = append([]string(nil), argv...)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"-h", "-sn", "uac"}
			if len(got) != len(want) {
				t.Fatalf("forward argv = %#v", got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("forward argv = %#v", got)
				}
			}
		})
	}
}

func TestRunRejectsGossipperSubcommand(t *testing.T) {
	t.Parallel()
	called := false
	err := sipp.Run([]string{"tui"}, func([]string) error {
		called = true
		return nil
	})
	if !errors.Is(err, sipp.ErrRootSubcommandAfterSipp) {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("forward should not run")
	}
}

func TestRunRejectsInteractiveFlag(t *testing.T) {
	t.Parallel()
	called := false
	err := sipp.Run([]string{"-interactive"}, func([]string) error {
		called = true
		return nil
	})
	if !errors.Is(err, sipp.ErrRootSubcommandAfterSipp) {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("forward should not run")
	}
}

func TestRunStripsLeadingSippTokens(t *testing.T) {
	t.Parallel()
	var got []string
	err := sipp.Run([]string{"sipp", "sipp", "-sn", "uac"}, func(argv []string) error {
		got = append([]string(nil), argv...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-sn", "uac"}
	if len(got) != len(want) {
		t.Fatalf("forwarded argv = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("forwarded argv = %#v", got)
		}
	}
}
