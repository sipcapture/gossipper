package pcaplink

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// ResolveDecoder returns the first-layer gopacket decoder for PCAP payloads.
// spec is the user override (CLI -pcap-link / profile): empty or "auto" uses headerLink
// from the file when possible (including DLT 276 LINUX_SLL2, which gopacket's layers.LinkType truncates).
func ResolveDecoder(spec string, headerLink uint32) (gopacket.Decoder, error) {
	s := strings.ToLower(strings.TrimSpace(spec))
	switch s {
	case "", "auto":
		if headerLink == dltLinuxSLL2 {
			return LayerTypeLinuxSLL2, nil
		}
		if headerLink > 255 {
			return nil, fmt.Errorf("pcap link-type %d is not supported in auto mode; set -pcap-link explicitly (e.g. linux_sll2)", headerLink)
		}
		return layers.LinkType(headerLink), nil
	case "ethernet", "en10mb", "1":
		return layers.LinkTypeEthernet, nil
	case "linux_sll", "sll":
		return layers.LinkTypeLinuxSLL, nil
	case "linux_sll2", "sll2":
		return LayerTypeLinuxSLL2, nil
	case "raw", "cooked":
		return layers.LinkTypeRaw, nil
	case "null":
		return layers.LinkTypeNull, nil
	case "loop":
		return layers.LinkTypeLoop, nil
	case "ipv4":
		return layers.LinkTypeIPv4, nil
	case "ipv6":
		return layers.LinkTypeIPv6, nil
	default:
		if n, err := strconv.ParseUint(s, 10, 32); err == nil {
			if n == dltLinuxSLL2 {
				return LayerTypeLinuxSLL2, nil
			}
			if n > 255 {
				return nil, fmt.Errorf("numeric link-type %d: only %d (linux_sll2) is supported above 255", n, dltLinuxSLL2)
			}
			return layers.LinkType(n), nil
		}
		return nil, fmt.Errorf("unknown pcap link layer %q", spec)
	}
}
