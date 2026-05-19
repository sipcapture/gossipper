package engine

import (
	"context"

	"github.com/sipcapture/gossipper/internal/media"
)

func (e *Engine) startScaleEngine(ctx context.Context) {
	e.scaleMu.Lock()
	defer e.scaleMu.Unlock()
	if e.scaleEngine != nil {
		return
	}
	se := media.NewScaleEngine()
	se.Run(ctx)
	e.scaleEngine = se
}

func (e *Engine) stopScaleEngine() {
	e.scaleMu.Lock()
	se := e.scaleEngine
	e.scaleEngine = nil
	e.scaleMu.Unlock()
	if se != nil {
		se.Stop()
	}
}

func (e *Engine) scaleMedia() *media.ScaleEngine {
	e.scaleMu.Lock()
	defer e.scaleMu.Unlock()
	return e.scaleEngine
}

func (e *Engine) scaleUnregisterCall(callID string) media.Stats {
	se := e.scaleMedia()
	if se == nil {
		return media.Stats{}
	}
	return se.UnregisterCall(callID)
}
