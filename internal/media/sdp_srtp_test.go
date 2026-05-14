package media

import "testing"

func TestSDPHintsSRTP(t *testing.T) {
	t.Parallel()
	if !SDPHintsSRTP("m=audio 9 RTP/SAVP 0\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:abcd") {
		t.Fatal("expected SAVP+crypto to hint SRTP")
	}
	if !SDPHintsSRTP("m=audio 9 RTP/SAVPF 111\r\n") {
		t.Fatal("expected SAVPF to hint SRTP")
	}
	if !SDPHintsSRTP("m=audio 9 RTP/AVP 0\r\na=fingerprint:sha-256 AA:BB\r\n") {
		t.Fatal("expected DTLS fingerprint to hint SRTP")
	}
	if SDPHintsSRTP("m=audio 9 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n") {
		t.Fatal("plain RTP/AVP should not hint SRTP")
	}
}
