package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// StartFuncStreaming binds RTP/RTCP like Start, but each packet payload is
// produced by nextPayload (e.g. live PCM from stdin). The callback is invoked
// on each PacketDuration tick; returning (nil, nil) stops the stream cleanly;
// any other error aborts the sender.
func (s *Session) StartFuncStreaming(ctx context.Context, endpoint Endpoint, cfg StreamConfig, localIP string, localPort int, nextPayload func() ([]byte, error)) error {
	if nextPayload == nil {
		return fmt.Errorf("nextPayload is nil")
	}
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
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

	var wavRecv, wavSend *wavPCMRecorder
	if cfg.RecordRecvWAV != "" {
		var rerr error
		wavRecv, rerr = newWavPCMRecorder(cfg.RecordRecvWAV, int(cfg.ClockRate))
		if rerr != nil {
			_ = conn.Close()
			return rerr
		}
	}
	if cfg.RecordSendWAV != "" {
		var serr error
		wavSend, serr = newWavPCMRecorder(cfg.RecordSendWAV, int(cfg.ClockRate))
		if serr != nil {
			if wavRecv != nil {
				_ = wavRecv.Close()
			}
			_ = conn.Close()
			return serr
		}
	}

	childCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.conn = conn
	s.wavRecv = wavRecv
	s.wavSend = wavSend
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
	rtcpLocalAddr := &net.UDPAddr{Port: localAddr.Port + 1}
	if localAddr.IP != nil {
		rtcpLocalAddr.IP = localAddr.IP
	}
	rtcpConn, _ := net.ListenUDP("udp", rtcpLocalAddr)
	s.rtcpConn = rtcpConn
	obs := s.hepObserver
	callID := s.callID
	s.mu.Unlock()

	go s.funcStreamLoop(childCtx, conn, remoteAddr, cfg, nextPayload, obs, callID)
	go s.rtpReceiveLoop(childCtx, conn)
	if rtcpConn != nil {
		go s.rtcpLoop(childCtx, rtcpConn, &net.UDPAddr{IP: remoteAddr.IP, Port: remoteAddr.Port + 1}, cfg, obs, callID)
		go s.rtcpReceiveLoop(childCtx, rtcpConn)
	}
	return nil
}

func (s *Session) funcStreamLoop(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr, cfg StreamConfig, nextPayload func() ([]byte, error), obs HEPObserver, callID string) {
	defer s.Stop()

	if cfg.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Duration)
		defer cancel()
	}

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	localIP := localAddr.IP.String()
	localPort := localAddr.Port

	sequence := cfg.Sequence
	timestamp := cfg.Timestamp

	ticker := time.NewTicker(cfg.PacketDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		s.waitIfPaused(ctx)

		payload, err := nextPayload()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			return
		}
		if len(payload) == 0 {
			return
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
		s.appendWAVSend(cfg.PayloadType, payload)
		if obs != nil {
			_ = obs.SendRTP(time.Now(), localIP, localPort, remote.IP.String(), remote.Port, callID, frame)
		}
		s.mu.Lock()
		s.packetCount++
		s.octetCount += uint32(len(payload))
		s.lastTimestamp = timestamp
		s.stats.RTPPacketsSent++
		s.stats.RTPOctetsSent += uint32(len(payload))
		s.mu.Unlock()
		sequence++
		timestamp += cfg.SamplesPerPkt
	}
}
