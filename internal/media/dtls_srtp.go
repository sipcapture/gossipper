package media

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/srtp/v3"
	"github.com/pion/stun/v3"
)

const udpMuxChanDepth = 128

// dtlsUDPMux demultiplexes inbound UDP on the RTP port: DTLS record-layer (RFC 7983)
// vs RTP/RTCP (first byte 128–191). Outbound writes go straight to the underlying socket.
// STUN (RFC 5389) is optionally handled for ICE (WebRTC) before dropping unknown packets.
type dtlsUDPMux struct {
	ctx         context.Context
	udp         net.PacketConn
	remote      *net.UDPAddr
	dtlsIn      chan []byte
	rtpIn       chan []byte
	stunHandler func(net.PacketConn, []byte, *net.UDPAddr) bool
}

func newDtlsUDPMux(ctx context.Context, udp net.PacketConn, remote *net.UDPAddr, stunHandler func(net.PacketConn, []byte, *net.UDPAddr) bool) *dtlsUDPMux {
	return &dtlsUDPMux{
		ctx:         ctx,
		udp:         udp,
		remote:      remote,
		dtlsIn:      make(chan []byte, udpMuxChanDepth),
		rtpIn:       make(chan []byte, udpMuxChanDepth),
		stunHandler: stunHandler,
	}
}

func isLikelyDTLSRecordUDP(b []byte) bool {
	if len(b) < 1 {
		return false
	}
	// TLS/DTLS content types 20–63; RTP/RTCP use 128–191 (V=2).
	return b[0] >= 20 && b[0] <= 63
}

func (m *dtlsUDPMux) loop() {
	buf := make([]byte, 2048)
	for {
		if m.ctx.Err() != nil {
			return
		}
		_ = m.udp.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, addr, err := m.udp.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		if n <= 0 {
			continue
		}
		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok || !udpAddrsMatch(udpAddr, m.remote) {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		if stun.IsMessage(pkt) {
			if m.stunHandler != nil && m.stunHandler(m.udp, pkt, udpAddr) {
				continue
			}
			continue
		}
		if isLikelyDTLSRecordUDP(pkt) {
			select {
			case <-m.ctx.Done():
				return
			case m.dtlsIn <- pkt:
			}
		} else {
			select {
			case <-m.ctx.Done():
				return
			case m.rtpIn <- pkt:
			}
		}
	}
}

func udpAddrsMatch(a *net.UDPAddr, want *net.UDPAddr) bool {
	if a == nil || want == nil {
		return false
	}
	return a.Port == want.Port && a.IP.String() == want.IP.String()
}

// udpChanReader implements net.PacketConn for one side of the mux (DTLS or RTP).
type udpChanReader struct {
	ctx    context.Context
	ch     <-chan []byte
	remote net.Addr
	udp    net.PacketConn
}

func (r *udpChanReader) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case <-r.ctx.Done():
		return 0, nil, r.ctx.Err()
	case pkt, ok := <-r.ch:
		if !ok {
			return 0, nil, io.EOF
		}
		if len(pkt) > len(p) {
			return 0, nil, io.ErrShortBuffer
		}
		copy(p, pkt)
		return len(pkt), r.remote, nil
	}
}

func (r *udpChanReader) WriteTo(p []byte, addr net.Addr) (int, error) {
	return r.udp.WriteTo(p, addr)
}

func (r *udpChanReader) Close() error                       { return nil }
func (r *udpChanReader) LocalAddr() net.Addr                { return r.udp.LocalAddr() }
func (r *udpChanReader) SetDeadline(t time.Time) error      { return r.udp.SetDeadline(t) }
func (r *udpChanReader) SetReadDeadline(t time.Time) error  { return r.udp.SetReadDeadline(t) }
func (r *udpChanReader) SetWriteDeadline(t time.Time) error { return r.udp.SetWriteDeadline(t) }

func srtpProfileFromDTLS(p dtls.SRTPProtectionProfile) (srtp.ProtectionProfile, error) {
	switch p {
	case dtls.SRTP_AES128_CM_HMAC_SHA1_80:
		return srtp.ProtectionProfileAes128CmHmacSha1_80, nil
	case dtls.SRTP_AES128_CM_HMAC_SHA1_32:
		return srtp.ProtectionProfileAes128CmHmacSha1_32, nil
	default:
		return 0, fmt.Errorf("unsupported negotiated DTLS-SRTP profile %d", uint16(p))
	}
}

func peerCertFingerprintDER(algo string, der []byte) []byte {
	switch strings.ToLower(strings.TrimSpace(algo)) {
	case "sha-256":
		b := sha256.Sum256(der)
		return b[:]
	case "sha-384":
		b := sha512.Sum384(der)
		return b[:]
	default:
		return nil
	}
}

func compareFingerprint(a, b []byte) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// runDTLSSRTP performs DTLS-SRTP on udp toward remote, verifies the peer leaf certificate
// fingerprint (sha-256 or sha-384). If asServer is true (remote SDP a=setup:active), runs
// dtls.Server; otherwise dtls.Client. Fills s.srtpMaterial (asymmetric keys).
// It starts the mux loop and stores s.dtlsConn for Session.Stop.
func (s *Session) runDTLSSRTP(ctx context.Context, udp net.PacketConn, remote *net.UDPAddr, fpAlgo string, expectedFP []byte, asServer bool) (rtpReader net.PacketConn, err error) {
	fpAlgo = strings.ToLower(strings.TrimSpace(fpAlgo))
	switch fpAlgo {
	case "sha-256":
		if len(expectedFP) != 32 {
			return nil, fmt.Errorf("dtls: want sha-256 fingerprint (32 bytes), got %d", len(expectedFP))
		}
	case "sha-384":
		if len(expectedFP) != 48 {
			return nil, fmt.Errorf("dtls: want sha-384 fingerprint (48 bytes), got %d", len(expectedFP))
		}
	default:
		return nil, fmt.Errorf("dtls: unsupported fingerprint algorithm %q", fpAlgo)
	}
	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return nil, fmt.Errorf("dtls self-signed cert: %w", err)
	}

	stunHandler := func(c net.PacketConn, pkt []byte, from *net.UDPAddr) bool {
		return s.tryICEStunBindingResponse(c, pkt, from)
	}
	mux := newDtlsUDPMux(ctx, udp, remote, stunHandler)
	go mux.loop()

	if s.hasRemoteICE() {
		go s.iceConnectivityPingLoop(ctx, udp, remote)
	}

	dtlsSide := &udpChanReader{ctx: ctx, ch: mux.dtlsIn, remote: remote, udp: udp}
	rtpSide := &udpChanReader{ctx: ctx, ch: mux.rtpIn, remote: remote, udp: udp}

	verify := func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("dtls: no peer certificate")
		}
		fp := peerCertFingerprintDER(fpAlgo, rawCerts[0])
		if len(fp) == 0 || !compareFingerprint(fp, expectedFP) {
			return fmt.Errorf("dtls: peer certificate fingerprint mismatch")
		}
		return nil
	}

	dtlsCfg := &dtls.Config{
		// WebRTC DTLS 1.2 with default pion cipher suites (ECDHE, RSA/ECDSA as built).
		// Extended Master Secret is required; SRTP profiles match pion/srtp AES-CM+HMAC-SHA1.
		Certificates:         []tls.Certificate{cert},
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
		SRTPProtectionProfiles: []dtls.SRTPProtectionProfile{
			dtls.SRTP_AES128_CM_HMAC_SHA1_80,
			dtls.SRTP_AES128_CM_HMAC_SHA1_32,
		},
		InsecureSkipVerify:    true,
		VerifyPeerCertificate: verify,
	}
	var dtlsConn *dtls.Conn
	if asServer {
		dtlsCfg.ClientAuth = dtls.RequireAnyClientCert
		dtlsConn, err = dtls.Server(dtlsSide, remote, dtlsCfg)
		if err != nil {
			return nil, fmt.Errorf("dtls server: %w", err)
		}
	} else {
		dtlsConn, err = dtls.Client(dtlsSide, remote, dtlsCfg)
		if err != nil {
			return nil, fmt.Errorf("dtls client: %w", err)
		}
	}
	if err := dtlsConn.HandshakeContext(ctx); err != nil {
		_ = dtlsConn.Close()
		role := "client"
		if asServer {
			role = "server"
		}
		return nil, fmt.Errorf("dtls handshake (%s): %w", role, err)
	}

	dtlsProf, ok := dtlsConn.SelectedSRTPProtectionProfile()
	if !ok {
		_ = dtlsConn.Close()
		return nil, fmt.Errorf("dtls: no negotiated SRTP protection profile")
	}
	srtpProf, err := srtpProfileFromDTLS(dtlsProf)
	if err != nil {
		_ = dtlsConn.Close()
		return nil, err
	}

	st, ok := dtlsConn.ConnectionState()
	if !ok {
		_ = dtlsConn.Close()
		return nil, fmt.Errorf("dtls: connection state unavailable")
	}
	isClient := !asServer
	cfg := &srtp.Config{Profile: srtpProf, Keys: srtp.SessionKeys{}}
	if err := cfg.ExtractSessionKeysFromDTLS(&st, isClient); err != nil {
		_ = dtlsConn.Close()
		return nil, fmt.Errorf("srtp key export: %w", err)
	}

	s.mu.Lock()
	s.dtlsConn = dtlsConn
	s.srtpMaterial = &srtpMaterial{
		profile:    srtpProf,
		localKey:   append([]byte(nil), cfg.Keys.LocalMasterKey...),
		localSalt:  append([]byte(nil), cfg.Keys.LocalMasterSalt...),
		remoteKey:  append([]byte(nil), cfg.Keys.RemoteMasterKey...),
		remoteSalt: append([]byte(nil), cfg.Keys.RemoteMasterSalt...),
	}
	s.dtlsPeerCertAlgo = ""
	s.dtlsPeerFP = nil
	s.dtlsPeerSetup = ""
	s.mu.Unlock()

	return rtpSide, nil
}

// SetDTLSFingerprintFromSDP stores the first supported a=fingerprint (sha-256 or sha-384) from m=audio
// for a later DTLS-SRTP handshake (client or server per a=setup in m=audio). Clears SDES material.
func (s *Session) SetDTLSFingerprintFromSDP(sdpBody string) error {
	algo, digest, err := ParseAudioFingerprint(sdpBody)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.srtpMaterial = nil
	s.srtpSend = nil
	s.srtpRecv = nil
	s.dtlsPeerCertAlgo = algo
	s.dtlsPeerFP = append([]byte(nil), digest...)
	s.dtlsPeerSetup = ParseAudioDTLSSetup(sdpBody)
	return nil
}

// SetDTLSFingerprintSHA256FromSDP stores a SHA-256 a=fingerprint from m=audio for a later
// DTLS-SRTP handshake (client or server per a=setup in m=audio). Clears SDES material.
func (s *Session) SetDTLSFingerprintSHA256FromSDP(sdpBody string) error {
	fp, err := ParseAudioFingerprintSHA256(sdpBody)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.srtpMaterial = nil
	s.srtpSend = nil
	s.srtpRecv = nil
	s.dtlsPeerCertAlgo = "sha-256"
	s.dtlsPeerFP = append([]byte(nil), fp...)
	s.dtlsPeerSetup = ParseAudioDTLSSetup(sdpBody)
	return nil
}

func (s *Session) takeDTLSConfigForHandshake() (algo string, fp []byte, asServer bool, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.dtlsPeerFP) == 0 {
		return "", nil, false, false
	}
	asServer = s.dtlsPeerSetup == "active"
	return s.dtlsPeerCertAlgo, append([]byte(nil), s.dtlsPeerFP...), asServer, true
}

// prepareRTPReadConn returns the PacketConn for inbound RTP: the UDP socket, or a muxed
// reader when a DTLS fingerprint was configured (DTLS-SRTP).
func (s *Session) prepareRTPReadConn(ctx context.Context, udp net.PacketConn, remote *net.UDPAddr) (net.PacketConn, error) {
	algo, fp, asServer, ok := s.takeDTLSConfigForHandshake()
	if !ok {
		return udp, nil
	}
	return s.runDTLSSRTP(ctx, udp, remote, algo, fp, asServer)
}
