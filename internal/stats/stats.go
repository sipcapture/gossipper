package stats

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/adubovikov/gossipper/internal/media"
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
	media           MediaSummary
	rtds            map[string]rtdAccumulator
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
}

type RTDSummary struct {
	Count   int           `json:"count"`
	Average time.Duration `json:"average"`
	Min     time.Duration `json:"min"`
	Max     time.Duration `json:"max"`
	Last    time.Duration `json:"last"`
}

type Summary struct {
	StartedAt          time.Time             `json:"started_at"`
	FinishedAt         time.Time             `json:"finished_at"`
	Duration           time.Duration         `json:"duration"`
	TotalCalls         int                   `json:"total_calls"`
	SuccessCalls       int                   `json:"success_calls"`
	FailedCalls        int                   `json:"failed_calls"`
	ActiveCalls        int                   `json:"active_calls"`
	SuccessRatio       float64               `json:"success_ratio"`
	CallsPerSecond     float64               `json:"calls_per_second"`
	Retransmits        int                   `json:"retransmits"`
	Timeouts           int                   `json:"timeouts"`
	AverageCallLatency time.Duration         `json:"average_call_latency"`
	AverageInviteRTT   time.Duration         `json:"average_invite_rtt"`
	Media              MediaSummary          `json:"media"`
	RTD                map[string]RTDSummary `json:"rtd,omitempty"`
	Counters           map[string]int        `json:"counters,omitempty"`
	Displays           map[string]int        `json:"displays,omitempty"`
	FailureClasses     map[string]int        `json:"failure_classes,omitempty"`
}

func New() *Collector {
	return &Collector{
		startedAt:      time.Now(),
		rtds:           make(map[string]rtdAccumulator),
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
}

func (c *Collector) AddInviteLatency(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inviteLatencies = append(c.inviteLatencies, d)
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
}

func (c *Collector) AddRTD(name string, value time.Duration) {
	if name == "" || value < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	acc := c.rtds[name]
	acc.count++
	acc.total += value
	acc.last = value
	if acc.count == 1 || value < acc.min {
		acc.min = value
	}
	if value > acc.max {
		acc.max = value
	}
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

	var rtdSummary map[string]RTDSummary
	if len(c.rtds) > 0 {
		rtdSummary = make(map[string]RTDSummary, len(c.rtds))
		for name, acc := range c.rtds {
			if acc.count == 0 {
				continue
			}
			rtdSummary[name] = RTDSummary{
				Count:   acc.count,
				Average: time.Duration(int64(acc.total) / int64(acc.count)),
				Min:     acc.min,
				Max:     acc.max,
				Last:    acc.last,
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
		Media:              c.media,
		RTD:                rtdSummary,
		Counters:           counters,
		Displays:           displays,
		FailureClasses:     failureClasses,
	}
}

type rtdAccumulator struct {
	count int
	total time.Duration
	min   time.Duration
	max   time.Duration
	last  time.Duration
}

func (c *Collector) WriteJSON(path string) error {
	summary := c.Snapshot()
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
