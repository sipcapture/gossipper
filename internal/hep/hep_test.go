package hep

import (
	"testing"
	"time"
)

func TestEncodeDecodeIPv4SIP(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 123_000_000).UTC()
	packet, err := Encode(Message{
		Time:       now,
		SrcIP:      "127.0.0.1",
		DstIP:      "127.0.0.2",
		SrcPort:    5060,
		DstPort:    9060,
		IPProtocol: 17,
		ProtoType:  ProtocolSIP,
		CaptureID:  42,
		AuthKey:    "secret",
		Payload:    []byte("INVITE sip:test@example.com SIP/2.0\r\n\r\n"),
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	decoded, err := Decode(packet)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := decoded.SrcIP.String(); got != "127.0.0.1" {
		t.Fatalf("unexpected src ip %q", got)
	}
	if got := decoded.DstIP.String(); got != "127.0.0.2" {
		t.Fatalf("unexpected dst ip %q", got)
	}
	if decoded.SrcPort != 5060 || decoded.DstPort != 9060 {
		t.Fatalf("unexpected ports: %+v", decoded)
	}
	if decoded.IPProtocol != 17 || decoded.ProtoType != ProtocolSIP {
		t.Fatalf("unexpected protocols: %+v", decoded)
	}
	if decoded.CaptureID != 42 || decoded.AuthKey != "secret" {
		t.Fatalf("unexpected capture/auth fields: %+v", decoded)
	}
	if string(decoded.Payload) != "INVITE sip:test@example.com SIP/2.0\r\n\r\n" {
		t.Fatalf("unexpected payload %q", decoded.Payload)
	}
	if !decoded.Time.Equal(now) {
		t.Fatalf("unexpected timestamp %s", decoded.Time)
	}
}

func TestEncodeRejectsBadIP(t *testing.T) {
	t.Parallel()

	_, err := Encode(Message{
		SrcIP:      "not-an-ip",
		DstIP:      "127.0.0.1",
		SrcPort:    1,
		DstPort:    2,
		IPProtocol: 17,
		ProtoType:  ProtocolSIP,
		Payload:    []byte("x"),
	})
	if err == nil {
		t.Fatal("expected Encode() to reject invalid source IP")
	}
}
