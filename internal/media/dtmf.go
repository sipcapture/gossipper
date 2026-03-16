package media

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/pion/rtp"
)

// digitToEvent maps DTMF character to RFC 2833 event code.
// 0-9 -> 0-9, * -> 10, # -> 11, A-D -> 12-15
var digitToEvent = map[rune]uint8{
	'0': 0, '1': 1, '2': 2, '3': 3, '4': 4,
	'5': 5, '6': 6, '7': 7, '8': 8, '9': 9,
	'*': 10, '#': 11,
	'a': 12, 'A': 12, 'b': 13, 'B': 13, 'c': 14, 'C': 14, 'd': 15, 'D': 15,
}

const (
	defaultDTMFPayloadType   = 101
	defaultDTMFClockRate    = 8000
	defaultInterDigitPause  = 100 * time.Millisecond
)

// BuildDTMFPackets generates RFC 2833 telephone-event RTP packets for a digit.
// Returns packets: start (marker), zero or more continuation, end (E bit set).
func BuildDTMFPackets(digit rune, payloadType uint8, ssrc uint32, sequence *uint16, timestamp *uint32, clockRate uint32) ([][]byte, error) {
	event, ok := digitToEvent[digit]
	if !ok {
		return nil, fmt.Errorf("unsupported DTMF digit %q", digit)
	}
	if payloadType == 0 {
		payloadType = defaultDTMFPayloadType
	}
	if clockRate == 0 {
		clockRate = defaultDTMFClockRate
	}

	// RFC 2833: event byte, E bit, volume (0), duration (big-endian 16-bit)
	// Packets: start (no E), continuation(s) with duration, end (E=0x80)
	// Per digit: send ~3 packets at 50ms intervals, last has E bit
	packetDuration := uint32(4000) // 50ms at 8kHz = 4000
	duration := uint32(0)
	var packets [][]byte

	for i := 0; i < 3; i++ {
		duration += packetDuration
		isEnd := i == 2
		eventByte := event
		if isEnd {
			eventByte |= 0x80
		}
		payload := []byte{
			eventByte,
			0,    // volume
			byte(duration >> 8),
			byte(duration & 0xff),
		}
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    payloadType,
				SequenceNumber: *sequence,
				Timestamp:      *timestamp,
				SSRC:           ssrc,
				Marker:         i == 0,
			},
			Payload: payload,
		}
		raw, err := pkt.Marshal()
		if err != nil {
			return nil, err
		}
		packets = append(packets, raw)
		*sequence++
		*timestamp += packetDuration
	}
	return packets, nil
}

// SendDTMF sends RFC 2833 DTMF packets to the remote endpoint.
// Digits: 0-9, *, #, A-D. Blocks until all digits are sent.
func (s *Session) SendDTMF(ctx context.Context, endpoint Endpoint, digits string, localIP string, localPort int) error {
	if endpoint.IP == "" || endpoint.Port <= 0 {
		return fmt.Errorf("invalid RTP endpoint %s:%d", endpoint.IP, endpoint.Port)
	}
	digits = strings.TrimSpace(digits)
	if digits == "" {
		return nil
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
	defer conn.Close()

	ssrc := rand.Uint32()
	sequence := uint16(rand.Intn(65535))
	timestamp := rand.Uint32()
	clockRate := uint32(defaultDTMFClockRate)
	payloadType := uint8(defaultDTMFPayloadType)

	runes := []rune(digits)
	for idx, digit := range runes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		packets, err := BuildDTMFPackets(digit, uint8(payloadType), ssrc, &sequence, &timestamp, uint32(clockRate))
		if err != nil {
			return err
		}
		for _, raw := range packets {
			if _, err := conn.WriteToUDP(raw, remoteAddr); err != nil {
				return err
			}
			s.mu.Lock()
			s.stats.RTPPacketsSent++
			s.stats.RTPOctetsSent += uint32(len(raw))
			s.mu.Unlock()
		}
		// Pause between digits (except after last)
		if idx < len(runes)-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(defaultInterDigitPause):
			}
		}
	}
	return nil
}
