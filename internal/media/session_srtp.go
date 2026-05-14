package media

import (
	"fmt"
	"strings"

	"github.com/pion/srtp/v3"
)

type srtpMaterial struct {
	profile srtp.ProtectionProfile
	// SDES symmetric: masterKey set, localKey empty.
	masterKey  []byte
	masterSalt []byte
	// DTLS-SRTP asymmetric: local* = keys to protect outbound, remote* = keys to unprotect inbound.
	localKey   []byte
	localSalt  []byte
	remoteKey  []byte
	remoteSalt []byte
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
	s.dtlsPeerCertAlgo = ""
	s.dtlsPeerFP = nil
	s.dtlsPeerSetup = ""
	s.rtcpMux = false
	s.iceRemoteUfrag = ""
	s.iceRemotePwd = ""
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
	s.dtlsPeerCertAlgo = ""
	s.dtlsPeerFP = nil
	s.dtlsPeerSetup = ""
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
	m := s.srtpMaterial
	var enc, dec *srtp.Context
	var err error
	if len(m.localKey) > 0 {
		enc, err = srtp.CreateContext(m.localKey, m.localSalt, m.profile)
		if err != nil {
			return fmt.Errorf("srtp encrypt context: %w", err)
		}
		dec, err = srtp.CreateContext(m.remoteKey, m.remoteSalt, m.profile)
		if err != nil {
			return fmt.Errorf("srtp decrypt context: %w", err)
		}
	} else {
		enc, err = srtp.CreateContext(m.masterKey, m.masterSalt, m.profile)
		if err != nil {
			return fmt.Errorf("srtp encrypt context: %w", err)
		}
		dec, err = srtp.CreateContext(m.masterKey, m.masterSalt, m.profile)
		if err != nil {
			return fmt.Errorf("srtp decrypt context: %w", err)
		}
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
	m.localKey = append([]byte(nil), s.srtpMaterial.localKey...)
	m.localSalt = append([]byte(nil), s.srtpMaterial.localSalt...)
	m.remoteKey = append([]byte(nil), s.srtpMaterial.remoteKey...)
	m.remoteSalt = append([]byte(nil), s.srtpMaterial.remoteSalt...)
	return &m
}
