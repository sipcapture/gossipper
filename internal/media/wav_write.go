package media

import (
	"encoding/binary"
	"fmt"
	"os"
)

// writeWAVPCM16LE writes a PCM WAV file (mono or stereo interleaved int16 LE, 8 kHz).
func writeWAVPCM16LE(path string, sampleRate uint32, channels uint16, samples []int16) error {
	if channels != 1 && channels != 2 {
		return fmt.Errorf("wav: channels must be 1 or 2, got %d", channels)
	}
	if len(samples)%int(channels) != 0 {
		return fmt.Errorf("wav: sample count %d not divisible by channels %d", len(samples), channels)
	}
	dataBytes := len(samples) * 2
	// RIFF + fmt + data headers = 12 + 24 + 8 + dataBytes
	fileSize := uint32(36 + dataBytes)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	write := func(b []byte) error {
		_, e := f.Write(b)
		return e
	}
	if err := write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, fileSize); err != nil {
		return err
	}
	if err := write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	// Subchunk1Size for PCM = 16
	if err := binary.Write(f, binary.LittleEndian, uint32(16)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(1)); err != nil { // audio format PCM
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, channels); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, sampleRate); err != nil {
		return err
	}
	byteRate := sampleRate * uint32(channels) * 2
	if err := binary.Write(f, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	blockAlign := channels * 2
	if err := binary.Write(f, binary.LittleEndian, uint16(blockAlign)); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(16)); err != nil { // bits per sample
		return err
	}
	if err := write([]byte("data")); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(dataBytes)); err != nil {
		return err
	}
	for _, s := range samples {
		if err := binary.Write(f, binary.LittleEndian, s); err != nil {
			return err
		}
	}
	return f.Sync()
}
