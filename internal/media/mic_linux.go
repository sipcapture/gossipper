//go:build linux

package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"time"
)

// StartMicrophone captures mono S16_LE PCM at 8 kHz via arecord(1) and sends PCMU RTP.
func (s *Session) StartMicrophone(ctx context.Context, endpoint Endpoint, localIP string, localPort int) error {
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
	}
	arec, err := exec.LookPath("arecord")
	if err != nil {
		return fmt.Errorf("rtp_stream mic: arecord not found in PATH (install alsa-utils): %w", err)
	}

	s.Stop()

	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", endpoint.IP, endpoint.Port))
	if err != nil {
		return err
	}

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

	cfg := DefaultConfig("")
	cfg.Synthetic = false
	ApplyPayloadParams(&cfg, "PCMU/8000")

	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, arec, "-q", "-t", "raw", "-f", "S16_LE", "-c", "1", "-r", "8000", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = conn.Close()
		return fmt.Errorf("rtp_stream mic: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = conn.Close()
		return fmt.Errorf("rtp_stream mic start arecord: %w", err)
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
	s.localPort = conn.LocalAddr().(*net.UDPAddr).Port
	rtcpLocalAddr := &net.UDPAddr{Port: conn.LocalAddr().(*net.UDPAddr).Port + 1}
	if localAddr.IP != nil {
		rtcpLocalAddr.IP = localAddr.IP
	}
	rtcpConn, _ := net.ListenUDP("udp", rtcpLocalAddr)
	s.rtcpConn = rtcpConn
	obs := s.hepObserver
	callID := s.callID
	s.mu.Unlock()

	go func() {
		_ = cmd.Wait()
	}()
	go s.micStreamLoop(childCtx, conn, remoteAddr, stdout, cfg, obs, callID)
	go s.rtpReceiveLoop(childCtx, conn)
	if rtcpConn != nil {
		go s.rtcpLoop(childCtx, rtcpConn, &net.UDPAddr{IP: remoteAddr.IP, Port: remoteAddr.Port + 1}, cfg, obs, callID)
		go s.rtcpReceiveLoop(childCtx, rtcpConn)
	}
	s.maybeStartAutoRecord()
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
		if _, err := conn.WriteToUDP(frame, remote); err != nil {
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
