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
func (s *Session) attachMicSession(childCtx context.Context, cancel context.CancelFunc, conn *net.UDPConn, dataPC net.PacketConn, remoteAddr *net.UDPAddr, localIP string, r io.Reader) error {
	rtpReadConn, err := s.prepareRTPReadConn(childCtx, dataPC, remoteAddr)
	if err != nil {
		cancel()
		s.closeTurnResources()
		_ = conn.Close()
		return fmt.Errorf("mic DTLS-SRTP: %w", err)
	}
	cfg := DefaultConfig("")
	cfg.Synthetic = false
	ApplyPayloadParams(&cfg, "PCMU/8000")

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	rtpBoundPort := localAddr.Port

	s.mu.Lock()
	rtcpMux := s.rtcpMux
	var rtcpConn *net.UDPConn
	if !rtcpMux {
		rtcpLocalAddr := &net.UDPAddr{Port: rtpBoundPort + 1}
		if localAddr.IP != nil {
			rtcpLocalAddr.IP = localAddr.IP
		}
		rtcpConn, _ = net.ListenUDP("udp", rtcpLocalAddr)
	}
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
	s.localPort = rtpBoundPort
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

	go s.micStreamLoop(childCtx, dataPC, remoteAddr, r, cfg, obs, callID)
	go s.rtpReceiveLoop(childCtx, rtpReadConn, rtcpMux)
	if rtcpMux {
		go s.rtcpLoop(childCtx, dataPC, remoteAddr, cfg, obs, callID)
	} else if rtcpConn != nil {
		go s.rtcpLoop(childCtx, rtcpConn, &net.UDPAddr{IP: remoteAddr.IP, Port: remoteAddr.Port + 1}, cfg, obs, callID)
		go s.rtpReceiveLoop(childCtx, rtcpConn, false)
	}
	s.maybeStartAutoRecord()
	return nil
}

// startMicrophonePCMFromReader binds UDP and streams mono s16le 8 kHz PCM from r into PCMU RTP.
func (s *Session) startMicrophonePCMFromReader(ctx context.Context, endpoint Endpoint, localIP string, localPort int, r io.Reader) error {
	conn, dataPC, remoteAddr, err := s.openRTPDatapath(endpoint, localIP, localPort)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	return s.attachMicSession(childCtx, cancel, conn, dataPC, remoteAddr, localIP, r)
}

// startMicrophonePCMReader starts bin/args (mono s16le 8 kHz PCM on stdout) and streams PCMU RTP.
func (s *Session) startMicrophonePCMReader(ctx context.Context, endpoint Endpoint, localIP string, localPort int, bin string, args []string) error {
	conn, dataPC, remoteAddr, err := s.openRTPDatapath(endpoint, localIP, localPort)
	if err != nil {
		return err
	}

	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		s.closeTurnResources()
		_ = conn.Close()
		return fmt.Errorf("rtp_stream mic: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		s.closeTurnResources()
		_ = conn.Close()
		return fmt.Errorf("rtp_stream mic start: %w", err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	if err := s.attachMicSession(childCtx, cancel, conn, dataPC, remoteAddr, localIP, stdout); err != nil {
		cancel()
		s.closeTurnResources()
		_ = conn.Close()
		return err
	}
	return nil
}

func (s *Session) micStreamLoop(ctx context.Context, w net.PacketConn, remote net.Addr, r io.Reader, cfg StreamConfig, obs HEPObserver, callID string) {
	defer s.Stop()

	localAddr := w.LocalAddr()
	localUDP, _ := localAddr.(*net.UDPAddr)
	localIP := "0.0.0.0"
	localPort := 0
	if localUDP != nil {
		localIP = localUDP.IP.String()
		localPort = localUDP.Port
	}
	rip := ""
	rport := 0
	if ua, ok := remote.(*net.UDPAddr); ok {
		rip = ua.IP.String()
		rport = ua.Port
	}

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
		if _, err := w.WriteTo(wire, remote); err != nil {
			return
		}
		if obs != nil {
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
