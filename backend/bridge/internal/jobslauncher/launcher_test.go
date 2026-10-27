package jobslauncher

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

// socketPair returns the two ends of a connected unix stream socket.
func socketPair(t *testing.T) (server, client *net.UnixConn) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	var conns [2]*net.UnixConn
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "socketpair")
		c, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("net.FileConn: %v", err)
		}
		uc, ok := c.(*net.UnixConn)
		if !ok {
			t.Fatalf("FileConn returned %T, want *net.UnixConn", c)
		}
		conns[i] = uc
		t.Cleanup(func() { _ = conns[i].Close() })
	}
	return conns[0], conns[1]
}

// serve runs Serve on one end and returns what the other end reads.
func serve(t *testing.T, d Deps, clientWrites string) (reply string, err error) {
	t.Helper()
	server, client := socketPair(t)
	if clientWrites != "" {
		if _, werr := client.Write([]byte(clientWrites)); werr != nil {
			t.Fatalf("client write: %v", werr)
		}
	}
	err = Serve(t.Context(), server, d)
	_ = server.Close()
	reply, rerr := bufio.NewReader(client).ReadString('\n')
	if rerr != nil {
		t.Fatalf("read reply: %v", rerr)
	}
	return reply, err
}

type fakes struct {
	units      []string
	socketDir  string
	startErr   error
	disallowed bool
	lookupErr  error
	createSock bool
}

func (f *fakes) deps(uid uint32) Deps {
	return Deps{
		Peer: func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: uid}, nil },
		LookupUser: func(uint32) (string, error) {
			if f.lookupErr != nil {
				return "", f.lookupErr
			}
			return "alice", nil
		},
		Disallowed: func(string) (bool, error) { return f.disallowed, nil },
		StartUnit: func(_ context.Context, unit string) error {
			f.units = append(f.units, unit)
			if f.startErr != nil {
				return f.startErr
			}
			if f.createSock {
				return os.WriteFile(filepath.Join(f.socketDir, "host.sock"), nil, 0o600)
			}
			return nil
		},
		SocketPath:  func(uint32) string { return filepath.Join(f.socketDir, "host.sock") },
		WaitTimeout: 100 * time.Millisecond,
		Poll:        5 * time.Millisecond,
	}
}

func TestLauncherStartsCallersOwnHost(t *testing.T) {
	f := &fakes{socketDir: t.TempDir(), createSock: true}
	d := f.deps(0)
	d.Peer = peercred.Read // a socketpair peer is this process
	reply, err := serve(t, d, "")
	if err != nil || reply != "ok\n" {
		t.Fatalf("reply = %q, err = %v; want ok", reply, err)
	}
	want := UnitName(uint32(os.Getuid()))
	if len(f.units) != 1 || f.units[0] != want {
		t.Fatalf("started %v, want [%s]", f.units, want)
	}
	if !strings.HasPrefix(want, "linuxio-jobs@") || !strings.HasSuffix(want, ".service") {
		t.Fatalf("UnitName = %q", want)
	}
}

func TestLauncherRefusesUnknownUID(t *testing.T) {
	f := &fakes{socketDir: t.TempDir(), lookupErr: os.ErrNotExist}
	reply, err := serve(t, f.deps(4242), "")
	if err == nil || reply != "error: unknown uid\n" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
	if len(f.units) != 0 {
		t.Fatalf("started %v for an unknown uid", f.units)
	}
}

func TestLauncherRefusesDisallowedUser(t *testing.T) {
	f := &fakes{socketDir: t.TempDir(), disallowed: true}
	reply, err := serve(t, f.deps(1000), "")
	if err == nil || reply != "error: user is not allowed\n" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
	if len(f.units) != 0 {
		t.Fatalf("started %v for a disallowed user", f.units)
	}
}

func TestLauncherTimesOutWaitingForSocket(t *testing.T) {
	f := &fakes{socketDir: t.TempDir()}
	reply, err := serve(t, f.deps(1000), "")
	if err == nil || reply != "error: host did not start\n" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
}

func TestLauncherReportsStartFailure(t *testing.T) {
	f := &fakes{socketDir: t.TempDir(), startErr: os.ErrPermission}
	reply, err := serve(t, f.deps(1000), "")
	if err == nil || reply != "error: start failed\n" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
}

func TestLauncherIgnoresClientInput(t *testing.T) {
	f := &fakes{socketDir: t.TempDir(), createSock: true}
	reply, err := serve(t, f.deps(1000), "sshd.service\n")
	if err != nil || reply != "ok\n" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
	if len(f.units) != 1 || f.units[0] != "linuxio-jobs@1000.service" {
		t.Fatalf("started %v, want only the caller's own host", f.units)
	}
}

func TestDisallowedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disallowed-users")
	if err := os.WriteFile(path, []byte("# comment\n\nroot\n  mallory  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[string]bool{"root": true, "mallory": true, "alice": false} {
		if got, err := disallowedIn(path, user); err != nil || got != want {
			t.Errorf("disallowedIn(%q) = %v, %v; want %v", user, got, err, want)
		}
	}
	if got, err := disallowedIn(filepath.Join(t.TempDir(), "missing"), "root"); err != nil || got {
		t.Errorf("missing file: %v, %v; want false, nil", got, err)
	}
}
