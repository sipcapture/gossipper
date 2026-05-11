package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/pion/rtp"
	"github.com/sipcapture/gossipper/internal/pcaplink"
)

type pcapPacket struct {
	data  []byte
	delay time.Duration
}

func (s *Session) StartPCAPReplay(ctx context.Context, endpoint Endpoint, path, localIP string, localPort int) error {
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
	}

	s.mu.Lock()
	linkSpec := s.pcapLinkLayer
	s.mu.Unlock()

	packets, firstSSRC, err := loadPCAPPackets(path, linkSpec)
	if err != nil {
		return err
	}
	if len(packets) == 0 {
		return fmt.Errorf("pcap contains no replayable RTP packets")
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

	childCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.conn = conn
	s.ssrc = firstSSRC
	s.packetCount = 0
	s.octetCount = 0
	s.lastTimestamp = 0
	s.paused = false
	s.running = true
	s.echoMode = false
	s.stats = Stats{}
	s.rrReset()
	s.localIP = localIP
	s.localPort = conn.LocalAddr().(*net.UDPAddr).Port
	rtcpPort := conn.LocalAddr().(*net.UDPAddr).Port + 1
	rtcpLocalAddr := &net.UDPAddr{Port: rtcpPort}
	if localAddr.IP != nil {
		rtcpLocalAddr.IP = localAddr.IP
	}
	rtcpConn, _ := net.ListenUDP("udp", rtcpLocalAddr)
	s.rtcpConn = rtcpConn
	obs := s.hepObserver
	callID := s.callID
	s.mu.Unlock()

	go s.pcapLoop(childCtx, conn, remoteAddr, packets, obs, callID)
	go s.rtpReceiveLoop(childCtx, conn)
	if rtcpConn != nil {
		go s.rtcpLoop(childCtx, rtcpConn, &net.UDPAddr{IP: remoteAddr.IP, Port: remoteAddr.Port + 1}, StreamConfig{SSRC: firstSSRC}, obs, callID)
		go s.rtcpReceiveLoop(childCtx, rtcpConn)
	}
	return nil
}

func (s *Session) pcapLoop(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr, packets []pcapPacket, obs HEPObserver, callID string) {
	defer s.Stop()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	localIP := localAddr.IP.String()
	localPort := localAddr.Port

	for _, packet := range packets {
		if err := ctx.Err(); err != nil {
			return
		}
		s.waitIfPaused(ctx)
		if packet.delay > 0 {
			timer := time.NewTimer(packet.delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if _, err := conn.WriteToUDP(packet.data, remote); err != nil {
			return
		}
		if obs != nil {
			_ = obs.SendRTP(time.Now(), localIP, localPort, remote.IP.String(), remote.Port, callID, packet.data)
		}

		payloadBytes := len(packet.data)
		var parsed rtp.Packet
		if err := parsed.Unmarshal(packet.data); err == nil {
			payloadBytes = len(parsed.Payload)
			s.mu.Lock()
			s.ssrc = parsed.SSRC
			s.lastTimestamp = parsed.Timestamp
			s.packetCount++
			s.octetCount += uint32(len(parsed.Payload))
			s.stats.RTPPacketsSent++
			s.stats.RTPOctetsSent += uint32(len(parsed.Payload))
			s.mu.Unlock()
			continue
		}

		s.mu.Lock()
		s.packetCount++
		s.octetCount += uint32(payloadBytes)
		s.stats.RTPPacketsSent++
		s.stats.RTPOctetsSent += uint32(payloadBytes)
		s.mu.Unlock()
	}
}

func loadPCAPPackets(path, linkSpec string) ([]pcapPacket, uint32, error) {
	headerLT, err := pcaplink.PeekFileLinkType(path)
	if err != nil {
		return nil, 0, fmt.Errorf("pcap header: %w", err)
	}

	dec, err := pcaplink.ResolveDecoder(linkSpec, headerLT)
	if err != nil {
		return nil, 0, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	reader, err := pcapgo.NewReader(file)
	if err != nil {
		return nil, 0, err
	}

	var (
		packets   []pcapPacket
		lastStamp time.Time
		firstSSRC uint32
	)
	for {
		data, ci, err := reader.ReadPacketData()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, 0, err
		}
		if len(data) == 0 {
			break
		}

		packet := gopacket.NewPacket(data, dec, gopacket.NoCopy)
		udpLayer := packet.Layer(layers.LayerTypeUDP)
		if udpLayer == nil {
			continue
		}
		udp, ok := udpLayer.(*layers.UDP)
		if !ok || len(udp.Payload) == 0 {
			continue
		}

		raw := make([]byte, len(udp.Payload))
		copy(raw, udp.Payload)

		delay := time.Duration(0)
		if !lastStamp.IsZero() {
			delay = ci.Timestamp.Sub(lastStamp)
			if delay < 0 {
				delay = 0
			}
		}
		lastStamp = ci.Timestamp

		if firstSSRC == 0 {
			var parsed rtp.Packet
			if err := parsed.Unmarshal(raw); err == nil {
				firstSSRC = parsed.SSRC
			}
		}
		packets = append(packets, pcapPacket{data: raw, delay: delay})
	}

	if len(packets) == 0 {
		return nil, 0, fmt.Errorf("pcap contains no UDP payloads")
	}
	return packets, firstSSRC, nil
}
