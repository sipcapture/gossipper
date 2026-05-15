package engine

import (
	"strings"
	"testing"

	"github.com/sipcapture/gossipper/internal/scenario"
)

func TestSnapshotLiveFirstRecvTracksTryReplace(t *testing.T) {
	t.Parallel()
	base := `<?xml version="1.0"?><scenario name="a"><recv request="OPTIONS" crlf="true"/></scenario>`
	scA, err := scenario.ParseString(base)
	if err != nil {
		t.Fatal(err)
	}
	eng := New(Config{
		Scenario:      scA,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     5060,
		RemoteHost:    "127.0.0.1",
		RemotePort:    9,
		DefaultRecvTO: 1,
	})
	cmd0, ok := eng.snapshotLiveFirstRecvCommand()
	if !ok || cmd0.RecvReq != "OPTIONS" {
		t.Fatalf("first recv: ok=%v req=%q", ok, cmd0.RecvReq)
	}

	scB, err := scenario.ParseString(strings.Replace(base, `OPTIONS`, `INVITE`, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.TryReplaceLiveScenario(scB); err != nil {
		t.Fatal(err)
	}
	cmd1, ok := eng.snapshotLiveFirstRecvCommand()
	if !ok || cmd1.RecvReq != "INVITE" {
		t.Fatalf("after replace: ok=%v req=%q", ok, cmd1.RecvReq)
	}
}
