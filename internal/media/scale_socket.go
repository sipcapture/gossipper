package media

import (
	"net"
	"syscall"
)

const (
	scaleUDPRecvBuf = 256 * 1024
	scaleUDPSendBuf = 4 * 1024 * 1024
)

func openScaleUDP(localIP string, localPort int) (*net.UDPConn, error) {
	conn, err := listenRTP(localIP, localPort)
	if err != nil {
		return nil, err
	}
	tuneScaleUDPConn(conn)
	return conn, nil
}

func tuneScaleUDPConn(c *net.UDPConn) {
	_ = c.SetReadBuffer(scaleUDPRecvBuf)
	_ = c.SetWriteBuffer(scaleUDPSendBuf)
	raw, err := c.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		setsockoptReusePort(fd)
	})
}
