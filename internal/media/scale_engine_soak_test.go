package media

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"
)

func TestScaleEngineSoakStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak in -short mode")
	}
	streamCounts := []int{100, 1000}
	for _, n := range streamCounts {
		t.Run(fmt.Sprintf("%dstreams", n), func(t *testing.T) {
			soakScaleStreams(t, n, 2*time.Second)
		})
	}
}

func soakScaleStreams(t *testing.T, streams int, dur time.Duration) {
	t.Helper()
	recvConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer recvConn.Close()
	dst := recvConn.LocalAddr().(*net.UDPAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := NewScaleEngine()
	eng.Run(ctx)
	defer eng.Stop()

	cfg := DefaultConfig("")
	cfg.Synthetic = true
	cfg.PacketDuration = 20 * time.Millisecond
	for i := 0; i < streams; i++ {
		callID := fmt.Sprintf("soak-%d", i)
		if err := eng.RegisterStream(ctx, callID, Endpoint{IP: "127.0.0.1", Port: dst.Port}, cfg, "127.0.0.1", 0); err != nil {
			t.Fatalf("RegisterStream %d: %v", i, err)
		}
	}
	if eng.StreamCount() != streams {
		t.Fatalf("StreamCount=%d want %d", eng.StreamCount(), streams)
	}

	startG := runtime.NumGoroutine()
	time.Sleep(dur)
	endG := runtime.NumGoroutine()

	var total Stats
	for i := 0; i < streams; i++ {
		st := eng.UnregisterCall(fmt.Sprintf("soak-%d", i))
		total.RTPPacketsSent += st.RTPPacketsSent
	}

	wantMin := uint32(streams) * uint32(dur/(20*time.Millisecond)) * 8 / 10 // 80% of expected
	if total.RTPPacketsSent < wantMin {
		t.Fatalf("packets sent %d below %d (80%% of nominal)", total.RTPPacketsSent, wantMin)
	}
	if endG-startG > 200 {
		t.Fatalf("goroutine growth %d->%d exceeds 200", startG, endG)
	}
	t.Logf("streams=%d packets=%d goroutines %d->%d", streams, total.RTPPacketsSent, startG, endG)
}
