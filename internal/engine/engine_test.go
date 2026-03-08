package engine

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/csv"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adubovikov/gossipper/internal/hep"
	"github.com/adubovikov/gossipper/internal/media"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/pion/rtcp"

	"github.com/adubovikov/gossipper/internal/scenario"
	"github.com/adubovikov/gossipper/internal/sip"
)

func TestEngineRunsBasicUACScenario(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				trying := fmt.Sprintf(
					"SIP/2.0 100 Trying\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(trying), addr)
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	summary := app.Stats().Snapshot()
	if summary.SuccessCalls != 1 {
		t.Fatalf("expected one successful call, got %+v", summary)
	}
	if summary.FailedCalls != 0 {
		t.Fatalf("expected zero failed calls, got %+v", summary)
	}
}

func TestClassifyCallFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		err              error
		sawUnexpectedSIP bool
		want             string
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: "timeout"},
		{name: "unexpected sip timeout", err: context.DeadlineExceeded, sawUnexpectedSIP: true, want: "unexpected_sip"},
		{name: "cancelled", err: context.Canceled, want: "cancelled"},
		{name: "transport eof", err: io.EOF, want: "transport_error"},
		{name: "parse malformed", err: errors.New("malformed SIP header"), want: "parse_error"},
		{name: "scenario generic", err: errors.New("exec command failed"), want: "scenario_error"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyCallFailure(tc.err, tc.sawUnexpectedSIP); got != tc.want {
				t.Fatalf("classifyCallFailure() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEngineMirrorsSIPToHEP(t *testing.T) {
	t.Parallel()

	hepConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(hep) error = %v", err)
	}
	defer hepConn.Close()

	hepPackets := make(chan hep.Decoded, 16)
	hepDone := make(chan struct{})
	go func() {
		defer close(hepDone)
		buffer := make([]byte, 65535)
		for {
			_ = hepConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, _, err := hepConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			packet, err := hep.Decode(buffer[:n])
			if err != nil {
				return
			}
			hepPackets <- packet
		}
	}()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(sip) error = %v", err)
	}
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				trying := fmt.Sprintf(
					"SIP/2.0 100 Trying\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(trying), addr)
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		HEPAddr:       hepConn.LocalAddr().String(),
		HEPCaptureID:  1001,
		HEPPassword:   "secret",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done
	_ = hepConn.Close()
	<-hepDone
	close(hepPackets)

	var packets []hep.Decoded
	for packet := range hepPackets {
		packets = append(packets, packet)
	}
	if len(packets) < 4 {
		t.Fatalf("expected HEP packets for SIP dialog, got %d", len(packets))
	}

	var sawInvite bool
	var sawOK bool
	for _, packet := range packets {
		if packet.ProtoType != hep.ProtocolSIP {
			t.Fatalf("unexpected HEP proto type %d", packet.ProtoType)
		}
		if packet.CaptureID != 1001 || packet.AuthKey != "secret" {
			t.Fatalf("unexpected HEP metadata: %+v", packet)
		}
		payload := string(packet.Payload)
		if strings.HasPrefix(payload, "INVITE ") {
			sawInvite = true
			if packet.SrcPort == 0 || packet.DstPort != uint16(serverConn.LocalAddr().(*net.UDPAddr).Port) {
				t.Fatalf("unexpected INVITE ports in HEP packet: %+v", packet)
			}
		}
		if strings.HasPrefix(payload, "SIP/2.0 200 OK") {
			sawOK = true
		}
	}
	if !sawInvite || !sawOK {
		t.Fatalf("expected both INVITE and 200 OK in HEP export, got %d packets", len(packets))
	}
}

func TestEngineTraceStatWritesPeriodicCSV(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	scenarioXML := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="Trace Stat UAC">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200"/>
  <pause milliseconds="1200"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`
	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	messagePath := filepath.Join(t.TempDir(), "messages.log")
	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		TraceStats:    true,
		MessageFile:   messagePath,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	statsPath := deriveStatsTracePath(messagePath)
	raw, err := os.ReadFile(statsPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_stat) error = %v", err)
	}

	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll(trace_stat) error = %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("expected header plus periodic/final rows, got %d rows: %q", len(rows), raw)
	}
	header := rows[0]
	if len(header) == 0 || header[0] != "timestamp" {
		t.Fatalf("unexpected trace_stat header: %v", header)
	}
	if len(header) < 41 || header[17] != "failure_timeout" || header[23] != "interval_ms" || header[26] != "delta_success_calls" || header[35] != "delta_failure_timeout" {
		t.Fatalf("expected richer trace_stat header with interval/delta fields, got %v", header)
	}
	last := rows[len(rows)-1]
	if last[2] != "1" || last[3] != "1" || last[4] != "0" {
		t.Fatalf("unexpected final trace_stat counters: %v", last)
	}
	if last[26] != "1" {
		t.Fatalf("expected final delta_success_calls=1, got %v", last)
	}
}

func TestEngineFailureClassesUnexpectedSIP(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			if strings.ToUpper(msg.Method) == "INVITE" {
				response := fmt.Sprintf(
					"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	scenarioXML := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="Unexpected SIP">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200" timeout="200"/>
</scenario>`
	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err == nil {
		t.Fatal("expected Run() to fail on unexpected SIP response")
	}
	<-done

	summary := app.Stats().Snapshot()
	if summary.FailureClasses["unexpected_sip"] != 1 {
		t.Fatalf("expected unexpected_sip failure classification, got %+v", summary.FailureClasses)
	}
}

func TestEngineTraceRTTWritesCSV(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				time.Sleep(40 * time.Millisecond)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	scenarioXML := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="Trace RTT UAC">
  <send start_rtd="invite"><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200" rtd="invite"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`
	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	messagePath := filepath.Join(t.TempDir(), "messages.log")
	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		TraceRTT:      true,
		MessageFile:   messagePath,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	rttPath := deriveRTTTracePath(messagePath)
	raw, err := os.ReadFile(rttPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_rtt) error = %v", err)
	}

	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll(trace_rtt) error = %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header plus RTD row, got %d rows: %q", len(rows), raw)
	}
	if rows[0][0] != "timestamp" || rows[0][2] != "name" {
		t.Fatalf("unexpected trace_rtt header: %v", rows[0])
	}
	last := rows[len(rows)-1]
	if last[1] != "1" || last[2] != "invite" {
		t.Fatalf("unexpected trace_rtt row: %v", last)
	}
}

func TestEngineCollectsNamedRTDStats(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				time.Sleep(15 * time.Millisecond)
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="rtd-uac">
  <send start_rtd="invite"><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200" rtd="invite"/>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	summary := app.Stats().Snapshot()
	rtd, ok := summary.RTD["invite"]
	if !ok {
		t.Fatalf("expected invite RTD summary, got %+v", summary.RTD)
	}
	if rtd.Count != 1 {
		t.Fatalf("expected one invite RTD sample, got %+v", rtd)
	}
	if rtd.Average <= 0 || rtd.Last <= 0 {
		t.Fatalf("expected positive invite RTD values, got %+v", rtd)
	}
}

func TestEngineCollectsCounterAndDisplayStats(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="counter-display-uac">
  <send counter="invite_sent" display="Invite sent"><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200" counter="invite_ok" display="Invite accepted"/>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	summary := app.Stats().Snapshot()
	if summary.Counters["invite_sent"] != 1 || summary.Counters["invite_ok"] != 1 {
		t.Fatalf("expected counter stats, got %+v", summary.Counters)
	}
	if summary.Displays["Invite sent"] != 1 || summary.Displays["Invite accepted"] != 1 {
		t.Fatalf("expected display stats, got %+v", summary.Displays)
	}
}

func TestEngineRunsTCPScenario(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		for {
			msg, err := sip.ReadMessage(reader)
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = conn.Write([]byte(response))
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = conn.Write([]byte(response))
				return
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "t1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    listener.Addr().(*net.TCPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	summary := app.Stats().Snapshot()
	if summary.SuccessCalls != 1 {
		t.Fatalf("expected one successful call, got %+v", summary)
	}
}

func TestEngineAppliesActionsAndVariables(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	seenExtracted := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			if extracted, ok := sip.Header(msg.Headers, "X-Extracted-URI"); ok {
				seenExtracted <- extracted
			}

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:peer@127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/actions_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	close(seenExtracted)
	var values []string
	for v := range seenExtracted {
		values = append(values, v)
	}
	if len(values) == 0 {
		t.Fatal("expected extracted header values")
	}
	found := false
	for _, value := range values {
		if strings.Contains(value, "echo@127.0.0.1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected extracted URI to propagate, got %v", values)
	}
}

func TestEngineRunsSendCmdRecvCmdScenario(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	seenHeader := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				if header, ok := sip.Header(msg.Headers, "X-Cmd"); ok {
					seenHeader <- header
				}
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="cmd-flow">
  <sendCmd dest="s1"><![CDATA[
Call-ID: [call_id]
X-Value: hello-cmd

]]></sendCmd>
  <recvCmd src="s1">
    <action>
      <ereg regexp="X-Value:\s*(.*)" search_in="msg" assign_to="0,1"/>
    </action>
  </recvCmd>
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
X-Cmd: [$1]
Content-Length: 0

]]></send>
  <recv response="200"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	select {
	case got := <-seenHeader:
		if got != "hello-cmd" {
			t.Fatalf("expected injected command payload, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected INVITE header derived from recvCmd")
	}
}

func TestEngineRunsExternalSendCmdRecvCmdScenario(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	masterAddr := reserveTCPAddr(t)
	slaveAddr := reserveTCPAddr(t)
	peers := map[string]string{
		"m":  masterAddr,
		"s1": slaveAddr,
	}

	seenHeader := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				if header, ok := sip.Header(msg.Headers, "X-Cmd"); ok {
					seenHeader <- header
				}
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	masterScenario, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="external-master">
  <sendCmd dest="s1"><![CDATA[
Call-ID: [call_id]
From: m
X-Value: external

]]></sendCmd>
  <recvCmd src="s1">
    <action>
      <ereg regexp="X-Reply:\s*(.*)" search_in="msg" assign_to="0,1"/>
    </action>
  </recvCmd>
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
X-Cmd: [$1]
Content-Length: 0

]]></send>
  <recv response="200"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString(master) error = %v", err)
	}

	slaveScenario, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="external-slave">
  <recvCmd src="m">
    <action>
      <ereg regexp="X-Value:\s*(.*)" search_in="msg" assign_to="0,1"/>
    </action>
  </recvCmd>
  <sendCmd dest="m"><![CDATA[
Call-ID: [call_id]
From: s1
X-Reply: [$1]-ack

]]></sendCmd>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString(slave) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	slaveDone := make(chan error, 1)
	go func() {
		app := New(Config{
			Scenario:      slaveScenario,
			Transport:     "u1",
			LocalIP:       "127.0.0.1",
			Service:       "echo",
			Rate:          100,
			TotalCalls:    1,
			MaxConcurrent: 1,
			DefaultPause:  10 * time.Millisecond,
			DefaultRecvTO: time.Second,
			CommandName:   "s1",
			CommandPeers:  peers,
		})
		slaveDone <- app.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	masterApp := New(Config{
		Scenario:      masterScenario,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		CommandName:   "m",
		CommandPeers:  peers,
	})
	if err := masterApp.Run(ctx); err != nil {
		t.Fatalf("Run(master) error = %v", err)
	}
	if err := <-slaveDone; err != nil {
		t.Fatalf("Run(slave) error = %v", err)
	}
	<-done

	select {
	case got := <-seenHeader:
		if got != "external-ack" {
			t.Fatalf("expected external command payload, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected INVITE header derived from external recvCmd")
	}
}

func TestEngineRunsCommandOnlyScenarioWithoutRemoteAddress(t *testing.T) {
	t.Parallel()

	masterAddr := reserveTCPAddr(t)
	slaveAddr := reserveTCPAddr(t)
	peers := map[string]string{
		"m":  masterAddr,
		"s1": slaveAddr,
	}

	masterScenario, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="command-only-master">
  <sendCmd dest="s1"><![CDATA[
Call-ID: [call_id]
From: m
X-Value: no-sip

]]></sendCmd>
  <recvCmd src="s1">
    <action>
      <ereg regexp="X-Reply:\s*(.*)" search_in="msg" assign_to="0,1"/>
    </action>
  </recvCmd>
  <nop>
    <action>
      <test assign_to="ok" variable="1" compare="equal" value="no-sip-ok"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString(master) error = %v", err)
	}

	slaveScenario, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="command-only-slave">
  <recvCmd src="m">
    <action>
      <ereg regexp="X-Value:\s*(.*)" search_in="msg" assign_to="0,1"/>
    </action>
  </recvCmd>
  <sendCmd dest="m"><![CDATA[
Call-ID: [call_id]
From: s1
X-Reply: [$1]-ok

]]></sendCmd>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString(slave) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	slaveDone := make(chan error, 1)
	go func() {
		app := New(Config{
			Scenario:      slaveScenario,
			Transport:     "u1",
			LocalIP:       "127.0.0.1",
			Service:       "echo",
			Rate:          100,
			TotalCalls:    1,
			MaxConcurrent: 1,
			DefaultPause:  10 * time.Millisecond,
			DefaultRecvTO: time.Second,
			CommandName:   "s1",
			CommandPeers:  peers,
		})
		slaveDone <- app.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	masterApp := New(Config{
		Scenario:      masterScenario,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		CommandName:   "m",
		CommandPeers:  peers,
	})
	if err := masterApp.Run(ctx); err != nil {
		t.Fatalf("Run(master) error = %v", err)
	}
	if err := <-slaveDone; err != nil {
		t.Fatalf("Run(slave) error = %v", err)
	}

	summary := masterApp.Stats().Snapshot()
	if summary.SuccessCalls != 1 || summary.FailedCalls != 0 {
		t.Fatalf("expected command-only success, got %+v", summary)
	}
}

func TestEngineRunsTLSScenario(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTestCertificate(t)
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadX509KeyPair() error = %v", err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("tls.Listen() error = %v", err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			msg, err := sip.ReadMessage(reader)
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = conn.Write([]byte(response))
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = conn.Write([]byte(response))
				return
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	app := New(Config{
		Scenario:      sc,
		Transport:     "l1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    listener.Addr().(*net.TCPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		TLSSkipVerify: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done
	if summary := app.Stats().Snapshot(); summary.SuccessCalls != 1 {
		t.Fatalf("expected one successful call, got %+v", summary)
	}
}

func TestEngineInitInjectionAndUserScope(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	type inviteHeaders struct {
		greeting string
		field    string
		user     string
	}
	var invites []inviteHeaders

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		byeCount := 0
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				greeting, _ := sip.Header(msg.Headers, "X-Greeting")
				field, _ := sip.Header(msg.Headers, "X-CSV-Field")
				user, _ := sip.Header(msg.Headers, "X-User-State")
				invites = append(invites, inviteHeaders{greeting: greeting, field: field, user: user})
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:peer@127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				byeCount++
				if byeCount == 2 {
					return
				}
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/init_injection_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    2,
		MaxConcurrent: 1,
		Users:         1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	if len(invites) != 2 {
		t.Fatalf("expected two invites, got %d", len(invites))
	}
	for _, invite := range invites {
		if invite.greeting != "hello-from-file" {
			t.Fatalf("unexpected greeting: %+v", invite)
		}
		if invite.field != "alice" {
			t.Fatalf("unexpected CSV field: %+v", invite)
		}
	}
	if invites[0].user != "" {
		t.Fatalf("expected first user state to be empty, got %+v", invites[0])
	}
	if invites[1].user != "persisted" {
		t.Fatalf("expected second user state to persist, got %+v", invites[1])
	}
}

func TestEngineInitExternalSendCmdRecvCmdScope(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	masterAddr := reserveTCPAddr(t)
	slaveAddr := reserveTCPAddr(t)
	peers := map[string]string{
		"m":  masterAddr,
		"s1": slaveAddr,
	}

	initHeader := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				if value, ok := sip.Header(msg.Headers, "X-Init"); ok {
					initHeader <- value
				}
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	masterScenario, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="init-master">
  <init>
    <sendCmd dest="s1"><![CDATA[
Call-ID: [call_id]
From: m
X-Init: boot-token

]]></sendCmd>
  </init>
  <pause milliseconds="50"/>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString(master) error = %v", err)
	}

	slaveScenario, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="init-slave">
  <Global variables="TOKEN"/>
  <Reference variables="TOKEN"/>
  <init>
    <recvCmd src="m">
      <action>
        <ereg regexp="X-Init:\s*(.*)" search_in="msg" assign_to="0,TOKEN"/>
      </action>
    </recvCmd>
  </init>
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
X-Init: [$TOKEN]
Content-Length: 0

]]></send>
  <recv response="200"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString(slave) error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	slaveDone := make(chan error, 1)
	go func() {
		app := New(Config{
			Scenario:      slaveScenario,
			Transport:     "u1",
			LocalIP:       "127.0.0.1",
			RemoteHost:    "127.0.0.1",
			RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
			Service:       "echo",
			Rate:          100,
			TotalCalls:    1,
			MaxConcurrent: 1,
			DefaultPause:  10 * time.Millisecond,
			DefaultRecvTO: time.Second,
			CommandName:   "s1",
			CommandPeers:  peers,
		})
		slaveDone <- app.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	masterApp := New(Config{
		Scenario:      masterScenario,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		CommandName:   "m",
		CommandPeers:  peers,
	})
	if err := masterApp.Run(ctx); err != nil {
		t.Fatalf("Run(master) error = %v", err)
	}
	if err := <-slaveDone; err != nil {
		t.Fatalf("Run(slave) error = %v", err)
	}
	<-done

	select {
	case got := <-initHeader:
		if got != "boot-token" {
			t.Fatalf("expected init token in INVITE, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected INVITE carrying init token")
	}
}

func TestEngineExecRTPStreamPauseResume(t *testing.T) {
	t.Parallel()

	wavPath := writeTestWAV(t, 8000)

	rtpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(rtp) error = %v", err)
	}
	defer rtpConn.Close()

	packetTimes := make(chan time.Time, 64)
	rtpDone := make(chan struct{})
	go func() {
		defer close(rtpDone)
		buffer := make([]byte, 2048)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			_ = rtpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, _, err := rtpConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if n > 0 {
				packetTimes <- time.Now()
			}
		}
	}()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(sip) error = %v", err)
	}
	defer serverConn.Close()

	scenarioXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="RTP UAC">
  <send>
    <![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]>
  </send>
  <recv response="200">
    <action>
      <exec rtp_stream="%s"/>
    </action>
  </recv>
  <pause milliseconds="70"/>
  <nop>
    <action><exec rtp_stream="pause"/></action>
  </nop>
  <pause milliseconds="70"/>
  <nop>
    <action><exec rtp_stream="resume"/></action>
  </nop>
  <pause milliseconds="70"/>
  <send>
    <![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]>
  </send>
  <recv response="200"/>
</scenario>`, wavPath)

	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	sc.BasePath = filepath.Dir(wavPath)

	sipDone := make(chan struct{})
	go func() {
		defer close(sipDone)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\nv=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n",
					via, from, to, callID, cseq,
					len(fmt.Sprintf("v=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n", rtpConn.LocalAddr().(*net.UDPAddr).Port)),
					rtpConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-sipDone
	<-rtpDone
	close(packetTimes)

	var arrivals []time.Time
	for ts := range packetTimes {
		arrivals = append(arrivals, ts)
	}
	if len(arrivals) < 4 {
		t.Fatalf("expected RTP packets, got %d", len(arrivals))
	}
	foundGap := false
	for i := 1; i < len(arrivals); i++ {
		if arrivals[i].Sub(arrivals[i-1]) > 40*time.Millisecond {
			foundGap = true
			break
		}
	}
	if !foundGap {
		t.Fatalf("expected pause/resume gap in RTP stream, got %v packets", len(arrivals))
	}
}

func TestEngineExecRTPStreamEcho(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(sip) error = %v", err)
	}
	defer serverConn.Close()

	scenarioXML := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="RTP Echo UAC">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Type: application/sdp
Content-Length: [len]

v=0
o=test 1 1 IN IP4 [local_ip]
s=-
c=IN IP4 [local_ip]
t=0 0
m=audio [media_port] RTP/AVP 0
a=rtpmap:0 PCMU/8000
]]></send>
  <recv response="200"><action><exec rtp_stream="echo"/></action></recv>
  <pause milliseconds="120"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`

	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	echoed := make(chan []byte, 1)
	sipDone := make(chan struct{})
	go func() {
		defer close(sipDone)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				endpoint, err := media.ParseAudioEndpoint(msg, "127.0.0.1")
				if err != nil {
					return
				}
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)

				clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
				if err != nil {
					return
				}
				defer clientConn.Close()
				packet, err := media.BuildPacket(media.StreamConfig{
					PayloadType: 0,
					SSRC:        7,
					Sequence:    1,
					Timestamp:   160,
				}, []byte{1, 2, 3, 4})
				if err != nil {
					return
				}
				time.Sleep(50 * time.Millisecond)
				if _, err := clientConn.WriteToUDP(packet, &net.UDPAddr{IP: net.ParseIP(endpoint.IP), Port: endpoint.Port}); err != nil {
					return
				}
				echoBuf := make([]byte, 1500)
				_ = clientConn.SetReadDeadline(time.Now().Add(time.Second))
				n, _, err := clientConn.ReadFromUDP(echoBuf)
				if err != nil {
					return
				}
				out := make([]byte, n)
				copy(out, echoBuf[:n])
				echoed <- out
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-sipDone

	select {
	case packet := <-echoed:
		if len(packet) == 0 {
			t.Fatal("expected echoed RTP packet")
		}
	case <-time.After(time.Second):
		t.Fatal("expected RTP echo packet")
	}

	summary := app.Stats().Snapshot()
	if summary.Media.RTPPacketsReceived == 0 || summary.Media.RTPPacketsSent == 0 {
		t.Fatalf("expected media counters in summary, got %+v", summary.Media)
	}
}

func TestEngineExecRTPStreamSendsRTCP(t *testing.T) {
	t.Parallel()

	wavPath := writeTestWAV(t, 8000)

	rtpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(rtp) error = %v", err)
	}
	defer rtpConn.Close()

	rtcpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpConn.LocalAddr().(*net.UDPAddr).Port + 1})
	if err != nil {
		t.Fatalf("ListenUDP(rtcp) error = %v", err)
	}
	defer rtcpConn.Close()

	rtcpReceived := make(chan *rtcp.SenderReport, 1)
	go func() {
		buffer := make([]byte, 1500)
		_ = rtcpConn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := rtcpConn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		packets, err := rtcp.Unmarshal(buffer[:n])
		if err != nil {
			return
		}
		for _, packet := range packets {
			if sr, ok := packet.(*rtcp.SenderReport); ok {
				rtcpReceived <- sr
				return
			}
		}
	}()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(sip) error = %v", err)
	}
	defer serverConn.Close()

	scenarioXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="RTP+RTCP UAC">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200"><action><exec rtp_stream="%s,1,0,PCMU/8000"/></action></recv>
  <pause milliseconds="650"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`, wavPath)

	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	sc.BasePath = filepath.Dir(wavPath)

	sipDone := make(chan struct{})
	go func() {
		defer close(sipDone)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\nv=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n",
					via, from, to, callID, cseq,
					len(fmt.Sprintf("v=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n", rtpConn.LocalAddr().(*net.UDPAddr).Port)),
					rtpConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-sipDone

	select {
	case sr := <-rtcpReceived:
		if sr.PacketCount == 0 {
			t.Fatalf("expected RTCP sender report with packets, got %+v", sr)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected RTCP sender report")
	}

	summary := app.Stats().Snapshot()
	if summary.Media.RTPPacketsSent == 0 || summary.Media.RTCPSenderReports == 0 {
		t.Fatalf("expected RTP/RTCP counters in summary, got %+v", summary.Media)
	}
}

func TestEngineExecPlayPCAPAudio(t *testing.T) {
	t.Parallel()

	pcapPath := writeTestPCAP(t)

	rtpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(rtp) error = %v", err)
	}
	defer rtpConn.Close()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(sip) error = %v", err)
	}
	defer serverConn.Close()

	scenarioXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="PCAP UAC">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200">
    <action>
      <exec play_pcap_audio="%s"/>
    </action>
  </recv>
  <pause milliseconds="140"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`, pcapPath)

	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}
	sc.BasePath = filepath.Dir(pcapPath)

	packetTimes := make(chan time.Time, 8)
	rtpDone := make(chan struct{})
	go func() {
		defer close(rtpDone)
		buffer := make([]byte, 2048)
		for {
			_ = rtpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, _, err := rtpConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if n > 0 {
				packetTimes <- time.Now()
			}
		}
	}()

	sipDone := make(chan struct{})
	go func() {
		defer close(sipDone)
		buffer := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg, err := sip.Parse(buffer[:n])
			if err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				body := fmt.Sprintf("v=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n", rtpConn.LocalAddr().(*net.UDPAddr).Port)
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s",
					via, from, to, callID, cseq, len(body), body,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "BYE":
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
				return
			}
		}
	}()

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-sipDone
	<-rtpDone
	close(packetTimes)

	var arrivals []time.Time
	for ts := range packetTimes {
		arrivals = append(arrivals, ts)
	}
	if len(arrivals) < 2 {
		t.Fatalf("expected RTP packets from PCAP replay, got %d", len(arrivals))
	}
	if gap := arrivals[1].Sub(arrivals[0]); gap < 40*time.Millisecond {
		t.Fatalf("expected PCAP timing gap, got %v", gap)
	}

	summary := app.Stats().Snapshot()
	if summary.Media.RTPPacketsSent < 2 {
		t.Fatalf("expected media counters for PCAP replay, got %+v", summary.Media)
	}
}

func writeTestCertificate(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}), 0o644); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}
	if err := os.WriteFile(
		keyFile,
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}
	return certFile, keyFile
}

func reserveTCPAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen(tcp) error = %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func writeTestWAV(t *testing.T, samples int) string {
	t.Helper()

	if samples <= 0 {
		samples = 8000
	}
	dataSize := samples * 2
	totalSize := 36 + dataSize

	var payload []byte
	payload = append(payload, []byte("RIFF")...)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(totalSize))
	payload = append(payload, []byte("WAVE")...)
	payload = append(payload, []byte("fmt ")...)
	payload = binary.LittleEndian.AppendUint32(payload, 16)
	payload = binary.LittleEndian.AppendUint16(payload, 1)
	payload = binary.LittleEndian.AppendUint16(payload, 1)
	payload = binary.LittleEndian.AppendUint32(payload, 8000)
	payload = binary.LittleEndian.AppendUint32(payload, 16000)
	payload = binary.LittleEndian.AppendUint16(payload, 2)
	payload = binary.LittleEndian.AppendUint16(payload, 16)
	payload = append(payload, []byte("data")...)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(dataSize))
	for i := 0; i < samples; i++ {
		value := int16(12000 * math.Sin(2*math.Pi*440*float64(i)/8000))
		payload = binary.LittleEndian.AppendUint16(payload, uint16(value))
	}

	path := filepath.Join(t.TempDir(), "tone.wav")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(wav) error = %v", err)
	}
	return path
}

func writeTestPCAP(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "audio.pcap")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(pcap) error = %v", err)
	}
	defer file.Close()

	writer := pcapgo.NewWriter(file)
	if err := writer.WriteFileHeader(65536, layers.LinkTypeEthernet); err != nil {
		t.Fatalf("WriteFileHeader() error = %v", err)
	}

	payloads := [][]byte{
		buildPCAPRTPPacket(t, 1, 160, []byte{1, 2, 3, 4}),
		buildPCAPRTPPacket(t, 2, 320, []byte{5, 6, 7, 8}),
	}
	timestamps := []time.Time{
		time.Unix(1700000000, 0),
		time.Unix(1700000000, int64(60*time.Millisecond)),
	}
	for i, payload := range payloads {
		packet := buildPCAPUDPPacket(t, payload)
		if err := writer.WritePacket(gopacket.CaptureInfo{
			Timestamp:     timestamps[i],
			CaptureLength: len(packet),
			Length:        len(packet),
		}, packet); err != nil {
			t.Fatalf("WritePacket() error = %v", err)
		}
	}

	return path
}

func buildPCAPRTPPacket(t *testing.T, seq uint16, ts uint32, payload []byte) []byte {
	t.Helper()

	packet, err := media.BuildPacket(media.StreamConfig{
		PayloadType: 0,
		SSRC:        99,
		Sequence:    seq,
		Timestamp:   ts,
	}, payload)
	if err != nil {
		t.Fatalf("BuildPacket() error = %v", err)
	}
	return packet
}

func buildPCAPUDPPacket(t *testing.T, payload []byte) []byte {
	t.Helper()

	eth := &layers.Ethernet{
		SrcMAC:       []byte{0, 1, 2, 3, 4, 5},
		DstMAC:       []byte{6, 7, 8, 9, 10, 11},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    net.ParseIP("192.0.2.10").To4(),
		DstIP:    net.ParseIP("198.51.100.20").To4(),
	}
	udp := &layers.UDP{
		SrcPort: layers.UDPPort(4000),
		DstPort: layers.UDPPort(5000),
	}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatalf("SetNetworkLayerForChecksum() error = %v", err)
	}

	buffer := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}, eth, ip, udp, gopacket.Payload(payload)); err != nil {
		t.Fatalf("SerializeLayers() error = %v", err)
	}
	return buffer.Bytes()
}
