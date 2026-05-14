package media

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrNoAudioSDESCrypto means no usable a=crypto inline line was found in the first m=audio section.
var ErrNoAudioSDESCrypto = errors.New("no a=crypto inline in m=audio")

// SDPHintsSRTP returns true if the SDP body suggests SRTP (SAVP profile, crypto, or DTLS fingerprint).
func SDPHintsSRTP(sdpBody string) bool {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	low := strings.ToLower(body)
	if strings.Contains(low, "rtp/savp") || strings.Contains(low, "rtp/savpf") {
		return true
	}
	for _, line := range strings.Split(low, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "a=crypto:") || strings.HasPrefix(line, "a=fingerprint:") {
			return true
		}
	}
	return false
}

// AudioSectionHasFingerprint returns true if any a=fingerprint line appears while inside an m=audio block.
func AudioSectionHasFingerprint(sdpBody string) bool {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	inAudio := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "m=audio") {
			inAudio = true
			continue
		}
		if strings.HasPrefix(low, "m=") {
			inAudio = false
			continue
		}
		if inAudio && strings.HasPrefix(low, "a=fingerprint:") {
			return true
		}
	}
	return false
}

// ParseAudioSDESCrypto extracts the first SDES crypto suite and master key/salt from the first m=audio section.
// Supported suites: AES_CM_128_HMAC_SHA1_80, AES_CM_128_HMAC_SHA1_32 (RFC 4568 inline base64 = key||salt).
func ParseAudioSDESCrypto(sdpBody string) (suite string, masterKey, masterSalt []byte, err error) {
	body := strings.ReplaceAll(sdpBody, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	inAudio := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "m=audio") {
			inAudio = true
			continue
		}
		if strings.HasPrefix(low, "m=") {
			if inAudio {
				break
			}
			continue
		}
		if !inAudio {
			continue
		}
		if !strings.HasPrefix(low, "a=crypto:") {
			continue
		}
		rest := strings.TrimSpace(line[len("a=crypto:"):])
		fields := strings.Fields(rest)
		if len(fields) < 3 {
			return "", nil, nil, fmt.Errorf("a=crypto: expected tag suite inline:…, got %q", rest)
		}
		suite = strings.TrimSpace(fields[1])
		inlineField := ""
		for _, f := range fields[2:] {
			if strings.HasPrefix(strings.ToLower(f), "inline:") {
				inlineField = f
				break
			}
		}
		if inlineField == "" {
			return "", nil, nil, fmt.Errorf("a=crypto: missing inline: parameter in %q", rest)
		}
		inlineVal := strings.TrimPrefix(inlineField, "inline:")
		inlineVal = strings.TrimPrefix(inlineVal, "INLINE:")
		pipe := strings.IndexByte(inlineVal, '|')
		if pipe >= 0 {
			inlineVal = inlineVal[:pipe]
		}
		raw, decErr := base64.StdEncoding.DecodeString(inlineVal)
		if decErr != nil {
			return "", nil, nil, fmt.Errorf("a=crypto inline base64: %w", decErr)
		}
		keyLen, saltLen, kerr := sdesSuiteKeySaltLen(suite)
		if kerr != nil {
			return "", nil, nil, kerr
		}
		if len(raw) < keyLen+saltLen {
			return "", nil, nil, fmt.Errorf("inline key material too short for %s: got %d need %d", suite, len(raw), keyLen+saltLen)
		}
		masterKey = append([]byte(nil), raw[:keyLen]...)
		masterSalt = append([]byte(nil), raw[keyLen:keyLen+saltLen]...)
		return suite, masterKey, masterSalt, nil
	}
	return "", nil, nil, ErrNoAudioSDESCrypto
}

func sdesSuiteKeySaltLen(suite string) (keyLen, saltLen int, err error) {
	switch strings.ToUpper(strings.TrimSpace(suite)) {
	case "AES_CM_128_HMAC_SHA1_80", "AES_CM_128_HMAC_SHA1_32":
		return 16, 14, nil
	default:
		return 0, 0, fmt.Errorf("unsupported SRTP crypto suite %q (supported: AES_CM_128_HMAC_SHA1_80, AES_CM_128_HMAC_SHA1_32)", suite)
	}
}
