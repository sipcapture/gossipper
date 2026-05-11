package pcaplink

// Linux SLL2 decoder — adapted from hepagent (Google BSD-style license in gopacket/layers).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

const dltLinuxSLL2 = 276

type linuxSLL2PacketType uint16

const (
	linuxSLL2PacketTypeHost      linuxSLL2PacketType = 0
	linuxSLL2PacketTypeBroadcast linuxSLL2PacketType = 1
	linuxSLL2PacketTypeMulticast linuxSLL2PacketType = 2
	linuxSLL2PacketTypeOtherhost linuxSLL2PacketType = 3
	linuxSLL2PacketTypeOutgoing  linuxSLL2PacketType = 4
	linuxSLL2PacketTypeLoopback  linuxSLL2PacketType = 5
	linuxSLL2PacketTypeFastroute linuxSLL2PacketType = 6
)

const (
	ethPIP     = 0x0800
	ethPARP    = 0x0806
	ethPIPv6   = 0x86dd
	ethP8021Q  = 0x8100
	ethP8021AD = 0x88a8
)

type linuxSLL2Decoder struct{}

func (d *linuxSLL2Decoder) Decode(data []byte, p gopacket.PacketBuilder) error {
	return decodeLinuxSLL2(data, p)
}

// LayerTypeLinuxSLL2 is the gopacket decoder for DLT_LINUX_SLL2 (276).
var LayerTypeLinuxSLL2 = gopacket.RegisterLayerType(dltLinuxSLL2, gopacket.LayerTypeMetadata{
	Name:    "LinuxSLL2",
	Decoder: &linuxSLL2Decoder{},
})

type LinuxSLL2 struct {
	layers.BaseLayer
	PacketType linuxSLL2PacketType
	AddrType   uint16
	AddrLen    uint16
	Addr       net.HardwareAddr
	Protocol   uint16
	Interface  uint32
	Payload    []byte
}

func (sll *LinuxSLL2) LayerType() gopacket.LayerType { return LayerTypeLinuxSLL2 }

func (sll *LinuxSLL2) CanDecode() gopacket.LayerClass {
	return LayerTypeLinuxSLL2
}

func (sll *LinuxSLL2) LinkFlow() gopacket.Flow {
	return gopacket.NewFlow(layers.EndpointMAC, sll.Addr, nil)
}

func (sll *LinuxSLL2) NextLayerType() gopacket.LayerType {
	if len(sll.Payload) > 0 {
		version := (sll.Payload[0] >> 4) & 0x0F
		if version == 4 {
			return layers.LayerTypeIPv4
		}
		if version == 6 {
			return layers.LayerTypeIPv6
		}
	}
	switch sll.Protocol {
	case ethPIP:
		return layers.LayerTypeIPv4
	case ethPIPv6:
		return layers.LayerTypeIPv6
	case ethPARP:
		return layers.LayerTypeARP
	case ethP8021Q, ethP8021AD:
		return layers.LayerTypeDot1Q
	}
	return layers.LayerTypeEthernet
}

func (sll *LinuxSLL2) DecodeFromBytes(data []byte, df gopacket.DecodeFeedback) error {
	if len(data) < 20 {
		return errors.New("Linux SLL2 packet too small")
	}
	sll.PacketType = linuxSLL2PacketType(binary.BigEndian.Uint16(data[0:2]))
	sll.AddrType = binary.BigEndian.Uint16(data[2:4])
	sll.AddrLen = binary.BigEndian.Uint16(data[4:6])
	if sll.AddrLen > 8 {
		return fmt.Errorf("invalid address length %d", sll.AddrLen)
	}
	addr := make([]byte, 8)
	copy(addr, data[6:6+sll.AddrLen])
	sll.Addr = net.HardwareAddr(addr)
	sll.Protocol = binary.BigEndian.Uint16(data[14:16])
	sll.Interface = binary.BigEndian.Uint32(data[16:20])
	sll.Payload = data[20:]
	sll.BaseLayer = layers.BaseLayer{Contents: data[:20], Payload: data[20:]}
	return nil
}

func decodeLinuxSLL2(data []byte, p gopacket.PacketBuilder) error {
	sll := &LinuxSLL2{}
	if err := sll.DecodeFromBytes(data, p); err != nil {
		return err
	}
	p.AddLayer(sll)
	p.SetLinkLayer(sll)
	return p.NextDecoder(sll.NextLayerType())
}
