package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/srtp/v3"
	"github.com/pion/turn/v4"

	"github.com/pion/dtls/v3"
)

type Endpoint struct {
	IP   string
	Port int
	// ICECandidateTyp is set when the RTP endpoint was taken from an ICE candidate (e.g. host, srflx, relay).
	ICECandidateTyp string
}

type StreamConfig struct {
	PayloadType    uint8
	PayloadName    string
	SSRC           uint32
	Sequence       uint16
	Timestamp      uint32
	ClockRate      uint32
	SamplesPerPkt  uint32
	PacketDuration time.Duration
	LoopCount      int
	Path           string
	// Synthetic controls whether RTP payloads are generated without a media file.
	// When true, silence frames are produced based on PayloadType and Channels.
	Synthetic bool
	// Duration is the total streaming time. Zero means unlimited (stop only via
	// context cancellation or LoopCount exhaustion).
	Duration time.Duration
	// Channels is the number of audio channels (1 = mono, 2 = stereo).
	// Zero is treated as 1.
	Channels uint8
	// MicInput is used only for rtp_stream mic: ALSA device (-D), ffmpeg:… args, or non-Linux ffmpeg -i value.
	MicInput string
}

type Session struct {
	mu            sync.Mutex
	pauseCond     *sync.Cond
	cancel        context.CancelFunc
	conn          *net.UDPConn
	rtcpConn      *net.UDPConn
	paused        bool
	running       bool
	echoMode      bool
	packetCount   uint32
	octetCount    uint32
	lastTimestamp uint32
	ssrc          uint32
	stats         Stats
	hepObserver   HEPObserver
	localIP       string
	localPort     int
	callID        string
	pcapLinkLayer string

	// WAV recording (optional). Guarded by mu.
	recordOn         bool
	recordDuplex     bool
	recordRecv       []int16
	recordSent       []int16
	recordPath       string
	autoRecordDir    string
	autoRecordDuplex bool

	// WAV recording reorder (jitter) buffer: small window by RTP sequence.
	recordSeqBuf   []recordSeqSample
	recordNextSeq  uint16
	recordHaveNext bool

	// Incoming RTP stats for RTCP Receiver Reports (RFC 3550 §6.4.2).
	// All fields are guarded by mu.
	rrSSRC        uint32
	rrHasFirst    bool
	rrFirstSeq    uint16
	rrExtHighSeq  uint32  // extended highest sequence number received
	rrReceived    uint32  // total packets received from remote
	rrLost        int32   // cumulative packets lost (expected − received)
	rrJitter      float64 // interarrival jitter, in RTP timestamp units
	rrClockRate   uint32  // clock rate auto-detected from the RTP stream
	rrTSPrev      uint32  // RTP timestamp of previous received packet
	rrArrivalPrev time.Time
	rrLastSRNTP   uint32    // compact NTP from the last received SR (for DLSR)
	rrLastSRAt    time.Time // wall time when the last SR was received

	// SRTP (SDES): optional negotiated key material and active pion contexts.
	srtpMaterial *srtpMaterial
	srtpSend     *srtp.Context
	srtpRecv     *srtp.Context

	// DTLS-SRTP (WebRTC-style): expected peer cert fingerprint (sha-256 or sha-384) before Start; live DTLS conn after handshake.
	dtlsPeerCertAlgo string
	dtlsPeerFP       []byte
	// dtlsPeerSetup is the remote m=audio a=setup value (active|passive|actpass). If "active", peer starts DTLS as client → we act as DTLS server.
	dtlsPeerSetup string
	dtlsConn      *dtls.Conn

	// rtcp-mux: send/receive RTCP on the RTP socket and remote RTP port (from a=rtcp-mux in remote SDP).
	rtcpMux bool

	// ICE (WebRTC): local credentials for our SDP offer; remote from peer answer (DTLS-SRTP path).
	iceLocalUfrag  string
	iceLocalPwd    string
	iceRemoteUfrag string
	iceRemotePwd   string

	// TURN (optional): used when ICE picks typ relay for the remote RTP candidate.
	turnServer string
	turnUser   string
	turnPass   string
	turnRealm  string
	turnClient *turn.Client
	turnRelay  net.PacketConn
}

// HEPObserver is implemented by the HEP client to mirror RTP/RTCP traffic to Homer.
type HEPObserver interface {
	SendRTP(now time.Time, srcIP string, srcPort int, dstIP string, dstPort int, callID string, payload []byte) error
	SendRTCP(now time.Time, callID string, ssrc uint32, srcIP string, srcPort int, dstIP string, dstPort int, packetLoss uint32, payload []byte) error
}

type Stats struct {
	RTPPacketsSent      uint32
	RTPOctetsSent       uint32
	RTPPacketsReceived  uint32
	RTCPSenderReports   uint32
	RTCPReceiverReports uint32
	RTCPPacketsReceived uint32
	// RTCPReportBlocks counts ReceptionReport blocks from inbound RTCP RR (RFC 3550).
	RTCPReportBlocks    uint32
	RTCPMaxFractionLost uint8
	RTCPMaxJitter       uint32
	// RTCPMinJitter is the minimum non-zero jitter from inbound RR blocks (RFC 3550), in timestamp units.
	RTCPMinJitter     uint32
	RTCPJitterSum     float64
	RTCPJitterSamples uint64
	// RTPRecvMaxCumulativeLost is the peak RFC 3550-style cumulative loss estimate for inbound RTP (local recv path).
	RTPRecvMaxCumulativeLost uint32
	// RTPRecvInterarrivalJitterPeak is the peak interarrival jitter estimator (timestamp units) on inbound RTP.
	RTPRecvInterarrivalJitterPeak uint32
}

type RTPCheckDirection string

const (
	RTPCheckAny  RTPCheckDirection = "any"
	RTPCheckSend RTPCheckDirection = "send"
	RTPCheckRecv RTPCheckDirection = "recv"
	RTPCheckBoth RTPCheckDirection = "both"
)

func NewSession() *Session {
	s := &Session{}
	s.pauseCond = sync.NewCond(&s.mu)
	return s
}

func (s *Session) SetHEPObserver(obs HEPObserver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hepObserver = obs
}

func (s *Session) SetCallID(callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callID = callID
}

// SetRtcpMuxFromSDP sets whether RTCP is multiplexed on the RTP port (a=rtcp-mux in m=audio).
func (s *Session) SetRtcpMuxFromSDP(sdpBody string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rtcpMux = AudioSectionHasRtcpMux(sdpBody)
}

// SetPCAPLinkLayer sets the datalink override for PCAP replay (play_pcap_*).
// Empty string means auto: use the file's DLT from the global header (including LINUX_SLL2).
func (s *Session) SetPCAPLinkLayer(spec string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pcapLinkLayer = strings.TrimSpace(spec)
}

// SetTURN configures a TURN/STUN server (host:port) and long-term credentials for ICE relay paths.
func (s *Session) SetTURN(server, user, pass, realm string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnServer = strings.TrimSpace(server)
	s.turnUser = user
	s.turnPass = pass
	s.turnRealm = realm
}

func BuildPacket(cfg StreamConfig, payload []byte) ([]byte, error) {
	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    cfg.PayloadType,
			SequenceNumber: cfg.Sequence,
			Timestamp:      cfg.Timestamp,
			SSRC:           cfg.SSRC,
		},
		Payload: payload,
	}
	return packet.Marshal()
}

func BuildSilentPCMU(cfg StreamConfig, payloadBytes int) ([]byte, error) {
	if payloadBytes <= 0 {
		return nil, fmt.Errorf("payloadBytes must be positive")
	}
	payload := make([]byte, payloadBytes)
	for i := range payload {
		payload[i] = 0xff
	}
	return BuildPacket(cfg, payload)
}

// buildSyntheticPayload creates a single silence RTP payload frame according to
// cfg.PayloadType, cfg.PayloadName, cfg.SamplesPerPkt and cfg.Channels.
//
// For PCM-based codecs (PCMU, PCMA, G722) the payload is SamplesPerPkt×Channels
// bytes of codec-appropriate silence.  For Opus, a structurally valid minimal
// DTX frame is returned instead because Opus frames are not raw PCM.
func buildSyntheticPayload(cfg StreamConfig) []byte {
	// Opus requires a structurally valid packet rather than a fixed-size PCM
	// silence buffer.  The three bytes below encode a CELT Full-Band 20 ms
	// mono single-frame TOC (0xF8) followed by a minimal payload that most
	// Opus decoders accept as comfort noise / DTX.
	if strings.HasPrefix(strings.ToUpper(cfg.PayloadName), "OPUS") {
		return []byte{0xF8, 0xFF, 0xFE}
	}

	channels := int(cfg.Channels)
	if channels < 1 {
		channels = 1
	}
	size := int(cfg.SamplesPerPkt) * channels
	if size <= 0 {
		size = 160
	}
	payload := make([]byte, size)
	switch cfg.PayloadType {
	case 0: // PCMU — μ-law silence
		for i := range payload {
			payload[i] = 0xFF
		}
	case 8: // PCMA — A-law silence
		for i := range payload {
			payload[i] = 0xD5
		}
		// All other codecs: zero bytes are a reasonable silence representation.
	}
	return payload
}

func DefaultConfig(path string) StreamConfig {
	return StreamConfig{
		PayloadType:    0,
		PayloadName:    "PCMU/8000",
		SSRC:           rand.Uint32(),
		Sequence:       uint16(rand.Intn(65535)),
		Timestamp:      rand.Uint32(),
		ClockRate:      8000,
		SamplesPerPkt:  160,
		PacketDuration: 20 * time.Millisecond,
		LoopCount:      1,
		Path:           path,
	}
}

// ParseRTPStreamSpec parses the value of an rtp_stream action.
//
// Control commands (no config):
//
//	pause | resume | stop | echo
//
// File-based streaming (existing behaviour):
//
//	<path>[,<loopCount>[,<pt>[,<codecName>]]]
//
// Synthetic streaming (no file required):
//
//	synthetic[,<loopCount>[,<pt>[,<codecName>[,<freqMs>[,<durationMs>[,<channels>]]]]]]
//
// All numeric parts are optional and default to sensible values.
func ParseRTPStreamSpec(spec string, basePath string) (string, StreamConfig, error) {
	spec = strings.TrimSpace(spec)
	switch strings.ToLower(spec) {
	case "pause", "resume", "stop", "echo":
		return strings.ToLower(spec), StreamConfig{}, nil
	}

	parts := strings.Split(spec, ",")
	first := strings.TrimSpace(parts[0])
	if first == "" {
		return "", StreamConfig{}, errors.New("rtp_stream requires a path or 'synthetic'")
	}
	switch strings.ToLower(first) {
	case "mic", "microphone":
		cfg := DefaultConfig("")
		if len(parts) > 1 {
			cfg.MicInput = strings.TrimSpace(strings.Join(parts[1:], ","))
		}
		return "mic", cfg, nil
	}

	if strings.ToLower(first) == "synthetic" {
		cfg := DefaultConfig("")
		cfg.Synthetic = true
		// part[1]: loopCount — accepted but ignored for synthetic (Duration is the stop
		// condition); kept for spec symmetry with file-based streaming.
		if len(parts) > 2 {
			var pt int
			fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &pt)
			cfg.PayloadType = uint8(pt)
		}
		if len(parts) > 3 {
			ApplyPayloadParams(&cfg, strings.TrimSpace(parts[3]))
		} else {
			ApplyPayloadParams(&cfg, cfg.PayloadName)
		}
		if len(parts) > 4 {
			var freqMs int
			fmt.Sscanf(strings.TrimSpace(parts[4]), "%d", &freqMs)
			if freqMs > 0 {
				cfg.PacketDuration = time.Duration(freqMs) * time.Millisecond
			}
		}
		if len(parts) > 5 {
			var durMs int
			fmt.Sscanf(strings.TrimSpace(parts[5]), "%d", &durMs)
			if durMs > 0 {
				cfg.Duration = time.Duration(durMs) * time.Millisecond
			}
		}
		if len(parts) > 6 {
			var ch int
			fmt.Sscanf(strings.TrimSpace(parts[6]), "%d", &ch)
			if ch > 0 {
				cfg.Channels = uint8(ch)
			}
		}
		return "start", cfg, nil
	}

	cfg := DefaultConfig(ResolvePath(basePath, first))
	if len(parts) > 1 {
		fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &cfg.LoopCount)
	}
	if len(parts) > 2 {
		var pt int
		fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &pt)
		cfg.PayloadType = uint8(pt)
	}
	if len(parts) > 3 {
		payloadName := strings.TrimSpace(parts[3])
		ApplyPayloadParams(&cfg, payloadName)
	} else {
		ApplyPayloadParams(&cfg, cfg.PayloadName)
	}
	return "start", cfg, nil
}

// openRTPDatapath binds a local UDP socket and, for ICE typ relay, allocates a TURN relay transport.
func (s *Session) openRTPDatapath(endpoint Endpoint, localIP string, localPort int) (conn *net.UDPConn, dataPC net.PacketConn, remote *net.UDPAddr, err error) {
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return nil, nil, nil, fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
	}
	remote, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", endpoint.IP, endpoint.Port))
	if err != nil {
		return nil, nil, nil, err
	}
	localAddr := &net.UDPAddr{Port: localPort}
	if localIP != "" && localIP != "0.0.0.0" && localIP != "::" {
		localAddr.IP = net.ParseIP(localIP)
	}
	conn, err = net.ListenUDP("udp", localAddr)
	if err != nil {
		conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: localAddr.IP, Port: 0})
	}
	if err != nil {
		return nil, nil, nil, err
	}
	dataPC = conn
	if !strings.EqualFold(endpoint.ICECandidateTyp, "relay") {
		return conn, dataPC, remote, nil
	}
	s.mu.Lock()
	svr := s.turnServer
	user := s.turnUser
	pass := s.turnPass
	realm := s.turnRealm
	s.mu.Unlock()
	if svr == "" {
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("ICE relay candidate: set -turn_server -turn_user -turn_pass [-turn_realm]")
	}
	tc, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: svr,
		TURNServerAddr: svr,
		Username:       user,
		Password:       pass,
		Realm:          realm,
		Conn:           conn,
	})
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("TURN client: %w", err)
	}
	if err := tc.Listen(); err != nil {
		tc.Close()
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("TURN listen: %w", err)
	}
	relayPC, err := tc.Allocate()
	if err != nil {
		tc.Close()
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("TURN allocate: %w", err)
	}
	s.mu.Lock()
	s.turnClient = tc
	s.turnRelay = relayPC
	s.mu.Unlock()
	return conn, relayPC, remote, nil
}

func (s *Session) closeTurnResources() {
	s.mu.Lock()
	relay := s.turnRelay
	tc := s.turnClient
	s.turnRelay = nil
	s.turnClient = nil
	s.mu.Unlock()
	if relay != nil {
		_ = relay.Close()
	}
	if tc != nil {
		tc.Close()
	}
}

func (s *Session) Start(ctx context.Context, endpoint Endpoint, cfg StreamConfig, localIP string, localPort int) error {
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
	}

	var packets [][]byte
	if !cfg.Synthetic {
		var err error
		packets, err = loadPackets(cfg)
		if err != nil {
			return err
		}
	}

	s.Stop()

	conn, muxPC, remoteAddr, err := s.openRTPDatapath(endpoint, localIP, localPort)
	if err != nil {
		return err
	}

	childCtx, cancel := context.WithCancel(ctx)
	rtpReadConn, err := s.prepareRTPReadConn(childCtx, muxPC, remoteAddr)
	if err != nil {
		cancel()
		s.closeTurnResources()
		_ = conn.Close()
		return fmt.Errorf("media DTLS-SRTP: %w", err)
	}
	s.mu.Lock()
	s.cancel = cancel
	s.conn = conn
	s.ssrc = cfg.SSRC
	s.packetCount = 0
	s.octetCount = 0
	s.lastTimestamp = cfg.Timestamp
	s.paused = false
	s.running = true
	s.echoMode = false
	s.stats = Stats{}
	s.rrReset()
	s.localIP = localIP
	rtpBoundPort := conn.LocalAddr().(*net.UDPAddr).Port
	s.localPort = rtpBoundPort
	rtcpMux := s.rtcpMux
	var rtcpConn *net.UDPConn
	if !rtcpMux {
		boundLA := conn.LocalAddr().(*net.UDPAddr)
		rtcpLocalAddr := &net.UDPAddr{Port: rtpBoundPort + 1}
		if boundLA.IP != nil {
			rtcpLocalAddr.IP = boundLA.IP
		}
		rtcpConn, _ = net.ListenUDP("udp", rtcpLocalAddr)
	}
	s.rtcpConn = rtcpConn
	if err := s.buildSRTPContextsLocked(); err != nil {
		if s.dtlsConn != nil {
			_ = s.dtlsConn.Close()
			s.dtlsConn = nil
		}
		s.conn = nil
		s.rtcpConn = nil
		s.cancel = nil
		s.running = false
		s.mu.Unlock()
		cancel()
		s.closeTurnResources()
		_ = conn.Close()
		if rtcpConn != nil {
			_ = rtcpConn.Close()
		}
		return fmt.Errorf("media SRTP: %w", err)
	}
	obs := s.hepObserver
	callID := s.callID
	s.mu.Unlock()

	go s.streamLoop(childCtx, muxPC, remoteAddr, cfg, packets, obs, callID)
	go s.rtpReceiveLoop(childCtx, rtpReadConn, rtcpMux)
	if rtcpMux {
		go s.rtcpLoop(childCtx, muxPC, remoteAddr, cfg, obs, callID)
	} else if rtcpConn != nil {
		go s.rtcpLoop(childCtx, rtcpConn, &net.UDPAddr{IP: remoteAddr.IP, Port: remoteAddr.Port + 1}, cfg, obs, callID)
		go s.rtcpReceiveLoop(childCtx, rtcpConn)
	}
	s.maybeStartAutoRecord()
	return nil
}

func (s *Session) StartEcho(ctx context.Context, localIP string, localPort int) error {
	s.Stop()

	localAddr := &net.UDPAddr{Port: localPort}
	if localIP != "" && localIP != "0.0.0.0" && localIP != "::" {
		localAddr.IP = net.ParseIP(localIP)
	}
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: localAddr.IP, Port: 0})
		if err != nil {
			return err
		}
	}

	childCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.conn = conn
	s.paused = false
	s.running = true
	s.echoMode = true
	s.stats = Stats{}
	s.localIP = localIP
	s.localPort = conn.LocalAddr().(*net.UDPAddr).Port
	rtcpLocalAddr := &net.UDPAddr{Port: conn.LocalAddr().(*net.UDPAddr).Port + 1}
	if localAddr.IP != nil {
		rtcpLocalAddr.IP = localAddr.IP
	}
	rtcpConn, _ := net.ListenUDP("udp", rtcpLocalAddr)
	s.rtcpConn = rtcpConn
	s.mu.Unlock()

	go s.echoLoop(childCtx, conn)
	if rtcpConn != nil {
		go s.rtcpReceiveLoop(childCtx, rtcpConn)
	}
	return nil
}

func (s *Session) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.paused = true
	}
}

func (s *Session) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.paused = false
		s.pauseCond.Broadcast()
	}
}

func (s *Session) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	conn := s.conn
	rtcpConn := s.rtcpConn
	relay := s.turnRelay
	tc := s.turnClient
	s.cancel = nil
	s.conn = nil
	s.rtcpConn = nil
	s.turnRelay = nil
	s.turnClient = nil
	if s.dtlsConn != nil {
		_ = s.dtlsConn.Close()
		s.dtlsConn = nil
	}
	s.srtpSend = nil
	s.srtpRecv = nil
	s.paused = false
	s.running = false
	s.echoMode = false
	s.pauseCond.Broadcast()
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if relay != nil {
		_ = relay.Close()
	}
	if tc != nil {
		tc.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if rtcpConn != nil {
		_ = rtcpConn.Close()
	}
	s.flushRecording()
}

func (s *Session) streamLoop(ctx context.Context, w net.PacketConn, remote net.Addr, cfg StreamConfig, packets [][]byte, obs HEPObserver, callID string) {
	defer s.Stop()

	// Limit total stream time when Duration is set.
	if cfg.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Duration)
		defer cancel()
	}

	// Synthetic mode: generate a single silence frame and loop indefinitely
	// (Duration or external context cancellation controls the stop).
	if cfg.Synthetic {
		packets = [][]byte{buildSyntheticPayload(cfg)}
	}

	localAddr := w.LocalAddr()
	localUDP, _ := localAddr.(*net.UDPAddr)
	localIP := "0.0.0.0"
	localPort := 0
	if localUDP != nil {
		localIP = localUDP.IP.String()
		localPort = localUDP.Port
	}

	loops := cfg.LoopCount
	if loops == 0 {
		loops = 1
	}
	// Synthetic streams always loop; Duration (or ctx) is the only stop condition.
	infinite := loops < 0 || cfg.Synthetic

	sequence := cfg.Sequence
	timestamp := cfg.Timestamp

	ticker := time.NewTicker(cfg.PacketDuration)
	defer ticker.Stop()

	for loop := 0; infinite || loop < loops; loop++ {
		for _, payload := range packets {
			// Wait for the next tick before sending — keeps inter-packet
			// delta stable at cfg.PacketDuration (default 20ms).
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			s.waitIfPaused(ctx)

			frame, err := BuildPacket(StreamConfig{
				PayloadType: cfg.PayloadType,
				SSRC:        cfg.SSRC,
				Sequence:    sequence,
				Timestamp:   timestamp,
			}, payload)
			if err != nil {
				return
			}
			wire := frame
			s.mu.Lock()
			enc := s.srtpSend
			s.mu.Unlock()
			if enc != nil {
				wire, err = enc.EncryptRTP(nil, frame, nil)
				if err != nil {
					return
				}
			}
			if _, err := w.WriteTo(wire, remote); err != nil {
				return
			}
			if obs != nil {
				rip := ""
				rport := 0
				if ua, ok := remote.(*net.UDPAddr); ok {
					rip = ua.IP.String()
					rport = ua.Port
				}
				_ = obs.SendRTP(time.Now(), localIP, localPort, rip, rport, callID, frame)
			}
			s.mu.Lock()
			s.packetCount++
			s.octetCount += uint32(len(payload))
			s.lastTimestamp = timestamp
			s.stats.RTPPacketsSent++
			s.stats.RTPOctetsSent += uint32(len(payload))
			s.appendRecordOutbound(cfg.PayloadType, payload)
			s.mu.Unlock()
			sequence++
			timestamp += cfg.SamplesPerPkt
		}
	}
}

func (s *Session) echoLoop(ctx context.Context, conn *net.UDPConn) {
	defer s.Stop()
	buffer := make([]byte, 2048)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		s.waitIfPaused(ctx)
		payload := make([]byte, n)
		copy(payload, buffer[:n])
		if _, err := conn.WriteToUDP(payload, addr); err != nil {
			return
		}
		s.mu.Lock()
		s.stats.RTPPacketsReceived++
		s.stats.RTPPacketsSent++
		s.stats.RTPOctetsSent += uint32(n)
		s.mu.Unlock()
	}
}

func (s *Session) rtcpLoop(ctx context.Context, w net.PacketConn, remote net.Addr, cfg StreamConfig, obs HEPObserver, callID string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	localAddr := w.LocalAddr()
	localUDP, _ := localAddr.(*net.UDPAddr)
	localIP := "0.0.0.0"
	localPort := 0
	if localUDP != nil {
		localIP = localUDP.IP.String()
		localPort = localUDP.Port
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.paused || !s.running {
				s.mu.Unlock()
				continue
			}
			packetCount := s.packetCount
			octetCount := s.octetCount
			rtpTime := s.lastTimestamp
			ssrc := s.ssrc
			rr := s.buildReceiverReport()
			s.mu.Unlock()

			sr := &rtcp.SenderReport{
				SSRC:        ssrc,
				NTPTime:     toNTPTime(time.Now()),
				RTPTime:     rtpTime,
				PacketCount: packetCount,
				OctetCount:  octetCount,
				Reports:     rr,
			}
			raw, err := sr.Marshal()
			if err != nil {
				continue
			}
			wire := raw
			s.mu.Lock()
			enc := s.srtpSend
			s.mu.Unlock()
			if enc != nil {
				var encErr error
				wire, encErr = enc.EncryptRTCP(nil, raw, nil)
				if encErr != nil {
					continue
				}
			}
			if _, err := w.WriteTo(wire, remote); err != nil {
				continue
			}
			if obs != nil {
				rip := ""
				rport := 0
				if ua, ok := remote.(*net.UDPAddr); ok {
					rip = ua.IP.String()
					rport = ua.Port
				}
				_ = obs.SendRTCP(time.Now(), callID, ssrc, localIP, localPort, rip, rport, 0, raw)
			}
			s.mu.Lock()
			s.stats.RTCPSenderReports++
			s.mu.Unlock()
		}
	}
}

func (s *Session) rtcpReceiveLoop(ctx context.Context, conn *net.UDPConn) {
	buffer := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		buf := buffer[:n]
		s.mu.Lock()
		dec := s.srtpRecv
		s.mu.Unlock()
		if dec != nil {
			if plain, derr := dec.DecryptRTCP(nil, buf, nil); derr == nil {
				buf = plain
			}
		}
		s.processInboundRTCPPayload(time.Now(), buf)
	}
}

func (s *Session) waitIfPaused(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.paused {
		if ctx.Err() != nil {
			return
		}
		s.pauseCond.Wait()
	}
}

func loadPackets(cfg StreamConfig) ([][]byte, error) {
	switch strings.ToLower(filepath.Ext(cfg.Path)) {
	case ".wav":
		return loadWAVPackets(cfg.Path, cfg)
	default:
		return loadRawPackets(cfg.Path, int(cfg.SamplesPerPkt))
	}
}

func loadRawPackets(path string, packetSize int) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if packetSize <= 0 {
		packetSize = 160
	}
	var packets [][]byte
	for len(data) > 0 {
		chunkSize := packetSize
		if len(data) < chunkSize {
			chunkSize = len(data)
		}
		chunk := make([]byte, chunkSize)
		copy(chunk, data[:chunkSize])
		packets = append(packets, chunk)
		data = data[chunkSize:]
	}
	return packets, nil
}

func loadWAVPackets(path string, cfg StreamConfig) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("unsupported wav file %q", path)
	}

	var (
		audioFormat   uint16
		numChannels   uint16
		sampleRate    uint32
		bitsPerSample uint16
		pcmData       []byte
	)
	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if offset+chunkSize > len(data) {
			break
		}
		switch chunkID {
		case "fmt ":
			if chunkSize >= 16 {
				audioFormat = binary.LittleEndian.Uint16(data[offset : offset+2])
				numChannels = binary.LittleEndian.Uint16(data[offset+2 : offset+4])
				sampleRate = binary.LittleEndian.Uint32(data[offset+4 : offset+8])
				bitsPerSample = binary.LittleEndian.Uint16(data[offset+14 : offset+16])
			}
		case "data":
			pcmData = data[offset : offset+chunkSize]
		}
		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if audioFormat != 1 || numChannels != 1 || sampleRate != 8000 {
		return nil, fmt.Errorf("wav must be PCM mono 8kHz")
	}
	if bitsPerSample != 8 && bitsPerSample != 16 {
		return nil, fmt.Errorf("wav bitsPerSample must be 8 or 16")
	}

	samplesPerPkt := int(cfg.SamplesPerPkt)
	if samplesPerPkt <= 0 {
		samplesPerPkt = 160
	}
	samples, err := decodePCM(pcmData, bitsPerSample)
	if err != nil {
		return nil, err
	}
	var packets [][]byte
	for len(samples) >= samplesPerPkt {
		frame := make([]byte, samplesPerPkt)
		for i := 0; i < samplesPerPkt; i++ {
			switch cfg.PayloadType {
			case 8:
				frame[i] = linearToALaw(samples[i])
			default:
				frame[i] = linearToMuLaw(samples[i])
			}
		}
		packets = append(packets, frame)
		samples = samples[samplesPerPkt:]
	}
	if len(packets) == 0 {
		return nil, fmt.Errorf("wav contains no full RTP frames")
	}
	return packets, nil
}

func decodePCM(data []byte, bits uint16) ([]int16, error) {
	switch bits {
	case 8:
		out := make([]int16, len(data))
		for i, b := range data {
			out[i] = int16(int(b)-128) << 8
		}
		return out, nil
	case 16:
		if len(data)%2 != 0 {
			return nil, fmt.Errorf("invalid 16-bit PCM length")
		}
		out := make([]int16, len(data)/2)
		for i := 0; i < len(out); i++ {
			out[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported PCM bit depth %d", bits)
	}
}

func linearToMuLaw(sample int16) byte {
	const (
		bias = 0x84
		clip = 32635
	)

	sign := byte(0)
	pcm := int(sample)
	if pcm < 0 {
		sign = 0x80
		pcm = -pcm
	}
	if pcm > clip {
		pcm = clip
	}
	pcm += bias

	exponent := 7
	for expMask := 0x4000; (pcm&expMask) == 0 && exponent > 0; expMask >>= 1 {
		exponent--
	}
	mantissa := (pcm >> (exponent + 3)) & 0x0F
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}

func linearToALaw(sample int16) byte {
	const clip = 32635

	pcm := int(sample)
	sign := byte(0x00)
	if pcm < 0 {
		sign = 0x80
		pcm = -pcm - 1
	}
	if pcm > clip {
		pcm = clip
	}

	var exponent byte
	switch {
	case pcm >= 4096:
		exponent = 7
	case pcm >= 2048:
		exponent = 6
	case pcm >= 1024:
		exponent = 5
	case pcm >= 512:
		exponent = 4
	case pcm >= 256:
		exponent = 3
	case pcm >= 128:
		exponent = 2
	case pcm >= 64:
		exponent = 1
	default:
		exponent = 0
	}

	var mantissa byte
	if exponent == 0 {
		mantissa = byte((pcm >> 4) & 0x0F)
	} else {
		mantissa = byte((pcm >> (exponent + 3)) & 0x0F)
	}
	return (sign | (exponent << 4) | mantissa) ^ 0xD5
}

// ApplyPayloadParams updates cfg with clock rate, samples-per-packet, packet
// duration and (where defined by a static assignment in RFC 3551) payload type
// for the given payloadName (e.g. "PCMU/8000", "PCMA/8000", "G722/8000",
// "ILBC/8000", "H264/90000").  Dynamic PT codecs (ILBC, H264) are assigned
// common defaults that can be overridden by the caller afterwards.
func ApplyPayloadParams(cfg *StreamConfig, payloadName string) {
	payloadName = strings.ToUpper(strings.TrimSpace(payloadName))
	if payloadName == "" {
		return
	}
	cfg.PayloadName = payloadName
	switch {
	case strings.HasPrefix(payloadName, "PCMA/8000"):
		cfg.PayloadType = 8
		cfg.ClockRate = 8000
		cfg.SamplesPerPkt = 160
		cfg.PacketDuration = 20 * time.Millisecond
	case strings.HasPrefix(payloadName, "PCMU/8000"):
		cfg.PayloadType = 0
		cfg.ClockRate = 8000
		cfg.SamplesPerPkt = 160
		cfg.PacketDuration = 20 * time.Millisecond
	case strings.HasPrefix(payloadName, "G722/8000"):
		cfg.PayloadType = 9
		cfg.ClockRate = 8000
		cfg.SamplesPerPkt = 160
		cfg.PacketDuration = 20 * time.Millisecond
	case strings.HasPrefix(payloadName, "ILBC/8000"):
		cfg.PayloadType = 97 // common dynamic PT per RFC 3952
		cfg.ClockRate = 8000
		cfg.SamplesPerPkt = 240
		cfg.PacketDuration = 30 * time.Millisecond
	case strings.HasPrefix(payloadName, "H264/90000"):
		cfg.PayloadType = 96 // common dynamic PT per RFC 6184
		cfg.ClockRate = 90000
		cfg.SamplesPerPkt = 3000
		cfg.PacketDuration = 33 * time.Millisecond
	case strings.HasPrefix(payloadName, "OPUS/48000"):
		cfg.PayloadType = 111 // common dynamic PT per WebRTC convention
		cfg.ClockRate = 48000
		// SamplesPerPkt drives the RTP timestamp increment only; the actual
		// payload bytes are produced by buildSyntheticPayload independently
		// of this value (Opus frames are not raw PCM).
		cfg.SamplesPerPkt = 960 // 20 ms × 48 kHz
		cfg.PacketDuration = 20 * time.Millisecond
	}
}

// rrReset zeros all incoming-RTP tracking fields. Must be called under mu or
// before goroutines start.
func (s *Session) rrReset() {
	s.rrSSRC = 0
	s.rrHasFirst = false
	s.rrFirstSeq = 0
	s.rrExtHighSeq = 0
	s.rrReceived = 0
	s.rrLost = 0
	s.rrJitter = 0
	s.rrClockRate = 0
	s.rrTSPrev = 0
	s.rrArrivalPrev = time.Time{}
	s.rrLastSRNTP = 0
	s.rrLastSRAt = time.Time{}
}

// observeRTPPacket updates RR tracking stats for a received RTP packet.
// Must be called under mu.
func (s *Session) observeRTPPacket(pkt *rtp.Packet, now time.Time) {
	if !s.rrHasFirst {
		s.rrSSRC = pkt.SSRC
		s.rrFirstSeq = pkt.SequenceNumber
		s.rrExtHighSeq = uint32(pkt.SequenceNumber)
		s.rrTSPrev = pkt.Timestamp
		s.rrArrivalPrev = now
		s.rrHasFirst = true
		s.rrReceived = 1
		return
	}
	if pkt.SSRC != s.rrSSRC {
		return
	}

	// Extended highest sequence number (RFC 3550 Appendix A.1).
	diff := int16(pkt.SequenceNumber - uint16(s.rrExtHighSeq))
	if diff > 0 {
		s.rrExtHighSeq += uint32(diff)
	}
	s.rrReceived++

	// Cumulative packets lost.
	expected := s.rrExtHighSeq - uint32(s.rrFirstSeq) + 1
	lost := int32(expected) - int32(s.rrReceived)
	if lost < 0 {
		lost = 0
	}
	s.rrLost = lost

	if uint32(lost) > s.stats.RTPRecvMaxCumulativeLost {
		s.stats.RTPRecvMaxCumulativeLost = uint32(lost)
	}

	// Interarrival jitter (RFC 3550 §A.8).
	// Auto-detect clock rate from the first pair of packets with different timestamps.
	if s.rrClockRate == 0 && !s.rrArrivalPrev.IsZero() {
		elapsed := now.Sub(s.rrArrivalPrev).Seconds()
		tsDelta := float64(int32(pkt.Timestamp - s.rrTSPrev))
		if elapsed > 0.001 && tsDelta > 0 {
			s.rrClockRate = uint32(tsDelta / elapsed)
		}
	}
	if s.rrClockRate > 0 && !s.rrArrivalPrev.IsZero() {
		arrivalDelta := now.Sub(s.rrArrivalPrev).Seconds() * float64(s.rrClockRate)
		tsDelta := float64(int32(pkt.Timestamp - s.rrTSPrev))
		d := arrivalDelta - tsDelta
		if d < 0 {
			d = -d
		}
		s.rrJitter += (d - s.rrJitter) / 16.0
	}
	j := uint32(s.rrJitter)
	if j > s.stats.RTPRecvInterarrivalJitterPeak {
		s.stats.RTPRecvInterarrivalJitterPeak = j
	}
	s.rrTSPrev = pkt.Timestamp
	s.rrArrivalPrev = now
}

// buildReceiverReport returns an RR block if we have received any RTP from the
// remote. Must be called under mu.
func (s *Session) buildReceiverReport() []rtcp.ReceptionReport {
	if !s.rrHasFirst {
		return nil
	}
	expected := s.rrExtHighSeq - uint32(s.rrFirstSeq) + 1
	var fractionLost uint8
	if expected > 0 && s.rrLost > 0 {
		f := float64(s.rrLost) / float64(expected)
		if f > 1 {
			f = 1
		}
		fractionLost = uint8(f * 256)
	}

	// Delay since last SR (DLSR): expressed in units of 1/65536 seconds.
	var dlsr uint32
	var lsr uint32 = s.rrLastSRNTP
	if !s.rrLastSRAt.IsZero() {
		delaySeconds := time.Since(s.rrLastSRAt).Seconds()
		dlsr = uint32(delaySeconds * 65536)
	}

	return []rtcp.ReceptionReport{{
		SSRC:               s.rrSSRC,
		FractionLost:       fractionLost,
		TotalLost:          uint32(s.rrLost),
		LastSequenceNumber: s.rrExtHighSeq,
		Jitter:             uint32(s.rrJitter),
		LastSenderReport:   lsr,
		Delay:              dlsr,
	}}
}

// rtpReceiveLoop reads incoming RTP on the media socket and updates RR stats.
// When rtcpMux is true, RTCP may share the same socket (cleartext heuristic or SRTP decrypt path).
func (s *Session) rtpReceiveLoop(ctx context.Context, readConn net.PacketConn, rtcpMux bool) {
	buffer := make([]byte, 2048)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = readConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := readConn.ReadFrom(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		buf := buffer[:n]
		now := time.Now()
		s.mu.Lock()
		dec := s.srtpRecv
		s.mu.Unlock()
		if dec != nil {
			var h rtp.Header
			if plain, derr := dec.DecryptRTP(nil, buf, &h); derr == nil {
				var pkt rtp.Packet
				if err := pkt.Unmarshal(plain); err != nil {
					continue
				}
				s.mu.Lock()
				s.observeRTPPacket(&pkt, now)
				s.stats.RTPPacketsReceived++
				if s.recordOn {
					s.appendRecordInboundWithJitter(&pkt)
				}
				s.mu.Unlock()
				continue
			}
			if rtcpMux {
				if plain, derr := dec.DecryptRTCP(nil, buf, nil); derr == nil {
					s.processInboundRTCPPayload(now, plain)
				}
			}
			continue
		}
		if rtcpMux && isProbableRTCPPacket(buf) {
			s.processInboundRTCPPayload(now, buf)
			continue
		}
		if s.handleInboundRTPPacketBuf(buf, now) {
			continue
		}
	}
}

func toNTPTime(ts time.Time) uint64 {
	secs := uint64(ts.Unix() + 2208988800)
	frac := uint64((float64(ts.Nanosecond()) / 1e9) * (1 << 32))
	return (secs << 32) | frac
}

func (s *Session) Snapshot() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func (s *Session) WaitForRTPActivity(ctx context.Context, minPackets uint32, direction RTPCheckDirection) error {
	if minPackets == 0 {
		minPackets = 1
	}
	if direction == "" {
		direction = RTPCheckAny
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		stats := s.Snapshot()
		total := stats.RTPPacketsSent + stats.RTPPacketsReceived
		switch direction {
		case RTPCheckSend:
			if stats.RTPPacketsSent >= minPackets {
				return nil
			}
		case RTPCheckRecv:
			if stats.RTPPacketsReceived >= minPackets {
				return nil
			}
		case RTPCheckBoth:
			if stats.RTPPacketsSent >= minPackets && stats.RTPPacketsReceived >= minPackets {
				return nil
			}
		default:
			if total >= minPackets {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			switch direction {
			case RTPCheckSend:
				return fmt.Errorf("rtpcheck timeout: mode=send sent=%d required=%d: %w", stats.RTPPacketsSent, minPackets, ctx.Err())
			case RTPCheckRecv:
				return fmt.Errorf("rtpcheck timeout: mode=recv recv=%d required=%d: %w", stats.RTPPacketsReceived, minPackets, ctx.Err())
			case RTPCheckBoth:
				return fmt.Errorf(
					"rtpcheck timeout: mode=both sent=%d recv=%d required_each=%d: %w",
					stats.RTPPacketsSent,
					stats.RTPPacketsReceived,
					minPackets,
					ctx.Err(),
				)
			default:
				return fmt.Errorf("rtpcheck timeout: mode=any total=%d required=%d: %w", total, minPackets, ctx.Err())
			}
		case <-ticker.C:
		}
	}
}

func (s *Session) Ports() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rtpPort, rtcpPort int
	if s.conn != nil {
		if addr, ok := s.conn.LocalAddr().(*net.UDPAddr); ok {
			rtpPort = addr.Port
		}
	}
	if s.rtcpConn != nil {
		if addr, ok := s.rtcpConn.LocalAddr().(*net.UDPAddr); ok {
			rtcpPort = addr.Port
		}
	}
	return rtpPort, rtcpPort
}

func ResolvePath(basePath, name string) string {
	if filepath.IsAbs(name) || basePath == "" {
		return name
	}
	return filepath.Join(basePath, name)
}
