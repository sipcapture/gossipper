package stats

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sipcapture/gossipper/internal/media"
)

type Collector struct {
	mu              sync.Mutex
	startedAt       time.Time
	totalCalls      int
	successCalls    int
	failedCalls     int
	activeCalls     int
	retransmits     int
	timeouts        int
	totalDuration   time.Duration
	inviteLatencies []time.Duration
	callLatency     latencyAccumulator
	inviteLatency   latencyAccumulator
	media           MediaSummary
	rtds            map[string]latencyAccumulator
	counters        map[string]int
	displays        map[string]int
	failureClasses  map[string]int
}

type MediaSummary struct {
	RTPPacketsSent      uint32 `json:"rtp_packets_sent"`
	RTPOctetsSent       uint32 `json:"rtp_octets_sent"`
	RTPPacketsReceived  uint32 `json:"rtp_packets_received"`
	RTCPSenderReports   uint32 `json:"rtcp_sender_reports"`
	RTCPReceiverReports uint32 `json:"rtcp_receiver_reports"`
	RTCPPacketsReceived uint32 `json:"rtcp_packets_received"`
	// RTCPReceptionReports counts ReceptionReport blocks from inbound RTCP RR (RFC 3550).
	RTCPReceptionReports uint32 `json:"rtcp_reception_reports,omitempty"`
	// RTCPMaxFractionLost is the maximum observed fraction lost (0..1) across reports.
	RTCPMaxFractionLost float64 `json:"rtcp_max_fraction_lost,omitempty"`
	// RTCPMaxJitterTS is the maximum reported interarrival jitter in RTP timestamp units.
	RTCPMaxJitterTS uint32 `json:"rtcp_max_jitter_ts,omitempty"`
	// RTCPAvgJitterTS is the mean jitter across all sampled reception report blocks (timestamp units).
	RTCPAvgJitterTS float64 `json:"rtcp_avg_jitter_ts,omitempty"`

	rtcpJitterSum     float64 `json:"-"`
	rtcpJitterSamples uint64  `json:"-"`
}

type LatencyBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type LatencySummary struct {
	Count   int             `json:"count"`
	Average time.Duration   `json:"average"`
	Min     time.Duration   `json:"min"`
	Max     time.Duration   `json:"max"`
	Last    time.Duration   `json:"last"`
	StdDev  time.Duration   `json:"stddev"`
	Buckets []LatencyBucket `json:"buckets,omitempty"`
}

type Summary struct {
	SchemaVersion      string                    `json:"schema_version,omitempty"`
	ToolVersion        string                    `json:"tool_version,omitempty"`
	StartedAt          time.Time                 `json:"started_at"`
	FinishedAt         time.Time                 `json:"finished_at"`
	Duration           time.Duration             `json:"duration"`
	TotalCalls         int                       `json:"total_calls"`
	SuccessCalls       int                       `json:"success_calls"`
	FailedCalls        int                       `json:"failed_calls"`
	ActiveCalls        int                       `json:"active_calls"`
	SuccessRatio       float64                   `json:"success_ratio"`
	CallsPerSecond     float64                   `json:"calls_per_second"`
	Retransmits        int                       `json:"retransmits"`
	Timeouts           int                       `json:"timeouts"`
	AverageCallLatency time.Duration             `json:"average_call_latency"`
	AverageInviteRTT   time.Duration             `json:"average_invite_rtt"`
	CallLength         *LatencySummary           `json:"call_length,omitempty"`
	InviteRTT          *LatencySummary           `json:"invite_rtt,omitempty"`
	Media              MediaSummary              `json:"media"`
	RTD                map[string]LatencySummary `json:"rtd,omitempty"`
	Counters           map[string]int            `json:"counters,omitempty"`
	Displays           map[string]int            `json:"displays,omitempty"`
	FailureClasses     map[string]int            `json:"failure_classes,omitempty"`
	Health             *HealthSummary            `json:"health,omitempty"`
	Findings           []string                  `json:"findings,omitempty"`
}

var latencyBucketBoundsMS = []int64{10, 20, 50, 100, 200, 500, 1000, 2000, 5000}

func New() *Collector {
	return &Collector{
		startedAt:      time.Now(),
		rtds:           make(map[string]latencyAccumulator),
		counters:       make(map[string]int),
		displays:       make(map[string]int),
		failureClasses: make(map[string]int),
	}
}

func (c *Collector) StartCall() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalCalls++
	c.activeCalls++
}

func (c *Collector) FinishCall(success bool, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeCalls--
	if success {
		c.successCalls++
	} else {
		c.failedCalls++
	}
	c.totalDuration += duration
	c.callLatency.add(duration)
}

func (c *Collector) AddInviteLatency(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inviteLatencies = append(c.inviteLatencies, d)
	c.inviteLatency.add(d)
}

func (c *Collector) AddRetransmit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.retransmits++
}

func (c *Collector) AddTimeout() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeouts++
}

func (c *Collector) AddMediaStats(value media.Stats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.media.RTPPacketsSent += value.RTPPacketsSent
	c.media.RTPOctetsSent += value.RTPOctetsSent
	c.media.RTPPacketsReceived += value.RTPPacketsReceived
	c.media.RTCPSenderReports += value.RTCPSenderReports
	c.media.RTCPReceiverReports += value.RTCPReceiverReports
	c.media.RTCPPacketsReceived += value.RTCPPacketsReceived
	c.media.RTCPReceptionReports += value.RTCPReportBlocks
	if value.RTCPMaxFractionLost > 0 {
		ratio := float64(value.RTCPMaxFractionLost) / 256.0
		if ratio > c.media.RTCPMaxFractionLost {
			c.media.RTCPMaxFractionLost = ratio
		}
	}
	if value.RTCPMaxJitter > c.media.RTCPMaxJitterTS {
		c.media.RTCPMaxJitterTS = value.RTCPMaxJitter
	}
	c.media.rtcpJitterSum += value.RTCPJitterSum
	c.media.rtcpJitterSamples += value.RTCPJitterSamples
}

func (c *Collector) AddRTD(name string, value time.Duration) {
	if name == "" || value < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	acc := c.rtds[name]
	acc.add(value)
	c.rtds[name] = acc
}

func (c *Collector) AddCounter(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters[name]++
}

func (c *Collector) AddDisplay(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.displays[name]++
}

func (c *Collector) AddFailureClass(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failureClasses[name]++
}

func (c *Collector) Snapshot() Summary {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	duration := now.Sub(c.startedAt)
	successRatio := 0.0
	cps := 0.0
	if c.totalCalls > 0 {
		successRatio = float64(c.successCalls) / float64(c.totalCalls)
	}
	if duration > 0 {
		cps = float64(c.totalCalls) / duration.Seconds()
	}

	var avgCall time.Duration
	if finished := c.successCalls + c.failedCalls; finished > 0 {
		avgCall = time.Duration(int64(c.totalDuration) / int64(finished))
	}

	var avgInvite time.Duration
	if len(c.inviteLatencies) > 0 {
		var total time.Duration
		for _, latency := range c.inviteLatencies {
			total += latency
		}
		avgInvite = time.Duration(int64(total) / int64(len(c.inviteLatencies)))
	}

	var rtdSummary map[string]LatencySummary
	if len(c.rtds) > 0 {
		rtdSummary = make(map[string]LatencySummary, len(c.rtds))
		for name, acc := range c.rtds {
			if summary, ok := acc.snapshot(); ok {
				rtdSummary[name] = summary
			}
		}
	}

	var counters map[string]int
	if len(c.counters) > 0 {
		counters = make(map[string]int, len(c.counters))
		for name, value := range c.counters {
			counters[name] = value
		}
	}

	var displays map[string]int
	if len(c.displays) > 0 {
		displays = make(map[string]int, len(c.displays))
		for name, value := range c.displays {
			displays[name] = value
		}
	}

	var failureClasses map[string]int
	if len(c.failureClasses) > 0 {
		failureClasses = make(map[string]int, len(c.failureClasses))
		for name, value := range c.failureClasses {
			failureClasses[name] = value
		}
	}

	callLength, callLengthOK := c.callLatency.snapshot()
	inviteRTT, inviteRTTOK := c.inviteLatency.snapshot()

	return Summary{
		StartedAt:          c.startedAt,
		FinishedAt:         now,
		Duration:           duration,
		TotalCalls:         c.totalCalls,
		SuccessCalls:       c.successCalls,
		FailedCalls:        c.failedCalls,
		ActiveCalls:        c.activeCalls,
		SuccessRatio:       successRatio,
		CallsPerSecond:     cps,
		Retransmits:        c.retransmits,
		Timeouts:           c.timeouts,
		AverageCallLatency: avgCall,
		AverageInviteRTT:   avgInvite,
		CallLength:         latencySummaryOrNil(callLength, callLengthOK),
		InviteRTT:          latencySummaryOrNil(inviteRTT, inviteRTTOK),
		Media:              finalizeMediaSummary(c.media),
		RTD:                rtdSummary,
		Counters:           counters,
		Displays:           displays,
		FailureClasses:     failureClasses,
	}
}

type latencyAccumulator struct {
	count   int
	total   time.Duration
	min     time.Duration
	max     time.Duration
	last    time.Duration
	mean    float64
	m2      float64
	buckets [10]int
}

func (a *latencyAccumulator) add(value time.Duration) {
	a.count++
	a.total += value
	a.last = value
	if a.count == 1 || value < a.min {
		a.min = value
	}
	if value > a.max {
		a.max = value
	}
	valueFloat := float64(value)
	delta := valueFloat - a.mean
	a.mean += delta / float64(a.count)
	delta2 := valueFloat - a.mean
	a.m2 += delta * delta2
	a.buckets[latencyBucketIndex(value)]++
}

func (a latencyAccumulator) snapshot() (LatencySummary, bool) {
	if a.count == 0 {
		return LatencySummary{}, false
	}
	average := time.Duration(int64(a.total) / int64(a.count))
	stdDev := time.Duration(0)
	if a.count > 0 {
		stdDev = time.Duration(math.Sqrt(a.m2 / float64(a.count)))
	}
	return LatencySummary{
		Count:   a.count,
		Average: average,
		Min:     a.min,
		Max:     a.max,
		Last:    a.last,
		StdDev:  stdDev,
		Buckets: buildLatencyBuckets(a.buckets),
	}, true
}

func latencyBucketIndex(value time.Duration) int {
	ms := value.Milliseconds()
	for i, upper := range latencyBucketBoundsMS {
		if ms <= upper {
			return i
		}
	}
	return len(latencyBucketBoundsMS)
}

func buildLatencyBuckets(counts [10]int) []LatencyBucket {
	buckets := make([]LatencyBucket, 0, len(counts))
	for i, count := range counts {
		label := ""
		if i < len(latencyBucketBoundsMS) {
			label = "le_" + strconv.FormatInt(latencyBucketBoundsMS[i], 10) + "ms"
		} else {
			label = "gt_" + strconv.FormatInt(latencyBucketBoundsMS[len(latencyBucketBoundsMS)-1], 10) + "ms"
		}
		buckets = append(buckets, LatencyBucket{
			Label: label,
			Count: count,
		})
	}
	return buckets
}

func latencySummaryOrNil(summary LatencySummary, ok bool) *LatencySummary {
	if !ok {
		return nil
	}
	copy := summary
	return &copy
}

func finalizeMediaSummary(m MediaSummary) MediaSummary {
	out := m
	if out.rtcpJitterSamples > 0 {
		out.RTCPAvgJitterTS = out.rtcpJitterSum / float64(out.rtcpJitterSamples)
	}
	out.rtcpJitterSum = 0
	out.rtcpJitterSamples = 0
	return out
}

// SummaryWriteOptions configures JSON export for -summary_json.
type SummaryWriteOptions struct {
	ToolVersion string
	Health      HealthConfig
}

// FinalizeSummary returns a snapshot enriched with schema metadata, health, and findings.
func (c *Collector) FinalizeSummary(toolVersion string, healthCfg HealthConfig) Summary {
	s := c.Snapshot()
	s.SchemaVersion = SummarySchemaVersion
	if tv := strings.TrimSpace(toolVersion); tv != "" {
		s.ToolVersion = tv
	}
	var reasons []string
	s.Health, reasons = EvaluateHealth(healthCfg, s)
	s.Findings = BuildFindings(s, reasons)
	return s
}

func (c *Collector) WriteJSON(path string, opts *SummaryWriteOptions) error {
	var tool string
	var hc HealthConfig
	if opts != nil {
		tool = opts.ToolVersion
		hc = opts.Health
	}
	summary := c.FinalizeSummary(tool, hc)
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
