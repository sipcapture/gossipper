package media

import "net"

// scaleBatchWriter sends UDP datagram batches (sendmmsg on Linux via x/net/ipv4).
type scaleBatchWriter interface {
	WriteBatch(msgs []udpSendMsg) (int, error)
}

func newScaleBatchWriter(conn *net.UDPConn) scaleBatchWriter {
	return newPlatformScaleBatchWriter(conn)
}
