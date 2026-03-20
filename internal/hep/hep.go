package hep

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	ProtocolSIP        = 0x01
	ProtocolRTCP       = 0x05 // raw RTCP binary (type 5)
	ProtocolRTPReport  = 0x22 // Full RTP Report JSON = 34 (TypeFullRTPReport in hepagent-go)
	ProtocolRTCPReport = 0x24 // Full RTCP Report JSON = 36 (TypeFullRTCPReport in hepagent-go)
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
	chunkCaptureID      = 0x000c
	chunkAuthKey        = 0x000e
	chunkPayload        = 0x000f
	chunkCorrelationID  = 0x0011

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
	Time          time.Time
	SrcIP         string
	DstIP         string
	SrcPort       int
	DstPort       int
	IPProtocol    uint8
	ProtoType     uint8
	CaptureID     uint32
	AuthKey       string
	CorrelationID string
	Payload       []byte
}

// Decoded is a parsed HEP message.
type Decoded struct {
	IPFamily      uint8
	IPProtocol    uint8
	SrcIP         net.IP
	DstIP         net.IP
	SrcPort       uint16
	DstPort       uint16
	Time          time.Time
	ProtoType     uint8
	CaptureID     uint32
	AuthKey       string
	CorrelationID string
	Payload       []byte
}

// rtpStreamState tracks per-SSRC RTP/RTCP statistics for aggregated reporting.
type rtpStreamState struct {
	packetCount uint32
	octetCount  uint32
	lastSeen    time.Time
	reportStart time.Time // time of first packet in current period

	srcIP   string
	srcPort int
	dstIP   string
	dstPort int
	callID  string

	// RTP header fields (from last packet)
	lastRTPTimestamp uint32
	prevRTPTimestamp uint32 // RTP timestamp of previous packet for jitter calc
	lastSeq          uint16
	lastArrivalTime  time.Time
	firstPacket      bool // true until second packet arrives
	mediaPT          uint8 // payload type for media (PCMU, PCMA, etc.)

	// per-period jitter/delta/skew accumulators (RFC 3550 interarrival jitter)
	jitter     float64 // current interarrival jitter estimate (RFC 3550)
	maxJitter  float64
	sumJitter  float64
	sumDelta   float64 // sum of inter-arrival deltas (ms) for mean calc
	deltaCount uint32
	maxDelta   float64 // max inter-packet arrival delta (ms)
	maxSkew    float64
	outOrder   uint32

	// RTCP SR fields (populated via SendRTCP in JSON mode)
	ntpMSW     uint32
	ntpLSW     uint32
	packetLoss uint32

	// DTMF accumulation (JSON mode only)
	dtmfEvents []string
	dtmfPT     uint8 // payload type for telephone-event (usually 101)

	// cumulative (global) stats — accumulated across all periodic periods for FINAL report
	globalPacketCount uint32
	globalOctetCount  uint64
	globalPacketLoss  uint32
	globalMaxJitter   float64
	globalSumJitter   float64
	globalJitterCount uint32
	globalMaxDelta    float64
	globalMaxSkew     float64
	globalMinMOS      float64
	globalSumMOS      float64
	globalMOSCount    uint32
	globalMinRFactor  float64
	globalSumRFactor  float64
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
// callID is the SIP Call-ID used as correlation_id in the HEP header (chunk 0x0011).
func (c *Client) SendSIP(now time.Time, srcIP string, srcPort int, dstIP string, dstPort int, transport string, callID string, payload []byte) error {
	if c == nil {
		return nil
	}
	packet, err := Encode(Message{
		Time:          now,
		SrcIP:         srcIP,
		DstIP:         dstIP,
		SrcPort:       srcPort,
		DstPort:       dstPort,
		IPProtocol:    transportProtocol(transport),
		ProtoType:     ProtocolSIP,
		CaptureID:     c.captureID,
		AuthKey:       c.password,
		CorrelationID: callID,
		Payload:       payload,
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
	seq := binary.BigEndian.Uint16(payload[2:4])
	rtpTS := binary.BigEndian.Uint32(payload[4:8])
	ssrc := binary.BigEndian.Uint32(payload[8:12])

	c.mu.Lock()
	state, ok := c.rtpStreams[ssrc]
	if !ok {
		state = &rtpStreamState{
			srcIP:       srcIP,
			srcPort:     srcPort,
			dstIP:       dstIP,
			dstPort:     dstPort,
			callID:      callID,
			dtmfPT:      pt,
			reportStart: now,
		}
		c.rtpStreams[ssrc] = state
	} else if callID != "" {
		state.callID = callID
	}

	state.packetCount++
	state.octetCount += uint32(len(payload) - 12)

	// Compute inter-arrival delta and RFC 3550 jitter estimate.
	if !state.lastArrivalTime.IsZero() {
		deltaSec := now.Sub(state.lastArrivalTime).Seconds()
		deltaMS := deltaSec * 1000

		state.sumDelta += deltaMS
		state.deltaCount++
		if deltaMS > state.maxDelta {
			state.maxDelta = deltaMS
		}

		// RFC 3550 §6.4.1 interarrival jitter: D(i,j) = |transit(j) - transit(i)|
		clockRate := payloadTypeClockRate(pt)
		if clockRate == 0 {
			clockRate = 8000
		}
		transitDiff := deltaSec - float64(int32(rtpTS-state.prevRTPTimestamp))/float64(clockRate)
		if transitDiff < 0 {
			transitDiff = -transitDiff
		}
		d := transitDiff * 1000
		state.jitter += (d - state.jitter) / 16.0
		if state.jitter > state.maxJitter {
			state.maxJitter = state.jitter
		}
		state.sumJitter += state.jitter

		// Out-of-order: seq wrapped correctly with int16 arithmetic.
		if !state.firstPacket && int16(seq-state.lastSeq) < 0 {
			state.outOrder++
		}
	} else {
		state.firstPacket = true
	}
	state.firstPacket = false

	state.prevRTPTimestamp = state.lastRTPTimestamp
	state.lastRTPTimestamp = rtpTS
	state.lastArrivalTime = now
	state.lastSeq = seq
	state.lastSeen = now

	if pt != 101 {
		state.mediaPT = pt
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
			Time:          now,
			SrcIP:         srcIP,
			DstIP:         dstIP,
			SrcPort:       srcPort,
			DstPort:       dstPort,
			IPProtocol:    ipProtoUDP,
			ProtoType:     ProtocolRTCP,
			CaptureID:     c.captureID,
			AuthKey:       c.password,
			CorrelationID: callID,
			Payload:       rawPayload,
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
		c.sendPeriodReportLocked(now, ssrc, state, "PERIODIC")
	}
}

// sendPeriodReportLocked sends RTP + RTCP short reports for one period interval.
// reportType is "PERIODIC" or "HANGUP". Must be called with c.mu held.
// It resets per-period accumulators and updates cumulative (global) stats.
func (c *Client) sendPeriodReportLocked(now time.Time, ssrc uint32, state *rtpStreamState, reportType string) {
	codecName, codecChannel := payloadTypeNameChannel(state.mediaPT)
	clockRate := payloadTypeClockRate(state.mediaPT)

	meanJitter := 0.0
	if state.packetCount > 1 {
		meanJitter = state.sumJitter / float64(state.packetCount-1)
	}

	meanDelta := 0.0
	if state.deltaCount > 0 {
		meanDelta = state.sumDelta / float64(state.deltaCount)
	}

	mos, rfactor := calculateMOS(state.packetLoss, meanJitter)

	reportStart := uint32(state.reportStart.Unix())
	reportEnd := uint32(now.Unix())

	// RTP Report (type 34) — matches hepagent.go RTPReport field set
	rpt := rtpReport{
		CorrelationID: state.callID,
		RTPSipCallID:  state.callID,
		Delta:         round3(meanDelta),
		Jitter:        round3(state.jitter),
		ReportTS:      now.UnixMicro(),
		TLByte:        uint64(state.octetCount),
		Skew:          round3(state.maxSkew),
		TotalPK:       state.packetCount,
		ExpectedPK:    state.packetCount,
		PacketLoss:    state.packetLoss,
		Seq:           0,
		MaxJitter:     round3(state.maxJitter),
		MaxDelta:      round3(state.maxDelta),
		MaxSkew:       round3(state.maxSkew),
		MeanJitter:    round3(meanJitter),
		MinMOS:        round3(mos),
		MeanMOS:       round3(mos),
		MOS:           round3(mos),
		MaxMOS:        round3(mos),
		RFactor:       round3(rfactor),
		MinRFactor:    round3(rfactor),
		MeanRFactor:   round3(rfactor),
		SrcIP:         state.srcIP,
		SrcPort:       state.srcPort,
		DstIP:         state.dstIP,
		DstPort:       state.dstPort,
		OutOrder:      state.outOrder,
		SSRC:          fmt.Sprintf("0x%x", ssrc),
		SSRCChg:       0,
		CodecPT:       state.mediaPT,
		ClockRate:     clockRate,
		CodecName:     codecName,
		CodecChannel:  codecChannel,
		Dir:           0,
		OneWayRTP:     0,
		ReportName:    fmt.Sprintf("%s-%d", state.srcIP, state.srcPort),
		Party:         0,
		SType:         "GOSSIPPER",
		Type:          reportType,
		ReportStart:   reportStart,
		ReportEnd:     reportEnd,
	}
	if jsonData, err := json.Marshal(rpt); err == nil {
		c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolRTPReport, state.callID, jsonData)
	}

	// RTCP Short Report (type 37) — matches hepagent.go RTCPShortReport
	rtcpRpt := rtcpShortReport{
		CorrelationID:  state.callID,
		RTPSipCallID:   state.callID,
		MOS:            round3(mos),
		RFactor:        round3(rfactor),
		Dir:            0,
		ReportName:     fmt.Sprintf("%s-%d", state.srcIP, state.srcPort),
		Party:          0,
		SSRC:           fmt.Sprintf("0x%x", ssrc),
		CumPacketLoss:  state.packetLoss,
		MeanPacketLoss: 0,
		Source:         "GOSSIPPER",
		Type:           reportType,
		ReportTS:       now.UnixMilli(),
		SrcIP:          state.srcIP,
		SrcPort:        state.srcPort,
		DstIP:          state.dstIP,
		DstPort:        state.dstPort,
		CodecPT:        state.mediaPT,
		ClockRate:      clockRate,
		CodecName:      codecName,
	}
	if jsonData, err := json.Marshal(rtcpRpt); err == nil {
		c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolRTCPReport, state.callID, jsonData)
	}

	// Accumulate into global (cumulative) stats for the FINAL report.
	state.globalPacketCount += state.packetCount
	state.globalOctetCount += uint64(state.octetCount)
	state.globalPacketLoss += state.packetLoss
	if state.maxJitter > state.globalMaxJitter {
		state.globalMaxJitter = state.maxJitter
	}
	state.globalSumJitter += meanJitter
	state.globalJitterCount++
	if state.maxDelta > state.globalMaxDelta {
		state.globalMaxDelta = state.maxDelta
	}
	if state.maxSkew > state.globalMaxSkew {
		state.globalMaxSkew = state.maxSkew
	}
	if state.globalMOSCount == 0 || mos < state.globalMinMOS {
		state.globalMinMOS = mos
	}
	state.globalSumMOS += mos
	state.globalMOSCount++
	if state.globalMOSCount == 1 || rfactor < state.globalMinRFactor {
		state.globalMinRFactor = rfactor
	}
	state.globalSumRFactor += rfactor

	// Reset per-period accumulators; preserve stream identity and global fields.
	state.packetCount = 0
	state.octetCount = 0
	state.jitter = 0
	state.maxJitter = 0
	state.sumJitter = 0
	state.sumDelta = 0
	state.deltaCount = 0
	state.maxDelta = 0
	state.maxSkew = 0
	state.outOrder = 0
	state.reportStart = now

	// DTMF Report (type 100) — only if events were accumulated.
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
			Type:          reportType,
		}
		if jsonData, err := json.Marshal(dtmfRpt); err == nil {
			c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolDTMF, state.callID, jsonData)
		}
		state.dtmfEvents = state.dtmfEvents[:0]
	}
}

// SendFinalReports flushes HANGUP and FINAL RTP reports for all streams matching callID.
// It is called when a call ends (SIP BYE / session teardown).
// In rawRTCP mode only a raw RTCP SR is sent; FINAL JSON is JSON-mode only.
func (c *Client) SendFinalReports(callID string) {
	if c == nil || !c.sendMediaReport || c.rawRTCP {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()

	for ssrc, state := range c.rtpStreams {
		if state.callID != callID {
			continue
		}

		// Send HANGUP report for the last (possibly incomplete) period.
		if state.packetCount > 0 {
			c.sendPeriodReportLocked(now, ssrc, state, "HANGUP")
		}

		// Build FINAL report only if there were any packets across all periods.
		if state.globalPacketCount == 0 {
			delete(c.rtpStreams, ssrc)
			continue
		}

		codecName, codecChannel := payloadTypeNameChannel(state.mediaPT)
		clockRate := payloadTypeClockRate(state.mediaPT)

		meanMOS := state.globalMinMOS
		meanRFactor := state.globalMinRFactor
		if state.globalMOSCount > 0 {
			meanMOS = state.globalSumMOS / float64(state.globalMOSCount)
			meanRFactor = state.globalSumRFactor / float64(state.globalMOSCount)
		}
		meanJitter := 0.0
		if state.globalJitterCount > 0 {
			meanJitter = state.globalSumJitter / float64(state.globalJitterCount)
		}

		final := rtpFinalReport{
			CorrelationID: state.callID,
			RTPSipCallID:  state.callID,
			TLByte:        state.globalOctetCount,
			TotalPK:       state.globalPacketCount,
			ExpectedPK:    state.globalPacketCount,
			PacketLoss:    state.globalPacketLoss,
			MinRFactor:    round3(state.globalMinRFactor),
			MeanRFactor:   round3(meanRFactor),
			MaxSkew:       round3(state.globalMaxSkew),
			MaxDelta:      round3(state.globalMaxDelta),
			MeanJitter:    round3(meanJitter),
			MaxJitter:     round3(state.globalMaxJitter),
			MinMOS:        round3(state.globalMinMOS),
			MeanMOS:       round3(meanMOS),
			CodecPT:       state.mediaPT,
			ClockRate:     clockRate,
			CodecName:     codecName,
			CodecChannel:  codecChannel,
			SSRC:          fmt.Sprintf("0x%x", ssrc),
			ReportName:    fmt.Sprintf("%s-%d", state.srcIP, state.srcPort),
			SType:         "GOSSIPPER",
			Type:          "FINAL",
		}
		if jsonData, err := json.Marshal(final); err == nil {
			c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolRTPReport, state.callID, jsonData)
		}

		delete(c.rtpStreams, ssrc)
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
		c.sendHEP(now, state.srcIP, state.srcPort, state.dstIP, state.dstPort, ProtocolRTCP, state.callID, sr)
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
func (c *Client) sendHEP(now time.Time, srcIP string, srcPort int, dstIP string, dstPort int, protoType uint8, correlationID string, payload []byte) {
	packet, err := Encode(Message{
		Time:          now,
		SrcIP:         srcIP,
		DstIP:         dstIP,
		SrcPort:       srcPort,
		DstPort:       dstPort,
		IPProtocol:    ipProtoUDP,
		ProtoType:     protoType,
		CaptureID:     c.captureID,
		AuthKey:       c.password,
		CorrelationID: correlationID,
		Payload:       payload,
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

// payloadTypeNameChannel returns the codec name and channel count for common RTP payload types.
func payloadTypeNameChannel(pt uint8) (string, uint8) {
	switch pt {
	case 0:
		return "PCMU/8000", 1
	case 3:
		return "GSM/8000", 1
	case 8:
		return "PCMA/8000", 1
	case 9:
		return "G722/8000", 1
	case 18:
		return "G729/8000", 1
	case 96:
		return "H264/90000", 1
	case 97:
		return "H263/90000", 1
	case 98:
		return "H263-1998/90000", 1
	default:
		if pt != 0 {
			return fmt.Sprintf("PT%d", pt), 1
		}
		return "PCMU/8000", 1
	}
}

// JSON report structures

// calculateMOS computes MOS and R-Factor from packet loss (count) and mean jitter (ms)
// using a simplified E-Model (ITU-T G.107).
func calculateMOS(lostPkts uint32, meanJitterMS float64) (mos, rfactor float64) {
	lossRate := 0.0
	if lostPkts > 0 {
		lossRate = float64(lostPkts) * 100.0
	}
	// Simplified R-Factor: R0=93.2, penalty for jitter and loss.
	r := 93.2 - (meanJitterMS * 0.1) - (lossRate * 2.5)
	if r < 0 {
		r = 0
	}
	if r > 100 {
		r = 100
	}
	var m float64
	if r < 0 {
		m = 1.0
	} else if r > 100 {
		m = 4.5
	} else {
		m = 1 + 0.035*r + r*(r-60)*(100-r)*7e-6
	}
	if m < 1.0 {
		m = 1.0
	}
	if m > 4.5 {
		m = 4.5
	}
	return m, r
}

// round3 rounds a float64 to 3 decimal places.
func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// rtpReport matches hepagent.go RTPReport (HEP type 35).
type rtpReport struct {
	CorrelationID string  `json:"CORRELATION_ID"`
	RTPSipCallID  string  `json:"RTP_SIP_CALL_ID"`
	Delta         float64 `json:"DELTA"`
	Jitter        float64 `json:"JITTER"`
	ReportTS      int64   `json:"REPORT_TS"`
	TLByte        uint64  `json:"TL_BYTE"`
	Skew          float64 `json:"SKEW"`
	TotalPK       uint32  `json:"TOTAL_PK"`
	ExpectedPK    uint32  `json:"EXPECTED_PK"`
	PacketLoss    uint32  `json:"PACKET_LOSS"`
	Seq           uint32  `json:"SEQ"`
	MaxJitter     float64 `json:"MAX_JITTER"`
	MaxDelta      float64 `json:"MAX_DELTA"`
	MaxSkew       float64 `json:"MAX_SKEW"`
	MeanJitter    float64 `json:"MEAN_JITTER"`
	MinMOS        float64 `json:"MIN_MOS"`
	MeanMOS       float64 `json:"MEAN_MOS"`
	MOS           float64 `json:"MOS"`
	MaxMOS        float64 `json:"MAX_MOS"`
	RFactor       float64 `json:"RFACTOR"`
	MinRFactor    float64 `json:"MIN_RFACTOR"`
	MeanRFactor   float64 `json:"MEAN_RFACTOR"`
	SrcIP         string  `json:"SRC_IP"`
	SrcPort       int     `json:"SRC_PORT"`
	DstIP         string  `json:"DST_IP"`
	DstPort       int     `json:"DST_PORT"`
	OutOrder      uint32  `json:"OUT_ORDER"`
	SSRC          string  `json:"SSRC"`
	SSRCChg       uint8   `json:"SSRC_CHG"`
	CodecPT       uint8   `json:"CODEC_PT"`
	ClockRate     uint32  `json:"CLOCK"`
	CodecName     string  `json:"CODEC_NAME"`
	CodecChannel  uint8   `json:"CODEC_CHANNEL"`
	Dir           uint8   `json:"DIR"`
	OneWayRTP     uint8   `json:"ONE_WAY_RTP"`
	ReportName    string  `json:"REPORT_NAME"`
	Party         uint8   `json:"PARTY"`
	SType         string  `json:"STYPE"`
	Type          string  `json:"TYPE"`
	ReportStart   uint32  `json:"REPORT_START"`
	ReportEnd     uint32  `json:"REPORT_END"`
}

// rtcpShortReport matches hepagent.go RTCPShortReport (HEP type 37).
type rtcpShortReport struct {
	CorrelationID  string  `json:"CORRELATION_ID"`
	RTPSipCallID   string  `json:"RTP_SIP_CALL_ID"`
	MOS            float64 `json:"MOS"`
	RFactor        float64 `json:"RFACTOR"`
	Dir            int     `json:"DIR"`
	ReportName     string  `json:"REPORT_NAME"`
	Party          int     `json:"PARTY"`
	SSRC           string  `json:"SSRC"`
	CumPacketLoss  uint32  `json:"CUM_PACKET_LOSS"`
	MeanPacketLoss float64 `json:"MEAN_PACKET_LOSS"`
	Source         string  `json:"SOURCE"`
	Type           string  `json:"TYPE"`
	ReportTS       int64   `json:"REPORT_TS"`
	SrcIP          string  `json:"SRC_IP"`
	SrcPort        int     `json:"SRC_PORT"`
	DstIP          string  `json:"DST_IP"`
	DstPort        int     `json:"DST_PORT"`
	CodecPT        uint8   `json:"CODEC_PT"`
	ClockRate      uint32  `json:"CLOCK"`
	CodecName      string  `json:"CODEC_NAME"`
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

// rtpFinalReport matches hepagent.go RTPFinalReport — cumulative stats over the entire call.
// Sent once at call teardown with TYPE="FINAL" using ProtocolRTPReport (HEP type 34).
type rtpFinalReport struct {
	CorrelationID string  `json:"CORRELATION_ID"`
	RTPSipCallID  string  `json:"RTP_SIP_CALL_ID"`
	TLByte        uint64  `json:"TOTAL_BYTES"`
	TotalPK       uint32  `json:"TOTAL_PACKETS"`
	ExpectedPK    uint32  `json:"TOTAL_EXPECTED_PK"`
	PacketLoss    uint32  `json:"TOTAL_PACKET_LOSS"`
	MinRFactor    float64 `json:"MIN_RFACTOR"`
	MeanRFactor   float64 `json:"MEAN_RFACTOR"`
	MaxSkew       float64 `json:"MAX_SKEW"`
	MaxDelta      float64 `json:"MAX_DELTA"`
	MeanJitter    float64 `json:"MEAN_JITTER"`
	MaxJitter     float64 `json:"MAX_JITTER"`
	MinMOS        float64 `json:"MIN_MOS"`
	MeanMOS       float64 `json:"MEAN_MOS"`
	OneWayRTP     uint8   `json:"ONE_WAY_RTP"`
	CodecChg      uint8   `json:"CODEC_CH"`
	CodecPT       uint8   `json:"CODEC_PT"`
	ClockRate     uint32  `json:"CLOCK"`
	CodecName     string  `json:"CODEC_NAME"`
	CodecChannel  uint8   `json:"CODEC_CHANNEL"`
	SSRC          string  `json:"SSRC"`
	ReportName    string  `json:"REPORT_NAME"`
	Dir           uint8   `json:"DIR"`
	Party         uint8   `json:"PARTY"`
	SType         string  `json:"STYPE"`
	Type          string  `json:"TYPE"`
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
	if msg.CorrelationID != "" {
		writeChunkBytes(&chunks, chunkCorrelationID, []byte(msg.CorrelationID))
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
		case chunkCorrelationID:
			out.CorrelationID = string(payload)
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
