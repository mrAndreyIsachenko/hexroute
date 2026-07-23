//go:build linux

package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func socketPeerUID(connection *net.UnixConn) (uint32, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("access Unix socket: %w", err)
	}

	var credential *unix.Ucred
	var credentialErr error
	if err := raw.Control(func(descriptor uintptr) {
		credential, credentialErr = unix.GetsockoptUcred(
			int(descriptor),
			unix.SOL_SOCKET,
			unix.SO_PEERCRED,
		)
	}); err != nil {
		return 0, fmt.Errorf("inspect Unix socket: %w", err)
	}
	if credentialErr != nil {
		return 0, fmt.Errorf("read Unix peer credentials: %w", credentialErr)
	}
	return credential.Uid, nil
}
