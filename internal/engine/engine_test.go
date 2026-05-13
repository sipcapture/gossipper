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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/pion/rtcp"
	"github.com/sipcapture/gossipper/internal/hep"
	"github.com/sipcapture/gossipper/internal/media"

	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/sip"
	"github.com/sipcapture/gossipper/internal/stats"
	templ "github.com/sipcapture/gossipper/internal/template"
)

const (
	expectedTraceStatHeader = "timestamp,elapsed_ms,total_calls,success_calls,failed_calls,active_calls,success_ratio,calls_per_second,retransmits,timeouts,avg_call_ms,call_stddev_ms,avg_invite_ms,invite_stddev_ms,rtp_packets_sent,rtp_packets_received,rtcp_sender_reports,rtcp_receiver_reports,rtcp_packets_received,failure_timeout,failure_unexpected_sip,failure_transport_error,failure_parse_error,failure_scenario_error,failure_cancelled,interval_ms,interval_calls_per_second,delta_total_calls,delta_success_calls,delta_failed_calls,delta_retransmits,delta_timeouts,delta_rtp_packets_sent,delta_rtp_packets_received,delta_rtcp_sender_reports,delta_rtcp_receiver_reports,delta_rtcp_packets_received,delta_failure_timeout,delta_failure_unexpected_sip,delta_failure_transport_error,delta_failure_parse_error,delta_failure_scenario_error,delta_failure_cancelled"
	expectedTraceRTTHeader  = "timestamp,call,name,value_ms"
	expectedTraceErrCodes   = "timestamp,call,code,reason,call_id,expected"
	expectedTraceScreen     = "timestamp,total_calls,success_calls,failed_calls,active_calls,success_ratio,calls_per_second,interval_ms,interval_calls_per_second,avg_call_ms,avg_invite_ms,retransmits,timeouts,failure_timeout,failure_unexpected_sip"
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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

func TestEngineAppliesBaseCSeqToRenderedToken(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	cseqSeen := make(chan string, 1)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			if strings.ToUpper(msg.Method) == "INVITE" {
				cseqSeen <- cseq
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
<scenario name="base-cseq-uac">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: [cseq] INVITE
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
		BaseCSeq:      42,
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
	case got := <-cseqSeen:
		if got != "42 INVITE" {
			t.Fatalf("expected CSeq '42 INVITE', got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected to capture INVITE CSeq header")
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

func TestResolveResponseAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		msg        sip.Message
		packetAddr *net.UDPAddr
		wantNil    bool
		wantIP     net.IP
		wantPort   int
	}{
		{
			name:       "Via IP differs from packet, use Via",
			msg:        sip.Message{Headers: map[string][]string{"Via": {"SIP/2.0/UDP 10.0.0.1:5061;branch=x"}}},
			packetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
			wantNil:    false,
			wantIP:     net.ParseIP("10.0.0.1"),
			wantPort:   5061,
		},
		{
			name:       "Via IP same as packet, use packet",
			msg:        sip.Message{Headers: map[string][]string{"Via": {"SIP/2.0/UDP 127.0.0.1:5060;branch=x"}}},
			packetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
			wantNil:    true,
		},
		{
			name:       "no Via, use packet",
			msg:        sip.Message{Headers: map[string][]string{}},
			packetAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
			wantNil:    true,
		},
		{
			name:       "Via hostname localhost resolves, differs from packet",
			msg:        sip.Message{Headers: map[string][]string{"Via": {"SIP/2.0/UDP localhost:5062;branch=x"}}},
			packetAddr: &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 5060},
			wantNil:    false,
			wantIP:     net.ParseIP("127.0.0.1"),
			wantPort:   5062,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveResponseAddr(tt.msg, tt.packetAddr)
			if tt.wantNil {
				if got != nil {
					t.Errorf("resolveResponseAddr() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("resolveResponseAddr() = nil, want *net.UDPAddr")
			}
			if !got.IP.Equal(tt.wantIP) {
				t.Errorf("resolveResponseAddr().IP = %v, want %v", got.IP, tt.wantIP)
			}
			if got.Port != tt.wantPort {
				t.Errorf("resolveResponseAddr().Port = %d, want %d", got.Port, tt.wantPort)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
	if strings.Join(header, ",") != expectedTraceStatHeader {
		t.Fatalf("unexpected trace_stat header contract: got=%q", strings.Join(header, ","))
	}
	headerIndex := make(map[string]int, len(header))
	for i, name := range header {
		headerIndex[name] = i
	}
	for _, required := range []string{"call_stddev_ms", "invite_stddev_ms", "failure_timeout", "interval_ms", "delta_success_calls", "delta_failure_timeout"} {
		if _, ok := headerIndex[required]; !ok {
			t.Fatalf("expected trace_stat column %q in header %v", required, header)
		}
	}
	last := rows[len(rows)-1]
	if last[headerIndex["total_calls"]] != "1" || last[headerIndex["success_calls"]] != "1" || last[headerIndex["failed_calls"]] != "0" {
		t.Fatalf("unexpected final trace_stat counters: %v", last)
	}
	if last[headerIndex["delta_success_calls"]] != "1" {
		t.Fatalf("expected final delta_success_calls=1, got %v", last)
	}
}

func TestEngineTraceStatHonorsCustomDumpFrequency(t *testing.T) {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
<scenario name="Trace Stat Frequency UAC">
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
  <pause milliseconds="700"/>
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
		Scenario:        sc,
		Transport:       "u1",
		LocalIP:         "127.0.0.1",
		RemoteHost:      "127.0.0.1",
		RemotePort:      serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:         "echo",
		Rate:            100,
		TotalCalls:      1,
		MaxConcurrent:   1,
		DefaultPause:    10 * time.Millisecond,
		DefaultRecvTO:   time.Second,
		TraceStats:      true,
		StatsDumpPeriod: 200 * time.Millisecond,
		MessageFile:     messagePath,
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
	if len(rows) < 4 {
		t.Fatalf("expected header plus multiple periodic rows, got %d rows: %q", len(rows), raw)
	}

	header := rows[0]
	headerIndex := make(map[string]int, len(header))
	for i, name := range header {
		headerIndex[name] = i
	}
	intervalIdx, ok := headerIndex["interval_ms"]
	if !ok {
		t.Fatalf("expected interval_ms column in header %v", header)
	}

	sawShortInterval := false
	for _, row := range rows[1:] {
		interval, err := strconv.Atoi(row[intervalIdx])
		if err != nil {
			t.Fatalf("invalid interval_ms value %q: %v", row[intervalIdx], err)
		}
		if interval > 0 && interval <= 350 {
			sawShortInterval = true
			break
		}
	}
	if !sawShortInterval {
		t.Fatalf("expected at least one interval_ms close to custom period, rows=%v", rows)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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

func TestEngineWarningActionWritesErrorTrace(t *testing.T) {
	t.Parallel()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="warning">
  <nop>
    <action>
      <assignstr assign_to="warn_value" value="watch-me"/>
      <warning message="custom warning [$warn_value]"/>
    </action>
  </nop>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	errorPath := filepath.Join(t.TempDir(), "errors.log")
	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    5060,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		TraceErrors:   true,
		ErrorFile:     errorPath,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	raw, err := os.ReadFile(errorPath)
	if err != nil {
		t.Fatalf("ReadFile(error trace) error = %v", err)
	}
	if !strings.Contains(string(raw), "warning[1] custom warning watch-me") {
		t.Fatalf("unexpected warning trace contents: %q", string(raw))
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
	if strings.Join(rows[0], ",") != expectedTraceRTTHeader {
		t.Fatalf("unexpected trace_rtt header contract: got=%q", strings.Join(rows[0], ","))
	}
	last := rows[len(rows)-1]
	if last[1] != "1" || last[2] != "invite" {
		t.Fatalf("unexpected trace_rtt row: %v", last)
	}
}

func TestEngineTraceErrorCodesWritesCSV(t *testing.T) {
	t.Parallel()

	errorPath := filepath.Join(t.TempDir(), "errors.log")
	logger, err := newTraceLogger(Config{
		TraceErrorCodes: true,
		ErrorFile:       errorPath,
	})
	if err != nil {
		t.Fatalf("newTraceLogger() error = %v", err)
	}
	engine := &Engine{trace: logger}
	engine.traceErrorCode(1, 486, "Busy Here", "call-1", "200")
	if err := logger.Close(); err != nil {
		t.Fatalf("trace logger close error = %v", err)
	}

	errCodesPath := deriveErrorCodesPath(Config{ErrorFile: errorPath}, "unused.log")
	raw, err := os.ReadFile(errCodesPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_error_codes) error = %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll(trace_error_codes) error = %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header plus at least one error-code row, got %d rows: %q", len(rows), raw)
	}
	if strings.Join(rows[0], ",") != expectedTraceErrCodes {
		t.Fatalf("unexpected trace_error_codes header: got=%q", strings.Join(rows[0], ","))
	}

	found := false
	for _, row := range rows[1:] {
		if len(row) < 6 {
			continue
		}
		if row[2] == "486" {
			found = true
			if row[3] != "Busy Here" {
				t.Fatalf("expected reason Busy Here, got %q", row[3])
			}
			if row[4] != "call-1" {
				t.Fatalf("expected call_id=call-1, got %q", row[4])
			}
			if row[5] != "200" {
				t.Fatalf("expected expected=200, got %q", row[5])
			}
		}
	}
	if !found {
		t.Fatalf("expected to find trace_error_codes row for 486 response, got %q", raw)
	}
}

func TestTraceRTTFlushesByCompletedCallFrequency(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	messagePath := filepath.Join(tempDir, "messages.log")
	logger, err := newTraceLogger(Config{
		TraceRTT:         true,
		MessageFile:      messagePath,
		RTTDumpFrequency: 2,
	})
	if err != nil {
		t.Fatalf("newTraceLogger() error = %v", err)
	}
	defer func() {
		_ = logger.Close()
	}()

	engine := &Engine{trace: logger}
	engine.traceRTD(1, "invite", 25*time.Millisecond)

	rttPath := deriveRTTTracePath(messagePath)
	raw, err := os.ReadFile(rttPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_rtt) error = %v", err)
	}
	if rows := strings.Count(string(raw), "\n"); rows != 1 {
		t.Fatalf("expected only header before call-frequency flush, got %q", string(raw))
	}

	engine.traceCallCompleted()
	raw, err = os.ReadFile(rttPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_rtt) after first call error = %v", err)
	}
	if rows := strings.Count(string(raw), "\n"); rows != 1 {
		t.Fatalf("expected no RTT row after first call completion with rtt_freq=2, got %q", string(raw))
	}

	engine.traceRTD(2, "invite", 30*time.Millisecond)
	engine.traceCallCompleted()
	raw, err = os.ReadFile(rttPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_rtt) after second call error = %v", err)
	}
	if rows := strings.Count(string(raw), "\n"); rows < 3 {
		t.Fatalf("expected header plus flushed RTT rows after second call, got %q", string(raw))
	}
}

func TestTraceScreenWritesSnapshots(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	messagePath := filepath.Join(tempDir, "messages.log")
	logger, err := newTraceLogger(Config{
		TraceScreen: true,
		MessageFile: messagePath,
	})
	if err != nil {
		t.Fatalf("newTraceLogger() error = %v", err)
	}

	collector := stats.New()
	collector.StartCall()
	collector.AddTimeout()
	collector.AddFailureClass("timeout")
	collector.FinishCall(true, 25*time.Millisecond)

	logger.startScreenLoop(collector, 50*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	if err := logger.Close(); err != nil {
		t.Fatalf("trace logger close error = %v", err)
	}

	screenPath := deriveScreenTracePath(messagePath)
	raw, err := os.ReadFile(screenPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_screen) error = %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll(trace_screen) error = %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header plus at least one screen snapshot row, got %d rows: %q", len(rows), string(raw))
	}
	if strings.Join(rows[0], ",") != expectedTraceScreen {
		t.Fatalf("unexpected trace_screen header: got=%q", strings.Join(rows[0], ","))
	}
	last := rows[len(rows)-1]
	if last[1] != "1" {
		t.Fatalf("expected total_calls=1 in screen snapshot, got row=%v", last)
	}
	if last[12] != "1" {
		t.Fatalf("expected timeouts=1 in screen snapshot, got row=%v", last)
	}
	if last[13] != "1" {
		t.Fatalf("expected failure_timeout=1 in screen snapshot, got row=%v", last)
	}
	if last[7] == "" || last[8] == "" {
		t.Fatalf("expected interval fields in screen snapshot, got row=%v", last)
	}
}

func TestEngineDumpScreenSnapshotWritesRow(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	messagePath := filepath.Join(tempDir, "messages.log")
	logger, err := newTraceLogger(Config{
		TraceScreen: true,
		MessageFile: messagePath,
	})
	if err != nil {
		t.Fatalf("newTraceLogger() error = %v", err)
	}

	engineStats := stats.New()
	engineStats.StartCall()
	engineStats.AddTimeout()
	engineStats.AddFailureClass("timeout")
	engineStats.FinishCall(false, 30*time.Millisecond)

	engine := &Engine{
		trace: logger,
		stats: engineStats,
	}
	engine.DumpScreenSnapshot()
	if err := logger.Close(); err != nil {
		t.Fatalf("trace logger close error = %v", err)
	}

	screenPath := deriveScreenTracePath(messagePath)
	raw, err := os.ReadFile(screenPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_screen) error = %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll(trace_screen) error = %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header plus screen snapshot row, got %d rows: %q", len(rows), string(raw))
	}
	last := rows[len(rows)-1]
	if last[1] != "1" || last[3] != "1" {
		t.Fatalf("unexpected calls/failures in screen snapshot: %v", last)
	}
	if last[13] != "1" {
		t.Fatalf("expected failure_timeout=1 in screen snapshot, got row=%v", last)
	}
}

func TestEngineTraceCountsWritesPerCommandCSV(t *testing.T) {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
<scenario name="Trace Counts UAC">
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
  <pause milliseconds="700"/>
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
		Scenario:        sc,
		Transport:       "u1",
		LocalIP:         "127.0.0.1",
		RemoteHost:      "127.0.0.1",
		RemotePort:      serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:         "echo",
		Rate:            100,
		TotalCalls:      1,
		MaxConcurrent:   1,
		DefaultPause:    10 * time.Millisecond,
		DefaultRecvTO:   time.Second,
		TraceCounts:     true,
		StatsDumpPeriod: 200 * time.Millisecond,
		MessageFile:     messagePath,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	countsPath := deriveCountsTracePath(messagePath)
	raw, err := os.ReadFile(countsPath)
	if err != nil {
		t.Fatalf("ReadFile(trace_counts) error = %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll(trace_counts) error = %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected header plus at least one snapshot, got %d rows: %q", len(rows), raw)
	}

	headerIndex := make(map[string]int, len(rows[0]))
	for i, name := range rows[0] {
		headerIndex[name] = i
	}
	for _, required := range []string{
		"0_INVITE_sent", "1_RESP_200_recv", "3_BYE_sent", "4_RESP_200_recv",
	} {
		if _, ok := headerIndex[required]; !ok {
			t.Fatalf("expected trace_counts column %q in header %v", required, rows[0])
		}
	}

	last := rows[len(rows)-1]
	if last[headerIndex["0_INVITE_sent"]] != "1" || last[headerIndex["3_BYE_sent"]] != "1" {
		t.Fatalf("unexpected sent counters in final trace_counts row: %v", last)
	}
	if last[headerIndex["1_RESP_200_recv"]] != "1" || last[headerIndex["4_RESP_200_recv"]] != "1" {
		t.Fatalf("unexpected recv counters in final trace_counts row: %v", last)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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

func TestEngineLookupActionFeedsFieldToken(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	seenLookup := make(chan string, 1)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				if header, ok := sip.Header(msg.Headers, "X-Lookup-Name"); ok {
					seenLookup <- header
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
<scenario name="lookup-uac">
  <nop>
    <action>
      <assignstr assign_to="lookup_key" value="2"/>
      <lookup assign_to="lookup_line" file="../../testdata/injection/inject.csv" key="[$lookup_key]"/>
    </action>
  </nop>
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
X-Lookup-Name: [field2 file=../../testdata/injection/inject.csv line=$lookup_line]
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
	close(seenLookup)

	var got []string
	for value := range seenLookup {
		got = append(got, value)
	}
	if len(got) != 1 || got[0] != "bob" {
		t.Fatalf("expected lookup header to be bob, got %v", got)
	}
}

func TestEngineUACPauseDistribution(t *testing.T) {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(response), addr)
			case "ACK":
				// OK
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

	sc, err := scenario.ParseFile("../../testdata/scenarios/uac_pause_distribution.xml")
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

func TestEngineAppliesStrCmpAndExtendedTestComparisons(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	seenCmp := make(chan string, 1)
	seenFlag := make(chan string, 1)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				if header, ok := sip.Header(msg.Headers, "X-StrCmp"); ok {
					seenCmp <- header
				}
				if header, ok := sip.Header(msg.Headers, "X-Test-Pass"); ok {
					seenFlag <- header
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
<scenario name="strcmp-uac">
  <nop>
    <action>
      <assignstr assign_to="left" value="20"/>
      <assignstr assign_to="right" value="3"/>
      <strcmp assign_to="cmp" variable="left" value="3"/>
      <test assign_to="is_greater" variable="left" compare="greater_than" variable2="right"/>
    </action>
  </nop>
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
X-StrCmp: [$cmp]
X-Test-Pass: [$is_greater]
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
	close(seenCmp)
	close(seenFlag)

	var cmpValues []string
	for value := range seenCmp {
		cmpValues = append(cmpValues, value)
	}
	var flagValues []string
	for value := range seenFlag {
		flagValues = append(flagValues, value)
	}
	if len(cmpValues) != 1 || cmpValues[0] != "-1" {
		t.Fatalf("expected strcmp result -1, got %v", cmpValues)
	}
	if len(flagValues) != 1 || flagValues[0] != "1" {
		t.Fatalf("expected greater_than test result 1, got %v", flagValues)
	}
}

func TestEngineSupportsArithmeticJumpAndHelperActions(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	seenNum := make(chan string, 1)
	seenText := make(chan string, 1)
	seenUserID := make(chan string, 1)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")

			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				if header, ok := sip.Header(msg.Headers, "X-Number"); ok {
					seenNum <- header
				}
				if header, ok := sip.Header(msg.Headers, "X-Text"); ok {
					seenText <- header
				}
				if header, ok := sip.Header(msg.Headers, "X-UserID"); ok {
					seenUserID <- header
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
<scenario name="math-uac">
  <nop>
    <action>
      <assign assign_to="num" value="1"/>
      <add assign_to="num" value="2"/>
      <multiply assign_to="num" value="4"/>
      <divide assign_to="num" value="2"/>
      <assignstr assign_to="text" value="hello world"/>
      <urlencode variable="text"/>
      <urldecode variable="text"/>
      <jump value="2"/>
    </action>
  </nop>
  <pause milliseconds="1500"/>
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
X-Number: [$num]
X-Text: [$text]
X-UserID: [userid]
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

	startedAt := time.Now()
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
		Users:         4,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if time.Since(startedAt) >= time.Second {
		t.Fatalf("expected jump to skip long pause, elapsed=%v", time.Since(startedAt))
	}
	<-done

	select {
	case got := <-seenNum:
		if got != "6" {
			t.Fatalf("expected arithmetic result 6, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected X-Number header")
	}
	select {
	case got := <-seenText:
		if got != "hello world" {
			t.Fatalf("expected decoded text, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected X-Text header")
	}
	select {
	case got := <-seenUserID:
		if got != "0" {
			t.Fatalf("expected userid 0, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected X-UserID header")
	}
}

func TestRenderSIPMessageSupportsInlineAuthenticationAndVerifyAuth(t *testing.T) {
	t.Parallel()

	app := New(Config{
		Service:      "service",
		AuthUsername: "default-user",
		AuthPassword: "default-pass",
	})
	ctx := templ.Context{
		LastMessage: "SIP/2.0 401 Unauthorized\r\nWWW-Authenticate: Digest realm=\"test.example.com\", nonce=\"abc123\", algorithm=SHA-256, qop=\"auth\"\r\nContent-Length: 0\r\n\r\n",
	}

	rendered, err := app.renderSIPMessage("REGISTER sip:test.example.com SIP/2.0\r\n[authentication username=alice password=secret]\r\nContent-Length: 0\r\n\r\n", ctx)
	if err != nil {
		t.Fatalf("renderSIPMessage() error = %v", err)
	}
	if !strings.Contains(rendered, "Authorization: Digest username=\"alice\"") {
		t.Fatalf("expected inline auth username, got %q", rendered)
	}
	valid, err := verifyAuthHeader(rendered, "alice", "secret")
	if err != nil {
		t.Fatalf("verifyAuthHeader() error = %v", err)
	}
	if !valid {
		t.Fatal("expected verifyAuthHeader to accept rendered authorization")
	}
}

func TestEngineFailsOnUnsupportedKeyword(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="bad-keyword">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
X-Bad: [not_supported_anymore]
Content-Length: 0

]]></send>
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = app.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "unsupported scenario keyword") {
		t.Fatalf("expected unsupported keyword error, got %v", err)
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				endpoint, err := media.ParseAudioEndpoint(*msg, "127.0.0.1")
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	case <-time.After(3 * time.Second):
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
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

func TestEngineExecPlayPCAPVideo(t *testing.T) {
	t.Parallel()
	runPlayPCAPByMediaType(t, "play_pcap_video", "video")
}

func TestEngineExecPlayPCAPImage(t *testing.T) {
	t.Parallel()
	runPlayPCAPByMediaType(t, "play_pcap_image", "image")
}

func runPlayPCAPByMediaType(t *testing.T, actionAttr string, mediaType string) {
	t.Helper()

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
<scenario name="PCAP Media UAC">
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
      <exec %s="%s"/>
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
</scenario>`, actionAttr, pcapPath)

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
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				body := fmt.Sprintf(
					"v=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=%s %d RTP/AVP 96\r\n",
					mediaType,
					rtpConn.LocalAddr().(*net.UDPAddr).Port,
				)
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
		t.Fatalf("expected RTP packets from %s replay, got %d", actionAttr, len(arrivals))
	}
	if gap := arrivals[1].Sub(arrivals[0]); gap < 40*time.Millisecond {
		t.Fatalf("expected PCAP timing gap, got %v", gap)
	}

	summary := app.Stats().Snapshot()
	if summary.Media.RTPPacketsSent < 2 {
		t.Fatalf("expected media counters for %s replay, got %+v", actionAttr, summary.Media)
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

func TestEngineUITransportUsesPerSourceIPSocketsAndKeywords(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	const totalCalls = 4
	errCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		seenByIP := map[string]int{}
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg := sip.GetMessage()
			defer sip.PutMessage(msg)
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			if !strings.EqualFold(msg.Method, "INVITE") {
				continue
			}
			srcIP := addr.IP.String()
			via, _ := sip.Header(msg.Headers, "Via")
			if !strings.Contains(via, srcIP) {
				select {
				case errCh <- fmt.Errorf("expected Via to contain source ip %s, got %q", srcIP, via):
				default:
				}
				return
			}

			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			response := fmt.Sprintf(
				"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
				via, from, to, callID, cseq,
			)
			_, _ = serverConn.WriteToUDP([]byte(response), addr)
			seenByIP[srcIP]++
			if seenByIP["127.0.0.2"] >= 2 && seenByIP["127.0.0.3"] >= 2 {
				return
			}
		}
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="ui-uac">
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
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "ui",
		LocalIP:       "0.0.0.0",
		RemoteHost:    "127.0.0.1",
		RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    totalCalls,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		UISourceIPs:   []string{"127.0.0.2", "127.0.0.3"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done
	select {
	case serverErr := <-errCh:
		t.Fatalf("server handler error = %v", serverErr)
	default:
	}

	summary := app.Stats().Snapshot()
	if summary.SuccessCalls != totalCalls || summary.FailedCalls != 0 {
		t.Fatalf("unexpected summary for ui transport: %+v", summary)
	}
}

func TestEngineUITransportServerUsesListenerIPForServerKeywords(t *testing.T) {
	t.Parallel()

	reserved, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(reserve) error = %v", err)
	}
	port := reserved.LocalAddr().(*net.UDPAddr).Port
	_ = reserved.Close()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="ui-uas-server-ip">
  <recv request="INVITE"/>
  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[pid]UiServerTag[call_number]
[last_Call-ID:]
[last_CSeq:]
Contact: <sip:[server_ip]:[local_port];transport=[transport]>
Content-Length: 0

]]>
  </send>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "ui",
		LocalIP:       "0.0.0.0",
		LocalPort:     port,
		RemoteHost:    "127.0.0.1",
		RemotePort:    5060,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    2,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		UISourceIPs:   []string{"127.0.0.2", "127.0.0.3"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	sendInvite := func(targetIP string, callNumber int) {
		t.Helper()
		clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
		if err != nil {
			t.Fatalf("ListenUDP(client) error = %v", err)
		}
		defer clientConn.Close()

		clientAddr := clientConn.LocalAddr().(*net.UDPAddr)
		callID := fmt.Sprintf("ui-server-%d@test", callNumber)
		invite := fmt.Sprintf(
			"INVITE sip:echo@%s:%d SIP/2.0\r\nVia: SIP/2.0/UDP %s:%d;branch=z9hG4bK-%d\r\nFrom: <sip:test@%s>;tag=t%d\r\nTo: <sip:echo@%s>\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			targetIP, port, clientAddr.IP.String(), clientAddr.Port, callNumber, clientAddr.IP.String(), callNumber, targetIP, callID,
		)
		if _, err := clientConn.WriteToUDP([]byte(invite), &net.UDPAddr{IP: net.ParseIP(targetIP), Port: port}); err != nil {
			t.Fatalf("WriteToUDP() error = %v", err)
		}

		buffer := make([]byte, 65535)
		_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, err := clientConn.ReadFromUDP(buffer)
		if err != nil {
			t.Fatalf("ReadFromUDP() error = %v", err)
		}
		if got := addr.IP.String(); got != targetIP {
			t.Fatalf("expected response from %s, got %s", targetIP, got)
		}
		msg := sip.GetMessage()
		defer sip.PutMessage(msg)
		if err := sip.ParseInto(msg, buffer[:n]); err != nil {
			t.Fatalf("sip.ParseInto() error = %v", err)
		}
		if msg.StatusCode != 200 {
			t.Fatalf("expected 200 OK, got %d", msg.StatusCode)
		}
		contact, _ := sip.Header(msg.Headers, "Contact")
		if !strings.Contains(contact, targetIP) {
			t.Fatalf("expected Contact to contain listener IP %s, got %q", targetIP, contact)
		}
	}

	sendInvite("127.0.0.2", 1)
	sendInvite("127.0.0.3", 2)

	if err := <-runErr; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	summary := app.Stats().Snapshot()
	if summary.SuccessCalls != 2 || summary.FailedCalls != 0 {
		t.Fatalf("unexpected summary for ui server transport: %+v", summary)
	}
}

func TestSourceIPForCallRoundRobinKeepsDuplicateWeights(t *testing.T) {
	t.Parallel()

	app := New(Config{
		LocalIP:     "127.0.0.1",
		UISourceIPs: []string{"127.0.0.2", "127.0.0.3", "127.0.0.2"},
	})
	got := []string{
		app.sourceIPForCall(1),
		app.sourceIPForCall(2),
		app.sourceIPForCall(3),
		app.sourceIPForCall(4),
		app.sourceIPForCall(5),
		app.sourceIPForCall(6),
	}
	want := []string{"127.0.0.2", "127.0.0.3", "127.0.0.2", "127.0.0.2", "127.0.0.3", "127.0.0.2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected source IP rotation at call %d: want %q, got %q (all=%v)", i+1, want[i], got[i], got)
		}
	}
}

func TestEngineUITransportServerAcceptsDuplicateSourceIPs(t *testing.T) {
	t.Parallel()

	reserved, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(reserve) error = %v", err)
	}
	port := reserved.LocalAddr().(*net.UDPAddr).Port
	_ = reserved.Close()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="ui-uas-dedup-ips">
  <recv request="INVITE"/>
  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[pid]UiServerDupTag[call_number]
[last_Call-ID:]
[last_CSeq:]
Contact: <sip:[server_ip]:[local_port];transport=[transport]>
Content-Length: 0

]]>
  </send>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "ui",
		LocalIP:       "0.0.0.0",
		LocalPort:     port,
		RemoteHost:    "127.0.0.1",
		RemotePort:    5060,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    2,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		UISourceIPs:   []string{"127.0.0.2", "127.0.0.2", "127.0.0.3"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	sendInvite := func(targetIP string, callNumber int) {
		t.Helper()
		clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
		if err != nil {
			t.Fatalf("ListenUDP(client) error = %v", err)
		}
		defer clientConn.Close()

		clientAddr := clientConn.LocalAddr().(*net.UDPAddr)
		callID := fmt.Sprintf("ui-server-dup-%d@test", callNumber)
		invite := fmt.Sprintf(
			"INVITE sip:echo@%s:%d SIP/2.0\r\nVia: SIP/2.0/UDP %s:%d;branch=z9hG4bK-%d\r\nFrom: <sip:test@%s>;tag=t%d\r\nTo: <sip:echo@%s>\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			targetIP, port, clientAddr.IP.String(), clientAddr.Port, callNumber, clientAddr.IP.String(), callNumber, targetIP, callID,
		)
		if _, err := clientConn.WriteToUDP([]byte(invite), &net.UDPAddr{IP: net.ParseIP(targetIP), Port: port}); err != nil {
			t.Fatalf("WriteToUDP() error = %v", err)
		}

		buffer := make([]byte, 65535)
		_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, err := clientConn.ReadFromUDP(buffer)
		if err != nil {
			t.Fatalf("ReadFromUDP() error = %v", err)
		}
		if got := addr.IP.String(); got != targetIP {
			t.Fatalf("expected response from %s, got %s", targetIP, got)
		}
		msg := sip.GetMessage()
		defer sip.PutMessage(msg)
		if err := sip.ParseInto(msg, buffer[:n]); err != nil {
			t.Fatalf("sip.ParseInto() error = %v", err)
		}
		if msg.StatusCode != 200 {
			t.Fatalf("expected 200 OK, got %d", msg.StatusCode)
		}
		contact, _ := sip.Header(msg.Headers, "Contact")
		if !strings.Contains(contact, targetIP) {
			t.Fatalf("expected Contact to contain listener IP %s, got %q", targetIP, contact)
		}
	}

	sendInvite("127.0.0.2", 1)
	sendInvite("127.0.0.3", 2)

	if err := <-runErr; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	summary := app.Stats().Snapshot()
	if summary.SuccessCalls != 2 || summary.FailedCalls != 0 {
		t.Fatalf("unexpected summary for ui server duplicate IPs: %+v", summary)
	}
}

func TestEngineUITransportServerFailsOnConflictingBind(t *testing.T) {
	t.Parallel()

	reserved, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(reserve) error = %v", err)
	}
	defer reserved.Close()
	port := reserved.LocalAddr().(*net.UDPAddr).Port

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="ui-uas-bind-conflict">
  <recv request="INVITE"/>
  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:]
[last_Call-ID:]
[last_CSeq:]
Content-Length: 0

]]>
  </send>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "ui",
		LocalPort:     port,
		Rate:          10,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
		UISourceIPs:   []string{"127.0.0.1", "127.0.0.2"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Run(ctx); err == nil {
		t.Fatal("expected Run() to fail when ui listener port is already in use")
	} else {
		if !strings.Contains(err.Error(), "transport ui failed to bind server listener on 127.0.0.1:"+strconv.Itoa(port)) {
			t.Fatalf("expected bind error to include listener address, got %v", err)
		}
	}
}

func TestEngineUITransportServerFailsOnInvalidSourceIPAddress(t *testing.T) {
	t.Parallel()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="ui-uas-invalid-ip">
  <recv request="INVITE"/>
  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:]
[last_Call-ID:]
[last_CSeq:]
Content-Length: 0

]]>
  </send>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "ui",
		LocalPort:     5060,
		Rate:          10,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
		UISourceIPs:   []string{"127.0.0.1:invalid"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Run(ctx); err == nil {
		t.Fatal("expected Run() to fail when ui source IP is malformed")
	} else {
		if !strings.Contains(err.Error(), "transport ui failed to bind server listener on 127.0.0.1:invalid:5060") {
			t.Fatalf("expected malformed bind address in error, got %v", err)
		}
	}
}

func TestEngineUITransportClientFailsOnConflictingBind(t *testing.T) {
	t.Parallel()

	reserved, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(reserve) error = %v", err)
	}
	defer reserved.Close()
	port := reserved.LocalAddr().(*net.UDPAddr).Port

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="ui-uac-bind-conflict">
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
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "ui",
		LocalPort:     port,
		RemoteHost:    "127.0.0.1",
		RemotePort:    5060,
		Service:       "echo",
		Rate:          10,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
		UISourceIPs:   []string{"127.0.0.1", "127.0.0.2"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Run(ctx); err == nil {
		t.Fatal("expected Run() to fail when ui client source bind port is already in use")
	} else {
		if !strings.Contains(err.Error(), "transport ui failed to bind client socket on 127.0.0.1:"+strconv.Itoa(port)) {
			t.Fatalf("expected client bind error to include socket address, got %v", err)
		}
	}
}

func TestEngineUITransportClientFailsOnInvalidSourceIPAddress(t *testing.T) {
	t.Parallel()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="ui-uac-invalid-ip">
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
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "ui",
		LocalPort:     5060,
		RemoteHost:    "127.0.0.1",
		RemotePort:    5060,
		Service:       "echo",
		Rate:          10,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
		UISourceIPs:   []string{"127.0.0.1:invalid"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Run(ctx); err == nil {
		t.Fatal("expected Run() to fail when ui client source IP is malformed")
	} else {
		if !strings.Contains(err.Error(), "transport ui failed to bind client socket on 127.0.0.1:invalid:5060") {
			t.Fatalf("expected malformed client bind address in error, got %v", err)
		}
	}
}

func TestEngineSetDestUpdatesUDPDestination(t *testing.T) {
	t.Parallel()

	listenerA, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(listenerA) error = %v", err)
	}
	defer listenerA.Close()

	listenerB, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(listenerB) error = %v", err)
	}
	defer listenerB.Close()

	serverErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		if err := listenerB.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, remote, err := listenerB.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		msg := sip.GetMessage()
		defer sip.PutMessage(msg)
		if err := sip.ParseInto(msg, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ := sip.Header(msg.Headers, "Via")
		from, _ := sip.Header(msg.Headers, "From")
		to, _ := sip.Header(msg.Headers, "To")
		callID, _ := sip.Header(msg.Headers, "Call-ID")
		resp := fmt.Sprintf(
			"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=server\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		_, err = listenerB.WriteToUDP([]byte(resp), remote)
		serverErr <- err
	}()

	scenarioXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="setdest-u1">
  <nop>
    <action>
      <setdest host="127.0.0.1" port="%d" protocol="UDP"/>
    </action>
  </nop>
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>`, listenerB.LocalAddr().(*net.UDPAddr).Port)
	sc, err := scenario.ParseString(scenarioXML)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     0,
		RemoteHost:    "127.0.0.1",
		RemotePort:    listenerA.LocalAddr().(*net.UDPAddr).Port,
		Service:       "echo",
		Rate:          1,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("udp responder error = %v", err)
	}
}

func TestEngineSetDestRejectsProtocolMismatch(t *testing.T) {
	t.Parallel()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="setdest-mismatch">
  <nop>
    <action>
      <setdest host="127.0.0.1" port="5070" protocol="TCP"/>
    </action>
  </nop>
  <send><![CDATA[
OPTIONS sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 OPTIONS
Content-Length: 0

]]></send>
</scenario>`)
	if err != nil {
		t.Fatalf("ParseString() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		LocalPort:     0,
		RemoteHost:    "127.0.0.1",
		RemotePort:    5070,
		Service:       "echo",
		Rate:          1,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = app.Run(ctx)
	if err == nil {
		t.Fatal("expected protocol mismatch error for setdest")
	}
	if !strings.Contains(err.Error(), `setdest protocol "TCP" is incompatible with current transport "udp"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEngineRoutesKeywordFromRecordRoute(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(server) error = %v", err)
	}
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, addr, err := serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		invite := sip.GetMessage()
		defer sip.PutMessage(invite)
		if err := sip.ParseInto(invite, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ := sip.Header(invite.Headers, "Via")
		from, _ := sip.Header(invite.Headers, "From")
		to, _ := sip.Header(invite.Headers, "To")
		callID, _ := sip.Header(invite.Headers, "Call-ID")
		response := fmt.Sprintf(
			"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=server\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nRecord-Route: <sip:edge1.example.com;lr>\r\nRecord-Route: <sip:edge2.example.com;lr>\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(response), addr); err != nil {
			serverErr <- err
			return
		}

		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, _, err = serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		byeRaw := string(buffer[:n])
		if !strings.Contains(byeRaw, "Route: <sip:edge2.example.com;lr>\r\nRoute: <sip:edge1.example.com;lr>") {
			serverErr <- fmt.Errorf("expected reversed Route headers in BYE, got %q", byeRaw)
			return
		}
		bye := sip.GetMessage()
		defer sip.PutMessage(bye)
		if err := sip.ParseInto(bye, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ = sip.Header(bye.Headers, "Via")
		from, _ = sip.Header(bye.Headers, "From")
		to, _ = sip.Header(bye.Headers, "To")
		callID, _ = sip.Header(bye.Headers, "Call-ID")
		ack := fmt.Sprintf(
			"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 2 BYE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		_, err = serverConn.WriteToUDP([]byte(ack), addr)
		serverErr <- err
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="routes-rrs">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="200" rrs="true"/>
  <send><![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
[routes]
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
		Rate:          1,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server flow error = %v", err)
	}
}

func TestEngineUnexpectedRoutesToUnexpMainLabel(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(server) error = %v", err)
	}
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, addr, err := serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		invite := sip.GetMessage()
		defer sip.PutMessage(invite)
		if err := sip.ParseInto(invite, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ := sip.Header(invite.Headers, "Via")
		from, _ := sip.Header(invite.Headers, "From")
		to, _ := sip.Header(invite.Headers, "To")
		callID, _ := sip.Header(invite.Headers, "Call-ID")
		resp := fmt.Sprintf(
			"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=busy\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(resp), addr); err != nil {
			serverErr <- err
			return
		}

		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, _, err = serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		ackRaw := string(buffer[:n])
		if !strings.Contains(ackRaw, "ACK ") {
			serverErr <- fmt.Errorf("expected ACK in _unexp.main flow, got %q", ackRaw)
			return
		}
		if !strings.Contains(ackRaw, "X-Unexp-Ret: 2") {
			serverErr <- fmt.Errorf("expected _unexp.retaddr header, got %q", ackRaw)
			return
		}
		serverErr <- nil
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="unexpected-to-main">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="180" timeout="300"/>
  <label id="_unexp.main"/>
  <send><![CDATA[
ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 1 ACK
X-Unexp-Ret: [$_unexp.retaddr]
Content-Length: 0

]]></send>
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
		Rate:          1,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultRecvTO: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server flow error = %v", err)
	}
}

func TestEngineOptionalRecvShortCircuitsOnUnexpected(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(server) error = %v", err)
	}
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, addr, err := serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		invite := sip.GetMessage()
		defer sip.PutMessage(invite)
		if err := sip.ParseInto(invite, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ := sip.Header(invite.Headers, "Via")
		from, _ := sip.Header(invite.Headers, "From")
		to, _ := sip.Header(invite.Headers, "To")
		callID, _ := sip.Header(invite.Headers, "Call-ID")
		resp := fmt.Sprintf(
			"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=busy\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		_, err = serverConn.WriteToUDP([]byte(resp), addr)
		serverErr <- err
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="optional-short-circuit">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="180" optional="true" timeout="1200"/>
  <recv response="486" timeout="1200"/>
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
		DefaultRecvTO: 1500 * time.Millisecond,
	})

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server flow error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("expected optional recv to short-circuit on unexpected SIP, elapsed=%v", elapsed)
	}
}

func TestEngineOptionalRecvRoutesToUnexpMainWithRetAddr(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(server) error = %v", err)
	}
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, addr, err := serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		invite := sip.GetMessage()
		defer sip.PutMessage(invite)
		if err := sip.ParseInto(invite, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ := sip.Header(invite.Headers, "Via")
		from, _ := sip.Header(invite.Headers, "From")
		to, _ := sip.Header(invite.Headers, "To")
		callID, _ := sip.Header(invite.Headers, "Call-ID")
		resp := fmt.Sprintf(
			"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=busy\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(resp), addr); err != nil {
			serverErr <- err
			return
		}

		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, _, err = serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		ackRaw := string(buffer[:n])
		if !strings.Contains(ackRaw, "ACK ") {
			serverErr <- fmt.Errorf("expected ACK in _unexp.main flow, got %q", ackRaw)
			return
		}
		if !strings.Contains(ackRaw, "X-Unexp-Ret: 2") {
			serverErr <- fmt.Errorf("expected _unexp.retaddr=2 from first optional recv, got %q", ackRaw)
			return
		}
		serverErr <- nil
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="optional-unexp-main-retaddr">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="100" optional="true" timeout="1200"/>
  <recv response="180" optional="true" timeout="1200"/>
  <label id="_unexp.main"/>
  <send><![CDATA[
ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 1 ACK
X-Unexp-Ret: [$_unexp.retaddr]
Content-Length: 0

]]></send>
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
		DefaultRecvTO: 1500 * time.Millisecond,
	})

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server flow error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("expected optional recv to route quickly to _unexp.main, elapsed=%v", elapsed)
	}
}

func TestEnginePendingMismatchDoesNotStarveFollowingRecv(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(server) error = %v", err)
	}
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, addr, err := serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		invite := sip.GetMessage()
		defer sip.PutMessage(invite)
		if err := sip.ParseInto(invite, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ := sip.Header(invite.Headers, "Via")
		from, _ := sip.Header(invite.Headers, "From")
		to, _ := sip.Header(invite.Headers, "To")
		callID, _ := sip.Header(invite.Headers, "Call-ID")

		// 183 will be stashed by optional recv(180), then must not block recv(486).
		r183 := fmt.Sprintf(
			"SIP/2.0 183 Session Progress\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=prg\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(r183), addr); err != nil {
			serverErr <- err
			return
		}
		r486 := fmt.Sprintf(
			"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=busy\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(r486), addr); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="pending-mismatch-no-starvation">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="180" optional="true" timeout="1200"/>
  <recv response="486" timeout="1200"/>
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
		DefaultRecvTO: 1500 * time.Millisecond,
	})

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server flow error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 600*time.Millisecond {
		t.Fatalf("expected recv(486) to proceed without pending starvation, elapsed=%v", elapsed)
	}
}

func TestEngineMultiplePendingMismatchesStillReachMandatoryRecv(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP(server) error = %v", err)
	}
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 8192)
		if err := serverConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			serverErr <- err
			return
		}
		n, addr, err := serverConn.ReadFromUDP(buffer)
		if err != nil {
			serverErr <- err
			return
		}
		invite := sip.GetMessage()
		defer sip.PutMessage(invite)
		if err := sip.ParseInto(invite, buffer[:n]); err != nil {
			serverErr <- err
			return
		}
		via, _ := sip.Header(invite.Headers, "Via")
		from, _ := sip.Header(invite.Headers, "From")
		to, _ := sip.Header(invite.Headers, "To")
		callID, _ := sip.Header(invite.Headers, "Call-ID")

		// Several mismatches before the target 486 must not trap engine in pending loop.
		r183 := fmt.Sprintf(
			"SIP/2.0 183 Session Progress\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=prg\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(r183), addr); err != nil {
			serverErr <- err
			return
		}
		r484 := fmt.Sprintf(
			"SIP/2.0 484 Address Incomplete\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=addr\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(r484), addr); err != nil {
			serverErr <- err
			return
		}
		r486 := fmt.Sprintf(
			"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=busy\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID,
		)
		if _, err := serverConn.WriteToUDP([]byte(r486), addr); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="multiple-pending-mismatch-no-starvation">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="180" optional="true" timeout="1200"/>
  <recv response="486" timeout="1200"/>
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
		DefaultRecvTO: 1500 * time.Millisecond,
	})

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server flow error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 650*time.Millisecond {
		t.Fatalf("expected mandatory recv(486) to pass after multiple mismatches, elapsed=%v", elapsed)
	}
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

// ─── Engine throughput benchmarks ────────────────────────────────────────────

// BenchmarkEngineUACUDPThroughput exercises the full engine hot path:
// RenderMessageStrict (template) → UDP send → UDP recv → sip.ParseInto (dispatch)
// → waitForMatch → executeCall — using the basic UAC scenario.
func BenchmarkEngineUACUDPThroughput(b *testing.B) {
	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		b.Fatalf("ListenUDP: %v", err)
	}
	defer serverConn.Close()

	// Minimal UAS: echo INVITE→200, BYE→200.
	go func() {
		buf := make([]byte, 65535)
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			msg := sip.GetMessage()
			if sip.ParseInto(msg, buf[:n]) != nil {
				sip.PutMessage(msg)
				continue
			}
			via, _ := sip.Header(msg.Headers, "Via")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			var resp string
			switch strings.ToUpper(msg.Method) {
			case "INVITE":
				resp = fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
			case "BYE":
				resp = fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
			}
			sip.PutMessage(msg)
			if resp != "" {
				_, _ = serverConn.WriteToUDP([]byte(resp), addr)
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		b.Fatalf("ParseFile: %v", err)
	}

	port := serverConn.LocalAddr().(*net.UDPAddr).Port
	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		app := New(Config{
			Scenario:      sc,
			Transport:     "u1",
			LocalIP:       "127.0.0.1",
			RemoteHost:    "127.0.0.1",
			RemotePort:    port,
			Service:       "echo",
			Rate:          10000,
			TotalCalls:    1,
			MaxConcurrent: 1,
			DefaultPause:  0,
			DefaultRecvTO: 2 * time.Second,
		})
		ctx := context.Background()
		if err := app.Run(ctx); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

func TestEffectiveSIPRecvTimeoutBYE(t *testing.T) {
	t.Parallel()
	cmd := scenario.Command{RecvReq: "BYE"}
	if got := effectiveSIPRecvTimeout(cmd, 5*time.Second, 90*time.Second); got != 90*time.Second {
		t.Fatalf("BYE floor: got %v", got)
	}
	cmd = scenario.Command{RecvReq: "BYE", Timeout: 3 * time.Second}
	if got := effectiveSIPRecvTimeout(cmd, 5*time.Second, 90*time.Second); got != 3*time.Second {
		t.Fatalf("explicit timeout: got %v", got)
	}
	cmd = scenario.Command{RecvReq: "INVITE"}
	if got := effectiveSIPRecvTimeout(cmd, 5*time.Second, 90*time.Second); got != 5*time.Second {
		t.Fatalf("INVITE: got %v", got)
	}
	cmd = scenario.Command{RecvReq: "BYE", Optional: true}
	if got := effectiveSIPRecvTimeout(cmd, 5*time.Second, 90*time.Second); got != sipTimerB {
		t.Fatalf("optional BYE: got %v, want %v", got, sipTimerB)
	}
	cmd = scenario.Command{RecvReq: "BYE"}
	if got := effectiveSIPRecvTimeout(cmd, 100*time.Second, 90*time.Second); got != 100*time.Second {
		t.Fatalf("default already above floor: got %v", got)
	}
	cmd = scenario.Command{RecvReq: "BYE"}
	if got := effectiveSIPRecvTimeout(cmd, 5*time.Second, 0); got != 5*time.Second {
		t.Fatalf("floor disabled: got %v", got)
	}
	// Provisional (1xx) optional recvs must NOT receive the sipTimerB floor.
	// Waiting 32s for a 100/180/183 would keep the connection idle long enough
	// for the server's TCP read timeout to fire and close the socket.
	for _, prov := range []string{"100", "180", "183"} {
		cmd = scenario.Command{RecvResp: prov, Optional: true}
		if got := effectiveSIPRecvTimeout(cmd, 5*time.Second, 90*time.Second); got != 5*time.Second {
			t.Fatalf("provisional optional %s: got %v, want default 5s", prov, got)
		}
	}
	// Final-response optional recvs (200, 4xx) still get sipTimerB.
	for _, final := range []string{"200", "4xx"} {
		cmd = scenario.Command{RecvResp: final, Optional: true}
		if got := effectiveSIPRecvTimeout(cmd, 5*time.Second, 90*time.Second); got != sipTimerB {
			t.Fatalf("final optional %s: got %v, want sipTimerB", final, got)
		}
	}
}
