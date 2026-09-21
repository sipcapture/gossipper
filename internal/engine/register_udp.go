package engine

import (
	"context"
	"errors"
	"net"

	"github.com/sipcapture/gossipper/internal/sip"
	"github.com/sipcapture/gossipper/internal/siplog"
	"github.com/sipcapture/gossipper/internal/transport"
)

var errUASUDPNotReady = errors.New("uas udp not ready")

func (e *Engine) setUASUDP(s *transport.SharedUDP) {
	e.uasUDPMu.Lock()
	if e.uasUDP != nil {
		e.uasUDP.SetTap(nil)
	}
	e.uasUDP = s
	if s != nil {
		s.SetTap(e.onUDPSIP)
	}
	e.uasUDPMu.Unlock()
}

func (e *Engine) onUDPSIP(dir string, payload []byte, addr *net.UDPAddr) {
	if e.sipLog == nil {
		return
	}
	peer := ""
	if addr != nil {
		peer = addr.String()
	}
	e.sipLog.AddTagged(dir, peer, e.sipTraceScenario(payload), payload)
}

// SIPTrace is the process-wide circular SIP buffer (UAS UDP plus in-process scenario engines).
func (e *Engine) SIPTrace() *siplog.Ring {
	if e == nil {
		return nil
	}
	return e.sipLog
}

// RegisterLocalAddr is the UAS UDP bind used as REGISTER Contact/Via port.
func (e *Engine) RegisterLocalAddr() *net.UDPAddr {
	e.uasUDPMu.RLock()
	s := e.uasUDP
	e.uasUDPMu.RUnlock()
	if s == nil {
		return nil
	}
	port := s.LocalPort()
	if port <= 0 {
		return nil
	}
	return &net.UDPAddr{IP: net.IPv4zero, Port: port}
}

// RegisterRoundTrip sends a SIP datagram on the UAS socket and waits for a
// response with the same Call-ID (so FreeSWITCH NAT pins inbound INVITE here).
func (e *Engine) RegisterRoundTrip(ctx context.Context, callID string, payload []byte, remote *net.UDPAddr) ([]byte, error) {
	callID = sip.NormalizeCallID(callID)
	if callID == "" {
		return nil, errors.New("empty Call-ID")
	}
	if remote == nil {
		return nil, errors.New("nil registrar addr")
	}
	ch := make(chan []byte, 1)
	e.waitMu.Lock()
	if e.waiters == nil {
		e.waiters = make(map[string]chan []byte)
	}
	e.waiters[callID] = ch
	e.waitMu.Unlock()
	defer func() {
		e.waitMu.Lock()
		delete(e.waiters, callID)
		e.waitMu.Unlock()
	}()

	e.uasUDPMu.RLock()
	s := e.uasUDP
	e.uasUDPMu.RUnlock()
	if s == nil {
		return nil, errUASUDPNotReady
	}
	if err := s.Send(payload, remote); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case raw := <-ch:
		return raw, nil
	}
}

// RegisterSend writes a datagram on the UAS socket without waiting (NAT keep-alive).
func (e *Engine) RegisterSend(payload []byte, remote *net.UDPAddr) error {
	if remote == nil {
		return errors.New("nil registrar addr")
	}
	e.uasUDPMu.RLock()
	s := e.uasUDP
	e.uasUDPMu.RUnlock()
	if s == nil {
		return errUASUDPNotReady
	}
	return s.Send(payload, remote)
}

func (e *Engine) deliverRegisterWaiter(callID string, raw []byte) bool {
	callID = sip.NormalizeCallID(callID)
	e.waitMu.Lock()
	ch, ok := e.waiters[callID]
	e.waitMu.Unlock()
	if !ok || ch == nil {
		return false
	}
	cp := append([]byte(nil), raw...)
	select {
	case ch <- cp:
	default:
	}
	return true
}
