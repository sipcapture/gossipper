package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sipcapture/gossipper/internal/eventlog"
	"github.com/sipcapture/gossipper/internal/hep"
	"github.com/sipcapture/gossipper/internal/sip"
)

func (e *Engine) startHEP() error {
	if e.cfg.HEPAddr == "" {
		return nil
	}
	sendMediaReport := e.cfg.SendMediaReport && !e.cfg.MediaScale
	client, err := hep.New(hep.Config{
		Addr:            e.cfg.HEPAddr,
		CaptureID:       e.cfg.HEPCaptureID,
		Password:        e.cfg.HEPPassword,
		RawRTCP:         e.cfg.HEPRawRTCP,
		HomerLakeRTCP:   e.cfg.HEPHomerLakeRTCP,
		SendMediaReport: sendMediaReport,
	})
	if err != nil {
		return err
	}
	e.hep = client
	return nil
}

func (e *Engine) stopHEP() {
	if e.hep == nil {
		return
	}
	_ = e.hep.Close()
	e.hep = nil
}

func (e *Engine) observeSIP(direction string, callNumber int, callID string, localIP string, localPort int, remoteIP string, remotePort int, raw []byte) {
	// Fast path: skip all work when no tracing, HEP, or event logging is active.
	if !e.cfg.TraceMessages && !e.cfg.TraceShortMsg && e.hep == nil && !e.logActive {
		return
	}
	if e.cfg.TraceMessages || e.cfg.TraceShortMsg {
		e.traceEvent(direction, callNumber, string(raw))
	}
	srcIP, srcPort := localIP, localPort
	dstIP, dstPort := remoteIP, remotePort
	if direction == "recv" {
		srcIP, srcPort = remoteIP, remotePort
		dstIP, dstPort = localIP, localPort
	}
	if e.hep != nil {
		if err := e.hep.SendSIP(time.Now(), srcIP, srcPort, dstIP, dstPort, e.cfg.Transport, callID, raw); err != nil {
			e.traceError("hep-export", callNumber, err.Error())
		}
	}
	e.emitSIPEvent(direction, callNumber, callID, srcIP, srcPort, dstIP, dstPort, raw)
}

// emitSIPEvent pushes a structured eventlog Event for one SIP message.
// Short-circuits when no real logger is configured to avoid re-parsing overhead.
func (e *Engine) emitSIPEvent(direction string, callNumber int, callID string, srcIP string, srcPort int, dstIP string, dstPort int, raw []byte) {
	if !e.logActive {
		return
	}
	kind := eventlog.KindSIPSend
	if direction == "recv" {
		kind = eventlog.KindSIPRecv
	}
	method, status, reason, summary := summarizeSIP(raw)
	attrs := map[string]any{
		"call_id":   callID,
		"call_num":  callNumber,
		"src_ip":    srcIP,
		"src_port":  srcPort,
		"dst_ip":    dstIP,
		"dst_port":  dstPort,
		"transport": e.cfg.Transport,
	}
	if method != "" {
		attrs["sip.method"] = method
	}
	if status > 0 {
		attrs["sip.status"] = status
		if reason != "" {
			attrs["sip.reason"] = reason
		}
	}
	e.log.Emit(eventlog.Event{
		Time:  time.Now(),
		Level: eventlog.LevelInfo,
		Kind:  kind,
		Msg:   summary,
		Attrs: attrs,
	})
}

// summarizeSIP extracts a compact summary from a SIP message:
// for requests, the method (e.g. "INVITE"); for responses, "CODE REASON".
// Falls back to the first line if parsing fails.
func summarizeSIP(raw []byte) (method string, status int, reason string, summary string) {
	msg, err := sip.Parse(raw)
	if err != nil {
		summary = firstNonEmptyLine(string(raw))
		return
	}
	method = msg.Method
	status = msg.StatusCode
	reason = strings.TrimSpace(msg.Reason)
	switch {
	case method != "":
		summary = method
	case status > 0:
		if reason != "" {
			summary = fmt.Sprintf("%d %s", status, reason)
		} else {
			summary = fmt.Sprintf("%d", status)
		}
	default:
		summary = firstNonEmptyLine(msg.Raw)
	}
	return
}

func firstNonEmptyLine(raw string) string {
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(line)
		if s != "" {
			return s
		}
	}
	return ""
}

func (e *Engine) wrapSIPSend(callNumber int, callID string, localIP string, localPort int, remoteIP string, remotePort int, send func([]byte) error) func([]byte) error {
	if !e.observeActive() {
		return send
	}
	return func(payload []byte) error {
		if err := send(payload); err != nil {
			return err
		}
		e.observeSIP("send", callNumber, callID, localIP, localPort, remoteIP, remotePort, payload)
		return nil
	}
}

func (e *Engine) wrapSIPReceive(callNumber int, callID string, localIP string, localPort int, remoteIP string, remotePort int, receive func(waitCtx context.Context) (*sip.Message, error)) func(context.Context) (*sip.Message, error) {
	if !e.observeActive() {
		return func(waitCtx context.Context) (*sip.Message, error) {
			msg, err := receive(waitCtx)
			if err != nil {
				return nil, err
			}
			if msg == nil || (msg.Raw == "" && msg.StatusCode == 0 && msg.Method == "") {
				return nil, errSIPMailboxClosed
			}
			return msg, nil
		}
	}
	return func(waitCtx context.Context) (*sip.Message, error) {
		msg, err := receive(waitCtx)
		if err != nil {
			return nil, err
		}
		if msg == nil || (msg.Raw == "" && msg.StatusCode == 0 && msg.Method == "") {
			return nil, errSIPMailboxClosed
		}
		e.observeSIP("recv", callNumber, callID, localIP, localPort, remoteIP, remotePort, []byte(msg.Raw))
		return msg, nil
	}
}

// observeActive reports whether any SIP observation is configured (tracing, HEP, or event logging).
func (e *Engine) observeActive() bool {
	return e.cfg.TraceMessages || e.cfg.TraceShortMsg || e.hep != nil || e.logActive
}
