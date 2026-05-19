package engine

import (
	"errors"
	"strings"

	"github.com/sipcapture/gossipper/internal/media"
	templ "github.com/sipcapture/gossipper/internal/template"
)

func prepareWebRTCAnswerKeyword(cm *callMedia, renderCtx *templ.Context, sendTemplate string) error {
	if cm == nil || !cm.usesWebRTC() {
		return nil
	}
	if !strings.Contains(sendTemplate, "[webrtc_answer]") {
		return nil
	}
	offer := media.SDPBodyFromRawMessage(renderCtx.LastMessage)
	if offer == "" {
		return errors.New("webrtc: last message has no SDP offer for [webrtc_answer]")
	}
	answer, err := cm.wrtc.answer(offer)
	if err != nil {
		return err
	}
	if renderCtx.ExtraKeywords == nil {
		renderCtx.ExtraKeywords = map[string]string{}
	}
	renderCtx.ExtraKeywords["webrtc_answer"] = answer
	return nil
}
