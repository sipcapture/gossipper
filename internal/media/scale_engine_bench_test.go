package media

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func BenchmarkScaleEngineStreams(b *testing.B) {
	sizes := []int{100, 1000}
	for _, n := range sizes {
		b.Run(streamCountLabel(n), func(b *testing.B) {
			benchmarkScaleEngineN(b, n)
		})
	}
}

func streamCountLabel(n int) string {
	switch n {
	case 100:
		return "100streams"
	case 1000:
		return "1000streams"
	default:
		return "custom"
	}
}

func benchmarkScaleEngineN(b *testing.B, streams int) {
	discard, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		b.Fatalf("ListenUDP: %v", err)
	}
	defer discard.Close()
	dst := discard.LocalAddr().(*net.UDPAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := NewScaleEngine()
	eng.Run(ctx)
	defer eng.Stop()

	cfg := DefaultConfig("")
	cfg.Synthetic = true
	cfg.PacketDuration = 20 * time.Millisecond
	for i := 0; i < streams; i++ {
		callID := fmt.Sprintf("bench-%d", i)
		if err := eng.RegisterStream(ctx, callID, Endpoint{IP: "127.0.0.1", Port: dst.Port}, cfg, "127.0.0.1", 0); err != nil {
			b.Fatalf("RegisterStream %d: %v", i, err)
		}
	}

	b.ResetTimer()
	time.Sleep(2 * time.Second)
	b.StopTimer()
	for i := 0; i < streams; i++ {
		_ = eng.UnregisterCall(fmt.Sprintf("bench-%d", i))
	}
}
