package launcher

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/qxip/gossipper/internal/cli"
	"github.com/qxip/gossipper/internal/media"
)

// RunRTPSender starts a standalone synthetic RTP sender that bypasses the SIP
// scenario engine.  It reads parameters from cfg (RTPAddr, RTPPT, RTPCodec,
// RTPFreqMs, RTPDurMs, RTPChannels) and streams silence frames until the
// supplied context is cancelled or the configured duration expires.
//
// A short summary is printed to stdout when the sender stops.
func RunRTPSender(ctx context.Context, cfg cli.Config) error {
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

	endpoint := media.Endpoint{IP: host, Port: port}

	streamCfg := media.DefaultConfig("")
	streamCfg.Synthetic = true
	streamCfg.Channels = uint8(cfg.RTPChannels)

	// Apply codec parameters first (sets PayloadType, ClockRate, SamplesPerPkt,
	// PacketDuration).  A non-zero -rtp_pt then overrides just the PT, allowing
	// dynamic payload type assignment without changing timing parameters.
	if cfg.RTPCodec != "" {
		media.ApplyPayloadParams(&streamCfg, cfg.RTPCodec)
	}
	if cfg.RTPPT > 0 {
		streamCfg.PayloadType = uint8(cfg.RTPPT)
	}

	if cfg.RTPFreqMs > 0 {
		streamCfg.PacketDuration = time.Duration(cfg.RTPFreqMs) * time.Millisecond
	}

	// For the standalone sender the Duration timeout is managed here via a
	// derived context so that the caller's ctx (e.g. SIGINT) still wins.
	senderCtx := ctx
	var senderCancel context.CancelFunc
	if cfg.RTPDurMs > 0 {
		senderCtx, senderCancel = context.WithTimeout(ctx, time.Duration(cfg.RTPDurMs)*time.Millisecond)
		defer senderCancel()
	}

	session := media.NewSession()
	if err := session.Start(senderCtx, endpoint, streamCfg, cfg.LocalIP, 0); err != nil {
		return fmt.Errorf("rtp sender: %w", err)
	}

	fmt.Printf("rtp_sender: streaming to %s  codec=%s pt=%d freq=%dms",
		cfg.RTPAddr, streamCfg.PayloadName, streamCfg.PayloadType, cfg.RTPFreqMs)
	if cfg.RTPDurMs > 0 {
		fmt.Printf(" duration=%dms", cfg.RTPDurMs)
	} else {
		fmt.Printf(" duration=unlimited")
	}
	fmt.Printf(" channels=%d\n", cfg.RTPChannels)

	<-senderCtx.Done()
	session.Stop()

	stats := session.Snapshot()
	fmt.Printf("rtp_sender: done  sent=%d octets=%d rtcp_sr=%d\n",
		stats.RTPPacketsSent, stats.RTPOctetsSent, stats.RTCPSenderReports)
	return nil
}
