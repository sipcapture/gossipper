package transport

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleInvite = "INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/WS gossipper.test;branch=z9hG4bK-ws-1\r\n" +
	"To: <sip:bob@example.com>\r\n" +
	"From: <sip:alice@example.com>;tag=1\r\n" +
	"Call-ID: ws-test\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n\r\n"

func TestWSRoundTrip(t *testing.T) {
	srv := NewWSServer()
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/sip"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := DialWS(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer client.Close()

	var server *SharedWS
	select {
	case server = <-srv.Accept():
	case <-time.After(time.Second):
		t.Fatal("server did not accept WS client in time")
	}
	defer server.Close()

	if err := client.Send([]byte(sampleInvite)); err != nil {
		t.Fatalf("client.Send: %v", err)
	}

	select {
	case msg := <-server.Incoming():
		if msg.Method != "INVITE" {
			t.Fatalf("expected INVITE, got %q (startline=%q)", msg.Method, msg.StartLine)
		}
	case <-time.After(time.Second):
		t.Fatal("server never received SIP message")
	}
}

func TestWSServerRejectsNonWS(t *testing.T) {
	srv := NewWSServer()
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()
	defer srv.Close()

	resp, err := hs.Client().Get(hs.URL + "/sip")
	if err != nil {
		t.Fatalf("http GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for plain HTTP probe, got %d", resp.StatusCode)
	}
}
