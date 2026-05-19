package uistore

import "time"

// ProfileKind enumerates the supported profile kinds.
type ProfileKind string

const (
	KindServer ProfileKind = "server"
	KindClient ProfileKind = "client"
	KindTool   ProfileKind = "tool"
)

// TransportSpec is a single SIP listener (server) or bind (client) entry.
//
// Transport codes follow the gossipper convention:
//
//	u1/un  → UDP (single bind / N-bind)
//	t1/tn  → TCP
//	l1/ln  → TLS
//	w1/wn  → WebSocket (plain) — Phase 4
//	ws1/wsn → WebSocket Secure — Phase 4
type TransportSpec struct {
	Transport string `json:"transport"`
	LocalIP   string `json:"local_ip,omitempty"`
	LocalPort int    `json:"local_port,omitempty"`
	// Enabled controls whether the listener starts on engine boot
	// (servers) / whether the client engine begins accepting work.
	Enabled bool `json:"enabled"`
	// Optional TLS material for `l1/ln` / `wss*` listeners (paths or PEM).
	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`
	// WSPath is the HTTP path used by the WebSocket transport handshake.
	// Defaults to "/" when empty.
	WSPath string `json:"ws_path,omitempty"`
	// WebRTC parameters — populated when Transport == "webrtc". ICEServers
	// is a list of STUN / TURN URLs (e.g. "stun:stun.l.google.com:19302").
	// ICEUsername / ICECredential are sent on every server URL that asks for
	// auth; most public STUN servers ignore them.
	ICEServers    []string `json:"ice_servers,omitempty"`
	ICEUsername   string   `json:"ice_username,omitempty"`
	ICECredential string   `json:"ice_credential,omitempty"`
	ICEAuthSecret string   `json:"ice_auth_secret,omitempty"`
	ICEAuthTTLSec int      `json:"ice_auth_ttl_sec,omitempty"`
	// PrefersPCMA picks PCMA (G.711 a-law) over PCMU when both are offered.
	PrefersPCMA bool `json:"prefers_pcma,omitempty"`
}

// SourceBuiltIn marks profiles that were seeded by the management process
// from cfg.Server / cfg.JoinedClients on first start. The UI should disable
// Start/Stop controls for these and the v2 shortcut handlers refuse to fork
// a supervisor worker, since the bind is already owned by the master.
const SourceBuiltIn = "built-in"

// ServerProfile is the editable representation of a SIP server (UAS) profile.
type ServerProfile struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	ScenarioRef   string          `json:"scenario_ref,omitempty"`
	Transports    []TransportSpec `json:"transports"`
	MaxConcurrent int             `json:"max_concurrent,omitempty"`
	Notes         string          `json:"notes,omitempty"`
	// Source flags how this profile arrived. Empty == user-created from UI.
	// SourceBuiltIn == seeded from management JSON; static, cannot be started
	// as a supervisor job (would collide on bind).
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ClientProfile is the editable representation of a SIP client (UAC) profile
// — what the UI uses to seed a future "Start job" command.
type ClientProfile struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	ScenarioRef   string          `json:"scenario_ref,omitempty"`
	Transports    []TransportSpec `json:"transports"`
	RemoteIP      string          `json:"remote_ip,omitempty"`
	RemotePort    int             `json:"remote_port,omitempty"`
	Rate          float64         `json:"rate,omitempty"`
	MaxConcurrent int             `json:"max_concurrent,omitempty"`
	DurationMs    int             `json:"duration_ms,omitempty"`
	Notes         string          `json:"notes,omitempty"`
	// Source: see ServerProfile.Source.
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ScenarioHistoryEntry describes a single archived prior version of a
// scenario. The Timestamp/TS pair is the same instant in two forms — TS is
// the filename-safe stamp the API uses as a path segment, Timestamp is
// re-parsed for client-side display.
type ScenarioHistoryEntry struct {
	TS        string       `json:"ts"`             // e.g. "20260518T170230.123456789Z"
	Timestamp time.Time    `json:"timestamp"`      // parsed UTC instant
	SizeBytes int64        `json:"size_bytes"`     // archived XML byte length
	Meta      ScenarioMeta `json:"meta,omitempty"` // sidecar at the time of archival
}

// ScenarioMeta is the sidecar JSON for a scenario XML file. The XML body is
// stored separately to keep diffs friendly.
type ScenarioMeta struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	// Role hints whether the scenario is intended for server (UAS) or client
	// (UAC) profiles; empty means "either".
	Role      string    `json:"role,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MediaKind enumerates supported media asset kinds.
type MediaKind string

const (
	MediaWav  MediaKind = "wav"
	MediaPcap MediaKind = "pcap"
)

// MediaAsset describes a single uploaded WAV / PCAP file.
type MediaAsset struct {
	Kind      MediaKind `json:"kind"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	ModTime   time.Time `json:"mod_time"`
}
