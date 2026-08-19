package bridge

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
)

// listenAuthSocket starts a unix listener on a fresh temp path, swaps
// authSocketPath to point at it, and returns the listener. Restoration and
// closing are both handled via t.Cleanup.
func listenAuthSocket(t *testing.T) net.Listener {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "auth.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	prev := authSocketPath
	authSocketPath = sockPath
	t.Cleanup(func() { authSocketPath = prev })

	return ln
}

// readDecodedRequest reads and decodes a full auth request frame: the
// 8-byte header followed by the four length-prefixed strings.
func readDecodedRequest(r io.Reader) (*authipc.AuthRequest, error) {
	var header [authipc.AuthReqHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	req := &authipc.AuthRequest{
		Verbose: header[4]&authipc.ReqFlagVerbose != 0,
	}

	var err error
	if req.User, err = readLenStrForTest(r); err != nil {
		return nil, err
	}
	if req.Password, err = readLenStrForTest(r); err != nil {
		return nil, err
	}
	if req.SessionID, err = readLenStrForTest(r); err != nil {
		return nil, err
	}
	if req.RemoteHost, err = readLenStrForTest(r); err != nil {
		return nil, err
	}
	return req, nil
}

func readLenStrForTest(r io.Reader) (string, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", err
	}
	length := binary.BigEndian.Uint16(lenBuf[:])
	if length == 0 {
		return "", nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func writeLenStrForTest(w io.Writer, s string) error {
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(s)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	if len(s) > 0 {
		if _, err := w.Write([]byte(s)); err != nil {
			return err
		}
	}
	return nil
}

// writeOKResponse writes a status-OK auth response frame.
func writeOKResponse(w io.Writer, mode uint8, uid, gid uint32, username string) error {
	header := [authipc.AuthRespHeaderSize]byte{
		authipc.ProtoMagic0, authipc.ProtoMagic1, authipc.ProtoMagic2, authipc.ProtoVersion,
		authipc.StatusOK, mode, byte(authipc.ResultOK), 0,
	}
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	var idBuf [4]byte
	binary.BigEndian.PutUint32(idBuf[:], uid)
	if _, err := w.Write(idBuf[:]); err != nil {
		return err
	}
	binary.BigEndian.PutUint32(idBuf[:], gid)
	if _, err := w.Write(idBuf[:]); err != nil {
		return err
	}
	return writeLenStrForTest(w, username)
}

// writeErrorResponse writes a status-error auth response frame.
func writeErrorResponse(w io.Writer, code authipc.AuthResultCode, message string) error {
	header := [authipc.AuthRespHeaderSize]byte{
		authipc.ProtoMagic0, authipc.ProtoMagic1, authipc.ProtoMagic2, authipc.ProtoVersion,
		authipc.StatusError, authipc.ModeUnprivileged, byte(code), 0,
	}
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	return writeLenStrForTest(w, message)
}

func TestAuthenticateSuccess(t *testing.T) {
	ln := listenAuthSocket(t)

	req := BuildRequest("alice", "sess-1", "s3cret", "10.0.0.1", true)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		got, err := readDecodedRequest(conn)
		if err != nil {
			serverErr <- err
			return
		}
		if got.User != req.User || got.Password != req.Password ||
			got.SessionID != req.SessionID || got.RemoteHost != req.RemoteHost ||
			got.Verbose != req.Verbose {
			serverErr <- errors.New("decoded request did not match BuildRequest output")
			return
		}

		if err := writeOKResponse(conn, authipc.ModePrivileged, 1000, 1000, "alice"); err != nil {
			serverErr <- err
			return
		}
		// Canary byte: proves the connection handover leaves the stream
		// byte-clean for the yamux session that follows.
		if _, err := conn.Write([]byte{0x42}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	result, err := Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	t.Cleanup(func() { _ = result.Conn.Close() })

	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}

	if result.User.Username != "alice" || result.User.UID != 1000 || result.User.GID != 1000 {
		t.Fatalf("unexpected user: %+v", result.User)
	}
	if !result.Privileged {
		t.Fatal("expected Privileged to be true")
	}

	var canary [1]byte
	if _, err := io.ReadFull(result.Conn, canary[:]); err != nil {
		t.Fatalf("read canary: %v", err)
	}
	if canary[0] != 0x42 {
		t.Fatalf("canary byte = %#x, want 0x42", canary[0])
	}
}

func TestAuthenticateUnauthorized(t *testing.T) {
	ln := listenAuthSocket(t)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		if _, err := readDecodedRequest(conn); err != nil {
			serverErr <- err
			return
		}
		serverErr <- writeErrorResponse(conn, authipc.ResultAuthFailed, "authentication failed")
	}()

	req := BuildRequest("bob", "sess-2", "wrong", "10.0.0.2", false)
	result, err := Authenticate(req)
	if serverErr := <-serverErr; serverErr != nil {
		t.Fatalf("server: %v", serverErr)
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	if result != nil {
		t.Fatal("expected nil result on error")
	}

	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if !authErr.IsUnauthorized() {
		t.Fatal("expected IsUnauthorized() to be true")
	}
	if authErr.Message != "authentication failed" {
		t.Fatalf("Message = %q, want %q", authErr.Message, "authentication failed")
	}
}

func TestAuthenticateErrorEmptyMessage(t *testing.T) {
	ln := listenAuthSocket(t)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		if _, err := readDecodedRequest(conn); err != nil {
			serverErr <- err
			return
		}
		serverErr <- writeErrorResponse(conn, authipc.ResultInternalError, "")
	}()

	req := BuildRequest("carol", "sess-3", "pw", "10.0.0.3", false)
	_, err := Authenticate(req)
	if serverErr := <-serverErr; serverErr != nil {
		t.Fatalf("server: %v", serverErr)
	}

	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	// Authenticate substitutes the code's default message when the wire
	// message is empty.
	want := authipc.ResultInternalError.DefaultMessage()
	if authErr.Message != want {
		t.Fatalf("Message = %q, want %q", authErr.Message, want)
	}
}

func TestAuthenticateTruncatedResponse(t *testing.T) {
	ln := listenAuthSocket(t)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		if _, readErr := readDecodedRequest(conn); readErr != nil {
			serverErr <- readErr
			return
		}
		// Write only half the response header, then close.
		half := []byte{authipc.ProtoMagic0, authipc.ProtoMagic1, authipc.ProtoMagic2, authipc.ProtoVersion}
		_, err = conn.Write(half)
		serverErr <- err
	}()

	req := BuildRequest("dave", "sess-4", "pw", "10.0.0.4", false)
	_, err := Authenticate(req)
	if serverErr := <-serverErr; serverErr != nil {
		t.Fatalf("server: %v", serverErr)
	}

	if err == nil {
		t.Fatal("expected an error")
	}
	if authErr, ok := errors.AsType[*AuthError](err); ok {
		t.Fatalf("expected non-AuthError, got *AuthError: %v", authErr)
	}
}

func TestAuthenticateDialFailure(t *testing.T) {
	prev := authSocketPath
	authSocketPath = filepath.Join(t.TempDir(), "does-not-exist.sock")
	t.Cleanup(func() { authSocketPath = prev })

	req := BuildRequest("erin", "sess-5", "pw", "10.0.0.5", false)
	_, err := Authenticate(req)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "connect") {
		t.Fatalf("error %q does not mention connecting", err.Error())
	}
}

func TestAuthenticateBadMagicResponse(t *testing.T) {
	ln := listenAuthSocket(t)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		if _, readErr := readDecodedRequest(conn); readErr != nil {
			serverErr <- readErr
			return
		}
		header := [authipc.AuthRespHeaderSize]byte{
			'X', 'X', 'X', authipc.ProtoVersion,
			authipc.StatusOK, authipc.ModeUnprivileged, byte(authipc.ResultOK), 0,
		}
		_, err = conn.Write(header[:])
		serverErr <- err
	}()

	req := BuildRequest("frank", "sess-6", "pw", "10.0.0.6", false)
	_, err := Authenticate(req)
	if serverErr := <-serverErr; serverErr != nil {
		t.Fatalf("server: %v", serverErr)
	}

	if err == nil {
		t.Fatal("expected an error")
	}
	if authErr, ok := errors.AsType[*AuthError](err); ok {
		t.Fatalf("expected non-AuthError, got *AuthError: %v", authErr)
	}
}
