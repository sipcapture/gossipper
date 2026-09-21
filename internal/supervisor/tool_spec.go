package supervisor

import (
	"strings"
)

// IsToolJob reports whether the worker should run a stress-tool command
// instead of a SIP engine profile.
func (s Spec) IsToolJob() bool {
	return strings.EqualFold(strings.TrimSpace(s.ProfileKind), ToolProfileKind)
}

// ToolProfileKind is the ProfileKind value stored for tool jobs.
const ToolProfileKind = "tool"

// GatewayProfileKind is the ProfileKind for SIP REGISTER gateway originate jobs.
const GatewayProfileKind = "gateway"

// ToolID returns the tool name for tool jobs (stored in ProfileID).
func (s Spec) ToolID() string {
	if !s.IsToolJob() {
		return ""
	}
	return strings.TrimSpace(s.ProfileID)
}
