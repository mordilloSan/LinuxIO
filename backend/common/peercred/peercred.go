// Package peercred reads SO_PEERCRED for unix-socket HTTP servers so handlers
// can gate on the connecting process's uid.
package peercred

import (
	"context"
	"net"
	"net/http"
	"syscall"
)

type contextKey struct{}

type Cred struct {
	UID uint32
	GID uint32
}

// ConnContext is an http.Server.ConnContext hook. It attaches the peer
// credentials of a unix connection; other connections pass through.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return ctx
	}
	cred, err := read(uc)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, cred)
}

// UID returns the peer uid attached by ConnContext.
func UID(ctx context.Context) (uint32, bool) {
	cred, ok := ctx.Value(contextKey{}).(Cred)
	if !ok {
		return 0, false
	}
	return cred.UID, true
}

// RequestUID is UID for an HTTP request.
func RequestUID(r *http.Request) (uint32, bool) {
	return UID(r.Context())
}

// WithCredForTest attaches a uid without a socket. Tests only.
func WithCredForTest(ctx context.Context, uid uint32) context.Context {
	return context.WithValue(ctx, contextKey{}, Cred{UID: uid})
}

func read(conn *net.UnixConn) (Cred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return Cred{}, err
	}
	var ucred *syscall.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return Cred{}, err
	}
	if sockErr != nil {
		return Cred{}, sockErr
	}
	return Cred{UID: ucred.Uid, GID: ucred.Gid}, nil
}
