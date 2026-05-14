package media

import (
	"fmt"
	"strings"

	"github.com/pion/srtp/v3"
)

type srtpMaterial struct {
	masterKey  []byte
	masterSalt []byte
	profile    srtp.ProtectionProfile
}

func protectionProfileFromSDESSuite(suite string) (srtp.ProtectionProfile, error) {
	switch strings.ToUpper(strings.TrimSpace(suite)) {
	case "AES_CM_128_HMAC_SHA1_80":
		return srtp.ProtectionProfileAes128CmHmacSha1_80, nil
	case "AES_CM_128_HMAC_SHA1_32":
		return srtp.ProtectionProfileAes128CmHmacSha1_32, nil
	default:
		return 0, fmt.Errorf("unsupported SRTP suite %q", suite)
	}
}

// ClearSDESSRTP removes negotiated SDES key material and active SRTP contexts.
func (s *Session) ClearSDESSRTP() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.srtpMaterial = nil
	s.srtpSend = nil
	s.srtpRecv = nil
}

// SetSDESSRTPFromSDP parses the first m=audio SDES a=crypto inline key and stores it for the next Start / mic session.
func (s *Session) SetSDESSRTPFromSDP(sdp string) error {
	suite, key, salt, err := ParseAudioSDESCrypto(sdp)
	if err != nil {
		return err
	}
	prof, err := protectionProfileFromSDESSuite(suite)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.srtpMaterial = &srtpMaterial{
		masterKey:  append([]byte(nil), key...),
		masterSalt: append([]byte(nil), salt...),
		profile:    prof,
	}
	return nil
}

// buildSRTPContextsLocked creates outbound/inbound SRTP contexts from srtpMaterial.
// Must be called with s.mu held. On error leaves srtpSend/srtpRecv nil.
func (s *Session) buildSRTPContextsLocked() error {
	s.srtpSend = nil
	s.srtpRecv = nil
	if s.srtpMaterial == nil {
		return nil
	}
	enc, err := srtp.CreateContext(s.srtpMaterial.masterKey, s.srtpMaterial.masterSalt, s.srtpMaterial.profile)
	if err != nil {
		return fmt.Errorf("srtp encrypt context: %w", err)
	}
	dec, err := srtp.CreateContext(s.srtpMaterial.masterKey, s.srtpMaterial.masterSalt, s.srtpMaterial.profile)
	if err != nil {
		return fmt.Errorf("srtp decrypt context: %w", err)
	}
	s.srtpSend = enc
	s.srtpRecv = dec
	return nil
}

func (s *Session) snapshotSRTPMaterial() *srtpMaterial {
	if s.srtpMaterial == nil {
		return nil
	}
	m := *s.srtpMaterial
	m.masterKey = append([]byte(nil), s.srtpMaterial.masterKey...)
	m.masterSalt = append([]byte(nil), s.srtpMaterial.masterSalt...)
	return &m
}
