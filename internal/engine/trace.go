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

	"github.com/adubovikov/gossipper/internal/scenario"
	"github.com/adubovikov/gossipper/internal/sip"
	"github.com/adubovikov/gossipper/internal/stats"
)

type traceLogger struct {
	mu           sync.Mutex
	full         *os.File
	short        *os.File
	errFile      *os.File
	errCodes     *os.File
	logFile      *os.File
	statsFile    *os.File
	fullPath     string
	shortPath    string
	errPath      string
	errCodesPath string
	logPath      string
	statsPath    string
	statsStop    chan struct{}
	statsDone    chan struct{}
}

func newTraceLogger(cfg Config) (*traceLogger, error) {
	if !cfg.TraceMessages && !cfg.TraceShortMsg && !cfg.TraceErrors && !cfg.TraceErrorCodes && !cfg.TraceLogs && !cfg.TraceStats {
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
		if _, err := file.WriteString("timestamp,elapsed_ms,total_calls,success_calls,failed_calls,active_calls,success_ratio,calls_per_second,retransmits,timeouts,avg_call_ms,avg_invite_ms,rtp_packets_sent,rtp_packets_received,rtcp_sender_reports,rtcp_receiver_reports,rtcp_packets_received\n"); err != nil {
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
	if t.statsFile != nil {
		if closeErr := t.statsFile.Close(); closeErr != nil {
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

func (e *Engine) startTrace() error {
	logger, err := newTraceLogger(e.cfg)
	if err != nil {
		return err
	}
	e.trace = logger
	if e.trace != nil {
		e.trace.startStatsLoop(e.stats)
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

func (t *traceLogger) startStatsLoop(collector *stats.Collector) {
	if t == nil || t.statsFile == nil || collector == nil {
		return
	}
	t.statsStop = make(chan struct{})
	t.statsDone = make(chan struct{})
	go func() {
		defer close(t.statsDone)

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				t.writeStatsSnapshot(collector.Snapshot())
			case <-t.statsStop:
				t.writeStatsSnapshot(collector.Snapshot())
				return
			}
		}
	}()
}

func (t *traceLogger) writeStatsSnapshot(summary stats.Summary) {
	if t == nil || t.statsFile == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

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
		strconv.FormatInt(summary.AverageInviteRTT.Milliseconds(), 10),
		strconv.FormatUint(uint64(summary.Media.RTPPacketsSent), 10),
		strconv.FormatUint(uint64(summary.Media.RTPPacketsReceived), 10),
		strconv.FormatUint(uint64(summary.Media.RTCPSenderReports), 10),
		strconv.FormatUint(uint64(summary.Media.RTCPReceiverReports), 10),
		strconv.FormatUint(uint64(summary.Media.RTCPPacketsReceived), 10),
	})
	writer.Flush()
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

func firstLine(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
