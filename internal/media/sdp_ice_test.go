package media

import (
	"testing"

	"github.com/sipcapture/gossipper/internal/sip"
)

func TestParseAudioIceCredentials(t *testing.T) {
	t.Parallel()
	sdp := "m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n" +
		"a=ice-ufrag:4ZcD\r\n" +
		"a=ice-pwd:abcdefghijklmnopqrstuvwxyz12\r\n"
	u, p, ok := ParseAudioIceCredentials(sdp)
	if !ok || u != "4ZcD" || p != "abcdefghijklmnopqrstuvwxyz12" {
		t.Fatalf("got u=%q p=%q ok=%v", u, p, ok)
	}
	if _, _, ok2 := ParseAudioIceCredentials("m=audio 9 RTP/AVP 0\r\n"); ok2 {
		t.Fatal("expected false")
	}
}

func TestParseAudioICERtpUDPCandidate(t *testing.T) {
	t.Parallel()
	sdp := "v=0\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n" +
		"a=candidate:3067871091 1 udp 2122260223 10.0.0.5 61794 typ srflx raddr 172.27.186.140 rport 9\r\n" +
		"a=candidate:1234567890 1 udp 2122194687 192.168.1.2 54321 typ host generation 0\r\n"
	ip, port, ok := ParseAudioICERtpUDPCandidate(sdp)
	if !ok || ip != "192.168.1.2" || port != 54321 {
		t.Fatalf("got %s:%d ok=%v", ip, port, ok)
	}
}

func TestParseAudioICERtpUDPCandidateIPv6(t *testing.T) {
	t.Parallel()
	sdp := "m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n" +
		"a=candidate:1 1 udp 2130706431 2001:db8::1 5000 typ host\r\n"
	ip, port, ok := ParseAudioICERtpUDPCandidate(sdp)
	if !ok || ip != "2001:db8::1" || port != 5000 {
		t.Fatalf("got %s:%d ok=%v", ip, port, ok)
	}
}

func TestParseAudioEndpointUsesICECandidate(t *testing.T) {
	t.Parallel()
	msg := sip.Message{
		Body: "v=0\r\n" +
			"c=IN IP4 0.0.0.0\r\n" +
			"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n" +
			"a=candidate:1 1 udp 2122260223 203.0.113.10 49152 typ host\r\n",
	}
	ep, err := ParseAudioEndpoint(msg, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if ep.IP != "203.0.113.10" || ep.Port != 49152 {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestParseAudioICERtpUDPCandidateTrickleFragment(t *testing.T) {
	t.Parallel()
	sdp := "a=candidate:1 1 udp 100 198.51.100.2 6000 typ host\r\n" +
		"a=candidate:2 1 udp 200 198.51.100.3 7000 typ srflx\r\n"
	ip, port, ok := ParseAudioICERtpUDPCandidateTrickleFragment(sdp)
	if !ok || ip != "198.51.100.2" || port != 6000 {
		t.Fatalf("got %s:%d ok=%v", ip, port, ok)
	}
	if _, _, ok2 := ParseAudioICERtpUDPCandidateTrickleFragment("m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=candidate:1 1 udp 1 1.1.1.1 1 typ host\r\n"); ok2 {
		t.Fatal("expected false when m=audio present")
	}
}

func TestParseAudioEndpointTrickleFragmentOnly(t *testing.T) {
	t.Parallel()
	msg := sip.Message{
		Body: "a=candidate:1 1 udp 2122260223 198.51.100.88 40000 typ host\r\n",
	}
	ep, err := ParseAudioEndpoint(msg, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if ep.IP != "198.51.100.88" || ep.Port != 40000 {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestParseAudioIceCredentialsTrickleFragment(t *testing.T) {
	t.Parallel()
	sdp := "a=ice-ufrag:fragZ\r\na=ice-pwd:longpasswordlongpasswordlong\r\n"
	u, p, ok := ParseAudioIceCredentialsTrickleFragment(sdp)
	if !ok || u != "fragZ" || p != "longpasswordlongpasswordlong" {
		t.Fatalf("got u=%q p=%q ok=%v", u, p, ok)
	}
	if _, _, ok2 := ParseAudioIceCredentialsTrickleFragment("m=audio 9\r\na=ice-ufrag:x\r\na=ice-pwd:y\r\n"); ok2 {
		t.Fatal("expected false when m=audio in body")
	}
}

func TestParseVideoICERtpUDPCandidateTrickleFragment(t *testing.T) {
	t.Parallel()
	sdp := "a=candidate:1 1 udp 100 198.51.100.10 6100 typ host\r\n"
	ip, port, ok := ParseVideoICERtpUDPCandidateTrickleFragment(sdp)
	if !ok || ip != "198.51.100.10" || port != 6100 {
		t.Fatalf("got %s:%d ok=%v", ip, port, ok)
	}
	if _, _, ok2 := ParseVideoICERtpUDPCandidateTrickleFragment("m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=candidate:1 1 udp 1 1.1.1.1 1 typ host\r\n"); ok2 {
		t.Fatal("expected false when m=video present")
	}
}
