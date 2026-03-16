package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qxip/gossipper/internal/sip"
	"github.com/pion/rtp"
)

func TestRunSupports3PCCMasterSlaveAliases(t *testing.T) {
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
				if header, ok := sip.Header(msg.Headers, "X-3PCC"); ok {
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

	masterAddr := reserveTCPAddr(t)
	slaveAddr := reserveTCPAddr(t)
	peersPath := filepath.Join(t.TempDir(), "peers.cfg")
	if err := os.WriteFile(peersPath, []byte(fmt.Sprintf("m;%s\ns1;%s\n", masterAddr, slaveAddr)), 0o644); err != nil {
		t.Fatalf("WriteFile(peers) error = %v", err)
	}

	slaveDone := make(chan error, 1)
	go func() {
		slaveDone <- run([]string{
			"-sf", "../../testdata/scenarios/3pcc_slave.xml",
			"-slave", "s1",
			"-slave_cfg", peersPath,
		})
	}()

	time.Sleep(100 * time.Millisecond)

	if err := run([]string{
		"-sf", "../../testdata/scenarios/3pcc_master.xml",
		"-master", "m",
		"-slave_cfg", peersPath,
		"-rsa", serverConn.LocalAddr().String(),
		"-m", "1",
		"-r", "100",
	}); err != nil {
		t.Fatalf("run(master) error = %v", err)
	}
	if err := <-slaveDone; err != nil {
		t.Fatalf("run(slave) error = %v", err)
	}
	<-done

	select {
	case got := <-seenHeader:
		if got != "invite-ok" {
			t.Fatalf("expected 3PCC header, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected INVITE header derived from slave command reply")
	}
}

func TestRunPrintsVersion(t *testing.T) {
	t.Parallel()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	if err := run([]string{"-version"}); err != nil {
		t.Fatalf("run(-version) error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close(writer) error = %v", err)
	}
	var output bytes.Buffer
	if _, err := io.Copy(&output, r); err != nil {
		t.Fatalf("io.Copy() error = %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "gossIpper ") || !strings.Contains(text, "commit ") {
		t.Fatalf("unexpected version output %q", text)
	}
}

func TestRunGeneratesInfIndex(t *testing.T) {
	t.Parallel()

	basePath := t.TempDir()
	csvPath := filepath.Join(basePath, "users.csv")
	if err := os.WriteFile(csvPath, []byte("alice,pass_A\nbob,pass_B\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(csv) error = %v", err)
	}

	if err := run([]string{"-infindex", csvPath, "0"}); err != nil {
		t.Fatalf("run(-infindex) error = %v", err)
	}
	indexPath := csvPath + ".gossipper.idx.0.json"
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("expected generated infindex file %s: %v", indexPath, err)
	}
}

func TestShouldRunTUI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "subcommand", args: []string{"tui"}, want: true},
		{name: "interactive flag", args: []string{"-interactive"}, want: true},
		{name: "interactive long flag", args: []string{"--interactive"}, want: true},
		{name: "regular cli", args: []string{"-sn", "uac"}, want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldRunTUI(tc.args); got != tc.want {
				t.Fatalf("shouldRunTUI(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestRunRejectsSlaveScenarioStartingWithSendCmd(t *testing.T) {
	t.Parallel()

	peersPath := filepath.Join(t.TempDir(), "peers.cfg")
	if err := os.WriteFile(peersPath, []byte("m;127.0.0.1:7001\ns1;127.0.0.1:7002\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(peers) error = %v", err)
	}
	scenarioPath := filepath.Join(t.TempDir(), "bad_slave.xml")
	if err := os.WriteFile(scenarioPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="bad-slave">
  <sendCmd dest="m"><![CDATA[
Call-ID: [call_id]
From: s1
X-Reply: too-early

]]></sendCmd>
</scenario>`), 0o644); err != nil {
		t.Fatalf("WriteFile(scenario) error = %v", err)
	}

	err := run([]string{
		"-sf", scenarioPath,
		"-slave", "s1",
		"-slave_cfg", peersPath,
	})
	if err == nil || !strings.Contains(err.Error(), "slave 3PCC scenario must receive via recvCmd before the first sendCmd") {
		t.Fatalf("expected slave validation error, got %v", err)
	}
}

func TestRunSupportsServerTransportAliasS1(t *testing.T) {
	t.Parallel()

	port := reserveUDPPort(t)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- run([]string{
			"-sn", "uas",
			"-t", "s1",
			"-i", "127.0.0.1",
			"-p", port,
			"-m", "1",
		})
	}()

	time.Sleep(100 * time.Millisecond)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer conn.Close()

	callID := "alias-s1@test"
	branch := "z9hG4bK-alias-s1"
	from := "From: tester <sip:tester@127.0.0.1>;tag=fromtag"
	to := "To: service <sip:service@127.0.0.1>"
	via := fmt.Sprintf("Via: SIP/2.0/UDP 127.0.0.1:%d;branch=%s", conn.LocalAddr().(*net.UDPAddr).Port, branch)

	invite := strings.Join([]string{
		"INVITE sip:service@127.0.0.1 SIP/2.0",
		via,
		from,
		to,
		"Call-ID: " + callID,
		"CSeq: 1 INVITE",
		"Contact: <sip:tester@127.0.0.1>",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	if _, err := conn.WriteToUDP([]byte(invite), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: mustAtoi(t, port)}); err != nil {
		t.Fatalf("WriteToUDP(INVITE) error = %v", err)
	}

	ringing := readSIPMessage(t, conn)
	if ringing.StatusCode != 180 {
		t.Fatalf("expected 180 Ringing, got %+v", ringing)
	}
	ok := readSIPMessage(t, conn)
	if ok.StatusCode != 200 {
		t.Fatalf("expected 200 OK, got %+v", ok)
	}

	ack := strings.Join([]string{
		"ACK sip:service@127.0.0.1 SIP/2.0",
		via,
		from,
		to + ";tag=peer",
		"Call-ID: " + callID,
		"CSeq: 1 ACK",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	if _, err := conn.WriteToUDP([]byte(ack), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: mustAtoi(t, port)}); err != nil {
		t.Fatalf("WriteToUDP(ACK) error = %v", err)
	}

	bye := strings.Join([]string{
		"BYE sip:service@127.0.0.1 SIP/2.0",
		via,
		from,
		to + ";tag=peer",
		"Call-ID: " + callID,
		"CSeq: 2 BYE",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	if _, err := conn.WriteToUDP([]byte(bye), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: mustAtoi(t, port)}); err != nil {
		t.Fatalf("WriteToUDP(BYE) error = %v", err)
	}

	byeOK := readSIPMessage(t, conn)
	if byeOK.StatusCode != 200 {
		t.Fatalf("expected 200 OK for BYE, got %+v", byeOK)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("run(server) error = %v", err)
	}
}

func TestRunRejectsServerTransportAliasForClientScenario(t *testing.T) {
	t.Parallel()

	err := run([]string{
		"-sn", "uac",
		"-t", "s1",
		"-rsa", "127.0.0.1:5060",
	})
	if err == nil || !strings.Contains(err.Error(), "transport s1 requires a server scenario") {
		t.Fatalf("expected server transport validation error, got %v", err)
	}
}

func TestRunWritesMessageAndShortTraceFiles(t *testing.T) {
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

	tracePath := filepath.Join(t.TempDir(), "messages.log")
	if err := run([]string{
		"-sf", "../../testdata/scenarios/basic_uac.xml",
		"-rsa", serverConn.LocalAddr().String(),
		"-m", "1",
		"-r", "100",
		"-trace_msg",
		"-trace_shortmsg",
		"-message_file", tracePath,
	}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	<-done

	fullLog, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(full log) error = %v", err)
	}
	fullText := string(fullLog)
	if !strings.Contains(fullText, "send[1]") || !strings.Contains(fullText, "INVITE sip:") || !strings.Contains(fullText, "recv[1]") {
		t.Fatalf("unexpected full trace log: %q", fullText)
	}

	shortPath := deriveShortPathForTest(tracePath)
	shortLog, err := os.ReadFile(shortPath)
	if err != nil {
		t.Fatalf("ReadFile(short log) error = %v", err)
	}
	shortText := string(shortLog)
	if !strings.Contains(shortText, "direction,call,proto,summary,call_id") {
		t.Fatalf("expected short trace header, got %q", shortText)
	}
	if !strings.Contains(shortText, ",send,1,sip,INVITE,") || !strings.Contains(shortText, ",recv,1,sip,200 OK,") {
		t.Fatalf("unexpected short trace log: %q", shortText)
	}
}

func TestRunWritesActionLogTraceFile(t *testing.T) {
	t.Parallel()

	scenarioPath := filepath.Join(t.TempDir(), "log_only.xml")
	if err := os.WriteFile(scenarioPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="log-only">
  <nop>
    <action>
      <log message="hello-log"/>
    </action>
  </nop>
</scenario>`), 0o644); err != nil {
		t.Fatalf("WriteFile(scenario) error = %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "actions.log")
	if err := run([]string{
		"-sf", scenarioPath,
		"-trace_logs",
		"-log_file", logPath,
	}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	if !strings.Contains(string(logData), "action-log hello-log") {
		t.Fatalf("unexpected action log trace: %q", string(logData))
	}
}

func TestRunHonorsTimeoutGlobal(t *testing.T) {
	t.Parallel()

	scenarioPath := filepath.Join(t.TempDir(), "long_pause.xml")
	if err := os.WriteFile(scenarioPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="long-pause">
  <pause milliseconds="5000"/>
</scenario>`), 0o644); err != nil {
		t.Fatalf("WriteFile(scenario) error = %v", err)
	}

	started := time.Now()
	err := run([]string{
		"-sf", scenarioPath,
		"-timeout_global", "1",
	})
	if err != nil {
		t.Fatalf("run(timeout_global) error = %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > 3*time.Second {
		t.Fatalf("expected timeout_global to stop run quickly, elapsed=%v", elapsed)
	}
}

func TestRunWritesUnexpectedResponseToErrorTraceFile(t *testing.T) {
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
		response := fmt.Sprintf(
			"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID, cseq,
		)
		_, _ = serverConn.WriteToUDP([]byte(response), addr)
	}()

	scenarioPath := filepath.Join(t.TempDir(), "expect_180.xml")
	if err := os.WriteFile(scenarioPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="expect-180">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="180"/>
</scenario>`), 0o644); err != nil {
		t.Fatalf("WriteFile(scenario) error = %v", err)
	}

	errorPath := filepath.Join(t.TempDir(), "errors.log")
	err = run([]string{
		"-sf", scenarioPath,
		"-rsa", serverConn.LocalAddr().String(),
		"-m", "1",
		"-r", "100",
		"-recv_timeout_ms", "200",
		"-trace_err",
		"-error_file", errorPath,
	})
	<-done
	if err == nil {
		t.Fatal("expected run to fail on unexpected response")
	}

	errorData, readErr := os.ReadFile(errorPath)
	if readErr != nil {
		t.Fatalf("ReadFile(error log) error = %v", readErr)
	}
	errorText := string(errorData)
	if !strings.Contains(errorText, "unexpected-sip") || !strings.Contains(errorText, "486 Busy Here") {
		t.Fatalf("unexpected error trace log: %q", errorText)
	}
}

func TestRunWritesUnexpectedResponseToErrorCodesTraceFile(t *testing.T) {
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
		response := fmt.Sprintf(
			"SIP/2.0 486 Busy Here\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
			via, from, to, callID, cseq,
		)
		_, _ = serverConn.WriteToUDP([]byte(response), addr)
	}()

	scenarioPath := filepath.Join(t.TempDir(), "expect_180.xml")
	if err := os.WriteFile(scenarioPath, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="expect-180">
  <send><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:test@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="180"/>
</scenario>`), 0o644); err != nil {
		t.Fatalf("WriteFile(scenario) error = %v", err)
	}

	errorBase := filepath.Join(t.TempDir(), "errors.log")
	err = run([]string{
		"-sf", scenarioPath,
		"-rsa", serverConn.LocalAddr().String(),
		"-m", "1",
		"-r", "100",
		"-recv_timeout_ms", "200",
		"-trace_error_codes",
		"-error_file", errorBase,
	})
	<-done
	if err == nil {
		t.Fatal("expected run to fail on unexpected response")
	}

	errorCodesPath := deriveNamedPathForTest(errorBase, "_error_codes")
	data, readErr := os.ReadFile(errorCodesPath)
	if readErr != nil {
		t.Fatalf("ReadFile(error codes) error = %v", readErr)
	}
	text := string(data)
	if !strings.Contains(text, "call,code,reason,call_id,expected") {
		t.Fatalf("expected error codes header, got %q", text)
	}
	if !strings.Contains(text, ",1,486,Busy Here,") || !strings.Contains(text, ",180") {
		t.Fatalf("unexpected error codes trace: %q", text)
	}
}

func TestRunSupportsOutOfCallOptionsWorkflow(t *testing.T) {
	t.Parallel()

	port := reserveUDPPort(t)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- run([]string{
			"-sf", "../../testdata/scenarios/options_server.xml",
			"-t", "s1",
			"-i", "127.0.0.1",
			"-p", port,
			"-m", "1",
		})
	}()

	time.Sleep(100 * time.Millisecond)

	if err := run([]string{
		"-sf", "../../testdata/scenarios/options_client.xml",
		"-rsa", net.JoinHostPort("127.0.0.1", port),
		"-s", "options",
		"-m", "1",
		"-r", "100",
	}); err != nil {
		t.Fatalf("run(client) error = %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("run(server) error = %v", err)
	}
}

func TestRunSupportsDigestAuthenticationKeyword(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	defer serverConn.Close()

	const (
		realm = "gossip"
		nonce = "abcdef123456"
	)

	authSeen := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		challenged := false
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
				if !challenged {
					challenged = true
					response := fmt.Sprintf(
						"SIP/2.0 401 Unauthorized\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=authpeer\r\nCall-ID: %s\r\nCSeq: %s\r\nWWW-Authenticate: Digest realm=%q, nonce=%q, algorithm=MD5, qop=%q\r\nContent-Length: 0\r\n\r\n",
						via, from, to, callID, cseq, realm, nonce, "auth",
					)
					_, _ = serverConn.WriteToUDP([]byte(response), addr)
					continue
				}
				authHeader, ok := sip.Header(msg.Headers, "Authorization")
				if !ok {
					return
				}
				authSeen <- authHeader
				if !authorizationMatches(authHeader, realm, nonce, "alice", "secret", msg.Method, msg.RequestURI) {
					return
				}
				response := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=authpeer\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
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

	if err := run([]string{
		"-sf", "../../testdata/scenarios/auth_uac.xml",
		"-rsa", serverConn.LocalAddr().String(),
		"-s", "authsvc",
		"-au", "alice",
		"-ap", "secret",
		"-m", "1",
		"-r", "100",
	}); err != nil {
		t.Fatalf("run(auth) error = %v", err)
	}
	<-done

	select {
	case header := <-authSeen:
		if !strings.Contains(header, `username="alice"`) {
			t.Fatalf("expected authorization header for alice, got %q", header)
		}
	case <-time.After(time.Second):
		t.Fatal("expected authorization header on challenged INVITE")
	}
}

func TestRunBundledPCAPScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		scenarioPath    string
		expectedPackets int
		expectedPT      uint8
		expectedMinGap  time.Duration
		expectedService string
		expectedBodyPT  string
	}{
		{
			name:            "audio fixture",
			scenarioPath:    "../../testdata/scenarios/uac_pcap.xml",
			expectedPackets: 2,
			expectedPT:      0,
			expectedMinGap:  40 * time.Millisecond,
			expectedService: "pcap",
			expectedBodyPT:  "0",
		},
		{
			name:            "dtmf fixture",
			scenarioPath:    "../../testdata/scenarios/uac_dtmf_pcap.xml",
			expectedPackets: 3,
			expectedPT:      101,
			expectedMinGap:  40 * time.Millisecond,
			expectedService: "pcap",
			expectedBodyPT:  "101",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rtpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
			if err != nil {
				t.Fatalf("ListenUDP(rtp) error = %v", err)
			}
			defer rtpConn.Close()

			type packetInfo struct {
				arrivedAt   time.Time
				payloadType uint8
			}
			packetCh := make(chan packetInfo, tc.expectedPackets+2)
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
					var packet rtp.Packet
					if err := packet.Unmarshal(buffer[:n]); err != nil {
						return
					}
					packetCh <- packetInfo{
						arrivedAt:   time.Now(),
						payloadType: packet.PayloadType,
					}
				}
			}()

			serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
			if err != nil {
				t.Fatalf("ListenUDP(sip) error = %v", err)
			}
			defer serverConn.Close()

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
						body := fmt.Sprintf(
							"v=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP %s\r\n",
							rtpConn.LocalAddr().(*net.UDPAddr).Port,
							tc.expectedBodyPT,
						)
						if tc.expectedPT == 101 {
							body += "a=rtpmap:101 telephone-event/8000\r\na=fmtp:101 0-16\r\n"
						} else {
							body += "a=rtpmap:0 PCMU/8000\r\n"
						}
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

			if err := run([]string{
				"-sf", tc.scenarioPath,
				"-rsa", serverConn.LocalAddr().String(),
				"-s", tc.expectedService,
				"-m", "1",
				"-r", "100",
			}); err != nil {
				t.Fatalf("run(%s) error = %v", tc.scenarioPath, err)
			}
			<-sipDone
			<-rtpDone
			close(packetCh)

			var packets []packetInfo
			for packet := range packetCh {
				packets = append(packets, packet)
			}
			if len(packets) < tc.expectedPackets {
				t.Fatalf("expected at least %d RTP packets, got %d", tc.expectedPackets, len(packets))
			}
			if packets[0].payloadType != tc.expectedPT {
				t.Fatalf("expected first RTP payload type %d, got %d", tc.expectedPT, packets[0].payloadType)
			}
			if gap := packets[1].arrivedAt.Sub(packets[0].arrivedAt); gap < tc.expectedMinGap {
				t.Fatalf("expected replay timing gap >= %v, got %v", tc.expectedMinGap, gap)
			}
		})
	}
}

func reserveTCPAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return addr
}

func reserveUDPPort(t *testing.T) string {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	port := fmt.Sprintf("%d", conn.LocalAddr().(*net.UDPAddr).Port)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return port
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()

	var port int
	if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
		t.Fatalf("Sscanf(%q) error = %v", value, err)
	}
	return port
}

func readSIPMessage(t *testing.T, conn *net.UDPConn) sip.Message {
	t.Helper()

	buffer := make([]byte, 65535)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("ReadFromUDP() error = %v", err)
	}
	msg, err := sip.Parse(buffer[:n])
	if err != nil {
		t.Fatalf("sip.Parse() error = %v; raw=%q", err, string(buffer[:n]))
	}
	return msg
}

func deriveShortPathForTest(fullPath string) string {
	return deriveNamedPathForTest(fullPath, "_shortmessages")
}

func deriveNamedPathForTest(fullPath, suffix string) string {
	ext := filepath.Ext(fullPath)
	base := strings.TrimSuffix(fullPath, ext)
	if ext == "" {
		return base + suffix + ".log"
	}
	return base + suffix + ext
}

func authorizationMatches(header, realm, nonce, username, password, method, uri string) bool {
	if !strings.HasPrefix(strings.ToLower(header), "digest ") {
		return false
	}
	params := parseAuthHeaderParams(strings.TrimSpace(header[len("Digest "):]))
	if params["username"] != username || params["realm"] != realm || params["nonce"] != nonce || params["uri"] != uri {
		return false
	}
	if params["qop"] != "auth" || params["nc"] != "00000001" || params["cnonce"] == "" {
		return false
	}
	ha1 := md5HexForTest(fmt.Sprintf("%s:%s:%s", username, realm, password))
	ha2 := md5HexForTest(fmt.Sprintf("%s:%s", method, uri))
	expected := md5HexForTest(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, nonce, params["nc"], params["cnonce"], params["qop"], ha2))
	return params["response"] == expected
}

func parseAuthHeaderParams(value string) map[string]string {
	out := make(map[string]string)
	var (
		current strings.Builder
		quoted  bool
	)
	flush := func() {
		part := strings.TrimSpace(current.String())
		current.Reset()
		if part == "" {
			return
		}
		key, rawValue, ok := strings.Cut(part, "=")
		if !ok {
			return
		}
		out[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(rawValue), `"`)
	}
	for _, r := range value {
		switch r {
		case '"':
			quoted = !quoted
			current.WriteRune(r)
		case ',':
			if quoted {
				current.WriteRune(r)
				continue
			}
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return out
}

func md5HexForTest(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
