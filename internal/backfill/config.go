package backfill

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config holds all parameters for a backfill run.
type Config struct {
	// Duration is how far back from Now to generate data.
	Duration time.Duration
	// CPS is the average calls-per-second to simulate.
	CPS float64
	// MetricsInterval controls how often aggregate metric snapshots are emitted.
	MetricsInterval time.Duration

	// HEP target.
	HEPAddr      string
	HEPCaptureID uint32
	HEPPassword  string

	// SBC log output path (JSON-Lines). Empty disables.
	SBCLogFile string
	// SBC metrics output path (JSON-Lines). Empty disables.
	SBCMetricsFile string

	// Network identity for synthetic SIP messages.
	SrcIP   string
	DstIP   string
	SrcPort int
	DstPort int

	// Call characteristics.
	CallDurationMin time.Duration
	CallDurationMax time.Duration
	FailRatio       float64 // 0.0–1.0, fraction of calls that fail

	// ScenarioFile is an optional XML scenario whose send templates are used
	// for SIP message rendering. Empty uses the built-in backfill dialog.
	ScenarioFile string

	// Progress controls whether a progress line is printed to stderr.
	Progress bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Duration:        24 * time.Hour,
		CPS:             1.0,
		MetricsInterval: 60 * time.Second,
		SrcIP:           "10.0.0.1",
		DstIP:           "10.0.0.2",
		SrcPort:         5060,
		DstPort:         5060,
		CallDurationMin: 10 * time.Second,
		CallDurationMax: 120 * time.Second,
		FailRatio:       0.03,
		Progress:        true,
	}
}

// Validate checks that the config is usable.
func (c Config) Validate() error {
	if c.Duration <= 0 {
		return fmt.Errorf("backfill: duration must be positive, got %v", c.Duration)
	}
	if c.CPS <= 0 {
		return fmt.Errorf("backfill: cps must be positive, got %v", c.CPS)
	}
	if c.HEPAddr == "" && c.SBCLogFile == "" && c.SBCMetricsFile == "" {
		return fmt.Errorf("backfill: at least one output must be configured (-hep_addr, -sbc_log_file, or -sbc_metrics_file)")
	}
	if c.CallDurationMin <= 0 || c.CallDurationMax <= 0 || c.CallDurationMin > c.CallDurationMax {
		return fmt.Errorf("backfill: invalid call duration range [%v, %v]", c.CallDurationMin, c.CallDurationMax)
	}
	if c.FailRatio < 0 || c.FailRatio > 1 {
		return fmt.Errorf("backfill: fail_ratio must be in [0, 1], got %v", c.FailRatio)
	}
	return nil
}

var durationRe = regexp.MustCompile(`(?i)^(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$`)

// ParseDuration extends time.ParseDuration with support for "d" (days).
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	// Try stdlib first for plain durations like "2h30m".
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	m := durationRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 30d, 7d12h, 2h30m)", s)
	}
	var total time.Duration
	if m[1] != "" {
		n, _ := strconv.Atoi(m[1])
		total += time.Duration(n) * 24 * time.Hour
	}
	if m[2] != "" {
		n, _ := strconv.Atoi(m[2])
		total += time.Duration(n) * time.Hour
	}
	if m[3] != "" {
		n, _ := strconv.Atoi(m[3])
		total += time.Duration(n) * time.Minute
	}
	if m[4] != "" {
		n, _ := strconv.Atoi(m[4])
		total += time.Duration(n) * time.Second
	}
	if total == 0 {
		return 0, fmt.Errorf("duration %q resolves to zero", s)
	}
	return total, nil
}
