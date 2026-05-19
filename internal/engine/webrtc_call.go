package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/sipcapture/gossipper/internal/media"
	"github.com/sipcapture/gossipper/internal/sip"
	templ "github.com/sipcapture/gossipper/internal/template"
	"github.com/sipcapture/gossipper/internal/webrtc"
)

func prepareWebRTCSendKeywords(ctx context.Context, cm *callMedia, renderCtx *templ.Context, sendTemplate string) error {
	if cm == nil || !cm.usesWebRTC() {
		return nil
	}
	if renderCtx.ExtraKeywords == nil {
		renderCtx.ExtraKeywords = map[string]string{}
	}
	if strings.Contains(sendTemplate, "[webrtc_offer]") {
		offer, err := cm.wrtc.createOffer(ctx)
		if err != nil {
			return err
		}
		renderCtx.ExtraKeywords["webrtc_offer"] = offer
	}
	if strings.Contains(sendTemplate, "[webrtc_answer]") {
		offer := media.SDPBodyFromRawMessage(renderCtx.LastMessage)
		if offer == "" {
			return errors.New("webrtc: last message has no SDP offer for [webrtc_answer]")
		}
		answer, err := cm.wrtc.answer(offer)
		if err != nil {
			return err
		}
		renderCtx.ExtraKeywords["webrtc_answer"] = answer
	}
	return nil
}

func maybeAcceptWebRTCAnswer(cm *callMedia, raw string) error {
	if cm == nil || !cm.usesWebRTC() || !cm.wrtc.needsAcceptAnswer() {
		return nil
	}
	if !sipResponseWithSDP(raw) {
		return nil
	}
	body := media.SDPBodyFromRawMessage(raw)
	if body == "" {
		return nil
	}
	return cm.wrtc.acceptAnswer(body)
}

func maybeTrickleWebRTCFromMessage(cm *callMedia, raw string) error {
	if cm == nil || !cm.usesWebRTC() {
		return nil
	}
	body := trickleBodyFromRaw(raw)
	if body == "" || !webrtc.IsTrickleICEFragment(body) {
		return nil
	}
	_, err := cm.wrtc.bridge.AddRemoteICECandidatesFromBody(body)
	return err
}

func trickleBodyFromRaw(raw string) string {
	msg := sip.GetMessage()
	defer sip.PutMessage(msg)
	if err := sip.ParseInto(msg, []byte(raw)); err != nil {
		return strings.TrimSpace(media.SDPBodyFromRawMessage(raw))
	}
	body := strings.TrimSpace(media.EffectiveMediaSDPBody(*msg))
	if body != "" {
		return body
	}
	return strings.TrimSpace(media.SDPBodyFromRawMessage(raw))
}

func sipResponseWithSDP(raw string) bool {
	upper := strings.ToUpper(raw)
	if !strings.HasPrefix(upper, "SIP/2.0 ") {
		return false
	}
	if strings.Contains(upper, "CONTENT-TYPE: APPLICATION/SDP") {
		return true
	}
	body := media.SDPBodyFromRawMessage(raw)
	return strings.HasPrefix(body, "v=0")
}
