package pcaplink

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/gopacket/layers"
)

func TestResolveDecoderAutoSLL2(t *testing.T) {
	dec, err := ResolveDecoder("", dltLinuxSLL2)
	if err != nil {
		t.Fatal(err)
	}
	if dec != LayerTypeLinuxSLL2 {
		t.Fatalf("got %T %v want LayerTypeLinuxSLL2", dec, dec)
	}
}

func TestResolveDecoderExplicit(t *testing.T) {
	cases := []struct {
		spec string
		want layers.LinkType
	}{
		{"ethernet", layers.LinkTypeEthernet},
		{"linux_sll", layers.LinkTypeLinuxSLL},
		{"raw", layers.LinkTypeRaw},
		{"113", layers.LinkTypeLinuxSLL},
	}
	for _, tc := range cases {
		dec, err := ResolveDecoder(tc.spec, 1)
		if err != nil {
			t.Fatalf("%q: %v", tc.spec, err)
		}
		if dec != tc.want {
			t.Fatalf("%q: got %v want %v", tc.spec, dec, tc.want)
		}
	}
}

func TestPeekFileLinkType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.pcap")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var hdr [24]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(hdr[4:6], 2)
	binary.LittleEndian.PutUint16(hdr[6:8], 4)
	binary.LittleEndian.PutUint32(hdr[16:20], 65535)
	binary.LittleEndian.PutUint32(hdr[20:24], dltLinuxSLL2)
	if _, err := f.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	lt, err := PeekFileLinkType(path)
	if err != nil {
		t.Fatal(err)
	}
	if lt != dltLinuxSLL2 {
		t.Fatalf("link type %d, want %d", lt, dltLinuxSLL2)
	}
}
