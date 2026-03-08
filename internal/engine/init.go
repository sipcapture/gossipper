package engine

import (
	"context"
	"time"

	"github.com/adubovikov/gossipper/internal/media"
	"github.com/adubovikov/gossipper/internal/scenario"
	templ "github.com/adubovikov/gossipper/internal/template"
)

func (e *Engine) runInit(ctx context.Context) error {
	if len(e.cfg.Scenario.InitCommands) == 0 {
		return nil
	}

	store := newVarStore(e.scopes, e.cfg.Scenario.GlobalVariables, e.cfg.Scenario.UserVariables, 0)
	mediaSession := media.NewSession()
	defer mediaSession.Stop()
	renderCtx := templ.Context{
		Service:     e.cfg.Service,
		Transport:   e.cfg.Transport,
		RemoteHost:  e.cfg.RemoteHost,
		RemoteIP:    e.cfg.RemoteHost,
		RemotePort:  e.cfg.RemotePort,
		LocalIP:     e.cfg.LocalIP,
		LocalIPType: ipType(e.cfg.LocalIP),
		LocalPort:   e.cfg.LocalPort,
		MediaIP:     e.cfg.LocalIP,
		MediaIPType: ipType(e.cfg.LocalIP),
		MediaPort:   e.cfg.LocalPort + 2,
		CallID:      newCallID(0),
		Variables:   store.Snapshot(),
		BasePath:    e.cfg.Scenario.BasePath,
	}
	commandCallKey := renderCtx.CallID

	for index := 0; index < len(e.cfg.Scenario.InitCommands); {
		cmd := e.cfg.Scenario.InitCommands[index]
		renderCtx.Variables = store.Snapshot()
		if !shouldExecute(cmd, store) {
			index++
			continue
		}
		renderCtx.MessageIndex = cmd.Index
		switch cmd.Type {
		case scenario.CommandNop, scenario.CommandLabel:
			if err := e.applyActions(ctx, cmd.Actions, renderCtx, store, mediaSession); err != nil {
				if err == errStopCall {
					return nil
				}
				return err
			}
		case scenario.CommandSendCmd:
			commandPayload := ensureMessageTerminator(templ.RenderMessage(cmd.SendText, renderCtx))
			if e.cfg.TraceMessages {
				e.traceEvent("init sendCmd", 0, commandPayload)
			}
			if err := e.sendCommand(cmd.CmdDest, commandPayload); err != nil {
				return err
			}
		case scenario.CommandRecvCmd:
			recvTimeout := cmd.Timeout
			if recvTimeout <= 0 {
				if cmd.Optional {
					recvTimeout = 250 * time.Millisecond
				} else {
					recvTimeout = e.cfg.DefaultRecvTO
				}
			}
			adoptCallID := cmd.Index == 0
			waitKey := commandCallKey
			if adoptCallID {
				waitKey = ""
			}
			callKey, msg, err := e.waitForCommand(ctx, waitKey, "", cmd.CmdSrc, recvTimeout)
			if err != nil {
				if cmd.Optional {
					index = resolveNext(index, cmd, store, e.random)
					continue
				}
				return err
			}
			if callKey != "" {
				commandCallKey = callKey
				if adoptCallID {
					renderCtx.CallID = callKey
				}
			}
			renderCtx.LastMessage = msg.raw
			renderCtx.LastHeaders = parseCommandHeaders(msg.raw)
			if e.cfg.TraceMessages || e.cfg.TraceShortMsg {
				e.traceEvent("init recvCmd", 0, msg.raw)
			}
			renderCtx.Variables = store.Snapshot()
			if err := e.applyActions(ctx, cmd.Actions, renderCtx, store, mediaSession); err != nil {
				if err == errStopCall {
					return nil
				}
				return err
			}
		case scenario.CommandPause, scenario.CommandTimeWait:
			pause := cmd.Pause
			if pause <= 0 {
				pause = e.cfg.DefaultPause
			}
			if err := e.sched.Sleep(ctx, pause); err != nil {
				return err
			}
		}
		renderCtx.Variables = store.Snapshot()
		index = resolveNext(index, cmd, store, e.random)
	}
	return nil
}
