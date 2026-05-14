package media

import (
	"bytes"
	"testing"

	"github.com/pion/srtp/v3"
)

func TestSDESKeyRoundTripEncryptDecrypt(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x3a}, 16)
	salt := bytes.Repeat([]byte{0x5c}, 14)
	profile := srtp.ProtectionProfileAes128CmHmacSha1_80
	enc, err := srtp.CreateContext(key, salt, profile)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := srtp.CreateContext(key, salt, profile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig("")
	cfg.PayloadType = 0
	cfg.SSRC = 0x11223344
	cfg.Sequence = 1000
	cfg.Timestamp = 2000
	payload := bytes.Repeat([]byte{0xff}, 160)
	plain, err := BuildPacket(cfg, payload)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := enc.EncryptRTP(nil, plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := dec.DecryptRTP(nil, wire, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatalf("decrypt mismatch len plain=%d out=%d", len(plain), len(out))
	}
}

func TestSRTCPRoundTripEncryptDecrypt(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x2b}, 16)
	salt := bytes.Repeat([]byte{0x4d}, 14)
	profile := srtp.ProtectionProfileAes128CmHmacSha1_80
	enc, err := srtp.CreateContext(key, salt, profile)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := srtp.CreateContext(key, salt, profile)
	if err != nil {
		t.Fatal(err)
	}
	sr := []byte{
		0x80, 0xc8, 0x00, 0x06,
		0x11, 0x22, 0x33, 0x44,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	wire, err := enc.EncryptRTCP(nil, sr, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := dec.DecryptRTCP(nil, wire, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, sr) {
		t.Fatalf("srtcp decrypt mismatch")
	}
}
