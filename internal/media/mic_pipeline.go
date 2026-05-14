package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"time"
)

// attachMicSession binds RTP/RTCP session state and starts mic + RTCP loops (r = mono s16le 8 kHz).
func (s *Session) attachMicSession(childCtx context.Context, cancel context.CancelFunc, conn *net.UDPConn, remoteAddr *net.UDPAddr, localIP string, r io.Reader) error {
	cfg := DefaultConfig("")
	cfg.Synthetic = false
	ApplyPayloadParams(&cfg, "PCMU/8000")

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	rtcpLocalAddr := &net.UDPAddr{Port: localAddr.Port + 1}
	if localAddr.IP != nil {
		rtcpLocalAddr.IP = localAddr.IP
	}
	rtcpConn, _ := net.ListenUDP("udp", rtcpLocalAddr)

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
	s.localPort = localAddr.Port
	s.rtcpConn = rtcpConn
	if err := s.buildSRTPContextsLocked(); err != nil {
		s.conn = nil
		s.rtcpConn = nil
		s.cancel = nil
		s.running = false
		s.mu.Unlock()
		cancel()
		_ = conn.Close()
		if rtcpConn != nil {
			_ = rtcpConn.Close()
		}
		return fmt.Errorf("media SRTP: %w", err)
	}
	obs := s.hepObserver
	callID := s.callID
	s.mu.Unlock()

	go s.micStreamLoop(childCtx, conn, remoteAddr, r, cfg, obs, callID)
	go s.rtpReceiveLoop(childCtx, conn)
	if rtcpConn != nil {
		go s.rtcpLoop(childCtx, rtcpConn, &net.UDPAddr{IP: remoteAddr.IP, Port: remoteAddr.Port + 1}, cfg, obs, callID)
		go s.rtcpReceiveLoop(childCtx, rtcpConn)
	}
	s.maybeStartAutoRecord()
	return nil
}

func listenMicUDP(localIP string, localPort int) (*net.UDPConn, error) {
	localAddr := &net.UDPAddr{Port: localPort}
	if localIP != "" && localIP != "0.0.0.0" && localIP != "::" {
		localAddr.IP = net.ParseIP(localIP)
	}
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: localAddr.IP, Port: 0})
	}
	return conn, err
}

func dialMicUDP(endpoint Endpoint, localIP string, localPort int) (*net.UDPConn, *net.UDPAddr, error) {
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return nil, nil, fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
	}
	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", endpoint.IP, endpoint.Port))
	if err != nil {
		return nil, nil, err
	}
	conn, err := listenMicUDP(localIP, localPort)
	if err != nil {
		return nil, nil, err
	}
	return conn, remoteAddr, nil
}

// startMicrophonePCMFromReader binds UDP and streams mono s16le 8 kHz PCM from r into PCMU RTP.
func (s *Session) startMicrophonePCMFromReader(ctx context.Context, endpoint Endpoint, localIP string, localPort int, r io.Reader) error {
	conn, remoteAddr, err := dialMicUDP(endpoint, localIP, localPort)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	return s.attachMicSession(childCtx, cancel, conn, remoteAddr, localIP, r)
}

// startMicrophonePCMReader starts bin/args (mono s16le 8 kHz PCM on stdout) and streams PCMU RTP.
func (s *Session) startMicrophonePCMReader(ctx context.Context, endpoint Endpoint, localIP string, localPort int, bin string, args []string) error {
	conn, remoteAddr, err := dialMicUDP(endpoint, localIP, localPort)
	if err != nil {
		return err
	}

	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = conn.Close()
		return fmt.Errorf("rtp_stream mic: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = conn.Close()
		return fmt.Errorf("rtp_stream mic start: %w", err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	if err := s.attachMicSession(childCtx, cancel, conn, remoteAddr, localIP, stdout); err != nil {
		cancel()
		_ = conn.Close()
		return err
	}
	return nil
}

func (s *Session) micStreamLoop(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr, r io.Reader, cfg StreamConfig, obs HEPObserver, callID string) {
	defer s.Stop()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	localIP := localAddr.IP.String()
	localPort := localAddr.Port

	sequence := cfg.Sequence
	timestamp := cfg.Timestamp
	frameBytes := int(cfg.SamplesPerPkt) * 2
	buf := make([]byte, frameBytes)
	ticker := time.NewTicker(cfg.PacketDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.waitIfPaused(ctx)
		if ctx.Err() != nil {
			return
		}
		n, err := io.ReadFull(r, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			return
		}
		if n < frameBytes {
			for i := n; i < frameBytes; i++ {
				buf[i] = 0
			}
		}
		samples, derr := decodePCM(buf[:frameBytes], 16)
		if derr != nil {
			return
		}
		payload := make([]byte, len(samples))
		for i, sample := range samples {
			payload[i] = linearToMuLaw(sample)
		}
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
		if _, err := conn.WriteToUDP(wire, remote); err != nil {
			return
		}
		if obs != nil {
			_ = obs.SendRTP(time.Now(), localIP, localPort, remote.IP.String(), remote.Port, callID, frame)
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
