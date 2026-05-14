//go:build audio && cgo

package media

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/gordonklaus/portaudio"
)

// StartMicrophone captures mono PCM via PortAudio at 8 kHz (or decimated from 16/48/96 kHz) and sends PCMU RTP.
// micInput: empty = default input device; decimal string = PortAudio device index; otherwise substring match on device name.
// Build: CGO_ENABLED=1 go build -tags audio (requires libportaudio + pkg-config portaudio-2.0).
func (s *Session) StartMicrophone(ctx context.Context, endpoint Endpoint, localIP string, localPort int, micInput string) error {
	s.Stop()
	conn, ra, err := dialMicUDP(endpoint, localIP, localPort)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	go runPortaudioMicCapture(childCtx, pw, strings.TrimSpace(micInput))
	return s.attachMicSession(childCtx, cancel, conn, ra, localIP, pr)
}

func pickPortaudioInputDevice(spec string) (*portaudio.DeviceInfo, error) {
	devs, err := portaudio.Devices()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec) == "" {
		return portaudio.DefaultInputDevice()
	}
	if n, err := strconv.Atoi(spec); err == nil && n >= 0 && n < len(devs) {
		d := devs[n]
		if d.MaxInputChannels < 1 {
			return nil, fmt.Errorf("portaudio device %d has no input channels", n)
		}
		return d, nil
	}
	low := strings.ToLower(spec)
	for _, d := range devs {
		if d.MaxInputChannels < 1 {
			continue
		}
		if strings.Contains(strings.ToLower(d.Name), low) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("portaudio: no input device matches %q", spec)
}

func decimateAvg160(in []int16, ratio int) []int16 {
	if ratio < 1 {
		ratio = 1
	}
	if len(in) < 160*ratio {
		return nil
	}
	in = in[:160*ratio]
	if ratio == 1 {
		out := make([]int16, 160)
		copy(out, in)
		return out
	}
	out := make([]int16, 160)
	for i := 0; i < 160; i++ {
		base := i * ratio
		var sum int64
		for j := 0; j < ratio; j++ {
			sum += int64(in[base+j])
		}
		out[i] = int16(sum / int64(ratio))
	}
	return out
}

func nearestIntRatio(sampleRate float64) (ratio int, ok bool) {
	for ratio = 1; ratio <= 24; ratio++ {
		if math.Abs(sampleRate/float64(ratio)-8000) < 1 {
			return ratio, true
		}
	}
	return 0, false
}

func pumpFramesToPCMWriter(ctx context.Context, pw *io.PipeWriter, frames <-chan []int16) {
	defer pw.Close()
	buf := make([]byte, 320)
	for {
		select {
		case <-ctx.Done():
			return
		case fr, ok := <-frames:
			if !ok {
				return
			}
			if len(fr) != 160 {
				continue
			}
			for i, v := range fr {
				binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
			}
			if _, err := pw.Write(buf); err != nil {
				return
			}
		}
	}
}

func runPortaudioMicCapture(ctx context.Context, pw *io.PipeWriter, micSpec string) {
	frames := make(chan []int16, 16)
	go pumpFramesToPCMWriter(ctx, pw, frames)

	if err := portaudio.Initialize(); err != nil {
		close(frames)
		return
	}
	defer func() { _ = portaudio.Terminate() }()

	dev, err := pickPortaudioInputDevice(micSpec)
	if err != nil {
		close(frames)
		return
	}

	rates := []float64{8000, 16000, 48000, 96000, dev.DefaultSampleRate}
	var stream *portaudio.Stream
	for _, sr := range rates {
		if sr <= 0 {
			continue
		}
		ratio, ok := nearestIntRatio(sr)
		if !ok {
			continue
		}
		fpb := 160 * ratio
		p := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   dev,
				Channels: 1,
				Latency:  dev.DefaultLowInputLatency,
			},
			SampleRate:      sr,
			FramesPerBuffer: fpb,
		}
		decRatio := ratio
		st, err := portaudio.OpenStream(p, func(in []int16) {
			out := decimateAvg160(in, decRatio)
			if out == nil {
				return
			}
			select {
			case frames <- out:
			default:
			}
		})
		if err != nil {
			continue
		}
		stream = st
		break
	}
	if stream == nil {
		close(frames)
		return
	}
	if err := stream.Start(); err != nil {
		_ = stream.Close()
		close(frames)
		return
	}
	defer func() {
		_ = stream.Stop()
		_ = stream.Close()
		close(frames)
	}()
	<-ctx.Done()
}
