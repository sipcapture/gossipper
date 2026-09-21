package sip

import (
	"strings"
	"testing"
)

func TestDigestHexRFCAlgorithms(t *testing.T) {
	t.Parallel()
	// echo -n 'alice:atlanta.com:secret' | md5sum / sha256sum
	if got, want := DigestHex("MD5", "alice:atlanta.com:secret"), "4c00389f118405a62dc666c595d7e0d8"; got != want {
		t.Fatalf("MD5 got %s want %s", got, want)
	}
	if got := DigestHex("", "alice:atlanta.com:secret"); got != DigestHex("MD5", "alice:atlanta.com:secret") {
		t.Fatalf("empty algorithm %s", got)
	}
	if got, want := DigestHex("SHA-256", "alice:atlanta.com:secret"), "a389bbc55b6a1287514f11b798e088a13d5d248f861791e6af89a51164feac0d"; got != want {
		t.Fatalf("SHA-256 got %s want %s", got, want)
	}
	if DigestHex("SHA-512", "alice:atlanta.com:secret") != "" {
		t.Fatal("unsupported algorithm must be empty")
	}
}

func TestBuildDigestAuthHeaderMD5(t *testing.T) {
	t.Parallel()
	challenge := `Digest realm="atlanta.com", nonce="9c166d70", algorithm=MD5, qop="auth"`
	got, err := BuildDigestAuthHeader("Authorization", challenge, "REGISTER", "sip:atlanta.com", "", "alice", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, `Authorization: Digest username="alice"`) {
		t.Fatalf("prefix %q", got)
	}
	if !strings.Contains(got, `response="`) || !strings.Contains(got, "algorithm=MD5") {
		t.Fatalf("digest fields %q", got)
	}
}
