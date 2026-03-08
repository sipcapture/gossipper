package scenario

import "testing"

func TestParseScenarioWithLabels(t *testing.T) {
	t.Parallel()

	sc, err := ParseFile("../../testdata/scenarios/branching.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if sc.Name != "Branching test" {
		t.Fatalf("unexpected scenario name %q", sc.Name)
	}
	if len(sc.Commands) == 0 {
		t.Fatal("expected commands")
	}
	if got := sc.Labels["failure"]; got == 0 {
		t.Fatalf("expected label index, got %d", got)
	}
	if sc.Commands[1].NextIndex != sc.Labels["failure"] {
		t.Fatalf("expected next index %d, got %d", sc.Labels["failure"], sc.Commands[1].NextIndex)
	}
}

func TestParseScenarioActions(t *testing.T) {
	t.Parallel()

	sc, err := ParseFile("../../testdata/scenarios/actions_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(sc.Commands) < 2 {
		t.Fatalf("expected commands, got %d", len(sc.Commands))
	}
	actions := sc.Commands[1].Actions
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}
	if actions[0].Type != ActionEReg || len(actions[0].AssignTo) != 2 {
		t.Fatalf("unexpected first action: %+v", actions[0])
	}
}

func TestParseScenarioInitAndScopes(t *testing.T) {
	t.Parallel()

	sc, err := ParseFile("../../testdata/scenarios/init_injection_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if len(sc.InitCommands) != 1 {
		t.Fatalf("expected 1 init command, got %d", len(sc.InitCommands))
	}
	if len(sc.GlobalVariables) != 2 || len(sc.UserVariables) != 1 || len(sc.References) != 3 {
		t.Fatalf("unexpected scope declarations: %+v %+v %+v", sc.GlobalVariables, sc.UserVariables, sc.References)
	}
}

func TestParseScenarioSendCmdRecvCmd(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="cmd">
  <sendCmd dest="s1"><![CDATA[Call-ID: [call_id]
X-Value: abc
]]></sendCmd>
  <recvCmd src="s1" timeout="250">
    <action>
      <ereg regexp="X-Value:\s*(.*)" search_in="msg" assign_to="1"/>
    </action>
  </recvCmd>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(sc.Commands))
	}
	if sc.Commands[0].Type != CommandSendCmd || sc.Commands[0].CmdDest != "s1" {
		t.Fatalf("unexpected sendCmd: %+v", sc.Commands[0])
	}
	if sc.Commands[1].Type != CommandRecvCmd || sc.Commands[1].CmdSrc != "s1" || sc.Commands[1].Timeout.Milliseconds() != 250 {
		t.Fatalf("unexpected recvCmd: %+v", sc.Commands[1])
	}
}

func TestParseScenarioPlayPCAPAudioAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="pcap">
  <nop>
    <action>
      <exec play_pcap_audio="pcap/g711a.pcap"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 1 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	if sc.Commands[0].Actions[0].Type != ActionExec || sc.Commands[0].Actions[0].PlayPCAPAudio != "pcap/g711a.pcap" {
		t.Fatalf("unexpected play_pcap_audio action: %+v", sc.Commands[0].Actions[0])
	}
}
