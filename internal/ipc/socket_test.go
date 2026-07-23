package ipc

import "net"

func netListenUnix(path string) (*net.UnixListener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}

func netDialUnix(path string) (*net.UnixConn, error) {
	return net.DialUnix(
		"unix",
		nil,
		&net.UnixAddr{Name: path, Net: "unix"},
	)
}
