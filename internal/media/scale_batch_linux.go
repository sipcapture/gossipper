//go:build linux

package media

import (
	"net"
	"sync"

	"golang.org/x/net/ipv4"
)

type linuxBatchWriter struct {
	pc *ipv4.PacketConn
	mu sync.Mutex
	ms []ipv4.Message
}

func newPlatformScaleBatchWriter(conn *net.UDPConn) scaleBatchWriter {
	return &linuxBatchWriter{
		pc: ipv4.NewPacketConn(conn),
		ms: make([]ipv4.Message, 0, scaleMaxBatch),
	}
}

func (w *linuxBatchWriter) WriteBatch(msgs []udpSendMsg) (int, error) {
	if len(msgs) == 0 {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ms = w.ms[:0]
	for _, m := range msgs {
		w.ms = append(w.ms, ipv4.Message{
			Buffers: [][]byte{m.Buf},
			Addr:    m.Addr,
		})
	}
	n, err := w.pc.WriteBatch(w.ms, 0)
	if err != nil {
		return udpSendBatchFallbackConn(w.pc.PacketConn, msgs)
	}
	return n, err
}

func udpSendBatchFallbackConn(pc net.PacketConn, msgs []udpSendMsg) (int, error) {
	sent := 0
	for _, m := range msgs {
		if _, err := pc.WriteTo(m.Buf, m.Addr); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}
