package launcher

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/media"
)

// RunRTPSenderScale runs many parallel synthetic RTP streams via ScaleEngine.
func RunRTPSenderScale(ctx context.Context, cfg cli.Config) error {
	host, portStr, err := net.SplitHostPort(cfg.RTPAddr)
	if err != nil {
		return fmt.Errorf("invalid rtp_addr %q: %w", cfg.RTPAddr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid rtp_addr port %q: %w", portStr, err)
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("rtp_addr port %d is out of range", port)
	}

	streamCfg := media.DefaultConfig("")
	streamCfg.Synthetic = true
	streamCfg.Channels = uint8(cfg.RTPChannels)
	if cfg.RTPCodec != "" {
		media.ApplyPayloadParams(&streamCfg, cfg.RTPCodec)
	}
	if cfg.RTPPT > 0 {
		streamCfg.PayloadType = uint8(cfg.RTPPT)
	}
	if cfg.RTPFreqMs > 0 {
		streamCfg.PacketDuration = time.Duration(cfg.RTPFreqMs) * time.Millisecond
	}

	senderCtx := ctx
	var senderCancel context.CancelFunc
	if cfg.RTPDurMs > 0 {
		senderCtx, senderCancel = context.WithTimeout(ctx, time.Duration(cfg.RTPDurMs)*time.Millisecond)
		defer senderCancel()
	}

	eng := media.NewScaleEngine()
	eng.Run(senderCtx)
	defer eng.Stop()

	endpoint := media.Endpoint{IP: host, Port: port}
	basePort := 0
	if cfg.LocalPort > 0 {
		basePort = cfg.LocalPort + 2
	}
	streams := cfg.RTPStreams
	if streams < 1 {
		streams = 1
	}
	for i := 0; i < streams; i++ {
		callID := fmt.Sprintf("rtp-%d", i+1)
		localPort := basePort + i*2
		if err := eng.RegisterStream(senderCtx, callID, endpoint, streamCfg, cfg.LocalIP, localPort); err != nil {
			return fmt.Errorf("register stream %d: %w", i+1, err)
		}
	}

	fmt.Printf("rtp_sender: scale mode %d streams -> %s  codec=%s pt=%d freq=%dms\n",
		streams, cfg.RTPAddr, streamCfg.PayloadName, streamCfg.PayloadType, cfg.RTPFreqMs)

	<-senderCtx.Done()

	var total media.Stats
	for i := 0; i < streams; i++ {
		st := eng.UnregisterCall(fmt.Sprintf("rtp-%d", i+1))
		total.RTPPacketsSent += st.RTPPacketsSent
		total.RTPOctetsSent += st.RTPOctetsSent
	}
	fmt.Printf("rtp_sender: done  streams=%d sent=%d octets=%d\n",
		streams, total.RTPPacketsSent, total.RTPOctetsSent)
	return nil
}
