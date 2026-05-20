package scenario

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sipcapture/gossipper/internal/distribution"
	"golang.org/x/text/encoding/charmap"
)

func ErrUnknownScenario(name string) error {
	return fmt.Errorf("unknown built-in scenario %q", name)
}

// rawDistributionAttrs holds XML attributes for pause distribution configuration.
type rawDistributionAttrs struct {
	Distribution string `xml:"distribution,attr"`
	Mean         string `xml:"mean,attr"`
	Stdev        string `xml:"stdev,attr"`
	Min          string `xml:"min,attr"`
	Max          string `xml:"max,attr"`
}

type rawScenario struct {
	XMLName  xml.Name          `xml:"scenario"`
	Name     string            `xml:"name,attr"`
	WebRTC   string            `xml:"webrtc,attr"`
	Elements []rawScenarioItem `xml:",any"`
}

type rawScenarioItem struct {
	XMLName         xml.Name
	Text            string       `xml:",innerxml"`
	Next            string       `xml:"next,attr"`
	Test            string       `xml:"test,attr"`
	Chance          string       `xml:"chance,attr"`
	CondExec        string       `xml:"condexec,attr"`
	CondExecInverse string       `xml:"condexec_inverse,attr"`
	Counter         string       `xml:"counter,attr"`
	Display         string       `xml:"display,attr"`
	StartRTD        string       `xml:"start_rtd,attr"`
	RTD             string       `xml:"rtd,attr"`
	Retrans         string       `xml:"retrans,attr"`
	Request         string       `xml:"request,attr"`
	Response        string       `xml:"response,attr"`
	RegexpMatch     string       `xml:"regexp_match,attr"`
	RRS             string       `xml:"rrs,attr"`
	Optional        string       `xml:"optional,attr"`
	Timeout         string       `xml:"timeout,attr"`
	Milliseconds    string       `xml:"milliseconds,attr"`
	ID              string       `xml:"id,attr"`
	Dest            string       `xml:"dest,attr"`
	Src             string       `xml:"src,attr"`
	Variables       string       `xml:"variables,attr"`
	Actions         []rawAction  `xml:"action"`
	Elements        []rawElement `xml:",any"`
	rawDistributionAttrs
}

type rawElement struct {
	XMLName         xml.Name
	Text            string      `xml:",innerxml"`
	Next            string      `xml:"next,attr"`
	Test            string      `xml:"test,attr"`
	Chance          string      `xml:"chance,attr"`
	CondExec        string      `xml:"condexec,attr"`
	CondExecInverse string      `xml:"condexec_inverse,attr"`
	Counter         string      `xml:"counter,attr"`
	Display         string      `xml:"display,attr"`
	StartRTD        string      `xml:"start_rtd,attr"`
	RTD             string      `xml:"rtd,attr"`
	Retrans         string      `xml:"retrans,attr"`
	Request         string      `xml:"request,attr"`
	Response        string      `xml:"response,attr"`
	RegexpMatch     string      `xml:"regexp_match,attr"`
	RRS             string      `xml:"rrs,attr"`
	Optional        string      `xml:"optional,attr"`
	Timeout         string      `xml:"timeout,attr"`
	Milliseconds    string      `xml:"milliseconds,attr"`
	ID              string      `xml:"id,attr"`
	Dest            string      `xml:"dest,attr"`
	Src             string      `xml:"src,attr"`
	Actions         []rawAction `xml:"action"`
	rawDistributionAttrs
}

type rawAction struct {
	Children []rawActionItem `xml:",any"`
}

type rawActionItem struct {
	XMLName        xml.Name
	Regexp         string `xml:"regexp,attr"`
	SearchIn       string `xml:"search_in,attr"`
	Header         string `xml:"header,attr"`
	Variable       string `xml:"variable,attr"`
	Variable2      string `xml:"variable2,attr"`
	AssignTo       string `xml:"assign_to,attr"`
	CheckIt        string `xml:"check_it,attr"`
	CheckItInverse string `xml:"check_it_inverse,attr"`
	Value          string `xml:"value,attr"`
	Compare        string `xml:"compare,attr"`
	Message        string `xml:"message,attr"`
	File           string `xml:"file,attr"`
	Key            string `xml:"key,attr"`
	Username       string `xml:"username,attr"`
	Password       string `xml:"password,attr"`
	Command        string `xml:"command,attr"`
	IntCmd         string `xml:"int_cmd,attr"`
	RTPStream      string `xml:"rtp_stream,attr"`
	RTPRecord      string `xml:"rtp_record,attr"`
	RTPCheck       string `xml:"rtpcheck,attr"`
	PlayPCAPAudio  string `xml:"play_pcap_audio,attr"`
	PlayPCAPVideo  string `xml:"play_pcap_video,attr"`
	PlayPCAPImage  string `xml:"play_pcap_image,attr"`
	SendDTMF       string `xml:"send_dtmf,attr"`
	Host           string `xml:"host,attr"`
	Port           string `xml:"port,attr"`
	Protocol       string `xml:"protocol,attr"`
}

func ParseFile(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}
	sc, err := ParseString(string(data))
	if err != nil {
		return Scenario{}, err
	}
	sc.BasePath = filepath.Dir(path)
	return sc, nil
}

func ParseString(data string) (Scenario, error) {
	var raw rawScenario
	if err := xmlDecodeScenario([]byte(data), &raw); err != nil {
		return Scenario{}, err
	}

	sc := Scenario{
		Name:       raw.Name,
		BasePath:   ".",
		Labels:     make(map[string]int),
		InitLabels: make(map[string]int),
		Mode:       ModeClient,
		WebRTC:     parseBool(raw.WebRTC),
	}

	for _, elem := range raw.Elements {
		switch elem.XMLName.Local {
		case "Global":
			sc.GlobalVariables = append(sc.GlobalVariables, parseVariablesList(elem.Variables)...)
		case "User":
			sc.UserVariables = append(sc.UserVariables, parseVariablesList(elem.Variables)...)
		case "Reference":
			sc.References = append(sc.References, parseVariablesList(elem.Variables)...)
		case "init":
			for _, child := range elem.Elements {
				cmd, err := rawElementToCommand(child, len(sc.InitCommands))
				if err != nil {
					return Scenario{}, err
				}
				if cmd.Type == "" {
					continue
				}
				if cmd.Type == CommandLabel {
					sc.InitLabels[cmd.LabelID] = len(sc.InitCommands)
				}
				sc.InitCommands = append(sc.InitCommands, cmd)
			}
		default:
			cmd, err := rawScenarioItemToCommand(elem, len(sc.Commands))
			if err != nil {
				return Scenario{}, err
			}
			if cmd.Type == "" {
				continue
			}
			if cmd.Type == CommandLabel {
				sc.Labels[cmd.LabelID] = len(sc.Commands)
			}
			if cmd.Type == CommandRecv && cmd.RecvReq != "" {
				sc.Mode = ModeServer
			}
			sc.Commands = append(sc.Commands, cmd)
		}
	}

	for i := range sc.Commands {
		if sc.Commands[i].NextLabel == "" {
			continue
		}
		next, ok := sc.Labels[sc.Commands[i].NextLabel]
		if !ok {
			return Scenario{}, fmt.Errorf("command %d references unknown label %q", i, sc.Commands[i].NextLabel)
		}
		sc.Commands[i].NextIndex = next
	}
	for i := range sc.InitCommands {
		if sc.InitCommands[i].NextLabel == "" {
			continue
		}
		next, ok := sc.InitLabels[sc.InitCommands[i].NextLabel]
		if !ok {
			return Scenario{}, fmt.Errorf("init command %d references unknown label %q", i, sc.InitCommands[i].NextLabel)
		}
		sc.InitCommands[i].NextIndex = next
	}

	if len(sc.Commands) == 0 {
		return Scenario{}, errors.New("scenario contains no executable commands")
	}

	return sc, nil
}

func rawScenarioItemToCommand(elem rawScenarioItem, index int) (Command, error) {
	return rawElementToCommand(rawElement{
		XMLName:              elem.XMLName,
		Text:                 elem.Text,
		Next:                 elem.Next,
		Test:                 elem.Test,
		Chance:               elem.Chance,
		CondExec:             elem.CondExec,
		CondExecInverse:      elem.CondExecInverse,
		Counter:              elem.Counter,
		Display:              elem.Display,
		StartRTD:             elem.StartRTD,
		RTD:                  elem.RTD,
		Retrans:              elem.Retrans,
		Request:              elem.Request,
		Response:             elem.Response,
		RegexpMatch:          elem.RegexpMatch,
		RRS:                  elem.RRS,
		Optional:             elem.Optional,
		Timeout:              elem.Timeout,
		Milliseconds:         elem.Milliseconds,
		ID:                   elem.ID,
		Dest:                 elem.Dest,
		Src:                  elem.Src,
		Actions:              elem.Actions,
		rawDistributionAttrs: elem.rawDistributionAttrs,
	}, index)
}

func rawElementToCommand(elem rawElement, index int) (Command, error) {
	cmd := Command{
		Index:     index,
		NextIndex: -1,
		Chance:    1.0,
	}

	switch elem.XMLName.Local {
	case "send":
		cmd.Type = CommandSend
		cmd.SendText = normalizeInnerXML(elem.Text)
		cmd.Retrans = parseDurationMilliseconds(elem.Retrans)
	case "sendCmd":
		cmd.Type = CommandSendCmd
		cmd.SendText = normalizeInnerXML(elem.Text)
		cmd.CmdDest = strings.TrimSpace(elem.Dest)
	case "recv":
		cmd.Type = CommandRecv
		cmd.RecvReq = strings.TrimSpace(elem.Request)
		cmd.RecvResp = strings.TrimSpace(elem.Response)
		cmd.RegexpMatch = parseBool(elem.RegexpMatch)
		if cmd.RegexpMatch {
			if cmd.RecvReq == "" {
				return Command{}, fmt.Errorf("recv at index %d has regexp_match but no request pattern", index)
			}
			re, err := regexp.Compile(cmd.RecvReq)
			if err != nil {
				return Command{}, fmt.Errorf("recv at index %d has invalid regexp for request: %w", index, err)
			}
			cmd.RecvReqRegex = re
		}
		cmd.RRS = parseBool(elem.RRS)
		cmd.Optional = parseBool(elem.Optional)
		cmd.Timeout = parseDurationMilliseconds(elem.Timeout)
	case "recvCmd":
		cmd.Type = CommandRecvCmd
		cmd.CmdSrc = strings.TrimSpace(elem.Src)
		cmd.Optional = parseBool(elem.Optional)
		cmd.Timeout = parseDurationMilliseconds(elem.Timeout)
	case "pause":
		cmd.Type = CommandPause
		sampler, err := distribution.NewFromXML(
			elem.Distribution,
			elem.Milliseconds,
			elem.Mean,
			elem.Stdev,
			elem.Min,
			elem.Max,
		)
		if err != nil {
			return Command{}, fmt.Errorf("pause at index %d: %w", index, err)
		}
		cmd.Pause = sampler
	case "nop":
		cmd.Type = CommandNop
	case "label":
		cmd.Type = CommandLabel
		cmd.LabelID = strings.TrimSpace(elem.ID)
	case "timewait":
		cmd.Type = CommandTimeWait
		cmd.TimeWait = true
		sampler, err := distribution.NewFromXML(
			"fixed",
			elem.Milliseconds,
			"", "", "", "",
		)
		if err != nil {
			return Command{}, fmt.Errorf("timewait at index %d: %w", index, err)
		}
		cmd.Pause = sampler
	default:
		return Command{}, nil
	}

	cmd.NextLabel = strings.TrimSpace(elem.Next)
	cmd.Test = strings.TrimSpace(elem.Test)
	cmd.Counter = strings.TrimSpace(elem.Counter)
	cmd.Display = strings.TrimSpace(elem.Display)
	cmd.StartRTD = strings.TrimSpace(elem.StartRTD)
	cmd.StopRTD = strings.TrimSpace(elem.RTD)
	cmd.CondExec = strings.TrimSpace(elem.CondExec)
	cmd.CondExecInverse = parseBool(elem.CondExecInverse)
	actions, err := parseActions(elem.Actions)
	if err != nil {
		return Command{}, err
	}
	cmd.Actions = actions

	if chance := strings.TrimSpace(elem.Chance); chance != "" {
		value, err := strconv.ParseFloat(chance, 64)
		if err != nil {
			return Command{}, fmt.Errorf("invalid chance value %q: %w", chance, err)
		}
		switch {
		case value < 0:
			cmd.Chance = 0
		case value > 1:
			cmd.Chance = 1
		default:
			cmd.Chance = value
		}
	}

	if cmd.Type == CommandLabel && cmd.LabelID == "" {
		return Command{}, fmt.Errorf("label at index %d has no id", index)
	}
	if cmd.Type == CommandRecv && cmd.RecvReq == "" && cmd.RecvResp == "" {
		return Command{}, fmt.Errorf("recv at index %d must define request or response", index)
	}

	return cmd, nil
}

func parseActions(raw []rawAction) ([]Action, error) {
	var actions []Action
	for _, actionBlock := range raw {
		for _, child := range actionBlock.Children {
			actionType, ok := parseActionType(child.XMLName.Local)
			if !ok {
				return nil, fmt.Errorf("unsupported action %q", child.XMLName.Local)
			}
			action := Action{
				Type:           actionType,
				Regexp:         strings.TrimSpace(child.Regexp),
				SearchIn:       strings.TrimSpace(child.SearchIn),
				Header:         strings.TrimSpace(child.Header),
				Variable:       strings.TrimSpace(child.Variable),
				Variable2:      strings.TrimSpace(child.Variable2),
				CheckIt:        parseBool(child.CheckIt),
				CheckItInverse: parseBool(child.CheckItInverse),
				Value:          strings.TrimSpace(child.Value),
				Compare:        strings.TrimSpace(child.Compare),
				Message:        strings.TrimSpace(child.Message),
				File:           strings.TrimSpace(child.File),
				Key:            strings.TrimSpace(child.Key),
				Username:       strings.TrimSpace(child.Username),
				Password:       strings.TrimSpace(child.Password),
				Command:        strings.TrimSpace(child.Command),
				IntCmd:         strings.TrimSpace(child.IntCmd),
				RTPStream:      strings.TrimSpace(child.RTPStream),
				RTPRecord:      strings.TrimSpace(child.RTPRecord),
				RTPCheck:       strings.TrimSpace(child.RTPCheck),
				PlayPCAPAudio:  strings.TrimSpace(child.PlayPCAPAudio),
				PlayPCAPVideo:  strings.TrimSpace(child.PlayPCAPVideo),
				PlayPCAPImage:  strings.TrimSpace(child.PlayPCAPImage),
				SendDTMF:       strings.TrimSpace(child.SendDTMF),
				Host:           strings.TrimSpace(child.Host),
				Port:           strings.TrimSpace(child.Port),
				Protocol:       strings.TrimSpace(child.Protocol),
			}
			if assignTo := strings.TrimSpace(child.AssignTo); assignTo != "" {
				for _, name := range strings.Split(assignTo, ",") {
					name = strings.TrimSpace(name)
					if name != "" {
						action.AssignTo = append(action.AssignTo, name)
					}
				}
			}
			actions = append(actions, action)
		}
	}
	return actions, nil
}

func parseActionType(name string) (ActionType, bool) {
	switch ActionType(strings.TrimSpace(name)) {
	case ActionEReg,
		ActionAssignStr,
		ActionAssign,
		ActionToDouble,
		ActionAdd,
		ActionSubtract,
		ActionMultiply,
		ActionDivide,
		ActionStrCmp,
		ActionTest,
		ActionLog,
		ActionWarning,
		ActionLookup,
		ActionSample,
		ActionInsert,
		ActionReplace,
		ActionJump,
		ActionGetTimeOfDay,
		ActionURLEncode,
		ActionURLDecode,
		ActionVerifyAuth,
		ActionExec,
		ActionSetDest:
		return ActionType(strings.TrimSpace(name)), true
	default:
		return "", false
	}
}

func parseVariablesList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeInnerXML(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "<![CDATA[", "")
	value = strings.ReplaceAll(value, "]]>", "")
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "\n", "\r\n") + "\r\n"
}

func parseDurationMilliseconds(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	ms, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// xmlDecodeScenario decodes SIPp-style scenario XML. If the prolog declares
// ISO-8859-1 (Latin-1), the document is transcoded to UTF-8 and the
// encoding declaration is rewritten so encoding/xml does not require
// CharsetReader (which is easy to misconfigure for attributes).
func xmlDecodeScenario(data []byte, v any) error {
	out, err := scenarioXMLToUTF8(data)
	if err != nil {
		return err
	}
	return xml.Unmarshal(out, v)
}

func scenarioXMLToUTF8(data []byte) ([]byte, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	enc := strings.TrimSpace(sniffXMLDeclEncoding(data))
	switch strings.ToLower(enc) {
	case "", "utf-8", "utf8":
		return data, nil
	default:
		if !isISO88591DeclEncoding(enc) {
			return nil, fmt.Errorf("scenario XML encoding %q is not supported (supported: utf-8, iso-8859-1)", enc)
		}
		out, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
		if err != nil {
			return nil, err
		}
		return rewriteXMLDeclEncodingToUTF8(out), nil
	}
}

// isISO88591DeclEncoding reports whether an XML declaration encoding value
// denotes ISO-8859-1 / Latin-1 (including IANA and legacy aliases).
func isISO88591DeclEncoding(enc string) bool {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "iso-8859-1", "iso8859-1", "latin-1", "latin1", "ibm819", "cp819", "csisolatin1":
		return true
	default:
		return false
	}
}

func sniffXMLDeclEncoding(data []byte) string {
	end := bytes.Index(data, []byte("?>"))
	if end < 0 {
		return ""
	}
	head := string(data[:end+2])
	lower := strings.ToLower(head)
	const key = "encoding="
	i := strings.Index(lower, key)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(head[i+len(key):])
	if len(rest) == 0 {
		return ""
	}
	q := rest[0]
	if q != '"' && q != '\'' {
		return ""
	}
	rest = rest[1:]
	j := strings.IndexByte(rest, q)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// rewriteXMLDeclEncodingToUTF8 sets encoding="UTF-8" (or single-quoted) in the
// first XML processing instruction when the declared encoding is an ISO-8859-1
// alias (same set as isISO88591DeclEncoding). This must match scenarioXMLToUTF8
// after Latin-1 transcoding so the prolog matches the actual UTF-8 bytes.
func rewriteXMLDeclEncodingToUTF8(b []byte) []byte {
	end := bytes.Index(b, []byte("?>"))
	if end < 0 {
		return b
	}
	pi := b[:end+2]
	lowerPI := bytes.ToLower(pi)
	key := []byte("encoding=")
	i := bytes.Index(lowerPI, key)
	if i < 0 {
		return b
	}
	rest := bytes.TrimSpace(pi[i+len(key):])
	if len(rest) < 3 {
		return b
	}
	q := rest[0]
	if q != '"' && q != '\'' {
		return b
	}
	valPart := rest[1:]
	j := bytes.IndexByte(valPart, q)
	if j < 0 {
		return b
	}
	if !isISO88591DeclEncoding(string(valPart[:j])) {
		return b
	}
	suffix := valPart[j+1:]
	prefix := pi[:i+len(key)]
	mid := append([]byte{q}, []byte("UTF-8")...)
	mid = append(mid, q)
	newPI := append(append(prefix, mid...), suffix...)
	return append(newPI, b[end+2:]...)
}
