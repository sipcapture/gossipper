package stats

import (
	"fmt"
	"sort"
	"strings"
)

// SummarySchemaVersion is the JSON envelope version written with -summary_json.
const SummarySchemaVersion = "gossipper_summary_v1"

// HealthConfig selects optional post-run checks (CI gates). Zero values disable each check.
type HealthConfig struct {
	// MinSuccessRatio, when > 0, requires SuccessRatio >= this value (e.g. 0.95).
	MinSuccessRatio float64
	// MaxFailedCalls: when >= 0, fails if FailedCalls > MaxFailedCalls (0 = any failure fails the run).
	MaxFailedCalls int
	// MaxTimeouts: when >= 0, fails if Timeouts > MaxTimeouts.
	MaxTimeouts int
}

func (h HealthConfig) Active() bool {
	return h.MinSuccessRatio > 0 || h.MaxFailedCalls >= 0 || h.MaxTimeouts >= 0
}

// HealthSummary is included in -summary_json when health checks are enabled.
type HealthSummary struct {
	Active  bool     `json:"active"`
	Pass    bool     `json:"pass"`
	Reasons []string `json:"reasons,omitempty"`
}

// EvaluateHealth runs configured checks against a finalized summary snapshot.
func EvaluateHealth(cfg HealthConfig, s Summary) (*HealthSummary, []string) {
	if !cfg.Active() {
		return nil, nil
	}
	out := &HealthSummary{Active: true, Pass: true}
	var reasons []string

	if cfg.MinSuccessRatio > 0 && s.SuccessRatio+1e-9 < cfg.MinSuccessRatio {
		out.Pass = false
		msg := fmt.Sprintf("success_ratio %.4f below minimum %.4f", s.SuccessRatio, cfg.MinSuccessRatio)
		out.Reasons = append(out.Reasons, msg)
		reasons = append(reasons, msg)
	}
	if cfg.MaxFailedCalls >= 0 && s.FailedCalls > cfg.MaxFailedCalls {
		out.Pass = false
		msg := fmt.Sprintf("failed_calls %d exceeds maximum %d", s.FailedCalls, cfg.MaxFailedCalls)
		out.Reasons = append(out.Reasons, msg)
		reasons = append(reasons, msg)
	}
	if cfg.MaxTimeouts >= 0 && s.Timeouts > cfg.MaxTimeouts {
		out.Pass = false
		msg := fmt.Sprintf("timeouts %d exceeds maximum %d", s.Timeouts, cfg.MaxTimeouts)
		out.Reasons = append(out.Reasons, msg)
		reasons = append(reasons, msg)
	}
	return out, reasons
}

// BuildFindings returns short human-oriented lines for JSON and logs.
// healthReasons should be the same slice returned by EvaluateHealth when checks fail.
func BuildFindings(s Summary, healthReasons []string) []string {
	var lines []string
	if len(healthReasons) > 0 {
		lines = append(lines, "Health check: FAIL — "+strings.Join(healthReasons, "; "))
	} else if s.Health != nil && s.Health.Active && s.Health.Pass {
		lines = append(lines, "Health check: PASS")
	}

	if s.TotalCalls == 0 {
		lines = append(lines, "No calls were started.")
		return lines
	}
	finished := s.SuccessCalls + s.FailedCalls
	pct := 0.0
	if s.TotalCalls > 0 {
		pct = 100.0 * float64(s.SuccessCalls) / float64(s.TotalCalls)
	}
	lines = append(lines, fmt.Sprintf(
		"Calls: total=%d finished=%d success=%d failed=%d (success %.1f%% of total).",
		s.TotalCalls, finished, s.SuccessCalls, s.FailedCalls, pct,
	))
	if s.Timeouts > 0 {
		lines = append(lines, fmt.Sprintf("SIP timeouts: %d", s.Timeouts))
	}
	if len(s.FailureClasses) > 0 {
		type kv struct {
			k string
			v int
		}
		var list []kv
		for k, v := range s.FailureClasses {
			list = append(list, kv{k: k, v: v})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].v == list[j].v {
				return list[i].k < list[j].k
			}
			return list[i].v > list[j].v
		})
		n := len(list)
		if n > 3 {
			n = 3
		}
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, fmt.Sprintf("%s=%d", list[i].k, list[i].v))
		}
		lines = append(lines, "Top failure classes: "+strings.Join(parts, ", "))
	}
	if s.Media.RTCPReceptionReports > 0 {
		line := fmt.Sprintf(
			"RTCP QoS: reception_reports=%d max_fraction_lost=%.4f max_jitter_ts=%d",
			s.Media.RTCPReceptionReports, s.Media.RTCPMaxFractionLost, s.Media.RTCPMaxJitterTS,
		)
		if s.Media.RTCPAvgJitterTS > 0 {
			line += fmt.Sprintf(" avg_jitter_ts=%.2f", s.Media.RTCPAvgJitterTS)
		}
		lines = append(lines, line)
	}
	return lines
}
