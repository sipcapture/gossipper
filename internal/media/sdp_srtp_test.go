package media

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"
)

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

func TestParseAudioSDESCrypto(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x01}, 16)
	salt := bytes.Repeat([]byte{0x02}, 14)
	inline := base64.StdEncoding.EncodeToString(append(append([]byte{}, key...), salt...))
	sdp := fmt.Sprintf("m=audio 5000 RTP/SAVP 0\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:%s\r\n", inline)
	suite, gotKey, gotSalt, err := ParseAudioSDESCrypto(sdp)
	if err != nil {
		t.Fatal(err)
	}
	if suite != "AES_CM_128_HMAC_SHA1_80" {
		t.Fatalf("suite %q", suite)
	}
	if !bytes.Equal(gotKey, key) || !bytes.Equal(gotSalt, salt) {
		t.Fatalf("key/salt mismatch")
	}
}

func TestParseAudioSDESCryptoSkipsVideoSection(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x07}, 16)
	salt := bytes.Repeat([]byte{0x08}, 14)
	inline := base64.StdEncoding.EncodeToString(append(append([]byte{}, key...), salt...))
	sdp := "m=video 9 RTP/SAVP 96\r\na=crypto:9 AES_CM_128_HMAC_SHA1_80 inline:AAAA\r\n" +
		fmt.Sprintf("m=audio 5000 RTP/SAVP 0\r\na=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:%s\r\n", inline)
	suite, gotKey, gotSalt, err := ParseAudioSDESCrypto(sdp)
	if err != nil {
		t.Fatal(err)
	}
	if suite != "AES_CM_128_HMAC_SHA1_80" || !bytes.Equal(gotKey, key) {
		t.Fatal("expected audio section crypto")
	}
	if len(gotSalt) != 14 {
		t.Fatalf("salt len %d", len(gotSalt))
	}
}
