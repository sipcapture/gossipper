package v2

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/gopacket/pcapgo"
	"github.com/gorilla/websocket"
	"github.com/sipcapture/gossipper/internal/siplog"
)

func TestSIPTracePollAndClear(t *testing.T) {
	h := newHarness(t, false)

	empty := decode[sipTraceResponse](t, h.do(http.MethodGet, "/api/v2/sip/trace", nil))
	if empty.Next != 0 || len(empty.Messages) != 0 {
		t.Fatalf("empty %+v", empty)
	}
	if !empty.Capture.SIP || empty.Capture.App {
		t.Fatalf("default capture %+v", empty.Capture)
	}

	h.sipLog.AddTagged("send", "10.0.0.1:5060", "REGISTER", []byte("REGISTER sip:ex SIP/2.0\r\nCall-ID: a1\r\n\r\n"))
	h.sipLog.AddTagged("recv", "10.0.0.1:5060", "REGISTER", []byte("SIP/2.0 200 OK\r\nCall-ID: a1\r\n\r\n"))

	got := decode[sipTraceResponse](t, h.do(http.MethodGet, "/api/v2/sip/trace?since=0", nil))
	if got.Next != 2 || len(got.Messages) != 2 {
		t.Fatalf("first poll %+v", got)
	}
	if got.Messages[0].Dir != "send" || got.Messages[0].Method != "REGISTER" || got.Messages[0].Scenario != "REGISTER" {
		t.Fatalf("send %+v", got.Messages[0])
	}
	if got.Messages[1].Status != 200 {
		t.Fatalf("recv %+v", got.Messages[1])
	}

	inc := decode[sipTraceResponse](t, h.do(http.MethodGet, "/api/v2/sip/trace?since=2", nil))
	if inc.Next != 2 || len(inc.Messages) != 0 {
		t.Fatalf("incremental %+v", inc)
	}

	cleared := decode[sipTraceResponse](t, h.do(http.MethodPost, "/api/v2/sip/trace/clear", map[string]any{}))
	if len(cleared.Messages) != 0 || !cleared.Cleared {
		t.Fatalf("clear body %+v", cleared)
	}
	h.sipLog.AddTagged("recv", "10.0.0.1:5060", "one_way_uas", []byte("INVITE sip:1001@ex SIP/2.0\r\nCall-ID: b2\r\n\r\n"))
	after := decode[sipTraceResponse](t, h.do(http.MethodGet, "/api/v2/sip/trace?since=2", nil))
	if len(after.Messages) != 1 || after.Messages[0].Method != "INVITE" || after.Messages[0].Scenario != "one_way_uas" {
		t.Fatalf("after clear %+v", after)
	}

	bad := h.do(http.MethodGet, "/api/v2/sip/trace?since=nope", nil)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid since status=%d", bad.StatusCode)
	}
	_ = bad.Body.Close()
}

func TestSIPTraceExportTextAndPCAP(t *testing.T) {
	h := newHarness(t, false)
	h.sipLog.AddTagged("send", "10.0.0.1:5060", "REGISTER", []byte("REGISTER sip:ex SIP/2.0\r\nCall-ID: a1\r\n\r\n"))
	h.sipLog.AddTagged("recv", "10.0.0.1:5060", "one_way_uas", []byte("INVITE sip:1001@ex SIP/2.0\r\nCall-ID: b2\r\n\r\n"))

	text := h.do(http.MethodGet, "/api/v2/sip/trace/export?format=text", nil)
	defer text.Body.Close()
	if text.StatusCode != http.StatusOK {
		t.Fatalf("text status=%d", text.StatusCode)
	}
	body, err := io.ReadAll(text.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "REGISTER sip:ex") || !strings.Contains(string(body), "INVITE sip:1001") {
		t.Fatalf("text body %q", body)
	}
	if !strings.Contains(text.Header.Get("Content-Disposition"), ".txt") {
		t.Fatalf("disposition %q", text.Header.Get("Content-Disposition"))
	}

	filtered := h.do(http.MethodGet, "/api/v2/sip/trace/export?format=text&scenario=REGISTER", nil)
	defer filtered.Body.Close()
	only, err := io.ReadAll(filtered.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(only), "REGISTER") || strings.Contains(string(only), "INVITE") {
		t.Fatalf("filtered %q", only)
	}

	pcapResp := h.do(http.MethodGet, "/api/v2/sip/trace/export?format=pcap", nil)
	defer pcapResp.Body.Close()
	if pcapResp.StatusCode != http.StatusOK {
		t.Fatalf("pcap status=%d", pcapResp.StatusCode)
	}
	raw, err := io.ReadAll(pcapResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := pcapgo.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for {
		data, _, err := reader.ReadPacketData()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
		if n == 1 && !bytes.Contains(data, []byte("REGISTER sip:ex")) {
			t.Fatalf("first packet missing REGISTER")
		}
	}
	if n != 2 {
		t.Fatalf("packets=%d", n)
	}

	bad := h.do(http.MethodGet, "/api/v2/sip/trace/export?format=json", nil)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad format status=%d", bad.StatusCode)
	}
	_ = bad.Body.Close()
}

func TestSIPTraceCaptureToggle(t *testing.T) {
	h := newHarness(t, false)
	got := decode[sipTraceCapture](t, h.do(http.MethodGet, "/api/v2/sip/trace/capture", nil))
	if !got.SIP || got.App || got.Level != "debug" {
		t.Fatalf("default %+v", got)
	}
	put := decode[sipTraceCapture](t, h.do(http.MethodPut, "/api/v2/sip/trace/capture", map[string]any{"sip": false, "app": true, "level": "info"}))
	if put.SIP || !put.App || put.Level != "info" {
		t.Fatalf("put %+v", put)
	}
	keep := decode[sipTraceCapture](t, h.do(http.MethodPut, "/api/v2/sip/trace/capture", map[string]any{"sip": false, "app": true}))
	if keep.Level != "info" {
		t.Fatalf("empty level should keep previous %+v", keep)
	}
	bad := h.do(http.MethodPut, "/api/v2/sip/trace/capture", map[string]any{"sip": true, "app": true, "level": "nope"})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid level status=%d", bad.StatusCode)
	}
	_ = bad.Body.Close()
	still := decode[sipTraceCapture](t, h.do(http.MethodGet, "/api/v2/sip/trace/capture", nil))
	if still.SIP || still.Level != "info" {
		t.Fatalf("invalid put must not change capture %+v", still)
	}
	h.sipLog.AddTagged("send", "10.0.0.1:5060", "REGISTER", []byte("REGISTER sip:ex SIP/2.0\r\nCall-ID: a1\r\n\r\n"))
	h.sipLog.AddApp("one_way_uas", "scenario.cmd", "send", "cid", "send", "debug")
	h.sipLog.AddApp("one_way_uas", "call.started", "call started", "cid", "call started", "info")
	poll := decode[sipTraceResponse](t, h.do(http.MethodGet, "/api/v2/sip/trace", nil))
	if poll.Capture.SIP || !poll.Capture.App || poll.Capture.Level != "info" {
		t.Fatalf("poll capture %+v", poll.Capture)
	}
	if len(poll.Messages) != 1 || poll.Messages[0].Kind != siplog.KindApp || poll.Messages[0].Level != "info" {
		t.Fatalf("poll %+v", poll)
	}
}

func TestSIPTraceWSPushAndClear(t *testing.T) {
	h := newHarness(t, false)
	c := dialSIPTraceWS(t, h, "")
	defer c.Close()

	var frame sipTraceResponse
	if err := readSIPTraceFrame(t, c, &frame); err != nil {
		t.Fatalf("hello: %v", err)
	}
	if len(frame.Messages) != 0 || frame.Next != 0 {
		t.Fatalf("hello %+v", frame)
	}
	if !frame.Capture.SIP || frame.Capture.App {
		t.Fatalf("hello capture %+v", frame.Capture)
	}

	h.sipLog.AddTagged("send", "10.0.0.1:5060", "REGISTER", []byte("REGISTER sip:ex SIP/2.0\r\nCall-ID: a1\r\n\r\n"))
	if err := readSIPTraceFrame(t, c, &frame); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(frame.Messages) != 1 || frame.Messages[0].Method != "REGISTER" || frame.Messages[0].Scenario != "REGISTER" {
		t.Fatalf("push %+v", frame)
	}

	h.sipLog.Clear()
	if err := readSIPTraceFrame(t, c, &frame); err != nil {
		t.Fatalf("cleared: %v", err)
	}
	if !frame.Cleared || len(frame.Messages) != 0 {
		t.Fatalf("cleared %+v", frame)
	}
}

func TestSIPTraceWSRequiresToken(t *testing.T) {
	h := newHarness(t, true)
	u := wsURL(h.srv.URL, "/api/v2/sip/trace/ws")
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatal("expected unauthorized upgrade")
	}
	if resp != nil {
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status=%d", resp.StatusCode)
		}
	}
	c := dialSIPTraceWS(t, h, h.token)
	defer c.Close()
	var frame sipTraceResponse
	if err := readSIPTraceFrame(t, c, &frame); err != nil {
		t.Fatalf("authed hello: %v", err)
	}
}

func dialSIPTraceWS(t *testing.T, h *harness, token string) *websocket.Conn {
	t.Helper()
	u := wsURL(h.srv.URL, "/api/v2/sip/trace/ws")
	if token != "" {
		u += "?token=" + token
	}
	c, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial %s: %v", u, err)
	}
	return c
}

func readSIPTraceFrame(t *testing.T, c *websocket.Conn, frame *sipTraceResponse) error {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	return c.ReadJSON(frame)
}

func wsURL(httpURL, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}
