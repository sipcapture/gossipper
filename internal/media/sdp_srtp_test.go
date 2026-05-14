package media

import (
	"bytes"
	"testing"
)

func TestParseAudioFingerprintSHA256(t *testing.T) {
	t.Parallel()
	sdp := "m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n" +
		"a=fingerprint:sha-256 12:34:56:78:9A:BC:DE:F0:12:34:56:78:9A:BC:DE:F0:12:34:56:78:9A:BC:DE:F0:12:34:56:78:9A:BC:DE:F0\r\n"
	fp, err := ParseAudioFingerprintSHA256(sdp)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp) != 32 {
		t.Fatalf("len %d", len(fp))
	}
	want := []byte{
		0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
		0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
		0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
		0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
	}
	if !bytes.Equal(fp, want) {
		t.Fatalf("fp mismatch")
	}
}

func TestParseAudioFingerprintSHA384(t *testing.T) {
	t.Parallel()
	hex48 := "00:01:02:03:04:05:06:07:08:09:0a:0b:0c:0d:0e:0f:10:11:12:13:14:15:16:17:18:19:1a:1b:1c:1d:1e:1f:20:21:22:23:24:25:26:27:28:29:2a:2b:2c:2d:2e:2f"
	sdp := "m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=fingerprint:sha-384 " + hex48 + "\r\n"
	algo, fp, err := ParseAudioFingerprint(sdp)
	if err != nil {
		t.Fatal(err)
	}
	if algo != "sha-384" || len(fp) != 48 {
		t.Fatalf("algo=%q len=%d", algo, len(fp))
	}
	for i := range fp {
		if fp[i] != byte(i) {
			t.Fatalf("byte %d: got %02x want %02x", i, fp[i], i)
		}
	}
}

func TestAudioSectionHasRtcpMux(t *testing.T) {
	t.Parallel()
	sdp := "m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=rtcp-mux\r\n"
	if !AudioSectionHasRtcpMux(sdp) {
		t.Fatal("expected mux")
	}
	if AudioSectionHasRtcpMux("m=audio 9 RTP/AVP 0\r\n") {
		t.Fatal("unexpected mux")
	}
}

func TestParseAudioDTLSSetup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sdp  string
		want string
	}{
		{"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=setup:active\r\n", "active"},
		{"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=setup:PASSIVE\r\n", "passive"},
		{"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=setup:actpass\r\n", "actpass"},
		{"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n", ""},
		{
			"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=setup:active\r\n" +
				"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=setup:passive\r\n",
			"passive",
		},
	}
	for _, tc := range cases {
		if got := ParseAudioDTLSSetup(tc.sdp); got != tc.want {
			t.Fatalf("ParseAudioDTLSSetup(%q) = %q, want %q", tc.sdp, got, tc.want)
		}
	}
}
