package engine

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qxip/gossipper/internal/scenario"
	"github.com/qxip/gossipper/internal/sip"
	"github.com/qxip/gossipper/internal/stats"
)

type traceLogger struct {
	mu                sync.Mutex
	full              *os.File
	short             *os.File
	errFile           *os.File
	errCodes          *os.File
	logFile           *os.File
	statsFile         *os.File
	rttFile           *os.File
	screenFile        *os.File
	countsFile        *os.File
	fullPath          string
	shortPath         string
	errPath           string
	errCodesPath      string
	logPath           string
	statsPath         string
	rttPath           string
	screenPath        string
	countsPath        string
	statsStop         chan struct{}
	statsDone         chan struct{}
	countsStop        chan struct{}
	countsDone        chan struct{}
	screenStop        chan struct{}
	screenDone        chan struct{}
	screenPrevious    *stats.Summary
	rttFlushEvery     int
	rttCompletedCalls int
	rttPending        [][]string
	countSpecs        []traceCountSpec
	countSent         map[int]int
	countRecv         map[int]int
	countUnexp        map[int]int
}

const (
	traceStatCSVHeader = "timestamp,elapsed_ms,total_calls,success_calls,failed_calls,active_calls,success_ratio,calls_per_second,retransmits,timeouts,avg_call_ms,call_stddev_ms,avg_invite_ms,invite_stddev_ms,rtp_packets_sent,rtp_packets_received,rtcp_sender_reports,rtcp_receiver_reports,rtcp_packets_received,failure_timeout,failure_unexpected_sip,failure_transport_error,failure_parse_error,failure_scenario_error,failure_cancelled,interval_ms,interval_calls_per_second,delta_total_calls,delta_success_calls,delta_failed_calls,delta_retransmits,delta_timeouts,delta_rtp_packets_sent,delta_rtp_packets_received,delta_rtcp_sender_reports,delta_rtcp_receiver_reports,delta_rtcp_packets_received,delta_failure_timeout,delta_failure_unexpected_sip,delta_failure_transport_error,delta_failure_parse_error,delta_failure_scenario_error,delta_failure_cancelled\n"
	traceRTTCSVHeader  = "timestamp,call,name,value_ms\n"
	traceScreenHeader  = "timestamp,total_calls,success_calls,failed_calls,active_calls,success_ratio,calls_per_second,interval_ms,interval_calls_per_second,avg_call_ms,avg_invite_ms,retransmits,timeouts,failure_timeout,failure_unexpected_sip\n"
)

type traceCountSpec struct {
	CommandIndex int
	Label        string
}

func newTraceLogger(cfg Config) (*traceLogger, error) {
	if !cfg.TraceMessages && !cfg.TraceShortMsg && !cfg.TraceCounts && !cfg.TraceErrors && !cfg.TraceErrorCodes && !cfg.TraceLogs && !cfg.TraceStats && !cfg.TraceRTT && !cfg.TraceScreen {
		return nil, nil
	}

	basePath := cfg.MessageFile
	if basePath == "" {
		basePath = filepath.Join(".", fmt.Sprintf("gossipper_%d_messages.log", os.Getpid()))
	}

	logger := &traceLogger{}
	if cfg.TraceMessages {
		file, err := os.Create(basePath)
		if err != nil {
			return nil, err
		}
		logger.full = file
		logger.fullPath = basePath
	}
	if cfg.TraceShortMsg {
		shortPath := deriveShortTracePath(basePath)
		file, err := os.Create(shortPath)
		if err != nil {
			if logger.full != nil {
				_ = logger.full.Close()
			}
			return nil, err
		}
		logger.short = file
		logger.shortPath = shortPath
		if _, err := file.WriteString("timestamp,direction,call,proto,summary,call_id\n"); err != nil {
			_ = file.Close()
			if logger.full != nil {
				_ = logger.full.Close()
			}
			return nil, err
		}
	}
	if cfg.TraceErrors {
		errPath := cfg.ErrorFile
		if errPath == "" {
			errPath = deriveNamedTracePath(basePath, "_errors")
		}
		file, err := os.Create(errPath)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.errFile = file
		logger.errPath = errPath
	}
	if cfg.TraceErrorCodes {
		errCodesPath := deriveErrorCodesPath(cfg, basePath)
		file, err := os.Create(errCodesPath)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.errCodes = file
		logger.errCodesPath = errCodesPath
		if _, err := file.WriteString("timestamp,call,code,reason,call_id,expected\n"); err != nil {
			_ = logger.Close()
			return nil, err
		}
	}
	if cfg.TraceLogs {
		logPath := cfg.LogFile
		if logPath == "" {
			logPath = deriveNamedTracePath(basePath, "_logs")
		}
		file, err := os.Create(logPath)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.logFile = file
		logger.logPath = logPath
	}
	if cfg.TraceStats {
		statsPath := deriveStatsTracePath(basePath)
		file, err := os.Create(statsPath)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.statsFile = file
		logger.statsPath = statsPath
		if _, err := file.WriteString(traceStatCSVHeader); err != nil {
			_ = logger.Close()
			return nil, err
		}
	}
	if cfg.TraceRTT {
		rttPath := deriveRTTTracePath(basePath)
		file, err := os.Create(rttPath)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.rttFile = file
		logger.rttPath = rttPath
		if _, err := file.WriteString(traceRTTCSVHeader); err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.rttFlushEvery = cfg.RTTDumpFrequency
		if logger.rttFlushEvery <= 0 {
			logger.rttFlushEvery = 200
		}
	}
	if cfg.TraceScreen {
		screenPath := cfg.ScreenFile
		if screenPath == "" {
			screenPath = deriveScreenTracePath(basePath)
		}
		file, err := os.Create(screenPath)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.screenFile = file
		logger.screenPath = screenPath
		if _, err := file.WriteString(traceScreenHeader); err != nil {
			_ = logger.Close()
			return nil, err
		}
	}
	if cfg.TraceCounts {
		specs := buildTraceCountSpecs(cfg.Scenario)
		countsPath := deriveCountsTracePath(basePath)
		file, err := os.Create(countsPath)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		logger.countsFile = file
		logger.countsPath = countsPath
		logger.countSpecs = specs
		logger.countSent = make(map[int]int)
		logger.countRecv = make(map[int]int)
		logger.countUnexp = make(map[int]int)
		if _, err := file.WriteString(buildTraceCountsHeader(specs)); err != nil {
			_ = logger.Close()
			return nil, err
		}
	}
	return logger, nil
}

func (t *traceLogger) Close() error {
	if t == nil {
		return nil
	}
	var errs []error
	if t.full != nil {
		if closeErr := t.full.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.short != nil {
		if closeErr := t.short.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.errFile != nil {
		if closeErr := t.errFile.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.errCodes != nil {
		if closeErr := t.errCodes.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.logFile != nil {
		if closeErr := t.logFile.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.statsStop != nil {
		close(t.statsStop)
		t.statsStop = nil
	}
	if t.statsDone != nil {
		<-t.statsDone
		t.statsDone = nil
	}
	if t.countsStop != nil {
		close(t.countsStop)
		t.countsStop = nil
	}
	if t.countsDone != nil {
		<-t.countsDone
		t.countsDone = nil
	}
	if t.screenStop != nil {
		close(t.screenStop)
		t.screenStop = nil
	}
	if t.screenDone != nil {
		<-t.screenDone
		t.screenDone = nil
	}
	if t.statsFile != nil {
		if closeErr := t.statsFile.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.rttFile != nil {
		t.mu.Lock()
		t.flushRTTLocked()
		t.mu.Unlock()
		if closeErr := t.rttFile.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.screenFile != nil {
		if closeErr := t.screenFile.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	if t.countsFile != nil {
		if closeErr := t.countsFile.Close(); closeErr != nil {
			errs = append(errs, closeErr)
		}
	}
	return errors.Join(errs...)
}

func deriveShortTracePath(fullPath string) string {
	return deriveNamedTracePath(fullPath, "_shortmessages")
}

func deriveNamedTracePath(fullPath, suffix string) string {
	ext := filepath.Ext(fullPath)
	base := strings.TrimSuffix(fullPath, ext)
	if ext == "" {
		return base + suffix + ".log"
	}
	return base + suffix + ext
}

func deriveErrorCodesPath(cfg Config, basePath string) string {
	source := cfg.ErrorFile
	if source == "" {
		source = basePath
	}
	return deriveNamedTracePath(source, "_error_codes")
}

func deriveStatsTracePath(basePath string) string {
	return deriveNamedTracePath(basePath, "_stats")
}

func deriveRTTTracePath(basePath string) string {
	return deriveNamedTracePath(basePath, "_rtt")
}

func deriveScreenTracePath(basePath string) string {
	return deriveNamedTracePath(basePath, "_screen")
}

func deriveCountsTracePath(basePath string) string {
	return deriveNamedTracePath(basePath, "_counts")
}

func (e *Engine) startTrace() error {
	logger, err := newTraceLogger(e.cfg)
	if err != nil {
		return err
	}
	e.trace = logger
	if e.trace != nil {
		e.trace.startStatsLoop(e.stats, e.cfg.StatsDumpPeriod)
		e.trace.startCountsLoop(e.cfg.StatsDumpPeriod)
		e.trace.startScreenLoop(e.stats, e.cfg.StatsDumpPeriod)
	}
	return nil
}

func (e *Engine) stopTrace() {
	if e.trace == nil {
		return
	}
	_ = e.trace.Close()
	e.trace = nil
}

func (e *Engine) traceEvent(direction string, callNumber int, raw string) {
	if e.trace == nil {
		if e.cfg.TraceMessages {
			fmt.Fprintf(os.Stdout, "%s[%d]\n%s\n", direction, callNumber, raw)
		}
		return
	}

	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()

	if e.trace.full != nil {
		_, _ = fmt.Fprintf(e.trace.full, "%s[%d]\n%s\n", direction, callNumber, raw)
	}
	if e.trace.short != nil {
		record := buildShortTraceRecord(direction, callNumber, raw)
		writer := csv.NewWriter(e.trace.short)
		_ = writer.Write(record)
		writer.Flush()
	}
}

func (e *Engine) traceActionLog(message string) {
	if e.trace == nil || e.trace.logFile == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	_, _ = fmt.Fprintf(e.trace.logFile, "%s action-log %s\n", time.Now().Format(time.RFC3339Nano), message)
}

func (e *Engine) traceError(kind string, callNumber int, message string) {
	if e.trace == nil || e.trace.errFile == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	_, _ = fmt.Fprintf(e.trace.errFile, "%s %s[%d] %s\n", time.Now().Format(time.RFC3339Nano), kind, callNumber, message)
}

func (e *Engine) traceUnexpectedSIP(callNumber int, expected scenario.Command, msg sip.Message) {
	if !e.cfg.TraceErrors && !e.cfg.TraceErrorCodes {
		return
	}
	summary := firstLine(msg.Raw)
	expectedText := expected.RecvResp
	if expected.RecvReq != "" {
		expectedText = expected.RecvReq
	}
	if e.cfg.TraceErrors {
		e.traceError("unexpected-sip", callNumber, fmt.Sprintf("expected=%q got=%q\n%s", expectedText, summary, msg.Raw))
	}
	if e.cfg.TraceErrorCodes && msg.StatusCode > 0 {
		e.traceErrorCode(callNumber, msg.StatusCode, strings.TrimSpace(msg.Reason), commandCallID(msg.Raw, ""), expectedText)
	}
}

func (e *Engine) traceErrorCode(callNumber, code int, reason, callID, expected string) {
	if e.trace == nil || e.trace.errCodes == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	writer := csv.NewWriter(e.trace.errCodes)
	_ = writer.Write([]string{
		time.Now().Format(time.RFC3339Nano),
		strconv.Itoa(callNumber),
		strconv.Itoa(code),
		reason,
		callID,
		expected,
	})
	writer.Flush()
}

func (e *Engine) traceRTD(callNumber int, name string, value time.Duration) {
	if e.trace == nil || e.trace.rttFile == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	e.trace.rttPending = append(e.trace.rttPending, []string{
		time.Now().Format(time.RFC3339Nano),
		strconv.Itoa(callNumber),
		name,
		strconv.FormatInt(value.Milliseconds(), 10),
	})
}

func (e *Engine) traceCallCompleted() {
	if e.trace == nil || e.trace.rttFile == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	e.trace.rttCompletedCalls++
	if e.trace.rttCompletedCalls >= e.trace.rttFlushEvery {
		e.trace.flushRTTLocked()
		e.trace.rttCompletedCalls = 0
	}
}

func (e *Engine) traceCountSent(commandIndex int) {
	if e.trace == nil || e.trace.countsFile == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	e.trace.countSent[commandIndex]++
}

func (e *Engine) traceCountRecv(commandIndex int) {
	if e.trace == nil || e.trace.countsFile == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	e.trace.countRecv[commandIndex]++
}

func (e *Engine) traceCountUnexpected(commandIndex int) {
	if e.trace == nil || e.trace.countsFile == nil {
		return
	}
	e.trace.mu.Lock()
	defer e.trace.mu.Unlock()
	e.trace.countUnexp[commandIndex]++
}

func (t *traceLogger) flushRTTLocked() {
	if t == nil || t.rttFile == nil || len(t.rttPending) == 0 {
		return
	}
	writer := csv.NewWriter(t.rttFile)
	for _, row := range t.rttPending {
		_ = writer.Write(row)
	}
	writer.Flush()
	t.rttPending = t.rttPending[:0]
}

func (t *traceLogger) startStatsLoop(collector *stats.Collector, period time.Duration) {
	if t == nil || t.statsFile == nil || collector == nil {
		return
	}
	if period <= 0 {
		period = time.Second
	}
	t.statsStop = make(chan struct{})
	t.statsDone = make(chan struct{})
	// Capture channels into locals so the goroutine never reads t.statsStop/t.statsDone
	// after Close() may have already set those fields to nil (data race).
	stop := t.statsStop
	done := t.statsDone
	go func() {
		defer close(done)

		ticker := time.NewTicker(period)
		defer ticker.Stop()
		var previous *stats.Summary

		for {
			select {
			case <-ticker.C:
				summary := collector.Snapshot()
				t.writeStatsSnapshot(summary, previous)
				snapshotCopy := summary
				previous = &snapshotCopy
			case <-stop:
				summary := collector.Snapshot()
				t.writeStatsSnapshot(summary, previous)
				return
			}
		}
	}()
}

func (t *traceLogger) startCountsLoop(period time.Duration) {
	if t == nil || t.countsFile == nil {
		return
	}
	if period <= 0 {
		period = time.Second
	}
	t.countsStop = make(chan struct{})
	t.countsDone = make(chan struct{})
	stop := t.countsStop
	done := t.countsDone
	go func() {
		defer close(done)
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.writeCountsSnapshot()
			case <-stop:
				t.writeCountsSnapshot()
				return
			}
		}
	}()
}

func (t *traceLogger) startScreenLoop(collector *stats.Collector, period time.Duration) {
	if t == nil || t.screenFile == nil || collector == nil {
		return
	}
	if period <= 0 {
		period = time.Second
	}
	t.screenStop = make(chan struct{})
	t.screenDone = make(chan struct{})
	stop := t.screenStop
	done := t.screenDone
	go func() {
		defer close(done)
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				summary := collector.Snapshot()
				t.writeScreenSnapshot(summary)
			case <-stop:
				summary := collector.Snapshot()
				t.writeScreenSnapshot(summary)
				return
			}
		}
	}()
}

func (t *traceLogger) writeScreenSnapshot(summary stats.Summary) {
	if t == nil || t.screenFile == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	intervalMS := summary.Duration.Milliseconds()
	deltaCalls := summary.TotalCalls
	if t.screenPrevious != nil {
		interval := summary.FinishedAt.Sub(t.screenPrevious.FinishedAt)
		if interval > 0 {
			intervalMS = interval.Milliseconds()
		} else {
			intervalMS = 0
		}
		deltaCalls = summary.TotalCalls - t.screenPrevious.TotalCalls
	}
	intervalCPS := 0.0
	if intervalMS > 0 {
		intervalCPS = float64(deltaCalls) / (float64(intervalMS) / 1000.0)
	}
	writer := csv.NewWriter(t.screenFile)
	_ = writer.Write([]string{
		summary.FinishedAt.Format(time.RFC3339Nano),
		strconv.Itoa(summary.TotalCalls),
		strconv.Itoa(summary.SuccessCalls),
		strconv.Itoa(summary.FailedCalls),
		strconv.Itoa(summary.ActiveCalls),
		strconv.FormatFloat(summary.SuccessRatio, 'f', 6, 64),
		strconv.FormatFloat(summary.CallsPerSecond, 'f', 6, 64),
		strconv.FormatInt(intervalMS, 10),
		strconv.FormatFloat(intervalCPS, 'f', 6, 64),
		strconv.FormatInt(summary.AverageCallLatency.Milliseconds(), 10),
		strconv.FormatInt(summary.AverageInviteRTT.Milliseconds(), 10),
		strconv.Itoa(summary.Retransmits),
		strconv.Itoa(summary.Timeouts),
		strconv.Itoa(failureClassCount(summary, "timeout")),
		strconv.Itoa(failureClassCount(summary, "unexpected_sip")),
	})
	writer.Flush()
	snapshotCopy := summary
	t.screenPrevious = &snapshotCopy
}

func (t *traceLogger) writeCountsSnapshot() {
	if t == nil || t.countsFile == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	row := []string{time.Now().Format(time.RFC3339Nano)}
	for _, spec := range t.countSpecs {
		row = append(row,
			strconv.Itoa(t.countSent[spec.CommandIndex]),
			strconv.Itoa(t.countRecv[spec.CommandIndex]),
			strconv.Itoa(t.countUnexp[spec.CommandIndex]),
		)
	}
	writer := csv.NewWriter(t.countsFile)
	_ = writer.Write(row)
	writer.Flush()
}

func (t *traceLogger) writeStatsSnapshot(summary stats.Summary, previous *stats.Summary) {
	if t == nil || t.statsFile == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	intervalMS := summary.Duration.Milliseconds()
	deltaTotalCalls := summary.TotalCalls
	deltaSuccessCalls := summary.SuccessCalls
	deltaFailedCalls := summary.FailedCalls
	deltaRetransmits := summary.Retransmits
	deltaTimeouts := summary.Timeouts
	deltaRTPPacketsSent := int(summary.Media.RTPPacketsSent)
	deltaRTPPacketsReceived := int(summary.Media.RTPPacketsReceived)
	deltaRTCPSenderReports := int(summary.Media.RTCPSenderReports)
	deltaRTCPReceiverReports := int(summary.Media.RTCPReceiverReports)
	deltaRTCPPacketsReceived := int(summary.Media.RTCPPacketsReceived)
	failureTimeout := failureClassCount(summary, "timeout")
	failureUnexpectedSIP := failureClassCount(summary, "unexpected_sip")
	failureTransportError := failureClassCount(summary, "transport_error")
	failureParseError := failureClassCount(summary, "parse_error")
	failureScenarioError := failureClassCount(summary, "scenario_error")
	failureCancelled := failureClassCount(summary, "cancelled")
	deltaFailureTimeout := failureTimeout
	deltaFailureUnexpectedSIP := failureUnexpectedSIP
	deltaFailureTransportError := failureTransportError
	deltaFailureParseError := failureParseError
	deltaFailureScenarioError := failureScenarioError
	deltaFailureCancelled := failureCancelled
	if previous != nil {
		interval := summary.FinishedAt.Sub(previous.FinishedAt)
		if interval > 0 {
			intervalMS = interval.Milliseconds()
		} else {
			intervalMS = 0
		}
		deltaTotalCalls -= previous.TotalCalls
		deltaSuccessCalls -= previous.SuccessCalls
		deltaFailedCalls -= previous.FailedCalls
		deltaRetransmits -= previous.Retransmits
		deltaTimeouts -= previous.Timeouts
		deltaRTPPacketsSent -= int(previous.Media.RTPPacketsSent)
		deltaRTPPacketsReceived -= int(previous.Media.RTPPacketsReceived)
		deltaRTCPSenderReports -= int(previous.Media.RTCPSenderReports)
		deltaRTCPReceiverReports -= int(previous.Media.RTCPReceiverReports)
		deltaRTCPPacketsReceived -= int(previous.Media.RTCPPacketsReceived)
		deltaFailureTimeout -= failureClassCount(*previous, "timeout")
		deltaFailureUnexpectedSIP -= failureClassCount(*previous, "unexpected_sip")
		deltaFailureTransportError -= failureClassCount(*previous, "transport_error")
		deltaFailureParseError -= failureClassCount(*previous, "parse_error")
		deltaFailureScenarioError -= failureClassCount(*previous, "scenario_error")
		deltaFailureCancelled -= failureClassCount(*previous, "cancelled")
	}
	intervalCPS := 0.0
	if intervalMS > 0 {
		intervalCPS = float64(deltaTotalCalls) / (float64(intervalMS) / 1000.0)
	}

	writer := csv.NewWriter(t.statsFile)
	_ = writer.Write([]string{
		summary.FinishedAt.Format(time.RFC3339Nano),
		strconv.FormatInt(summary.Duration.Milliseconds(), 10),
		strconv.Itoa(summary.TotalCalls),
		strconv.Itoa(summary.SuccessCalls),
		strconv.Itoa(summary.FailedCalls),
		strconv.Itoa(summary.ActiveCalls),
		strconv.FormatFloat(summary.SuccessRatio, 'f', 6, 64),
		strconv.FormatFloat(summary.CallsPerSecond, 'f', 6, 64),
		strconv.Itoa(summary.Retransmits),
		strconv.Itoa(summary.Timeouts),
		strconv.FormatInt(summary.AverageCallLatency.Milliseconds(), 10),
		strconv.FormatInt(latencyStdDevMS(summary.CallLength), 10),
		strconv.FormatInt(summary.AverageInviteRTT.Milliseconds(), 10),
		strconv.FormatInt(latencyStdDevMS(summary.InviteRTT), 10),
		strconv.FormatUint(uint64(summary.Media.RTPPacketsSent), 10),
		strconv.FormatUint(uint64(summary.Media.RTPPacketsReceived), 10),
		strconv.FormatUint(uint64(summary.Media.RTCPSenderReports), 10),
		strconv.FormatUint(uint64(summary.Media.RTCPReceiverReports), 10),
		strconv.FormatUint(uint64(summary.Media.RTCPPacketsReceived), 10),
		strconv.Itoa(failureTimeout),
		strconv.Itoa(failureUnexpectedSIP),
		strconv.Itoa(failureTransportError),
		strconv.Itoa(failureParseError),
		strconv.Itoa(failureScenarioError),
		strconv.Itoa(failureCancelled),
		strconv.FormatInt(intervalMS, 10),
		strconv.FormatFloat(intervalCPS, 'f', 6, 64),
		strconv.Itoa(deltaTotalCalls),
		strconv.Itoa(deltaSuccessCalls),
		strconv.Itoa(deltaFailedCalls),
		strconv.Itoa(deltaRetransmits),
		strconv.Itoa(deltaTimeouts),
		strconv.Itoa(deltaRTPPacketsSent),
		strconv.Itoa(deltaRTPPacketsReceived),
		strconv.Itoa(deltaRTCPSenderReports),
		strconv.Itoa(deltaRTCPReceiverReports),
		strconv.Itoa(deltaRTCPPacketsReceived),
		strconv.Itoa(deltaFailureTimeout),
		strconv.Itoa(deltaFailureUnexpectedSIP),
		strconv.Itoa(deltaFailureTransportError),
		strconv.Itoa(deltaFailureParseError),
		strconv.Itoa(deltaFailureScenarioError),
		strconv.Itoa(deltaFailureCancelled),
	})
	writer.Flush()
}

func failureClassCount(summary stats.Summary, name string) int {
	if summary.FailureClasses == nil {
		return 0
	}
	return summary.FailureClasses[name]
}

func latencyStdDevMS(summary *stats.LatencySummary) int64 {
	if summary == nil {
		return 0
	}
	return summary.StdDev.Milliseconds()
}

func buildShortTraceRecord(direction string, callNumber int, raw string) []string {
	now := time.Now().Format(time.RFC3339Nano)
	proto := "cmd"
	summary := firstLine(raw)
	callID := commandCallID(raw, "")

	if msg, err := sip.Parse([]byte(raw)); err == nil {
		proto = "sip"
		callID, _ = sip.Header(msg.Headers, "Call-ID")
		if msg.Method != "" {
			summary = msg.Method
		} else if msg.StatusCode > 0 {
			summary = fmt.Sprintf("%d %s", msg.StatusCode, strings.TrimSpace(msg.Reason))
		}
	}

	return []string{
		now,
		direction,
		strconv.Itoa(callNumber),
		proto,
		summary,
		callID,
	}
}

func buildTraceCountSpecs(sc scenario.Scenario) []traceCountSpec {
	specs := make([]traceCountSpec, 0, len(sc.Commands))
	for _, cmd := range sc.Commands {
		switch cmd.Type {
		case scenario.CommandSend, scenario.CommandRecv:
			specs = append(specs, traceCountSpec{
				CommandIndex: cmd.Index,
				Label:        traceCountLabel(cmd),
			})
		}
	}
	return specs
}

func traceCountLabel(cmd scenario.Command) string {
	switch cmd.Type {
	case scenario.CommandSend:
		line := firstLine(cmd.SendText)
		token := strings.TrimSpace(strings.SplitN(line, " ", 2)[0])
		if token == "" {
			token = "SEND"
		}
		return sanitizeTraceCountLabel(strings.ToUpper(token))
	case scenario.CommandRecv:
		if cmd.RecvReq != "" {
			return sanitizeTraceCountLabel(strings.ToUpper(cmd.RecvReq))
		}
		if cmd.RecvResp != "" {
			return sanitizeTraceCountLabel("RESP_" + cmd.RecvResp)
		}
		return "RECV"
	default:
		return "CMD"
	}
}

func sanitizeTraceCountLabel(value string) string {
	if value == "" {
		return "CMD"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	sanitized := strings.Trim(b.String(), "_")
	if sanitized == "" {
		return "CMD"
	}
	return sanitized
}

func buildTraceCountsHeader(specs []traceCountSpec) string {
	columns := []string{"timestamp"}
	for _, spec := range specs {
		base := fmt.Sprintf("%d_%s", spec.CommandIndex, spec.Label)
		columns = append(columns, base+"_sent", base+"_recv", base+"_unexp")
	}
	return strings.Join(columns, ",") + "\n"
}

func firstLine(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
