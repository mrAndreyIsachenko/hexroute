package ipc

import (
	"errors"
	"net"
)

var ErrUnauthorizedPeer = errors.New("unauthorized IPC peer")

func AuthorizePeer(connection *net.UnixConn, allowedUID uint32) (uint32, error) {
	uid, err := socketPeerUID(connection)
	if err != nil {
		return 0, err
	}
	if uid != allowedUID {
		return uid, ErrUnauthorizedPeer
	}
	return uid, nil
}
