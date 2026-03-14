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

func TestParseScenarioLookupAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="lookup">
  <nop>
    <action>
      <lookup assign_to="line" file="../injection/inject.csv" key="2"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 1 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	action := sc.Commands[0].Actions[0]
	if action.Type != ActionLookup || action.File != "../injection/inject.csv" || action.Key != "2" || len(action.AssignTo) != 1 || action.AssignTo[0] != "line" {
		t.Fatalf("unexpected lookup action: %+v", action)
	}
}

func TestParseScenarioStrCmpAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="strcmp">
  <nop>
    <action>
      <strcmp assign_to="cmp" variable="left" variable2="right"/>
      <test assign_to="ok" variable="cmp" compare="greater_than_equal" value="0"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 2 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	if sc.Commands[0].Actions[0].Type != ActionStrCmp || sc.Commands[0].Actions[0].Variable != "left" || sc.Commands[0].Actions[0].Variable2 != "right" {
		t.Fatalf("unexpected strcmp action: %+v", sc.Commands[0].Actions[0])
	}
	if sc.Commands[0].Actions[1].Type != ActionTest || sc.Commands[0].Actions[1].Compare != "greater_than_equal" {
		t.Fatalf("unexpected test action: %+v", sc.Commands[0].Actions[1])
	}
}

func TestParseScenarioArithmeticAndVerifyAuthActions(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="math-auth">
  <nop>
    <action>
      <assign assign_to="n" value="1"/>
      <todouble assign_to="m" variable="n"/>
      <add assign_to="m" value="2"/>
      <subtract assign_to="m" value="1"/>
      <multiply assign_to="m" value="4"/>
      <divide assign_to="m" value="2"/>
      <jump value="3"/>
      <gettimeofday assign_to="sec,usec"/>
      <urlencode variable="uri"/>
      <urldecode variable="uri"/>
      <verifyauth assign_to="ok" username="alice" password="secret"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 11 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
}

func TestParseScenarioRejectsUnsupportedAction(t *testing.T) {
	t.Parallel()

	_, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="bad">
  <nop>
    <action>
      <frobnicate assign_to="1"/>
    </action>
  </nop>
</scenario>`)
	if err == nil {
		t.Fatal("expected unsupported action error")
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

func TestParseScenarioPlayPCAPVideoAndImageAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="pcap-media">
  <nop>
    <action>
      <exec play_pcap_video="pcap/video.pcap"/>
      <exec play_pcap_image="pcap/image.pcap"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 2 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	if sc.Commands[0].Actions[0].Type != ActionExec || sc.Commands[0].Actions[0].PlayPCAPVideo != "pcap/video.pcap" {
		t.Fatalf("unexpected play_pcap_video action: %+v", sc.Commands[0].Actions[0])
	}
	if sc.Commands[0].Actions[1].Type != ActionExec || sc.Commands[0].Actions[1].PlayPCAPImage != "pcap/image.pcap" {
		t.Fatalf("unexpected play_pcap_image action: %+v", sc.Commands[0].Actions[1])
	}
}

func TestParseScenarioRTPCheckAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="rtpcheck">
  <nop>
    <action>
      <exec rtpcheck="min_packets=2 timeout_ms=500 bidirectional=1"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 1 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	action := sc.Commands[0].Actions[0]
	if action.Type != ActionExec || action.RTPCheck != "min_packets=2 timeout_ms=500 bidirectional=1" {
		t.Fatalf("unexpected rtpcheck action: %+v", action)
	}
}

func TestParseScenarioWarningAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="warning">
  <nop>
    <action>
      <warning message="call [$1] needs attention"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 1 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	if sc.Commands[0].Actions[0].Type != ActionWarning || sc.Commands[0].Actions[0].Message != "call [$1] needs attention" {
		t.Fatalf("unexpected warning action: %+v", sc.Commands[0].Actions[0])
	}
}

func TestParseScenarioSetDestAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="setdest">
  <nop>
    <action>
      <setdest host="[$host]" port="[$port]" protocol="[$transport]"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 1 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	action := sc.Commands[0].Actions[0]
	if action.Type != ActionSetDest || action.Host != "[$host]" || action.Port != "[$port]" || action.Protocol != "[$transport]" {
		t.Fatalf("unexpected setdest action: %+v", action)
	}
}

func TestParseScenarioSampleAction(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="sample">
  <nop>
    <action>
      <sample assign_to="picked" value="min=10 max=20 step=5 seed=7"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 1 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	action := sc.Commands[0].Actions[0]
	if action.Type != ActionSample || action.Value != "min=10 max=20 step=5 seed=7" {
		t.Fatalf("unexpected sample action: %+v", action)
	}
	if len(action.AssignTo) != 1 || action.AssignTo[0] != "picked" {
		t.Fatalf("unexpected sample assign_to: %+v", action.AssignTo)
	}
}

func TestParseScenarioInsertReplaceActions(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="insert-replace">
  <nop>
    <action>
      <insert file="inject.csv" value="line=2 field=1 text=_x"/>
      <replace file="inject.csv" value="line=2 field=1 text=hello"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 || len(sc.Commands[0].Actions) != 2 {
		t.Fatalf("unexpected parsed scenario: %+v", sc.Commands)
	}
	if sc.Commands[0].Actions[0].Type != ActionInsert {
		t.Fatalf("expected insert action, got %+v", sc.Commands[0].Actions[0])
	}
	if sc.Commands[0].Actions[1].Type != ActionReplace {
		t.Fatalf("expected replace action, got %+v", sc.Commands[0].Actions[1])
	}
}

func TestParseScenarioRecvRRSAttribute(t *testing.T) {
	t.Parallel()

	sc, err := ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="rrs">
  <recv response="200" rrs="true"/>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	if len(sc.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(sc.Commands))
	}
	if !sc.Commands[0].RRS {
		t.Fatalf("expected rrs=true for recv command: %+v", sc.Commands[0])
	}
}
