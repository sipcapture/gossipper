package engine

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/sipcapture/gossipper/internal/eventlog"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/sip"
)

// recordSIPTrace writes one message into the process SIP circular buffer.
// When the UAS listen UDP tap is attached, wrapSIP is skipped so datagrams are not doubled.
func (e *Engine) recordSIPTrace(dir, remoteIP string, remotePort int, raw []byte) {
	if e == nil || e.sipLog == nil || len(raw) == 0 {
		return
	}
	e.uasUDPMu.RLock()
	tapped := e.uasUDP != nil
	e.uasUDPMu.RUnlock()
	if tapped {
		return
	}
	peer := ""
	if remoteIP != "" {
		peer = net.JoinHostPort(remoteIP, strconv.Itoa(remotePort))
	}
	e.sipLog.AddTagged(dir, peer, e.sipTraceScenario(raw), raw)
}

func (e *Engine) appTraceOn() bool {
	return e != nil && e.sipLog != nil && e.sipLog.AppEnabled()
}

func (e *Engine) recordAppTrace(ev eventlog.Event) {
	if e == nil || e.sipLog == nil {
		return
	}
	if ev.Kind == eventlog.KindSIPSend || ev.Kind == eventlog.KindSIPRecv {
		return
	}
	callID := attrString(ev.Attrs, "call_id")
	summary := strings.TrimSpace(ev.Kind)
	if ev.Msg != "" {
		if summary != "" {
			summary = summary + " " + ev.Msg
		} else {
			summary = ev.Msg
		}
	}
	raw := ev.Kind
	if ev.Msg != "" {
		raw = ev.Kind + "\n" + ev.Msg
	}
	if attrs := eventlog.FormatAttrs(ev.Attrs); attrs != "" {
		raw = raw + "\n" + attrs
	}
	e.sipLog.AddApp(e.LiveScenario().Name, ev.Kind, summary, callID, raw, ev.Level.String())
}

func (e *Engine) traceScenarioCmd(cmd scenario.Command, callNumber int, callID string) {
	if !e.appTraceOn() || cmd.Type == scenario.CommandLabel {
		return
	}
	summary := scenarioCmdSummary(cmd)
	e.emitEvent(eventlog.Event{
		Level: eventlog.LevelDebug,
		Kind:  eventlog.KindScenarioCmd,
		Msg:   summary,
		Attrs: map[string]any{
			"call_id":       callID,
			"call_num":      callNumber,
			"command.index": cmd.Index,
			"command.type":  string(cmd.Type),
		},
	})
}

func scenarioCmdSummary(cmd scenario.Command) string {
	switch cmd.Type {
	case scenario.CommandSend:
		line := strings.TrimSpace(strings.Split(strings.ReplaceAll(cmd.SendText, "\r\n", "\n"), "\n")[0])
		if line != "" {
			return "send " + line
		}
		return "send"
	case scenario.CommandRecv:
		if cmd.RecvReq != "" {
			return "recv " + cmd.RecvReq
		}
		if cmd.RecvResp != "" {
			return "recv " + cmd.RecvResp
		}
		return "recv"
	default:
		return string(cmd.Type)
	}
}

func attrString(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	v, ok := attrs[key].(string)
	if !ok {
		if attrs[key] == nil {
			return ""
		}
		return fmt.Sprint(attrs[key])
	}
	return v
}

func (e *Engine) sipTraceScenario(raw []byte) string {
	line := sipTraceFirstLine(raw)
	method, rest, _ := strings.Cut(line, " ")
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "REGISTER" || method == "OPTIONS" {
		return method
	}
	if method != "" && !strings.HasPrefix(method, "SIP/") {
		uri, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
		user := sip.RequestURIUser(uri)
		e.inviteResMu.RLock()
		resolver := e.inviteResolver
		e.inviteResMu.RUnlock()
		if resolver != nil && user != "" {
			if sc, ok := resolver(user); ok && sc.Name != "" {
				return sc.Name
			}
		}
	}
	return e.LiveScenario().Name
}

func sipTraceFirstLine(raw []byte) string {
	s := string(raw)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
