package hep

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	ProtocolSIP        = 0x01
	ProtocolRTCP       = 0x05 // raw RTCP binary (type 5), processed by hepagent-go RTCPConverter
	ProtocolRTPReport  = 0x23 // Short RTP Report JSON = 35 (TypeShortRTPReport in hepagent-go)
	ProtocolRTCPReport = 0x25 // Short RTCP Report JSON = 37 (TypeShortRTCPReport in hepagent-go)
	ProtocolDTMF       = 0x64 // DTMF Report JSON = 100

	ipFamilyIPv4 = 0x02
	ipFamilyIPv6 = 0x0a

	ipProtoTCP = 0x06
	ipProtoUDP = 0x11

	chunkIPFamily  = 0x0001
	chunkIPProto   = 0x0002
	chunkIPv4Src   = 0x0003
	chunkIPv4Dst   = 0x0004
	chunkIPv6Src   = 0x0005
	chunkIPv6Dst   = 0x0006
	chunkSrcPort   = 0x0007
	chunkDstPort   = 0x0008
	chunkTimeSec   = 0x0009
	chunkTimeUsec  = 0x000a
	chunkProtoType = 0x000b
	chunkCaptureID = 0x000c
	chunkAuthKey   = 0x000e
	chunkPayload   = 0x000f

	// streamPruneAge is how long a stream can be idle before being pruned.
	streamPruneAge = 60 * time.Second
)

// Config holds the HEP client configuration.
type Config struct {
	Addr            string
	CaptureID       uint32
	Password        string
	RawRTCP         bool
	SendMediaReport bool
}

// Message is a HEP message to encode.
type Message struct {
	Time       time.Time
	SrcIP      string
	DstIP      string
	SrcPort    int
	DstPort    int
	IPProtocol uint8
	ProtoType  uint8
	CaptureID  uint32
	AuthKey    string
	Payload    []byte
}

// Decoded is a parsed HEP message.
type Decoded struct {
	IPFamily   uint8
	IPProtocol uint8
	SrcIP      net.IP
	DstIP      net.IP
	SrcPort    uint16
	DstPort    uint16
	Time       time.Time
	ProtoType  uint8
	CaptureID  uint32
	AuthKey    string
	Payload    []byte
}

// rtpStreamState tracks per-SSRC RTP/RTCP statistics for aggregated reporting.
type rtpStreamState struct {
	packetCount uint32
	octetCount  uint32
	lastSeen    time.Time

	srcIP   string
	srcPort int
	dstIP   string
	dstPort int
	callID  string

	// RTP header fields (from last packet)
	lastRTPTimestamp uint32
	mediaPT          uint8 // payload type for media (PCMU, PCMA, etc.)

	// RTCP SR fields (populated via SendRTCP in JSON mode)
	ntpMSW     uint32
	ntpLSW     uint32
	packetLoss uint32

	// DTMF accumulation (JSON mode only)
	dtmfEvents []string
	dtmfPT     uint8 // payload type for telephone-event (usually 101)
}

// Client is a HEP UDP client.
type Client struct {
	conn *net.UDPConn
	addr *net.UDPAddr

	captureID       uint32
	password        string
	rawRTCP         bool
	sendMediaReport bool

	mu         sync.Mutex
	rtpStreams  map[uint32]*rtpStreamState
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// New creates a new HEP client. If SendMediaReport is true, a background goroutine
// will periodically send aggregated RTP/RTCP (and DTMF) reports.
func New(cfg Config) (*Client, error) {
	if cfg.Addr == "" {
		return nil, errors.New("hep addr is required")
	}
	addr, err := net.ResolveUDPAddr("udp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:            conn,
		addr:            addr,
		captureID:       cfg.CaptureID,
		password:        cfg.Password,
		rawRTCP:         cfg.RawRTCP,
		sendMediaReport: cfg.SendMediaReport,
	}
	if cfg.SendMediaReport {
		c.rtpStreams = make(map[uint32]*rtpStreamState)
		c.stopCh = make(chan struct{})
		c.wg.Add(1)
		if cfg.RawRTCP {
			go c.rawRTCPLoop()
		} else {
			go c.reportLoop()
		}
	}
	return c, nil
}

// Close shuts down the HEP client and waits for background goroutines to finish.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	if c.sendMediaReport && c.stopCh != nil {
		close(c.stopCh)
		c.wg.Wait()
	}
	return c.conn.Close()
}

// SendSIP sends a SIP message via HEP.
func (c *Client) SendSIP(now time.Time, srcIP string, srcPort int, dstIP string, dstPort int, transport string, payload []byte) error {
	if c == nil {
		return nil
	}
	packet, err := Encode(Message{
		Time:       now,
		SrcIP:      srcIP,
		DstIP:      dstIP,
		SrcPort:    srcPort,
		DstPort:    dstPort,
		IPProtocol: transportProtocol(transport),
		ProtoType:  ProtocolSIP,
		CaptureID:  c.captureID,
		AuthKey:    c.password,
		Payload:    payload,
	})
	if err != nil {
		return err
	}
	_, err = c.conn.WriteToUDP(packet, c.addr)
	return err
}

// SendRTP processes an RTP frame for media reporting.
// In raw mode only RTCP is reported; RTP packets are tracked for stats aggregation.
// DTMF telephone-event packets (RFC 2833) are detected and accumulated.
// callID is the SIP Call-ID for correlation; when non-empty it is stored in stream state for reports.
func (c *Client) SendRTP(now time.Time, srcIP string, srcPort int, dstIP string, dstPort int, callID string, payload []byte) error {
	if c == nil || !c.sendMediaReport {
		return nil
	}
	if len(payload) < 12 {
		return nil
	}

	// Parse RTP header fields needed for aggregation.
	pt := payload[1] & 0x7F
	ssrc := binary.BigEndian.Uint32(payload[8:12])

	c.mu.Lock()
	state, ok := c.rtpStreams[ssrc]
	if !ok {
		state = &rtpStreamState{
			srcIP:   srcIP,
			srcPort: srcPort,
			dstIP:   dstIP,
			dstPort: dstPort,
			callID:  callID,
			dtmfPT:  pt,
		}
		c.rtpStreams[ssrc] = state
	} else if callID != "" {
		state.callID = callID
	}
	state.packetCount++
	state.octetCount += uint32(len(payload) - 12)
	state.lastSeen = now
	if len(payload) >= 12 {
		state.lastRTPTimestamp = binary.BigEndian.Uint32(payload[4:8])
		if pt != 101 { // not telephone-event
			state.mediaPT = pt
		}
	}

	// telephone-event detection (RFC 2833) — only in JSON mode
	if !c.rawRTCP && len(payload) >= 16 {
		eventByte := payload[12]
		endOfEvent := (eventByte & 0x80) != 0
		if endOfEvent {
			digit := eventByte & 0x7F
			volume := payload[13] & 0x3F
			duration := binary.BigEndian.Uint16(payload[14:16])
			sec := now.Unix()
			usec := now.UnixMicro() % 1_000_000
			entry := fmt.Sprintf("ts:%d,tsu:%d,e:%d,v:%d,d:%d,c:1",
				sec, usec, digit, volume, duration)
			state.dtmfEvents = append(state.dtmfEvents, entry)
			state.dtmfPT = pt
		}
	}
	c.mu.Unlock()
	return nil
}

// SendRTCP processes RTCP data for media reporting.
// In raw mode the binary RTCP SR is sent immediately.
// In JSON mode RTCP-derived fields are stored for aggregation.
func (c *Client) SendRTCP(now time.Time, callID string, ssrc uint32, srcIP string, srcPort int, dstIP string, dstPort int, packetLoss uint32, rawPayload []byte) error {
	if c == nil || !c.sendMediaReport {
		return nil
	}

	if c.rawRTCP {
		// Send raw binary RTCP immediately.
		packet, err := Encode(Message{
			Time:       now,
			SrcIP:      srcIP,
			DstIP:      dstIP,
			SrcPort:    srcPort,
			DstPort:    dstPort,
			IPProtocol: ipProtoUDP,
			ProtoType:  ProtocolRTCP,
			CaptureID:  c.captureID,
			AuthKey:    c.password,
			Payload:    rawPayload,
		})
		if err != nil {
			return err
		}
		_, err = c.conn.WriteToUDP(packet, c.addr)
		return err
	}

	// JSON mode: update RTCP-derived fields in the stream state.
	c.mu.Lock()
	state, ok := c.rtpStreams[ssrc]
	if !ok {
		state = &rtpStreamState{
			srcIP:   srcIP,
			srcPort: srcPort,
			dstIP:   dstIP,
			dstPort: dstPort,
		}
		c.rtpStreams[ssrc] = state
	}
	state.callID = callID
	state.packetLoss = packetLoss
	state.lastSeen = now
	if len(rawPayload) >= 20 {
		state.ntpMSW = binary.BigEndian.Uint32(rawPayload[8:12])
		state.ntpLSW = binary.BigEndian.Uint32(rawPayload[12:16])
	}
	c.mu.Unlock()
	return nil
}

// reportLoop runs every 10 seconds and sends JSON RTP (type 35), RTCP (type 37),
// and DTMF (type 100) reports for all active streams.
func (c *Client) reportLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case now := <-ticker.C:
			c.sendJSONReports(now)
		}
	}
}

// rawRTCPLoop runs every 5 seconds and sends synthetic binary RTCP SR packets (type 5)
// built from per-SSRC aggregated RTP statistics.
func (c *Client) rawRTCPLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case now := <-ticker.C:
			c.sendRawRTCPReports(now)
		}
	}
}

func (c *Client) sendJSONReports(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for ssrc, state := range c.rtpStreams {
		if now.Sub(state.lastSeen) > streamPruneAge {
			delete(c.rtpStreams, ssrc)
			continue
		}
		if state.packetCount == 0 {
			continue
		}

		// Short RTP Report (type 35)
		rtpReport := rtpShortReport{
			CorrelationID: state.callID,
			RTPSipCallID:  state.callID,
			SSRC:          fmt.Sprintf("0x%x", ssrc),
			PacketCount:   state.packetCount,
			OctetCount:    state.octetCount,
			ReportName:    fmt.Sprintf("%s-%d", state.srcIP, state.srcPort),
			Source:        "GOSSIPPER",
			Type:          "PERIODIC",
			ReportTS:      now.UnixMilli(),
			SrcIP:         state.srcIP,
			SrcPort:       state.srcPort,
			DstIP:         state.dstIP,
			DstPort:       state.dstPort,
			RTPTimestamp:  state.lastRTPTimestamp,
			CodecPT:       state.mediaPT,
			ClockRate:     payloadTypeClockRate(state.mediaPT),
			CodecName:     payloadTypeName(state.mediaPT),
		}
		if jsonData, err := json.Marshal(rtpReport); err == nil {
			c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolRTPReport, jsonData)
		}

		// Short RTCP Report (type 37)
		rtcpReport := rtcpShortReport{
			CorrelationID: state.callID,
			RTPSipCallID:  state.callID,
			SSRC:          fmt.Sprintf("0x%x", ssrc),
			CumPacketLoss: state.packetLoss,
			ReportName:    fmt.Sprintf("%s-%d", state.srcIP, state.srcPort),
			Source:        "GOSSIPPER",
			Type:          "PERIODIC",
			ReportTS:      now.UnixMilli(),
			SrcIP:         state.srcIP,
			SrcPort:       state.srcPort,
			DstIP:         state.dstIP,
			DstPort:       state.dstPort,
			CodecPT:       state.mediaPT,
			ClockRate:     payloadTypeClockRate(state.mediaPT),
			CodecName:     payloadTypeName(state.mediaPT),
		}
		if jsonData, err := json.Marshal(rtcpReport); err == nil {
			c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolRTCPReport, jsonData)
		}

		// DTMF Report (type 100) — only if events were accumulated
		if len(state.dtmfEvents) > 0 {
			dtmfRpt := dtmfReport{
				CorrelationID: state.callID,
				ReportTS:      now.UnixMilli(),
				DTMF:          strings.Join(state.dtmfEvents, ";"),
				SrcIP:         state.srcIP,
				SrcPort:       uint16(state.srcPort),
				DstIP:         state.dstIP,
				DstPort:       uint16(state.dstPort),
				CodecPT:       state.dtmfPT,
				CodecName:     "telephone-event",
				Party:         1,
				SType:         "GOSSIPPER-DTMF",
				Type:          "PERIODIC",
			}
			if jsonData, err := json.Marshal(dtmfRpt); err == nil {
				c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolDTMF, jsonData)
			}
			state.dtmfEvents = state.dtmfEvents[:0]
		}
	}
}

func (c *Client) sendRawRTCPReports(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for ssrc, state := range c.rtpStreams {
		if now.Sub(state.lastSeen) > streamPruneAge {
			delete(c.rtpStreams, ssrc)
			continue
		}
		if state.packetCount == 0 {
			continue
		}
		sr := buildRTCPSR(ssrc, state, now)
		c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolRTCP, sr)
	}
}

// buildRTCPSR constructs a minimal 28-byte RTCP Sender Report from aggregated stats.
func buildRTCPSR(ssrc uint32, state *rtpStreamState, now time.Time) []byte {
	sr := make([]byte, 28)
	// RTCP SR header: V=2, P=0, RC=0, PT=200 (SR), length=6 (28 bytes = 7 words - 1)
	sr[0] = 0x80
	sr[1] = 200
	binary.BigEndian.PutUint16(sr[2:4], 6)
	binary.BigEndian.PutUint32(sr[4:8], ssrc)

	// NTP timestamp — use stored values if available, otherwise derive from now
	ntpMSW := state.ntpMSW
	ntpLSW := state.ntpLSW
	if ntpMSW == 0 {
		// NTP epoch offset: seconds between 1900-01-01 and 1970-01-01
		const ntpEpochOffset = 2208988800
		ntpMSW = uint32(now.Unix()) + ntpEpochOffset
		ntpLSW = uint32(float64(now.Nanosecond()) / 1e9 * (1 << 32))
	}
	binary.BigEndian.PutUint32(sr[8:12], ntpMSW)
	binary.BigEndian.PutUint32(sr[12:16], ntpLSW)

	// RTP timestamp (approximate)
	binary.BigEndian.PutUint32(sr[16:20], uint32(now.UnixNano()/125)) // 8kHz ticks
	binary.BigEndian.PutUint32(sr[20:24], state.packetCount)
	binary.BigEndian.PutUint32(sr[24:28], state.octetCount)
	return sr
}

// sendHEP encodes and sends a HEP packet. Must be called with c.mu held or from a
// context where concurrent access to conn is safe (conn itself is goroutine-safe).
func (c *Client) sendHEP(now time.Time, srcIP string, srcPort int, dstIP string, dstPort int, protoType uint8, payload []byte) {
	packet, err := Encode(Message{
		Time:       now,
		SrcIP:      srcIP,
		DstIP:      dstIP,
		SrcPort:    srcPort,
		DstPort:    dstPort,
		IPProtocol: ipProtoUDP,
		ProtoType:  protoType,
		CaptureID:  c.captureID,
		AuthKey:    c.password,
		Payload:    payload,
	})
	if err != nil {
		return
	}
	_, _ = c.conn.WriteToUDP(packet, c.addr)
}

// payloadTypeClockRate returns the RTP clock rate for common payload types.
func payloadTypeClockRate(pt uint8) uint32 {
	switch pt {
	case 0: // PCMU
		return 8000
	case 3: // GSM
		return 8000
	case 8: // PCMA
		return 8000
	case 9: // G722
		return 8000
	case 18: // G729
		return 8000
	case 96, 97, 98: // H264, etc.
		return 90000
	default:
		return 8000
	}
}

// payloadTypeName returns the codec name for common RTP payload types.
func payloadTypeName(pt uint8) string {
	switch pt {
	case 0:
		return "PCMU/8000"
	case 3:
		return "GSM/8000"
	case 8:
		return "PCMA/8000"
	case 9:
		return "G722/8000"
	case 18:
		return "G729/8000"
	case 96:
		return "H264/90000"
	case 97:
		return "H263/90000"
	case 98:
		return "H263-1998/90000"
	default:
		if pt != 0 {
			return fmt.Sprintf("PT%d", pt)
		}
		return "PCMU/8000"
	}
}

// JSON report structures

type rtpShortReport struct {
	CorrelationID string `json:"CORRELATION_ID"`
	RTPSipCallID  string `json:"RTP_SIP_CALL_ID"`
	SSRC          string `json:"SSRC"`
	PacketCount   uint32 `json:"PACKET_COUNT"`
	OctetCount    uint32 `json:"OCTET_COUNT"`
	ReportName    string `json:"REPORT_NAME"`
	Source        string `json:"SOURCE"`
	Type          string `json:"TYPE"`
	ReportTS      int64  `json:"REPORT_TS"`
	SrcIP         string `json:"SRC_IP"`
	SrcPort       int    `json:"SRC_PORT"`
	DstIP         string `json:"DST_IP"`
	DstPort       int    `json:"DST_PORT"`
	RTPTimestamp  uint32 `json:"RTP_TS"`
	CodecPT       uint8  `json:"CODEC_PT"`
	ClockRate     uint32 `json:"CLOCK"`
	CodecName     string `json:"CODEC_NAME"`
}

type rtcpShortReport struct {
	CorrelationID string `json:"CORRELATION_ID"`
	RTPSipCallID  string `json:"RTP_SIP_CALL_ID"`
	SSRC          string `json:"SSRC"`
	CumPacketLoss uint32 `json:"CUM_PACKET_LOSS"`
	ReportName    string `json:"REPORT_NAME"`
	Source        string `json:"SOURCE"`
	Type          string `json:"TYPE"`
	ReportTS      int64  `json:"REPORT_TS"`
	SrcIP         string `json:"SRC_IP"`
	SrcPort       int    `json:"SRC_PORT"`
	DstIP         string `json:"DST_IP"`
	DstPort       int    `json:"DST_PORT"`
	CodecPT       uint8  `json:"CODEC_PT"`
	ClockRate     uint32 `json:"CLOCK"`
	CodecName     string `json:"CODEC_NAME"`
}

type dtmfReport struct {
	CorrelationID string `json:"CORRELATION_ID"`
	ReportTS      int64  `json:"REPORT_TS"`
	DTMF          string `json:"DTMF"`
	SrcIP         string `json:"SRC_IP"`
	SrcPort       uint16 `json:"SRC_PORT"`
	DstIP         string `json:"DST_IP"`
	DstPort       uint16 `json:"DST_PORT"`
	CodecPT       uint8  `json:"CODEC_PT"`
	CodecName     string `json:"CODEC_NAME"`
	Party         int    `json:"PARTY"`
	SType         string `json:"STYPE"`
	Type          string `json:"TYPE"`
}

// Encode encodes a HEP3 packet.
func Encode(msg Message) ([]byte, error) {
	srcIP, family, err := parseIP(msg.SrcIP)
	if err != nil {
		return nil, fmt.Errorf("invalid HEP source IP: %w", err)
	}
	dstIP, _, err := parseIP(msg.DstIP)
	if err != nil {
		return nil, fmt.Errorf("invalid HEP destination IP: %w", err)
	}
	if msg.SrcPort < 0 || msg.SrcPort > 65535 {
		return nil, fmt.Errorf("invalid HEP source port %d", msg.SrcPort)
	}
	if msg.DstPort < 0 || msg.DstPort > 65535 {
		return nil, fmt.Errorf("invalid HEP destination port %d", msg.DstPort)
	}
	if msg.Time.IsZero() {
		msg.Time = time.Now()
	}

	var chunks bytes.Buffer
	writeChunkUint8(&chunks, chunkIPFamily, family)
	writeChunkUint8(&chunks, chunkIPProto, msg.IPProtocol)
	if family == ipFamilyIPv6 {
		writeChunkBytes(&chunks, chunkIPv6Src, srcIP.To16())
		writeChunkBytes(&chunks, chunkIPv6Dst, dstIP.To16())
	} else {
		writeChunkBytes(&chunks, chunkIPv4Src, srcIP.To4())
		writeChunkBytes(&chunks, chunkIPv4Dst, dstIP.To4())
	}
	writeChunkUint16(&chunks, chunkSrcPort, uint16(msg.SrcPort))
	writeChunkUint16(&chunks, chunkDstPort, uint16(msg.DstPort))
	writeChunkUint32(&chunks, chunkTimeSec, uint32(msg.Time.Unix()))
	writeChunkUint32(&chunks, chunkTimeUsec, uint32(msg.Time.Nanosecond()/1000))
	writeChunkUint8(&chunks, chunkProtoType, msg.ProtoType)
	writeChunkUint32(&chunks, chunkCaptureID, msg.CaptureID)
	if msg.AuthKey != "" {
		writeChunkBytes(&chunks, chunkAuthKey, []byte(msg.AuthKey))
	}
	writeChunkBytes(&chunks, chunkPayload, msg.Payload)

	totalLen := 6 + chunks.Len()
	if totalLen > 65535 {
		return nil, fmt.Errorf("HEP packet too large: %d", totalLen)
	}

	packet := make([]byte, 0, totalLen)
	packet = append(packet, 'H', 'E', 'P', '3')
	packet = binary.BigEndian.AppendUint16(packet, uint16(totalLen))
	packet = append(packet, chunks.Bytes()...)
	return packet, nil
}

// Decode parses a HEP3 packet.
func Decode(packet []byte) (Decoded, error) {
	if len(packet) < 6 {
		return Decoded{}, errors.New("HEP packet too short")
	}
	if string(packet[:4]) != "HEP3" {
		return Decoded{}, errors.New("invalid HEP header")
	}
	totalLen := int(binary.BigEndian.Uint16(packet[4:6]))
	if totalLen != len(packet) {
		return Decoded{}, fmt.Errorf("invalid HEP length %d for %d-byte packet", totalLen, len(packet))
	}

	var out Decoded
	var sec uint32
	var usec uint32
	offset := 6
	for offset < len(packet) {
		if len(packet[offset:]) < 6 {
			return Decoded{}, errors.New("truncated HEP chunk header")
		}
		chunkType := binary.BigEndian.Uint16(packet[offset+2 : offset+4])
		chunkLen := int(binary.BigEndian.Uint16(packet[offset+4 : offset+6]))
		if chunkLen < 6 || offset+chunkLen > len(packet) {
			return Decoded{}, errors.New("invalid HEP chunk length")
		}
		payload := packet[offset+6 : offset+chunkLen]
		switch chunkType {
		case chunkIPFamily:
			if len(payload) == 1 {
				out.IPFamily = payload[0]
			}
		case chunkIPProto:
			if len(payload) == 1 {
				out.IPProtocol = payload[0]
			}
		case chunkIPv4Src, chunkIPv6Src:
			out.SrcIP = append(net.IP(nil), payload...)
		case chunkIPv4Dst, chunkIPv6Dst:
			out.DstIP = append(net.IP(nil), payload...)
		case chunkSrcPort:
			if len(payload) == 2 {
				out.SrcPort = binary.BigEndian.Uint16(payload)
			}
		case chunkDstPort:
			if len(payload) == 2 {
				out.DstPort = binary.BigEndian.Uint16(payload)
			}
		case chunkTimeSec:
			if len(payload) == 4 {
				sec = binary.BigEndian.Uint32(payload)
			}
		case chunkTimeUsec:
			if len(payload) == 4 {
				usec = binary.BigEndian.Uint32(payload)
			}
		case chunkProtoType:
			if len(payload) == 1 {
				out.ProtoType = payload[0]
			}
		case chunkCaptureID:
			if len(payload) == 4 {
				out.CaptureID = binary.BigEndian.Uint32(payload)
			}
		case chunkAuthKey:
			out.AuthKey = string(payload)
		case chunkPayload:
			out.Payload = append([]byte(nil), payload...)
		}
		offset += chunkLen
	}
	out.Time = time.Unix(int64(sec), int64(usec)*1000).UTC()
	return out, nil
}

func parseIP(raw string) (net.IP, uint8, error) {
	ip := net.ParseIP(raw)
	if ip == nil {
		addr, err := net.ResolveIPAddr("ip", raw)
		if err != nil || addr == nil || addr.IP == nil {
			if err == nil {
				err = errors.New("unable to resolve IP")
			}
			return nil, 0, err
		}
		ip = addr.IP
	}
	if v4 := ip.To4(); v4 != nil {
		return v4, ipFamilyIPv4, nil
	}
	if v6 := ip.To16(); v6 != nil {
		return v6, ipFamilyIPv6, nil
	}
	return nil, 0, errors.New("unsupported IP family")
}

func transportProtocol(transport string) uint8 {
	switch transport {
	case "t1", "tn", "l1", "ln", "TCP", "tcp", "TLS", "tls":
		return ipProtoTCP
	default:
		return ipProtoUDP
	}
}

func writeChunkUint8(buf *bytes.Buffer, chunkType uint16, value uint8) {
	writeChunkBytes(buf, chunkType, []byte{value})
}

func writeChunkUint16(buf *bytes.Buffer, chunkType uint16, value uint16) {
	var payload [2]byte
	binary.BigEndian.PutUint16(payload[:], value)
	writeChunkBytes(buf, chunkType, payload[:])
}

func writeChunkUint32(buf *bytes.Buffer, chunkType uint16, value uint32) {
	var payload [4]byte
	binary.BigEndian.PutUint32(payload[:], value)
	writeChunkBytes(buf, chunkType, payload[:])
}

func writeChunkBytes(buf *bytes.Buffer, chunkType uint16, payload []byte) {
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	_ = binary.Write(buf, binary.BigEndian, chunkType)
	_ = binary.Write(buf, binary.BigEndian, uint16(len(payload)+6))
	_, _ = buf.Write(payload)
}
