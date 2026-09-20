package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	StateOff            = "off"
	StateRegistering    = "registering"
	StateRegistered     = "registered"
	StateFailed         = "failed"
	DefaultExpiresSec   = 300
	DefaultContactPort  = 5060
	DefaultKeepaliveSec = 20
	maskedPassword      = "***"
)

var profileIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Config is one SIP REGISTER gateway profile (PBX AOR).
type Config struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Enabled          bool   `json:"enabled"`
	Domain           string `json:"domain"`
	Addr             string `json:"addr"`
	Transport        string `json:"transport,omitempty"`
	Username         string `json:"username"`
	Password         string `json:"password,omitempty"`
	Register         bool   `json:"register"`
	RegisterUser     string `json:"register_user,omitempty"`
	AdvertisedIP     string `json:"advertised_ip,omitempty"`
	RegisterExpires  int    `json:"register_expires,omitempty"`
	KeepaliveSeconds int    `json:"keepalive_seconds,omitempty"`
	ContactPort      int    `json:"contact_port,omitempty"`
	// ArmedScenarioID is the inbound UAS scenario for this AOR (persist across restart).
	ArmedScenarioID string `json:"armed_scenario_id,omitempty"`
	// OriginateScenarioID / OriginateTo / OriginateCalls are the last UAC originate defaults.
	OriginateScenarioID string `json:"originate_scenario_id,omitempty"`
	OriginateTo         string `json:"originate_to,omitempty"`
	OriginateCalls      int    `json:"originate_calls,omitempty"`
}

// Normalize fills defaults. Transport is udp in v1.
func (c *Config) Normalize() {
	if c == nil {
		return
	}
	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Domain = strings.TrimSpace(c.Domain)
	c.Addr = strings.TrimSpace(c.Addr)
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	if c.Transport == "" || c.Transport == "u1" || c.Transport == "udp" {
		c.Transport = "udp"
	}
	c.Username = strings.TrimSpace(c.Username)
	c.RegisterUser = strings.TrimSpace(c.RegisterUser)
	c.AdvertisedIP = strings.TrimSpace(c.AdvertisedIP)
	c.ArmedScenarioID = strings.TrimSpace(c.ArmedScenarioID)
	c.OriginateScenarioID = strings.TrimSpace(c.OriginateScenarioID)
	c.OriginateTo = strings.TrimSpace(c.OriginateTo)
	if c.OriginateCalls < 0 {
		c.OriginateCalls = 0
	}
	if c.RegisterExpires <= 0 {
		c.RegisterExpires = DefaultExpiresSec
	}
	// ContactPort 0 means "unset"; ContactURI and the launcher fill the UAS listen port.
	if host, port, err := SplitAddr(c.Addr); err == nil {
		if port == 0 {
			c.Addr = net.JoinHostPort(host, "5060")
		} else {
			c.Addr = net.JoinHostPort(host, strconv.Itoa(port))
		}
	}
}

// AORUser is the SIP user in From/To/Contact (register_user, else username).
func (c Config) AORUser() string {
	if u := strings.TrimSpace(c.RegisterUser); u != "" {
		return u
	}
	return strings.TrimSpace(c.Username)
}

// AOR is user@domain.
func (c Config) AOR() string {
	return c.AORUser() + "@" + strings.TrimSpace(c.Domain)
}

// SIPFrom is sip:{aor}@{domain} for UAC -sip_from / [trunk_from].
func (c Config) SIPFrom() string {
	return "sip:" + c.AOR()
}

// AuthUsername is Digest username (username, else AOR user).
func (c Config) AuthUsername() string {
	if u := strings.TrimSpace(c.Username); u != "" {
		return u
	}
	return c.AORUser()
}

// Expires is the requested REGISTER TTL.
func (c Config) Expires() time.Duration {
	sec := c.RegisterExpires
	if sec <= 0 {
		sec = DefaultExpiresSec
	}
	return time.Duration(sec) * time.Second
}

// Keepalive is the NAT UDP ping interval toward the registrar.
// 0 (omitted) uses DefaultKeepaliveSec; negative disables.
func (c Config) Keepalive() time.Duration {
	sec := c.KeepaliveSeconds
	if sec < 0 {
		return 0
	}
	if sec == 0 {
		sec = DefaultKeepaliveSec
	}
	return time.Duration(sec) * time.Second
}

// ContactURI is sip:{aor}@{advertised_ip}:{contact_port}.
func (c Config) ContactURI() string {
	return c.ContactURIAt(c.ContactPort)
}

// ContactURIAt is ContactURI with an explicit port (the live UAS/REGISTER socket).
func (c Config) ContactURIAt(port int) string {
	ip := strings.TrimSpace(c.AdvertisedIP)
	if ip == "" {
		ip = "127.0.0.1"
	}
	if port <= 0 {
		port = DefaultContactPort
	}
	return fmt.Sprintf("sip:%s@%s:%d", c.AORUser(), ip, port)
}

// HasRegistrar is true when domain, addr, and AOR user are set.
func (c Config) HasRegistrar() bool {
	return c.AORUser() != "" && strings.TrimSpace(c.Domain) != "" && strings.TrimSpace(c.Addr) != ""
}

// ShouldRegister reports whether this profile should run the Digest REGISTER loop.
func (c Config) ShouldRegister() bool {
	return c.Enabled && c.Register && c.HasRegistrar()
}

// CanOriginate reports whether originate may use this profile.
func (c Config) CanOriginate() bool {
	return c.Enabled && c.HasRegistrar()
}

// NewProfileID returns a short unique gateway profile id.
func NewProfileID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("gw-%d", time.Now().UnixNano())
	}
	return "gw-" + hex.EncodeToString(b[:])
}

// ValidProfileID reports whether id is usable as a path segment.
func ValidProfileID(id string) bool {
	return profileIDRe.MatchString(strings.TrimSpace(id))
}

// EnsureIdentity fills id/name when missing. defaultOn is used by callers that
// already decided the enabled flag (legacy migrate / CLI omitted key).
func EnsureIdentity(c *Config) {
	if c == nil {
		return
	}
	c.Normalize()
	if c.ID == "" {
		c.ID = NewProfileID()
	}
	if c.Name == "" {
		if u := c.AORUser(); u != "" {
			c.Name = u
		} else {
			c.Name = "Gateway"
		}
	}
}

// Masked returns a copy with password replaced when set.
func (c Config) Masked() Config {
	out := c
	if strings.TrimSpace(out.Password) != "" {
		out.Password = maskedPassword
	}
	return out
}

// MergePassword keeps the previous password when the incoming value is empty or masked.
func MergePassword(prev, next Config) Config {
	out := next
	p := strings.TrimSpace(next.Password)
	if p == "" || p == maskedPassword {
		out.Password = prev.Password
	}
	return out
}

// SplitAddr parses host:port with default SIP port 5060.
func SplitAddr(addr string) (host string, port int, err error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0, fmt.Errorf("empty registrar addr")
	}
	if !strings.Contains(addr, ":") {
		return addr, 5060, nil
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, err
	}
	return h, n, nil
}
