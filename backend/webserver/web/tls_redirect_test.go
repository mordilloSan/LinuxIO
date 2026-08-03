package web

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

type acceptResult struct {
	conn net.Conn
	err  error
}

type scriptedListener struct {
	results chan acceptResult
	done    chan struct{}
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	select {
	case result := <-l.results:
		return result.conn, result.err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *scriptedListener) Close() error {
	close(l.done)
	return nil
}

func (l *scriptedListener) Addr() net.Addr { return &net.TCPAddr{} }

type temporaryAcceptError struct{}

func (temporaryAcceptError) Error() string   { return "temporary accept error" }
func (temporaryAcceptError) Timeout() bool   { return false }
func (temporaryAcceptError) Temporary() bool { return true }

func TestTLSRedirectListenerSilentConnectionDoesNotBlockAccept(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := NewTLSRedirectListener(inner, &tls.Config{}, 443)
	defer l.Close()

	silent, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	classified, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer classified.Close()
	if _, err := classified.Write([]byte{0x16}); err != nil {
		t.Fatal(err)
	}

	result := make(chan net.Conn, 1)
	go func() {
		conn, _ := l.Accept()
		result <- conn
	}()
	select {
	case conn := <-result:
		if conn == nil {
			t.Fatal("Accept returned nil connection")
		}
		conn.Close()
	case <-time.After(time.Second):
		t.Fatal("Accept blocked behind silent connection")
	}
}

func TestTLSRedirectListenerCloseUnblocksAccept(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := NewTLSRedirectListener(inner, &tls.Config{}, 443)
	l, ok := listener.(*tlsRedirectListener)
	if !ok {
		_ = listener.Close()
		t.Fatal("NewTLSRedirectListener returned unexpected listener type")
	}
	result := make(chan error, 1)
	go func() {
		_, acceptErr := l.Accept()
		result <- acceptErr
	}()
	silent, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()

	deadline := time.Now().Add(time.Second)
	for {
		l.activeMu.Lock()
		active := len(l.active)
		l.activeMu.Unlock()
		if active > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("silent connection was not accepted")
		}
		time.Sleep(time.Millisecond)
	}

	closed := make(chan error, 1)
	go func() { closed <- l.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close remained blocked by silent connection")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Accept returned nil error after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Accept remained blocked after Close")
	}
}

func TestTLSRedirectListenerContinuesAfterAcceptError(t *testing.T) {
	inner := &scriptedListener{
		results: make(chan acceptResult, 2),
		done:    make(chan struct{}),
	}
	inner.results <- acceptResult{err: temporaryAcceptError{}}
	l := NewTLSRedirectListener(inner, &tls.Config{}, 443)
	defer l.Close()

	if _, err := l.Accept(); err == nil {
		t.Fatal("Accept returned nil error for temporary listener failure")
	}

	server, client := net.Pipe()
	defer client.Close()
	inner.results <- acceptResult{conn: server}
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write([]byte{0x16})
		writeDone <- err
	}()

	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept after temporary error: %v", err)
	}
	_ = conn.Close()
	if err := <-writeDone; err != nil {
		t.Fatalf("write TLS marker: %v", err)
	}
}
