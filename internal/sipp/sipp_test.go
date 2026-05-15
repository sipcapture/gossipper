package sipp_test

import (
	"bytes"
	"testing"

	"github.com/sipcapture/gossipper/internal/sipp"
)

func TestPrintUsage(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sipp.PrintUsage(&buf)
	if buf.Len() < 100 {
		t.Fatalf("usage too short: %d bytes", buf.Len())
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

func TestRunHelpDoesNotForward(t *testing.T) {
	t.Parallel()
	called := false
	err := sipp.Run([]string{"help"}, func([]string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("forward should not run for help")
	}
}
