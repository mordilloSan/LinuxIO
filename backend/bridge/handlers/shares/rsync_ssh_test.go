package shares

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestSSHIdentification(t *testing.T) {
	for _, test := range []struct {
		name   string
		banner string
		valid  bool
	}{
		{"ssh2", "SSH-2.0-OpenSSH_9.6\r\n", true},
		{"compatible", "SSH-1.99-OpenSSH\r\n", true},
		{"notice", "Authorized users only\r\nSSH-2.0-OpenSSH\r\n", true},
		{"rsync daemon", "@RSYNCD: 32.0\n", false},
		{"ssh1", "SSH-1.5-obsolete\r\n", false},
		{"http", "HTTP/1.1 200 OK\r\n", false},
		{"oversized line", strings.Repeat("x", 257) + "\nSSH-2.0-test\n", false},
		{"too many lines", strings.Repeat("notice\n", 50) + "SSH-2.0-test\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := readSSHIdentification(strings.NewReader(test.banner))
			if (err == nil) != test.valid {
				t.Fatalf("identification error = %v, valid = %v", err, test.valid)
			}
		})
	}
}

func TestRsyncSSHCheckHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handleGetRsyncSSH(ctx, apischema.RsyncSSHRequest{Port: 22}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled SSH check = %v", err)
	}
}

func TestRsyncSSHRejectsInvalidPorts(t *testing.T) {
	for _, port := range []int{-1, 0, 65536} {
		if _, err := handleGetRsyncSSH(context.Background(), apischema.RsyncSSHRequest{Port: port}); err == nil {
			t.Errorf("accepted invalid SSH port %d", port)
		}
	}
}

func TestRsyncSSHChecksSelectedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		if deadlineErr := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
			t.Error(deadlineErr)
			return
		}
		if _, writeErr := io.WriteString(conn, "SSH-2.0-test\r\n"); writeErr != nil {
			t.Error(writeErr)
		}
	}()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener did not return a TCP address")
	}
	request := apischema.RsyncSSHRequest{Port: address.Port}
	status, err := handleGetRsyncSSH(context.Background(), request)
	if err != nil || !status.Available || status.Port != request.Port {
		t.Fatalf("SSH check on selected port: status = %+v, error = %v", status, err)
	}
	<-done
	_ = listener.Close()
	status, err = handleGetRsyncSSH(context.Background(), request)
	if err != nil || status.Available || status.Error == nil || status.Port != request.Port {
		t.Fatalf("closed SSH port: status = %+v, error = %v", status, err)
	}
}
