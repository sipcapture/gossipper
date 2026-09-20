package engine

import (
	"strings"
	"testing"

	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/sip"
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

func TestArmEarly183ScenarioForCall(t *testing.T) {
	t.Parallel()
	uas, err := scenario.LoadNamed("uas")
	if err != nil {
		t.Fatal(err)
	}
	eng := New(Config{
		Scenario:      uas,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     5060,
		RemoteHost:    "127.0.0.1",
		RemotePort:    9,
		DefaultRecvTO: 1,
	})
	if eng.ScenarioForCall().Name != uas.Name {
		t.Fatalf("default live=%q want %q", eng.ScenarioForCall().Name, uas.Name)
	}
	early, err := scenario.LoadNamed("early_183")
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.TryReplaceLiveScenario(early); err != nil {
		t.Fatal(err)
	}
	got := eng.ScenarioForCall()
	if got.Name != early.Name {
		t.Fatalf("after arm live=%q want %q", got.Name, early.Name)
	}
}

func TestScenarioForInviteUsesResolver(t *testing.T) {
	t.Parallel()
	uas, err := scenario.LoadNamed("uas")
	if err != nil {
		t.Fatal(err)
	}
	early, err := scenario.LoadNamed("early_183")
	if err != nil {
		t.Fatal(err)
	}
	eng := New(Config{
		Scenario:      uas,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     5060,
		RemoteHost:    "127.0.0.1",
		RemotePort:    9,
		DefaultRecvTO: 1,
	})
	eng.SetInviteScenarioResolver(func(user string) (scenario.Scenario, bool) {
		if user == "1002" {
			return early, true
		}
		return scenario.Scenario{}, false
	})
	hit := eng.ScenarioForInvite("sip:1002@192.168.1.20:5060")
	if hit.Name != early.Name {
		t.Fatalf("resolver hit=%q want %q", hit.Name, early.Name)
	}
	miss := eng.ScenarioForInvite("sip:1001@192.168.1.20:5060")
	if miss.Name != uas.Name {
		t.Fatalf("fallback=%q want %q", miss.Name, uas.Name)
	}
	_ = sip.RequestURIUser
}

func TestTryAutoAnswerOPTIONS(t *testing.T) {
	t.Parallel()
	uas, err := scenario.LoadNamed("uas")
	if err != nil {
		t.Fatal(err)
	}
	eng := New(Config{Scenario: uas, Transport: "u1", LocalIP: "127.0.0.1", LocalPort: 5060, RemoteHost: "127.0.0.1", RemotePort: 9})
	opt, err := sip.Parse([]byte(
		"OPTIONS sip:1001@127.0.0.1:5060 SIP/2.0\r\nVia: SIP/2.0/UDP 127.0.0.1:9;branch=z9hG4bK1\r\nFrom: <sip:pbx@pbx.local>;tag=a\r\nTo: <sip:1001@127.0.0.1>\r\nCall-ID: opt1\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.tryAutoAnswerOPTIONS(opt); ok {
		t.Fatal("expected no auto-answer when flag off")
	}
	eng.SetAutoAnswerOPTIONS(true)
	payload, ok := eng.tryAutoAnswerOPTIONS(opt)
	if !ok || !strings.Contains(string(payload), "200 OK") {
		t.Fatalf("auto-answer ok=%v payload=%s", ok, payload)
	}
}
