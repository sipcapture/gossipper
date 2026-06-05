//go:build !linux

package media

import "net"

type udpSendMsg struct {
	Addr *net.UDPAddr
	Buf  []byte
	pool *[]byte
}

func udpSendBatch(conn *net.UDPConn, msgs []udpSendMsg) (int, error) {
	return udpSendBatchFallback(conn, msgs)
}

func udpSendBatchFallback(conn *net.UDPConn, msgs []udpSendMsg) (int, error) {
	sent := 0
	for _, m := range msgs {
		if _, err := conn.WriteTo(m.Buf, m.Addr); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}
