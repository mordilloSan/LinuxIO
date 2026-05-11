package testdbus

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	godbus "github.com/godbus/dbus/v5"
)

type Bus struct {
	address string
	cmd     *exec.Cmd
}

func Start(t *testing.T) *Bus {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "bus.conf")
	abstractName := fmt.Sprintf("linuxio-%d-%s", os.Getpid(), filepath.Base(dir))
	config := fmt.Sprintf(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN"
	"http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
	<busconfig>
		<type>session</type>
		<listen>unix:abstract=%s</listen>
		<auth>EXTERNAL</auth>
		<apparmor mode="disabled"/>
		<policy context="default">
		<allow send_destination="*" eavesdrop="true"/>
		<allow eavesdrop="true"/>
		<allow own="*"/>
		<allow user="*"/>
		</policy>
	</busconfig>
	`, abstractName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write dbus config: %v", err)
	}

	cmd := exec.Command("dbus-daemon", "--nofork", "--print-address", "--config-file", configPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("dbus stdout pipe: %v", err)
	}
	err = cmd.Start()
	if err != nil {
		t.Fatalf("start dbus-daemon: %v (%s)", err, stderr.String())
	}

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("read dbus address: %v (%s)", err, stderr.String())
	}

	bus := &Bus{
		address: strings.TrimSpace(line),
		cmd:     cmd,
	}

	t.Cleanup(func() {
		if bus.cmd.Process != nil {
			_ = bus.cmd.Process.Kill()
		}
		_ = bus.cmd.Wait()
	})

	return bus
}

func (b *Bus) Address() string {
	return b.address
}

func (b *Bus) SetSystemBus(t *testing.T) {
	t.Helper()
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", b.address)
}

func (b *Bus) SetSessionBus(t *testing.T) {
	t.Helper()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", b.address)
}

func (b *Bus) Connect(t *testing.T) *godbus.Conn {
	t.Helper()

	conn, err := godbus.Connect(b.address)
	if err != nil {
		t.Fatalf("connect test bus: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	return conn
}

func (b *Bus) OwnName(t *testing.T, name string) *godbus.Conn {
	t.Helper()

	conn := b.Connect(t)
	reply, err := conn.RequestName(name, godbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatalf("request name %s: %v", name, err)
	}
	if reply != godbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name %s: unexpected reply %v", name, reply)
	}
	return conn
}

type SystemdCall struct {
	Method string
	Args   []any
}

type SystemdManager struct {
	mu                       sync.Mutex
	calls                    []SystemdCall
	UnitFileState            string
	EnableCarriesInstallInfo bool
	EnableChanges            [][]any
	DisableChanges           [][]any
}

func NewSystemdManager() *SystemdManager {
	return &SystemdManager{
		UnitFileState:            "enabled",
		EnableCarriesInstallInfo: true,
		EnableChanges:            [][]any{{"symlink", "/etc/systemd/system/demo.service", "/usr/lib/systemd/system/demo.service"}},
		DisableChanges:           [][]any{{"unlink", "/etc/systemd/system/demo.service", ""}},
	}
}

func (m *SystemdManager) StartUnit(name, mode string) (string, *godbus.Error) {
	m.record("StartUnit", name, mode)
	return "/org/freedesktop/systemd1/job/1", nil
}

func (m *SystemdManager) StopUnit(name, mode string) (string, *godbus.Error) {
	m.record("StopUnit", name, mode)
	return "/org/freedesktop/systemd1/job/2", nil
}

func (m *SystemdManager) RestartUnit(name, mode string) (string, *godbus.Error) {
	m.record("RestartUnit", name, mode)
	return "/org/freedesktop/systemd1/job/3", nil
}

func (m *SystemdManager) ReloadUnit(name, mode string) (string, *godbus.Error) {
	m.record("ReloadUnit", name, mode)
	return "/org/freedesktop/systemd1/job/4", nil
}

func (m *SystemdManager) EnableUnitFiles(names []string, runtime, force bool) (bool, [][]any, *godbus.Error) {
	m.record("EnableUnitFiles", append([]string(nil), names...), runtime, force)
	return m.EnableCarriesInstallInfo, m.EnableChanges, nil
}

func (m *SystemdManager) DisableUnitFiles(names []string, runtime bool) ([][]any, *godbus.Error) {
	m.record("DisableUnitFiles", append([]string(nil), names...), runtime)
	return m.DisableChanges, nil
}

func (m *SystemdManager) MaskUnitFiles(names []string, runtime, force bool) ([][]any, *godbus.Error) {
	m.record("MaskUnitFiles", append([]string(nil), names...), runtime, force)
	return nil, nil
}

func (m *SystemdManager) UnmaskUnitFiles(names []string, runtime bool) ([][]any, *godbus.Error) {
	m.record("UnmaskUnitFiles", append([]string(nil), names...), runtime)
	return nil, nil
}

func (m *SystemdManager) GetUnitFileState(name string) (string, *godbus.Error) {
	m.record("GetUnitFileState", name)
	return m.UnitFileState, nil
}

func (m *SystemdManager) Reload() *godbus.Error {
	m.record("Reload")
	return nil
}

func (m *SystemdManager) ResetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = nil
}

func (m *SystemdManager) Calls() []SystemdCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]SystemdCall, len(m.calls))
	for i, call := range m.calls {
		args := make([]any, len(call.Args))
		copy(args, call.Args)
		out[i] = SystemdCall{
			Method: call.Method,
			Args:   args,
		}
	}
	return out
}

func (m *SystemdManager) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, SystemdCall{
		Method: method,
		Args:   args,
	})
}
