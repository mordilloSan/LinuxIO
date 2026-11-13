package systemd

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyWritesStateToSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify.sock")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	t.Setenv("NOTIFY_SOCKET", sock)

	if notifyErr := Notify("READY=1"); notifyErr != nil {
		t.Fatal(notifyErr)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Fatalf("notify payload = %q, want READY=1", got)
	}
}

// systemd may hand an abstract socket as "@name"; Notify dials it as "\x00name".
func TestNotifyReachesAbstractSocket(t *testing.T) {
	name := fmt.Sprintf("linuxio-notify-test-%d", os.Getpid())
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: "\x00" + name, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	t.Setenv("NOTIFY_SOCKET", "@"+name)

	if notifyErr := Notify("READY=1"); notifyErr != nil {
		t.Fatal(notifyErr)
	}

	if err = conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Fatalf("notify payload = %q, want READY=1", got)
	}
}

func TestNotifyIsNoopWithoutSocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := Notify("READY=1"); err != nil {
		t.Fatalf("Notify without NOTIFY_SOCKET = %v, want nil", err)
	}
}
