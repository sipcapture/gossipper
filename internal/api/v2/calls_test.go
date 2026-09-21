package v2

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/sipcapture/gossipper/internal/siplog"
)

func TestCallsListAndDetail(t *testing.T) {
	h := newHarness(t, false)

	empty := decode[callsListResponse](t, h.do(http.MethodGet, "/api/v2/calls", nil))
	if len(empty.Calls) != 0 {
		t.Fatalf("empty %+v", empty)
	}

	h.sipLog.AddTagged("send", "10.0.0.1:5060", "REGISTER", []byte("REGISTER sip:ex SIP/2.0\r\nCall-ID: reg\r\n\r\n"))
	h.sipLog.AddTagged("recv", "10.0.0.1:5060", "one_way_uas", []byte(
		"INVITE sip:1001@ex SIP/2.0\r\nFrom: <sip:alice@ex>\r\nTo: <sip:1001@ex>\r\nCall-ID: abc@host\r\nCSeq: 1 INVITE\r\n\r\n",
	))
	h.sipLog.AddTagged("send", "10.0.0.1:5060", "one_way_uas", []byte("SIP/2.0 200 OK\r\nCall-ID: abc@host\r\nCSeq: 1 INVITE\r\n\r\n"))
	h.sipLog.AddApp("one_way_uas", "call.started", "call started", "abc@host", "call.started\ncall started", "info")
	h.sipLog.AddRTP("send", "192.0.2.1", 4000, "192.0.2.8", 5004, "abc@host", []byte{0x80, 0x08, 0x00, 0x02})

	list := decode[callsListResponse](t, h.do(http.MethodGet, "/api/v2/calls", nil))
	if len(list.Calls) != 1 || list.Calls[0].CallID != "abc@host" {
		t.Fatalf("list %+v", list)
	}
	if list.Calls[0].SIPCount != 2 || list.Calls[0].RTPSend != 1 || list.Calls[0].RTPDest != "192.0.2.8:5004" {
		t.Fatalf("counts %+v", list.Calls[0])
	}

	path := "/api/v2/calls/" + url.PathEscape("abc@host")
	detail := decode[siplog.CallDetail](t, h.do(http.MethodGet, path, nil))
	if detail.CallID != "abc@host" || len(detail.SIP) != 2 || len(detail.Debug) != 1 || len(detail.RTP) != 1 {
		t.Fatalf("detail %+v", detail)
	}
	if detail.From != "alice@ex" || detail.Codec != "PCMA" {
		t.Fatalf("fields %+v", detail.CallSummary)
	}

	zipResp := h.do(http.MethodGet, path+"?format=zip", nil)
	defer zipResp.Body.Close()
	if zipResp.StatusCode != http.StatusOK {
		t.Fatalf("zip status=%d", zipResp.StatusCode)
	}
	zipRaw, err := io.ReadAll(zipResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipRaw), int64(len(zipRaw)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"README.txt": false, "call.log": false, "call.pcap": false}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
		if f.Name == "call.log" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			logBody, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(logBody, []byte("INVITE sip:1001")) {
				t.Fatalf("call.log missing INVITE: %s", logBody)
			}
		}
	}
	for name, ok := range want {
		if !ok {
			t.Fatalf("zip missing %s", name)
		}
	}

	suffix := h.do(http.MethodGet, path+"/dump", nil)
	if suffix.StatusCode != http.StatusOK {
		t.Fatalf("dump suffix status=%d", suffix.StatusCode)
	}
	_ = suffix.Body.Close()

	missing := h.do(http.MethodGet, "/api/v2/calls/nope", nil)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.StatusCode)
	}
	_ = missing.Body.Close()

	cleared := decode[callsListResponse](t, h.do(http.MethodPost, "/api/v2/calls/clear", map[string]any{}))
	if len(cleared.Calls) != 0 {
		t.Fatalf("clear %+v", cleared)
	}
	after := decode[callsListResponse](t, h.do(http.MethodGet, "/api/v2/calls", nil))
	if len(after.Calls) != 0 {
		t.Fatalf("after clear %+v", after)
	}
}
