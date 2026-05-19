//go:build !linux

package media

func setsockoptReusePort(fd uintptr) {}
