package pcaplink

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	magicMicroseconds          = 0xa1b2c3d4
	magicMicrosecondsBigendian = 0xd4c3b2a1
	magicNanoseconds           = 0xa1b23c4d
	magicNanosecondsBigendian  = 0x4d3cb2a1
	magicGzip1                 = 0x1f
	magicGzip2                 = 0x8b
)

// PeekFileLinkType reads the PCAP global header and returns the link-type field
// (full 32-bit DLT value). gzip-compressed PCAP is supported.
func PeekFileLinkType(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	peek2, err := br.Peek(2)
	if err != nil {
		return 0, err
	}
	r := io.Reader(br)
	if len(peek2) >= 2 && peek2[0] == magicGzip1 && peek2[1] == magicGzip2 {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return 0, err
		}
		defer gz.Close()
		r = gz
	}

	var buf [24]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}

	magic := binary.LittleEndian.Uint32(buf[0:4])
	var order binary.ByteOrder
	switch magic {
	case magicNanoseconds:
		order = binary.LittleEndian
	case magicNanosecondsBigendian:
		order = binary.BigEndian
	case magicMicroseconds:
		order = binary.LittleEndian
	case magicMicrosecondsBigendian:
		order = binary.BigEndian
	default:
		return 0, fmt.Errorf("unknown pcap magic %08x", magic)
	}
	return order.Uint32(buf[20:24]), nil
}
