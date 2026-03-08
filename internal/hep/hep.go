package hep

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	ProtocolSIP = 0x01

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
)

type Config struct {
	Addr      string
	CaptureID uint32
	Password  string
}

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

type Client struct {
	conn *net.UDPConn
	addr *net.UDPAddr

	captureID uint32
	password  string
}

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
	return &Client{
		conn:      conn,
		addr:      addr,
		captureID: cfg.CaptureID,
		password:  cfg.Password,
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

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
