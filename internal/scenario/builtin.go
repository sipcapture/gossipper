package scenario

import (
	"embed"
	"strings"
)

// BuiltinInviteMediaScale is the built-in UAC for high-scale cleartext RTP (requires ScaleEngine).
const BuiltinInviteMediaScale = "invite_media_scale"

//go:embed lab/*.xml
var labFS embed.FS

// BuiltinInfo describes a read-only scenario baked into the engine binary.
type BuiltinInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

var builtinCatalog = []BuiltinInfo{
	{ID: "uac", Name: "Basic Gossip UAC", Role: "uac", Description: "Minimal INVITE / ACK / BYE UAC flow.", Source: "builtin"},
	{ID: "uas", Name: "Basic Gossip UAS", Role: "uas", Description: "Answer INVITE with 200 OK, play synthetic RTP until BYE.", Source: "builtin"},
	{ID: "invite_media", Name: "invite_media", Role: "uac", Description: "UAC INVITE with SDP and RTP media.", Source: "builtin"},
	{ID: BuiltinInviteMediaScale, Name: BuiltinInviteMediaScale, Role: "uac", Description: "UAC INVITE with SDP and cleartext synthetic RTP for high-scale load (-media_scale auto).", Source: "builtin"},
	{ID: "invite_media_early", Name: "invite_media_early", Role: "uac", Description: "UAC media flow with early media (183).", Source: "builtin"},
	{ID: "invite_media_savpf", Name: "invite_media_savpf", Role: "uac", Description: "UAC media flow with SAVPF SDP profile.", Source: "builtin"},
	{ID: "invite_media_early_180", Name: "invite_media_early_180", Role: "uac", Description: "UAC media flow with 180 Ringing early media.", Source: "builtin"},
	{ID: "webrtc_uac", Name: "webrtc_uac", Role: "uac", Description: "WebRTC UAC: [webrtc_offer], synthetic rtp_stream over bridge.", Source: "builtin"},
	{ID: "webrtc_uas", Name: "webrtc_uas", Role: "uas", Description: "WebRTC UAS: [webrtc_answer], synthetic rtp_stream over bridge.", Source: "builtin"},
	{ID: "management", Name: "management", Role: "uas", Description: "Answer OPTIONS requests (management keep-alive).", Source: "builtin"},
}

// labCatalog is the kefir gateway lab port (same ids as kefir bundled TOML)
// plus inbound UAS twins (`{id}_uas`) for gateway Arm.
var labCatalog = []BuiltinInfo{
	{ID: "one_way", Name: "One-way audio (N1)", Role: "uac", Description: "SDP a=sendonly + synthetic RTP.", Source: "lab"},
	{ID: "one_way_uas", Name: "One-way audio (N1, inbound)", Role: "uas", Description: "Answer with a=sendonly + synthetic RTP.", Source: "lab"},
	{ID: "no_rtp", Name: "No RTP (N4)", Role: "uac", Description: "SDP a=inactive, no rtp_stream.", Source: "lab"},
	{ID: "no_rtp_uas", Name: "No RTP (N4, inbound)", Role: "uas", Description: "Answer with a=inactive, no rtp_stream.", Source: "lab"},
	{ID: "cn_only", Name: "Comfort Noise (N5)", Role: "uac", Description: "RTP PT 13 CN only.", Source: "lab"},
	{ID: "cn_only_uas", Name: "Comfort Noise (N5, inbound)", Role: "uas", Description: "Answer then RTP PT 13 CN only.", Source: "lab"},
	{ID: "audio_gap", Name: "Audio then gap (N6)", Role: "uac", Description: "2s RTP then re-INVITE a=inactive.", Source: "lab"},
	{ID: "audio_gap_uas", Name: "Audio then gap (N6, inbound)", Role: "uas", Description: "2s RTP then re-INVITE a=inactive toward caller.", Source: "lab"},
	{ID: "codec_chg", Name: "Codec change PT 0→8 (N8)", Role: "uac", Description: "PCMU then PCMA on the same 5-tuple.", Source: "lab"},
	{ID: "codec_chg_uas", Name: "Codec change PT 0→8 (N8, inbound)", Role: "uas", Description: "Answer PCMU then PCMA on the same 5-tuple.", Source: "lab"},
	{ID: "fas_cadence", Name: "FAS cadence (N13)", Role: "uac", Description: "1s on / 2s off ×3 after 1.5s grace.", Source: "lab"},
	{ID: "fas_cadence_uas", Name: "FAS cadence (N13, inbound)", Role: "uas", Description: "Answer then 1s on / 2s off ×3 after 1.5s grace.", Source: "lab"},
	{ID: "fas_speech", Name: "Speech not FAS (N14)", Role: "uac", Description: "Continuous synthetic G.711 for 10s.", Source: "lab"},
	{ID: "fas_speech_uas", Name: "Speech not FAS (N14, inbound)", Role: "uas", Description: "Answer then continuous synthetic G.711 for 10s.", Source: "lab"},
	{ID: "fas_early", Name: "FAS during grace (N16)", Role: "uac", Description: "Short cadence in the first 1.5s then speech.", Source: "lab"},
	{ID: "fas_early_uas", Name: "FAS during grace (N16, inbound)", Role: "uas", Description: "Answer then short cadence in the first 1.5s.", Source: "lab"},
	{ID: "fas_late", Name: "FAS after window (N16)", Role: "uac", Description: "Cadence starts 10s after CONNECT.", Source: "lab"},
	{ID: "fas_late_uas", Name: "FAS after window (N16, inbound)", Role: "uas", Description: "Answer then cadence 10s after CONNECT.", Source: "lab"},
	{ID: "fake_ringing", Name: "Fake ringing", Role: "uac", Description: "Same cadence as N13 (hepagent fake_ringing).", Source: "lab"},
	{ID: "fake_ringing_uas", Name: "Fake ringing (inbound)", Role: "uas", Description: "Answer then N13 cadence toward the caller.", Source: "lab"},
	{ID: "kws", Name: "KWS clip (approx)", Role: "uac", Description: "Synthetic PCMU stand-in for kefir kws.ulaw.", Source: "lab"},
	{ID: "kws_uas", Name: "KWS clip (approx, inbound)", Role: "uas", Description: "Answer then synthetic PCMU stand-in for kws.ulaw.", Source: "lab"},
	{ID: "late_180", Name: "Late 180 (FAS)", Role: "uas", Description: "INVITE 200 then illegal 180 (UAS only).", Source: "lab"},
	{ID: "hairpin", Name: "Hairpin RTP (N11, approx)", Role: "uac", Description: "Bidirectional synthetic RTP (no second local socket).", Source: "lab"},
	{ID: "hairpin_uas", Name: "Hairpin RTP (N11, inbound)", Role: "uas", Description: "Answer with bidirectional synthetic RTP.", Source: "lab"},
	{ID: "media_redirect", Name: "Media redirect (N12)", Role: "uac", Description: "re-INVITE c=127.0.0.2 at 2000 ms.", Source: "lab"},
	{ID: "media_redirect_uas", Name: "Media redirect (N12, inbound)", Role: "uas", Description: "Answer then re-INVITE c=127.0.0.2 toward caller.", Source: "lab"},
	{ID: "short_call", Name: "Short call (SHORT_CALL)", Role: "uac", Description: "BYE 1500 ms after CONNECT.", Source: "lab"},
	{ID: "short_call_uas", Name: "Short call (SHORT_CALL, inbound)", Role: "uas", Description: "Answer then BYE 1500 ms after CONNECT.", Source: "lab"},
	{ID: "fax_t38", Name: "T.38 fax (FAX_T38)", Role: "uac", Description: "INVITE m=image udptl t38.", Source: "lab"},
	{ID: "fax_t38_uas", Name: "T.38 fax (FAX_T38, inbound)", Role: "uas", Description: "Answer with m=image udptl t38.", Source: "lab"},
	{ID: "recvonly", Name: "Recvonly (N2)", Role: "uac", Description: "SDP a=recvonly, we do not send RTP.", Source: "lab"},
	{ID: "recvonly_uas", Name: "Recvonly (N2, inbound)", Role: "uas", Description: "Answer with a=recvonly, no TX.", Source: "lab"},
	{ID: "hold_moh", Name: "Hold with MoH", Role: "uac", Description: "re-INVITE a=sendonly at 2000 ms, keep TX.", Source: "lab"},
	{ID: "hold_moh_uas", Name: "Hold with MoH (inbound)", Role: "uas", Description: "Answer then re-INVITE a=sendonly toward caller.", Source: "lab"},
	{ID: "hold_resume", Name: "Hold then resume", Role: "uac", Description: "re-INVITE inactive then sendrecv.", Source: "lab"},
	{ID: "hold_resume_uas", Name: "Hold then resume (inbound)", Role: "uas", Description: "Answer then hold/resume re-INVITE toward caller.", Source: "lab"},
	{ID: "port_move", Name: "RTP port move", Role: "uac", Description: "re-INVITE same c=, m= [media_port+2].", Source: "lab"},
	{ID: "port_move_uas", Name: "RTP port move (inbound)", Role: "uas", Description: "Answer then re-INVITE m=[media_port+2] toward caller.", Source: "lab"},
	{ID: "codec_chg_alaw", Name: "Codec change PT 8→0", Role: "uac", Description: "PCMA then PCMU on the same 5-tuple.", Source: "lab"},
	{ID: "codec_chg_alaw_uas", Name: "Codec change PT 8→0 (inbound)", Role: "uas", Description: "Answer PCMA then PCMU on the same 5-tuple.", Source: "lab"},
	{ID: "rtp_mute", Name: "RTP mute", Role: "uac", Description: "sendrecv SDP, no rtp_stream.", Source: "lab"},
	{ID: "rtp_mute_uas", Name: "RTP mute (inbound)", Role: "uas", Description: "Answer sendrecv with no rtp_stream.", Source: "lab"},
	{ID: "g711_silence", Name: "G.711 silence", Role: "uac", Description: "Synthetic PCMU μ-law 0xFF frames.", Source: "lab"},
	{ID: "g711_silence_uas", Name: "G.711 silence (inbound)", Role: "uas", Description: "Answer then synthetic PCMU μ-law 0xFF.", Source: "lab"},
	{ID: "cn_then_speech", Name: "CN then speech", Role: "uac", Description: "PT 13 then PCMU at 2000 ms.", Source: "lab"},
	{ID: "cn_then_speech_uas", Name: "CN then speech (inbound)", Role: "uas", Description: "Answer PT 13 then PCMU at 2000 ms.", Source: "lab"},
	{ID: "early_183", Name: "Early media (183)", Role: "uas", Description: "UAS 183+SDP and RTP before CONNECT.", Source: "lab"},
	{ID: "fax_switch", Name: "Voice then T.38", Role: "uac", Description: "G.711 then re-INVITE m=image at 2000 ms.", Source: "lab"},
	{ID: "fax_switch_uas", Name: "Voice then T.38 (inbound)", Role: "uas", Description: "Answer G.711 then re-INVITE T.38 toward caller.", Source: "lab"},
}

// ListBuiltins returns metadata for engine-baked scenarios (read-only).
func ListBuiltins() []BuiltinInfo {
	out := make([]BuiltinInfo, 0, len(builtinCatalog)+len(labCatalog))
	out = append(out, builtinCatalog...)
	out = append(out, labCatalog...)
	return out
}

// BuiltinXML returns the raw XML for a built-in scenario id.
func BuiltinXML(id string) (string, error) {
	id = strings.TrimSpace(id)
	switch id {
	case "uac", "":
		return defaultUAC, nil
	case "uas":
		return defaultUAS, nil
	case "invite_media":
		return defaultInviteMedia, nil
	case BuiltinInviteMediaScale:
		return defaultInviteMediaScale, nil
	case "invite_media_early":
		return defaultInviteMediaEarly, nil
	case "invite_media_savpf":
		return defaultInviteMediaSavpf, nil
	case "invite_media_early_180":
		return defaultInviteMediaEarly180, nil
	case "management":
		return defaultManagement, nil
	case "webrtc_uas":
		return defaultWebRTCUAS, nil
	case "webrtc_uac":
		return defaultWebRTCUAC, nil
	default:
		raw, err := labFS.ReadFile("lab/" + id + ".xml")
		if err != nil {
			return "", ErrUnknownScenario(id)
		}
		return string(raw), nil
	}
}
