package media

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestScaleEngineRegisterAndSend(t *testing.T) {
	recvConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP recv: %v", err)
	}
	defer recvConn.Close()
	recvAddr := recvConn.LocalAddr().(*net.UDPAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := NewScaleEngine()
	eng.Run(ctx)
	defer eng.Stop()

	cfg := DefaultConfig("")
	cfg.Synthetic = true
	cfg.PacketDuration = 20 * time.Millisecond
	endpoint := Endpoint{IP: "127.0.0.1", Port: recvAddr.Port}
	if err := eng.RegisterStream(ctx, "call-1", endpoint, cfg, "127.0.0.1", 0); err != nil {
		t.Fatalf("RegisterStream: %v", err)
	}
	if eng.StreamCount() != 1 {
		t.Fatalf("StreamCount = %d, want 1", eng.StreamCount())
	}

	buf := make([]byte, 2048)
	_ = recvConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := recvConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	if n < 12 {
		t.Fatalf("short RTP packet: %d bytes", n)
	}

	time.Sleep(150 * time.Millisecond)
	st := eng.UnregisterCall("call-1")
	if st.RTPPacketsSent < 2 {
		t.Fatalf("RTPPacketsSent = %d, want at least 2", st.RTPPacketsSent)
	}
}

func TestScaleEnginePauseResume(t *testing.T) {
	recvConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer recvConn.Close()
	recvAddr := recvConn.LocalAddr().(*net.UDPAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng := NewScaleEngine()
	eng.Run(ctx)
	defer eng.Stop()

	cfg := DefaultConfig("")
	cfg.Synthetic = true
	cfg.PacketDuration = 20 * time.Millisecond
	if err := eng.RegisterStream(ctx, "c1", Endpoint{IP: "127.0.0.1", Port: recvAddr.Port}, cfg, "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	eng.PauseCall("c1")
	time.Sleep(100 * time.Millisecond)
	st1 := eng.Snapshot()
	eng.ResumeCall("c1")
	time.Sleep(100 * time.Millisecond)
	st2 := eng.UnregisterCall("c1")
	if st2.RTPPacketsSent <= st1.RTPPacketsSent {
		t.Fatalf("expected more packets after resume: before=%d after=%d", st1.RTPPacketsSent, st2.RTPPacketsSent)
	}
}
