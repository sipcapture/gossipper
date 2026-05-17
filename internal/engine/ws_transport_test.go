package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/sip"
)

// TestEngineRunsWSScenarioClient drives a UAC engine with transport=w1
// against a tiny stub WS server that echoes 200 OK to every INVITE and BYE.
// Verifies that the new ws transport adapter is wired correctly end-to-end.
func TestEngineRunsWSScenarioClient(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"sip"},
		CheckOrigin:  func(_ *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg, err := sip.Parse(data)
			if err != nil {
				continue
			}
			callID, _ := sip.Header(msg.Headers, "Call-ID")
			from, _ := sip.Header(msg.Headers, "From")
			to, _ := sip.Header(msg.Headers, "To")
			via, _ := sip.Header(msg.Headers, "Via")
			cseq, _ := sip.Header(msg.Headers, "CSeq")
			method := strings.ToUpper(msg.Method)
			var resp string
			switch method {
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
			default:
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(resp)); err != nil {
				return
			}
			if method == "BYE" {
				return
			}
		}
	}))
	defer srv.Close()

	u, err := net.ResolveTCPAddr("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("ResolveTCPAddr: %v", err)
	}

	sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	app := New(Config{
		Scenario:      sc,
		Transport:     "w1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    u.IP.String(),
		RemotePort:    u.Port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: time.Second,
		WSPath:        "/",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	summary := app.Stats().Snapshot()
	if summary.SuccessCalls != 1 {
		t.Fatalf("expected one successful call, got %+v", summary)
	}
}

// TestEngineWSRoundTripUASAndUAC plugs a UAS engine (transport=w1, server
// scenario) and a UAC engine (transport=w1, client scenario) into the same
// process, on a random local port. Validates the server-side accept loop +
// per-call dispatch.
func TestEngineWSRoundTripUASAndUAC(t *testing.T) {
	t.Parallel()

	// Pick a random port for the UAS.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	scServer, err := scenario.ParseFile("../../testdata/scenarios/options_server.xml")
	if err != nil {
		t.Fatalf("server scenario: %v", err)
	}
	scClient, err := scenario.ParseFile("../../testdata/scenarios/options_client.xml")
	if err != nil {
		t.Fatalf("client scenario: %v", err)
	}

	uas := New(Config{
		Scenario:      scServer,
		Transport:     "w1",
		LocalIP:       "127.0.0.1",
		LocalPort:     port,
		Service:       "echo",
		MaxConcurrent: 4,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: 2 * time.Second,
		WSPath:        "/",
	})
	uasCtx, uasCancel := context.WithCancel(context.Background())
	defer uasCancel()
	uasDone := make(chan error, 1)
	go func() { uasDone <- uas.Run(uasCtx) }()

	// Give the UAS a moment to bind.
	time.Sleep(150 * time.Millisecond)

	uac := New(Config{
		Scenario:      scClient,
		Transport:     "w1",
		LocalIP:       "127.0.0.1",
		RemoteHost:    "127.0.0.1",
		RemotePort:    port,
		Service:       "echo",
		Rate:          100,
		TotalCalls:    1,
		MaxConcurrent: 1,
		DefaultPause:  10 * time.Millisecond,
		DefaultRecvTO: 2 * time.Second,
		WSPath:        "/",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := uac.Run(ctx); err != nil {
		t.Fatalf("uac.Run: %v", err)
	}
	uasCancel()
	select {
	case <-uasDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("uas did not stop in time")
	}
	if got := uac.Stats().Snapshot().SuccessCalls; got != 1 {
		t.Fatalf("uac expected 1 success, got %d", got)
	}
}
