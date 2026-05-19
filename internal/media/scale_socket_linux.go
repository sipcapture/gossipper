//go:build linux

package media

import "syscall"

const scaleSOReusePort = 15 // SO_REUSEPORT

func setsockoptReusePort(fd uintptr) {
	_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, scaleSOReusePort, 1)
}
