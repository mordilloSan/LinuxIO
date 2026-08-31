//go:build linux

package daemon

import (
	"net"
	"syscall"
)

func readUnixPeerCred(conn *net.UnixConn) (peerCred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return peerCred{}, err
	}

	var ucred *syscall.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return peerCred{}, err
	}
	if sockErr != nil {
		return peerCred{}, sockErr
	}
	return peerCred{uid: ucred.Uid, gid: ucred.Gid}, nil
}
