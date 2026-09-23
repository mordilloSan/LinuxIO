package shares

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func setupRsyncTest(t *testing.T) {
	t.Helper()
	oldConfig, oldSecrets, oldUnit, oldLock := rsyncConfigFile, rsyncSecretsFile, rsyncUnitFile, rsyncLockFile
	oldSystemConfig := rsyncSystemConfigFile
	oldLookup, oldStop, oldStart := rsyncLookPath, rsyncStopUnit, rsyncStartUnit
	oldEnable, oldDisable := rsyncEnableUnit, rsyncDisableUnit
	oldCheckPort := rsyncCheckPort
	oldActive, oldState, oldReady := rsyncActiveState, rsyncUnitFileState, rsyncWaitReady
	t.Cleanup(func() {
		rsyncCheckPort = oldCheckPort
		rsyncConfigFile, rsyncSecretsFile, rsyncUnitFile, rsyncLockFile = oldConfig, oldSecrets, oldUnit, oldLock
		rsyncSystemConfigFile = oldSystemConfig
		rsyncLookPath, rsyncStopUnit, rsyncStartUnit = oldLookup, oldStop, oldStart
		rsyncEnableUnit, rsyncDisableUnit = oldEnable, oldDisable
		rsyncActiveState, rsyncUnitFileState, rsyncWaitReady = oldActive, oldState, oldReady
	})
	dir := t.TempDir()
	rsyncConfigFile, rsyncSecretsFile = filepath.Join(dir, "rsyncd.conf"), filepath.Join(dir, "rsyncd.secrets")
	rsyncUnitFile, rsyncLockFile = filepath.Join(dir, rsyncUnit), filepath.Join(dir, "lock")
	rsyncSystemConfigFile = filepath.Join(dir, "etc-rsyncd.conf")
	rsyncCheckPort = func(context.Context, int) error { return nil }
	rsyncLookPath = func(string) (string, error) { return "/usr/bin/rsync", nil }
	active, enabled := "inactive", "disabled"
	rsyncStopUnit = func(context.Context, string) error { active = "inactive"; return nil }
	rsyncStartUnit = func(context.Context, string) error { active = "active"; return nil }
	rsyncEnableUnit = func(context.Context, string) error { enabled = "enabled"; return nil }
	rsyncDisableUnit = func(context.Context, string) error { enabled = "disabled"; return nil }
	rsyncActiveState = func(context.Context, string) (string, error) { return active, nil }
	rsyncUnitFileState = func(context.Context, string) (string, error) { return enabled, nil }
	rsyncWaitReady = func(context.Context, int) error { return nil }
}

func testRsyncRequest(t *testing.T) apischema.RsyncSaveRequest {
	t.Helper()
	return apischema.RsyncSaveRequest{
		Module: "backup", Path: t.TempDir(), Username: "tnas", NASAddress: "192.168.1.249", Port: 873,
		Password: "a-strong-test-password",
	}
}

func TestRsyncSaveReadAndStop(t *testing.T) {
	setupRsyncTest(t)
	req := testRsyncRequest(t)
	status, err := handleSaveRsync(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Active || !status.Enabled || status.Config == nil || *status.Config != req.RsyncConfig {
		t.Fatalf("unexpected saved status: %+v", status)
	}
	assertRsyncProtection(t, status, req.Password)

	// Saving again keeps the secret, and the chosen listener port round trips.
	req.Password = ""
	req.Port = 8873
	if _, saveErr := handleSaveRsync(context.Background(), req); saveErr != nil {
		t.Fatal(saveErr)
	}
	status, err = handleGetRsync(context.Background(), apischema.NoRequest{})
	if err != nil || status.Config.Port != 8873 {
		t.Fatalf("read = %+v, %v", status, err)
	}
	secret, err := os.ReadFile(rsyncSecretsFile)
	if err != nil || string(secret) != "tnas:a-strong-test-password\n" {
		t.Fatal("saved password was not retained")
	}

	status, err = handleStopRsync(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Active || status.Enabled || status.Config == nil {
		t.Fatalf("stopped status = %+v", status)
	}
	if status.SSHReady || status.SSHError != nil {
		t.Fatalf("a stopped daemon fails TOS's ps check without a further error: %+v", status)
	}
}

func TestRsyncLeavesForeignSystemConfigAlone(t *testing.T) {
	setupRsyncTest(t)
	if err := os.WriteFile(rsyncSystemConfigFile, []byte("[distro]\npath = /srv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := handleSaveRsync(context.Background(), testRsyncRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if data, readErr := os.ReadFile(rsyncSystemConfigFile); readErr != nil || string(data) != "[distro]\npath = /srv\n" {
		t.Fatalf("foreign /etc/rsyncd.conf was altered: %q, %v", data, readErr)
	}
	if status.SSHReady || status.SSHError == nil || !strings.Contains(*status.SSHError, "not managed by LinuxIO") {
		t.Fatalf("status must explain why TOS's encryption mode sees another file: %+v", status)
	}
}

func assertRsyncProtection(t *testing.T, status apischema.RsyncStatus, password string) {
	t.Helper()
	data, err := os.ReadFile(rsyncConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"use chroot = yes", "read only = yes", "list = yes", "uid = root", "gid = root", "hosts allow = 192.168.1.249 127.0.0.1 ::1", "hosts deny = *", "auth users = tnas", "strict modes = yes", "port = 873"} {
		if !strings.Contains(string(data), setting+"\n") {
			t.Errorf("missing protection %q", setting)
		}
	}
	// TOS's SSH account reads the configuration; only the secrets stay private.
	for file, perm := range map[string]os.FileMode{rsyncConfigFile: 0o644, rsyncSecretsFile: 0o600} {
		info, statErr := os.Stat(file)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != perm {
			t.Errorf("%s permissions = %o, want %o", file, info.Mode().Perm(), perm)
		}
	}
	if link, linkErr := os.Readlink(rsyncSystemConfigFile); linkErr != nil || link != rsyncConfigFile {
		t.Errorf("%s -> %q, %v; want link to %s", rsyncSystemConfigFile, link, linkErr, rsyncConfigFile)
	}
	if !status.SSHReady || status.SSHError != nil {
		t.Errorf("running module must be ready for TOS's encryption mode: %+v", status)
	}
	wire, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), password) || strings.Contains(string(data), password) {
		t.Fatal("password leaked outside the secrets file")
	}

}

func TestRsyncRejectsUnsafeInputBeforeWriting(t *testing.T) {
	setupRsyncTest(t)
	for _, test := range []struct {
		name   string
		change func(*apischema.RsyncSaveRequest)
	}{
		{"module injection", func(r *apischema.RsyncSaveRequest) { r.Module = "backup]\nread only = no" }},
		{"reserved module", func(r *apischema.RsyncSaveRequest) { r.Module = "global" }},
		{"auth override", func(r *apischema.RsyncSaveRequest) { r.Username = "tnas:rw" }},
		{"wildcard host", func(r *apischema.RsyncSaveRequest) { r.NASAddress = "*" }},
		{"unspecified host", func(r *apischema.RsyncSaveRequest) { r.NASAddress = "0.0.0.0" }},
		{"multicast host", func(r *apischema.RsyncSaveRequest) { r.NASAddress = "224.0.0.1" }},
		{"path injection", func(r *apischema.RsyncSaveRequest) { r.Path += "\nread only = no" }},
		{"path expansion", func(r *apischema.RsyncSaveRequest) { r.Path += "/%HOME%" }},
		{"relative path", func(r *apischema.RsyncSaveRequest) { r.Path = "etc" }},
		{"missing path", func(r *apischema.RsyncSaveRequest) { r.Path += "/missing" }},
		{"bad port", func(r *apischema.RsyncSaveRequest) { r.Port = 65536 }},
		{"missing password", func(r *apischema.RsyncSaveRequest) { r.Password = "" }},
		{"short password", func(r *apischema.RsyncSaveRequest) { r.Password = "short" }},
		{"secret injection", func(r *apischema.RsyncSaveRequest) { r.Password += "\nother:secret" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := testRsyncRequest(t)
			test.change(&req)
			if _, err := handleSaveRsync(context.Background(), req); err == nil {
				t.Fatal("unsafe request accepted")
			}
			if _, err := os.Stat(rsyncConfigFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid request wrote configuration")
			}
		})
	}
}

func TestRsyncFailureAndCancellation(t *testing.T) {
	setupRsyncTest(t)
	req := testRsyncRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handleSaveRsync(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save: %v", err)
	}
	if _, err := os.Stat(rsyncConfigFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled save wrote configuration")
	}
	rsyncStartUnit = func(context.Context, string) error { return errors.New("unit start failed") }
	if _, err := handleSaveRsync(context.Background(), req); err == nil || !strings.Contains(err.Error(), "unit start failed") {
		t.Fatalf("start error: %v", err)
	}
	req.Username = "other"
	req.Password = ""
	if _, err := handleSaveRsync(context.Background(), req); err == nil {
		t.Fatal("reused a password for a different username")
	}
}

func TestRsyncRoutesRequirePrivilege(t *testing.T) {
	for _, route := range Routes {
		if strings.Contains(route.Route, "_rsync") && !route.Privileged {
			t.Errorf("%s is not privileged", route.Route)
		}
	}
}

func TestRsyncRejectsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener did not return a TCP address")
	}
	port := address.Port
	if err = checkRsyncPort(context.Background(), port); err == nil {
		t.Fatal("accepted a port already owned by another listener")
	}
	setupRsyncTest(t)
	rsyncCheckPort = func(context.Context, int) error { return errors.New("port occupied") }
	if _, err = handleSaveRsync(context.Background(), testRsyncRequest(t)); err == nil {
		t.Fatal("save ignored the port conflict")
	}
	if _, err = os.Stat(rsyncConfigFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("port conflict replaced the configuration")
	}
}
