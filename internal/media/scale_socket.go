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
	localAddr := &net.UDPAddr{Port: localPort}
	if localIP != "" && localIP != "0.0.0.0" && localIP != "::" {
		localAddr.IP = net.ParseIP(localIP)
	}
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: localAddr.IP, Port: 0})
		if err != nil {
			return nil, err
		}
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
