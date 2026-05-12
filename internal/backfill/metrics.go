package backfill

import (
	"encoding/json"
	"time"
)

// MetricsSnapshot is one periodic aggregate emitted every MetricsInterval.
type MetricsSnapshot struct {
	Timestamp    time.Time      `json:"timestamp"`
	IntervalSec  float64        `json:"interval_sec"`
	CallsStarted int            `json:"calls_started"`
	CallsEnded   int            `json:"calls_ended"`
	CallsActive  int            `json:"calls_active"`
	CallsFailed  int            `json:"calls_failed"`
	CPS          float64        `json:"cps"`
	ASR          float64        `json:"asr"`          // answer-seizure ratio
	ACDSec       float64        `json:"acd_sec"`      // average call duration
	Methods      map[string]int `json:"methods"`
	Responses    map[string]int `json:"responses"`
}

// metricsAccumulator tracks stats within a single metrics interval.
type metricsAccumulator struct {
	intervalStart time.Time
	intervalDur   time.Duration
	started       int
	ended         int
	failed        int
	active        int
	totalDurSec   float64
	methods       map[string]int
	responses     map[string]int
}

func newMetricsAccumulator(start time.Time, interval time.Duration) *metricsAccumulator {
	return &metricsAccumulator{
		intervalStart: start,
		intervalDur:   interval,
		methods:       make(map[string]int),
		responses:     make(map[string]int),
	}
}

func (a *metricsAccumulator) addCall(c syntheticCall) {
	a.started++
	a.ended++
	if c.Failed {
		a.failed++
	}
	a.totalDurSec += c.Duration.Seconds()

	for _, msg := range c.Messages {
		payload := string(msg.Payload)
		if len(payload) > 20 {
			first := payload[:20]
			if first[0] == 'S' && len(payload) > 8 && payload[:4] == "SIP/" {
				// Response line: "SIP/2.0 CODE ..."
				if len(payload) > 12 {
					code := payload[8:11]
					a.responses[code]++
				}
			} else {
				// Request line: "METHOD sip:..."
				for i := 0; i < len(first); i++ {
					if first[i] == ' ' {
						a.methods[first[:i]]++
						break
					}
				}
			}
		}
	}
}

func (a *metricsAccumulator) snapshot() MetricsSnapshot {
	intervalSec := a.intervalDur.Seconds()
	cps := 0.0
	if intervalSec > 0 {
		cps = float64(a.started) / intervalSec
	}
	asr := 0.0
	if a.started > 0 {
		asr = float64(a.started-a.failed) / float64(a.started)
	}
	acd := 0.0
	answered := a.started - a.failed
	if answered > 0 {
		acd = a.totalDurSec / float64(answered)
	}

	methods := make(map[string]int, len(a.methods))
	for k, v := range a.methods {
		methods[k] = v
	}
	responses := make(map[string]int, len(a.responses))
	for k, v := range a.responses {
		responses[k] = v
	}

	return MetricsSnapshot{
		Timestamp:    a.intervalStart.Add(a.intervalDur),
		IntervalSec:  intervalSec,
		CallsStarted: a.started,
		CallsEnded:   a.ended,
		CallsActive:  a.active,
		CallsFailed:  a.failed,
		CPS:          cps,
		ASR:          asr,
		ACDSec:       acd,
		Methods:      methods,
		Responses:    responses,
	}
}

func (a *metricsAccumulator) reset(newStart time.Time) {
	a.intervalStart = newStart
	a.started = 0
	a.ended = 0
	a.failed = 0
	a.active = 0
	a.totalDurSec = 0
	for k := range a.methods {
		delete(a.methods, k)
	}
	for k := range a.responses {
		delete(a.responses, k)
	}
}

// marshalMetrics serializes a snapshot to JSON.
func marshalMetrics(s MetricsSnapshot) ([]byte, error) {
	return json.Marshal(s)
}
