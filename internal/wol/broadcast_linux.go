//go:build linux

package wol

import (
	"net"
	"syscall"
)

func enableBroadcast(connection *net.UDPConn) error {
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var socketError error
	if err := rawConnection.Control(func(fileDescriptor uintptr) {
		socketError = syscall.SetsockoptInt(
			int(fileDescriptor),
			syscall.SOL_SOCKET,
			syscall.SO_BROADCAST,
			1,
		)
	}); err != nil {
		return err
	}
	return socketError
}
