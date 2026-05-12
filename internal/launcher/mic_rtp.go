package launcher

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/sipcapture/gossipper/internal/media"
)

// RunMicRTP streams mono little-endian s16 PCM from stdin as RTP G.711
// (PCMU or PCMA) to the given UDP endpoint. Input sample rate must match the
// codec clock (e.g. 8000 Hz for PCMU/8000). One RTP packet is sent each
// PacketDuration tick; frame size is cfg.SamplesPerPkt samples (default 160
// for 20 ms @ 8 kHz).
//
// Typical PulseAudio capture:
//
//	parec --format=s16le --rate=8000 --channels=1 | gossipper mic-rtp -addr HOST:PORT
func RunMicRTP(ctx context.Context, addr, codec, localIP string, rtpPT int, freqMs int) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("mic-rtp: invalid -addr %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("mic-rtp: invalid port in %q", addr)
	}

	endpoint := media.Endpoint{IP: host, Port: port}
	cfg := media.DefaultConfig("")
	cfg.Synthetic = false
	if codec != "" {
		media.ApplyPayloadParams(&cfg, codec)
	}
	if rtpPT > 0 {
		cfg.PayloadType = uint8(rtpPT)
	}
	if freqMs > 0 {
		cfg.PacketDuration = time.Duration(freqMs) * time.Millisecond
	}

	n := int(cfg.SamplesPerPkt)
	if n <= 0 {
		n = 160
	}
	pcmFrame := make([]byte, n*2)

	nextPayload := func() ([]byte, error) {
		if _, err := io.ReadFull(os.Stdin, pcmFrame); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, io.EOF
			}
			return nil, err
		}
		samples := make([]int16, n)
		for i := 0; i < n; i++ {
			samples[i] = int16(binary.LittleEndian.Uint16(pcmFrame[i*2:]))
		}
		out := media.EncodeG711Frame(cfg.PayloadType, samples)
		if len(out) == 0 {
			return nil, fmt.Errorf("mic-rtp: unsupported payload type %d (use PCMU=0 or PCMA=8)", cfg.PayloadType)
		}
		return out, nil
	}

	sess := media.NewSession()
	if err := sess.StartFuncStreaming(ctx, endpoint, cfg, localIP, 0, nextPayload); err != nil {
		return fmt.Errorf("mic-rtp: %w", err)
	}

	fmt.Fprintf(os.Stderr, "mic-rtp: streaming %s to %s (pt=%d samples/pkt=%d)\n",
		cfg.PayloadName, addr, cfg.PayloadType, n)

	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		if !sess.IsRunning() {
			break
		}
		select {
		case <-ctx.Done():
			sess.Stop()
		case <-tick.C:
		}
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	st := sess.Snapshot()
	fmt.Fprintf(os.Stderr, "mic-rtp: done sent=%d recv=%d\n", st.RTPPacketsSent, st.RTPPacketsReceived)
	return nil
}
