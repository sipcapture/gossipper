//go:build !linux

package media

import "net"

type fallbackBatchWriter struct {
	conn *net.UDPConn
}

func newPlatformScaleBatchWriter(conn *net.UDPConn) scaleBatchWriter {
	return &fallbackBatchWriter{conn: conn}
}

func (w *fallbackBatchWriter) WriteBatch(msgs []udpSendMsg) (int, error) {
	return udpSendBatchFallback(w.conn, msgs)
}
