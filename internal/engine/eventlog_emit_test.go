package engine

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/qxip/gossipper/internal/eventlog"
	"github.com/qxip/gossipper/internal/scenario"
	"github.com/qxip/gossipper/internal/sip"
)

// TestEngineEmitsStructuredEventsForUACDialog verifies that, for a basic
// UAC dialog (INVITE -> 200 -> ACK -> BYE), the structured logger emits
// the expected sequence of events: call.started, sip.send INVITE,
// sip.recv 100, sip.recv 200, sip.send ACK, sip.send BYE, sip.recv 200,
// call.ended.
func TestEngineEmitsStructuredEventsForUACDialog(t *testing.T) {
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
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				sip.PutMessage(msg)
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			method := strings.ToUpper(msg.Method)
			sip.PutMessage(msg)

			switch method {
			case "INVITE":
				trying := fmt.Sprintf(
					"SIP/2.0 100 Trying\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(trying), addr)
				ok := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
				)
				_, _ = serverConn.WriteToUDP([]byte(ok), addr)
			case "BYE":
				ok := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(ok), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	mem := eventlog.NewMemorySink()
	logger := eventlog.New(eventlog.Config{
		Sinks:      []eventlog.Sink{mem},
		BufferSize: 1024,
	})

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
		Log:           logger,
		Role:          "client",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	if err := logger.Close(); err != nil {
		t.Fatalf("logger close: %v", err)
	}

	events := eventlog.MemorySinkEvents(mem)
	if len(events) == 0 {
		t.Fatalf("expected at least one event, got none")
	}
	if got := events[0].Kind; got != eventlog.KindCallStart {
		t.Fatalf("expected first event call.started, got %q", got)
	}
	if got := events[len(events)-1].Kind; got != eventlog.KindCallEnd {
		t.Fatalf("expected last event call.ended, got %q", got)
	}

	wantSubsequence := []struct {
		Kind   string
		Method string
		Status int
	}{
		{eventlog.KindCallStart, "", 0},
		{eventlog.KindSIPSend, "INVITE", 0},
		{eventlog.KindSIPRecv, "", 100},
		{eventlog.KindSIPRecv, "", 200},
		{eventlog.KindSIPSend, "ACK", 0},
		{eventlog.KindSIPSend, "BYE", 0},
		{eventlog.KindSIPRecv, "", 200},
		{eventlog.KindCallEnd, "", 0},
	}

	idx := 0
	for _, ev := range events {
		want := wantSubsequence[idx]
		if ev.Kind != want.Kind {
			continue
		}
		if want.Method != "" {
			method, _ := ev.Attrs["sip.method"].(string)
			if method != want.Method {
				continue
			}
		}
		if want.Status != 0 {
			status, _ := ev.Attrs["sip.status"].(int)
			if status != want.Status {
				continue
			}
		}
		idx++
		if idx == len(wantSubsequence) {
			break
		}
	}
	if idx != len(wantSubsequence) {
		var got []string
		for _, ev := range events {
			method, _ := ev.Attrs["sip.method"].(string)
			status, _ := ev.Attrs["sip.status"].(int)
			got = append(got, fmt.Sprintf("%s(method=%q status=%d)", ev.Kind, method, status))
		}
		t.Fatalf("event subsequence not found at index %d/%d.\nwant prefix: %+v\nactual: %v",
			idx, len(wantSubsequence), wantSubsequence, got)
	}

	for _, ev := range events {
		if _, ok := ev.Attrs["call_id"]; !ok {
			t.Fatalf("event missing call_id attr: %+v", ev)
		}
	}
}

// TestEngineCallEndCarriesResultAndDuration verifies that the call.ended
// event includes a result classification and a duration in milliseconds.
func TestEngineCallEndCarriesResultAndDuration(t *testing.T) {
	t.Parallel()

	mem := eventlog.NewMemorySink()
	logger := eventlog.New(eventlog.Config{
		Sinks:      []eventlog.Sink{mem},
		BufferSize: 64,
	})

	// No SIP responder is started, so the UAC will time out.
	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "u1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    1, // closed port
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  time.Millisecond,
		DefaultRecvTO: 50 * time.Millisecond,
		Log:           logger,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = app.Run(ctx)

	if err := logger.Close(); err != nil {
		t.Fatalf("logger close: %v", err)
	}

	var end *eventlog.Event
	events := eventlog.MemorySinkEvents(mem)
	for i := range events {
		if events[i].Kind == eventlog.KindCallEnd {
			end = &events[i]
		}
	}
	if end == nil {
		t.Fatalf("expected at least one call.ended event, got %d events", len(events))
	}
	if _, ok := end.Attrs["duration_ms"]; !ok {
		t.Fatalf("call.ended missing duration_ms: %+v", end.Attrs)
	}
	result, _ := end.Attrs["result"].(string)
	if result == "" || result == "success" {
		t.Fatalf("expected non-success result on timeout, got %q (attrs=%+v)", result, end.Attrs)
	}

	var (
		gotTimeout bool
		gotError   bool
	)
	for _, ev := range events {
		switch ev.Kind {
		case eventlog.KindTimeout:
			gotTimeout = true
			if _, ok := ev.Attrs["timeout_ms"]; !ok {
				t.Fatalf("timeout event missing timeout_ms: %+v", ev.Attrs)
			}
		case eventlog.KindError:
			gotError = true
		}
	}
	if !gotTimeout {
		t.Fatalf("expected at least one %s event, got events: %v", eventlog.KindTimeout, kinds(events))
	}
	if !gotError {
		t.Fatalf("expected at least one %s event, got events: %v", eventlog.KindError, kinds(events))
	}
}

// TestEngineEmitsAuthEventOnDigestChallenge runs an auth scenario where the
// fake UAS replies with 401 + WWW-Authenticate; the engine must emit a
// KindAuth event carrying realm/algorithm before the second INVITE goes out.
func TestEngineEmitsAuthEventOnDigestChallenge(t *testing.T) {
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
		seenChallenge := false
		for {
			_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, addr, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			msg := sip.GetMessage()
			if err := sip.ParseInto(msg, buffer[:n]); err != nil {
				sip.PutMessage(msg)
				return
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			method := strings.ToUpper(msg.Method)
			sip.PutMessage(msg)

			switch method {
			case "INVITE":
				if !seenChallenge {
					seenChallenge = true
					reply := fmt.Sprintf(
						"SIP/2.0 401 Unauthorized\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nWWW-Authenticate: Digest realm=\"gossipper\",nonce=\"abc123\",algorithm=MD5\r\nContent-Length: 0\r\n\r\n",
						via, from, to, callID, cseq,
					)
					_, _ = serverConn.WriteToUDP([]byte(reply), addr)
				} else {
					ok := fmt.Sprintf(
						"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
						via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
					)
					_, _ = serverConn.WriteToUDP([]byte(ok), addr)
				}
			case "BYE":
				ok := fmt.Sprintf(
					"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
					via, from, to, callID, cseq,
				)
				_, _ = serverConn.WriteToUDP([]byte(ok), addr)
				return
			}
		}
	}()

	sc, err := scenario.ParseFile("../../testdata/scenarios/auth_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	mem := eventlog.NewMemorySink()
	logger := eventlog.New(eventlog.Config{
		Sinks:      []eventlog.Sink{mem},
		BufferSize: 1024,
	})

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
		AuthUsername:  "alice",
		AuthPassword:  "secret",
		Log:           logger,
		Role:          "client",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	<-done

	if err := logger.Close(); err != nil {
		t.Fatalf("logger close: %v", err)
	}

	events := eventlog.MemorySinkEvents(mem)
	var auth *eventlog.Event
	for i := range events {
		if events[i].Kind == eventlog.KindAuth {
			auth = &events[i]
			break
		}
	}
	if auth == nil {
		t.Fatalf("expected at least one %s event, got events: %v", eventlog.KindAuth, kinds(events))
	}
	if got, _ := auth.Attrs["realm"].(string); got != "gossipper" {
		t.Fatalf("expected realm=gossipper, got %q (attrs=%+v)", got, auth.Attrs)
	}
	if got, _ := auth.Attrs["result"].(string); got != "ok" {
		t.Fatalf("expected result=ok, got %q (attrs=%+v)", got, auth.Attrs)
	}
}

func kinds(events []eventlog.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	return out
}
