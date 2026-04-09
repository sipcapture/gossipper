package pcap2scenario

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/google/gopacket/tcpassembly"
	"github.com/google/gopacket/tcpassembly/tcpreader"
	"github.com/qxip/gossipper/internal/sip"
)

// ExtractResult holds all data extracted from a PCAP file.
type ExtractResult struct {
	SIPMessages []RawSIPPacket
	UDPPackets  []RawUDPPacket
	LinkType    layers.LinkType
}

// Extract reads a PCAP file and returns all SIP messages (from UDP and TCP)
// together with all raw UDP datagrams (used for RTP filtering in a later pass).
//
// sipPort controls SIP detection:
//   - > 0 : only that port is treated as SIP (both UDP and TCP)
//   - 0   : heuristic detection based on the SIP/2.0 keyword
func Extract(pcapPath string, sipPort int) (ExtractResult, error) {
	f, err := os.Open(pcapPath)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("open %s: %w", pcapPath, err)
	}
	defer f.Close()

	reader, err := pcapgo.NewReader(f)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("read pcap header: %w", err)
	}

	linkType := reader.LinkType()

	var (
		mu       sync.Mutex
		sipMsgs  []RawSIPPacket
		udpPkts  []RawUDPPacket
		msgIndex int
	)

	addSIP := func(pkt RawSIPPacket) {
		mu.Lock()
		pkt.Index = msgIndex
		msgIndex++
		sipMsgs = append(sipMsgs, pkt)
		mu.Unlock()
	}

	// Set up TCP SIP reassembly.
	factory := &sipStreamFactory{sipPort: sipPort, addMsg: addSIP}
	pool := tcpassembly.NewStreamPool(factory)
	assembler := tcpassembly.NewAssembler(pool)

	for {
		data, ci, err := reader.ReadPacketData()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // skip malformed frames
		}

		// Keep a copy of the full frame for RTP mini-PCAP writing.
		rawFrame := make([]byte, len(data))
		copy(rawFrame, data)

		pkt := gopacket.NewPacket(data, linkType, gopacket.NoCopy)
		netLayer := pkt.NetworkLayer()
		if netLayer == nil {
			continue
		}
		netFlow := netLayer.NetworkFlow()
		srcIP := netFlow.Src().String()
		dstIP := netFlow.Dst().String()

		// ── UDP ─────────────────────────────────────────────────────────────
		if udpLayer := pkt.Layer(layers.LayerTypeUDP); udpLayer != nil {
			udp, ok := udpLayer.(*layers.UDP)
			if !ok || len(udp.Payload) == 0 {
				continue
			}
			srcPort := int(udp.SrcPort)
			dstPort := int(udp.DstPort)

			// Retain every UDP packet for the RTP filtering pass.
			udpPkts = append(udpPkts, RawUDPPacket{
				Timestamp: ci.Timestamp,
				SrcIP:     srcIP,
				SrcPort:   srcPort,
				DstIP:     dstIP,
				DstPort:   dstPort,
				RawFrame:  rawFrame,
			})

			// SIP detection.
			isSIP := false
			if sipPort > 0 {
				isSIP = srcPort == sipPort || dstPort == sipPort
			} else {
				isSIP = looksLikeSIP(udp.Payload)
			}
			if isSIP {
				if msg, parseErr := sip.Parse(udp.Payload); parseErr == nil &&
					(msg.Method != "" || msg.StatusCode > 0) {
					addSIP(RawSIPPacket{
						Timestamp: ci.Timestamp,
						SrcIP:     srcIP, SrcPort: srcPort,
						DstIP: dstIP, DstPort: dstPort,
						Message: msg,
					})
				}
			}
			continue
		}

		// ── TCP ─────────────────────────────────────────────────────────────
		if tcpLayer := pkt.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp, ok := tcpLayer.(*layers.TCP)
			if !ok {
				continue
			}
			// Skip streams that cannot carry SIP when a port is configured.
			if sipPort > 0 &&
				int(tcp.SrcPort) != sipPort &&
				int(tcp.DstPort) != sipPort {
				continue
			}
			assembler.AssembleWithTimestamp(netFlow, tcp, ci.Timestamp)
		}
	}

	// Flush remaining TCP stream data and wait for goroutines.
	assembler.FlushAll()
	factory.wg.Wait()

	return ExtractResult{
		SIPMessages: sipMsgs,
		UDPPackets:  udpPkts,
		LinkType:    linkType,
	}, nil
}

// looksLikeSIP returns true if the UDP payload starts with a SIP method or
// a SIP status line — a fast heuristic to avoid parsing non-SIP datagrams.
func looksLikeSIP(data []byte) bool {
	if len(data) < 7 {
		return false
	}
	n := 20
	if len(data) < n {
		n = len(data)
	}
	prefix := string(data[:n])
	for _, kw := range []string{
		"INVITE ", "ACK ", "BYE ", "CANCEL ",
		"OPTIONS ", "REGISTER ", "NOTIFY ",
		"SUBSCRIBE ", "PUBLISH ", "REFER ",
		"INFO ", "UPDATE ", "PRACK ",
		"SIP/2.0 ",
	} {
		if strings.HasPrefix(prefix, kw) {
			return true
		}
	}
	return false
}

// ── TCP SIP stream ────────────────────────────────────────────────────────────

// sipStreamFactory creates a sipTCPStream goroutine for each new TCP stream.
type sipStreamFactory struct {
	wg      sync.WaitGroup
	sipPort int
	addMsg  func(RawSIPPacket)
}

type sipTCPStream struct {
	net, transport gopacket.Flow
	r              tcpreader.ReaderStream
	factory        *sipStreamFactory
}

// New implements tcpassembly.StreamFactory.
func (f *sipStreamFactory) New(net, transport gopacket.Flow) tcpassembly.Stream {
	s := &sipTCPStream{
		net:       net,
		transport: transport,
		r:         tcpreader.NewReaderStream(),
		factory:   f,
	}
	f.wg.Add(1)
	go s.run()
	return &s.r
}

func (s *sipTCPStream) run() {
	defer s.factory.wg.Done()

	// Derive network addresses from the gopacket flows.
	srcIP := s.net.Src().String()
	dstIP := s.net.Dst().String()
	// TCP/UDP port endpoint raw bytes are 2-byte big-endian.
	srcPort := int(binary.BigEndian.Uint16(s.transport.Src().Raw()))
	dstPort := int(binary.BigEndian.Uint16(s.transport.Dst().Raw()))

	reader := bufio.NewReader(&s.r)
	for {
		msg, err := sip.ReadMessage(reader)
		if err != nil {
			// EOF or hard error — stream is done.
			return
		}
		if msg.Method == "" && msg.StatusCode == 0 {
			continue
		}
		s.factory.addMsg(RawSIPPacket{
			SrcIP: srcIP, SrcPort: srcPort,
			DstIP: dstIP, DstPort: dstPort,
			Message: msg,
		})
	}
}
