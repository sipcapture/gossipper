package engine

import (
	"context"
	"time"

	"github.com/qxip/gossipper/internal/hep"
	"github.com/qxip/gossipper/internal/sip"
)

func (e *Engine) startHEP() error {
	if e.cfg.HEPAddr == "" {
		return nil
	}
	client, err := hep.New(hep.Config{
		Addr:      e.cfg.HEPAddr,
		CaptureID: e.cfg.HEPCaptureID,
		Password:  e.cfg.HEPPassword,
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

func (e *Engine) observeSIP(direction string, callNumber int, localIP string, localPort int, remoteIP string, remotePort int, raw []byte) {
	if e.cfg.TraceMessages || e.cfg.TraceShortMsg {
		e.traceEvent(direction, callNumber, string(raw))
	}
	if e.hep != nil {
		srcIP, srcPort := localIP, localPort
		dstIP, dstPort := remoteIP, remotePort
		if direction == "recv" {
			srcIP, srcPort = remoteIP, remotePort
			dstIP, dstPort = localIP, localPort
		}
		if err := e.hep.SendSIP(time.Now(), srcIP, srcPort, dstIP, dstPort, e.cfg.Transport, raw); err != nil {
			e.traceError("hep-export", callNumber, err.Error())
		}
	}
}

func (e *Engine) wrapSIPSend(callNumber int, localIP string, localPort int, remoteIP string, remotePort int, send func([]byte) error) func([]byte) error {
	return func(payload []byte) error {
		if err := send(payload); err != nil {
			return err
		}
		e.observeSIP("send", callNumber, localIP, localPort, remoteIP, remotePort, payload)
		return nil
	}
}

func (e *Engine) wrapSIPReceive(callNumber int, localIP string, localPort int, remoteIP string, remotePort int, receive func(waitCtx context.Context) (*sip.Message, error)) func(context.Context) (*sip.Message, error) {
	return func(waitCtx context.Context) (*sip.Message, error) {
		msg, err := receive(waitCtx)
		if err != nil {
			return nil, err
		}
		e.observeSIP("recv", callNumber, localIP, localPort, remoteIP, remotePort, []byte(msg.Raw))
		return msg, nil
	}
}
