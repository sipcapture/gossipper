package scenario

import "strings"

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
	{ID: "uas", Name: "Basic Gossip UAS", Role: "uas", Description: "Answer INVITE with 200 OK and handle BYE.", Source: "builtin"},
	{ID: "invite_media", Name: "invite_media", Role: "uac", Description: "UAC INVITE with SDP and RTP media.", Source: "builtin"},
	{ID: "invite_media_early", Name: "invite_media_early", Role: "uac", Description: "UAC media flow with early media (183).", Source: "builtin"},
	{ID: "invite_media_savpf", Name: "invite_media_savpf", Role: "uac", Description: "UAC media flow with SAVPF SDP profile.", Source: "builtin"},
	{ID: "invite_media_early_180", Name: "invite_media_early_180", Role: "uac", Description: "UAC media flow with 180 Ringing early media.", Source: "builtin"},
	{ID: "management", Name: "management", Role: "uas", Description: "Answer OPTIONS requests (management keep-alive).", Source: "builtin"},
}

// ListBuiltins returns metadata for engine-baked scenarios (read-only).
func ListBuiltins() []BuiltinInfo {
	out := make([]BuiltinInfo, len(builtinCatalog))
	copy(out, builtinCatalog)
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
	case "invite_media_early":
		return defaultInviteMediaEarly, nil
	case "invite_media_savpf":
		return defaultInviteMediaSavpf, nil
	case "invite_media_early_180":
		return defaultInviteMediaEarly180, nil
	case "management":
		return defaultManagement, nil
	default:
		return "", ErrUnknownScenario(id)
	}
}
