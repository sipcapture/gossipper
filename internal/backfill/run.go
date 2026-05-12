package backfill

import (
	"context"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"os"
	"time"

	"github.com/sipcapture/gossipper/hepcodec"
)

// Result summarizes a completed backfill run.
type Result struct {
	TotalCalls   int
	TotalHEP     int
	TotalLogs    int
	TotalMetrics int
	Elapsed      time.Duration
}

// Run executes the backfill data generation. It walks time from (now - duration)
// to now, synthesizing SIP calls at the configured CPS, and emitting HEP packets,
// SBC logs, and aggregate metrics. No real SIP traffic is sent.
func Run(ctx context.Context, cfg Config) (Result, error) {
	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}

	wallStart := time.Now()
	rng := mrand.New(mrand.NewSource(wallStart.UnixNano()))

	// Open HEP UDP socket if configured.
	var hepConn *net.UDPConn
	var hepAddr *net.UDPAddr
	if cfg.HEPAddr != "" {
		addr, err := net.ResolveUDPAddr("udp", cfg.HEPAddr)
		if err != nil {
			return Result{}, fmt.Errorf("backfill: resolve hep addr: %w", err)
		}
		conn, err := net.ListenUDP("udp", nil)
		if err != nil {
			return Result{}, fmt.Errorf("backfill: open hep socket: %w", err)
		}
		defer conn.Close()
		hepConn = conn
		hepAddr = addr
	}

	// Open SBC log file if configured.
	var logFile *os.File
	if cfg.SBCLogFile != "" {
		f, err := os.Create(cfg.SBCLogFile)
		if err != nil {
			return Result{}, fmt.Errorf("backfill: create sbc log file: %w", err)
		}
		defer f.Close()
		logFile = f
	}

	// Open SBC metrics file if configured.
	var metricsFile *os.File
	if cfg.SBCMetricsFile != "" {
		f, err := os.Create(cfg.SBCMetricsFile)
		if err != nil {
			return Result{}, fmt.Errorf("backfill: create sbc metrics file: %w", err)
		}
		defer f.Close()
		metricsFile = f
	}

	simEnd := wallStart
	simStart := simEnd.Add(-cfg.Duration)
	callInterval := time.Duration(float64(time.Second) / cfg.CPS)

	totalCalls := int(cfg.Duration / callInterval)
	if totalCalls == 0 {
		totalCalls = 1
	}

	var result Result
	metrics := newMetricsAccumulator(simStart, cfg.MetricsInterval)
	nextMetrics := simStart.Add(cfg.MetricsInterval)

	var progressWriter io.Writer
	if cfg.Progress {
		progressWriter = os.Stderr
	}

	current := simStart
	for callNum := 0; current.Before(simEnd); callNum++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		call := synthesizeCall(cfg, current, callNum, rng)
		result.TotalCalls++

		// Emit HEP packets for each SIP message in the call.
		if hepConn != nil {
			for _, msg := range call.Messages {
				msgTime := call.Start.Add(msg.Offset)
				srcIP, dstIP := cfg.SrcIP, cfg.DstIP
				srcPort, dstPort := cfg.SrcPort, cfg.DstPort
				if msg.Direction == "recv" {
					srcIP, dstIP = dstIP, srcIP
					srcPort, dstPort = dstPort, srcPort
				}
				packet, err := hepcodec.Encode(hepcodec.Message{
					Time:          msgTime,
					SrcIP:         srcIP,
					DstIP:         dstIP,
					SrcPort:       srcPort,
					DstPort:       dstPort,
					IPProtocol:    hepcodec.IPProtoUDP,
					ProtoType:     hepcodec.ProtocolSIP,
					CaptureID:     cfg.HEPCaptureID,
					AuthKey:       cfg.HEPPassword,
					CorrelationID: call.CallID,
					Payload:       msg.Payload,
				})
				if err != nil {
					continue
				}
				_, _ = hepConn.WriteToUDP(packet, hepAddr)
				result.TotalHEP++
			}
		}

		// Emit SBC log entry.
		if logFile != nil {
			entry := buildLogEntry(cfg, call)
			data, err := marshalLogEntry(entry)
			if err == nil {
				data = append(data, '\n')
				_, _ = logFile.Write(data)
				result.TotalLogs++
			}
		}

		// Accumulate metrics.
		metrics.addCall(call)

		// Flush metrics snapshot when we cross the interval boundary.
		for !current.Before(nextMetrics) {
			if metricsFile != nil {
				snap := metrics.snapshot()
				data, err := marshalMetrics(snap)
				if err == nil {
					data = append(data, '\n')
					_, _ = metricsFile.Write(data)
					result.TotalMetrics++
				}
			}
			metrics.reset(nextMetrics)
			nextMetrics = nextMetrics.Add(cfg.MetricsInterval)
		}

		// Progress.
		if progressWriter != nil && callNum%5000 == 0 && callNum > 0 {
			pct := float64(callNum) / float64(totalCalls) * 100
			elapsed := time.Since(wallStart)
			fmt.Fprintf(progressWriter, "\rbackfill: %d/%d calls (%.1f%%) elapsed=%s", callNum, totalCalls, pct, elapsed.Truncate(time.Millisecond))
		}

		current = current.Add(callInterval)
	}

	// Flush any remaining metrics.
	if metricsFile != nil && metrics.started > 0 {
		snap := metrics.snapshot()
		data, err := marshalMetrics(snap)
		if err == nil {
			data = append(data, '\n')
			_, _ = metricsFile.Write(data)
			result.TotalMetrics++
		}
	}

	result.Elapsed = time.Since(wallStart)
	if progressWriter != nil {
		fmt.Fprintf(progressWriter, "\rbackfill: done — %d calls, %d HEP packets, %d logs, %d metric snapshots in %s\n",
			result.TotalCalls, result.TotalHEP, result.TotalLogs, result.TotalMetrics, result.Elapsed.Truncate(time.Millisecond))
	}

	return result, nil
}
