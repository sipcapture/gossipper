package pcaplink

import (
	"encoding/binary"
	"io"
)

const (
	writeMagicMicroseconds = 0xa1b2c3d4
	writeVersionMajor      = 2
	writeVersionMinor      = 4
)

// WriteMicrosecondsFileHeader writes a classic PCAP 2.4 global header with microsecond
// timestamps and the given full 32-bit link-type (DLT). layers.LinkType is uint8 in
// gopacket v1.1.19 and cannot represent DLT_LINUX_SLL2 (276) for writers.
func WriteMicrosecondsFileHeader(w io.Writer, snaplen, linkType uint32) error {
	var buf [24]byte
	binary.LittleEndian.PutUint32(buf[0:4], writeMagicMicroseconds)
	binary.LittleEndian.PutUint16(buf[4:6], writeVersionMajor)
	binary.LittleEndian.PutUint16(buf[6:8], writeVersionMinor)
	// 8:12 timezone, 12:16 sigfigs — zero
	binary.LittleEndian.PutUint32(buf[16:20], snaplen)
	binary.LittleEndian.PutUint32(buf[20:24], linkType)
	_, err := w.Write(buf[:])
	return err
}
