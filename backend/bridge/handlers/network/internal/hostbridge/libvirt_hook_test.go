package hostbridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

const testHookScript = "#!/bin/sh\nexit 0\n"

// hookTestEnvironment is a host whose firewall drops bridged traffic, with
// the hook and the package's copy of it in a temporary folder; the copy is
// there and the hook is not. It returns the units the restore restarted.
func hookTestEnvironment(t *testing.T) (Environment, *fakeRunner, *[]string) {
	t.Helper()
	env, runner := bridgeTestEnvironment(t, nil)
	runner.outputs = map[string][]byte{"iptables -S FORWARD": []byte("-P FORWARD DROP\n")}
	env.ReadFile = func(path string) ([]byte, error) {
		if path == "/proc/sys/net/bridge/bridge-nf-call-iptables" {
			return []byte("1\n"), nil
		}
		return nil, os.ErrNotExist
	}
	root := t.TempDir()
	env.LibvirtHookPath = filepath.Join(root, "etc", "libvirt", "hooks", "qemu.d", "linuxio-bridge-netfilter")
	env.LibvirtHookSource = filepath.Join(root, "usr", "share", "linuxio", "libvirt", "linuxio-bridge-netfilter")
	writeTestFile(t, env.LibvirtHookSource, testHookScript, 0o644)
	restarted := &[]string{}
	env.TryRestartUnit = func(_ context.Context, name string) error {
		*restarted = append(*restarted, name)
		return nil
	}
	return env, runner, restarted
}

// writeTestFile writes path, making its folder, and sets its mode.
func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestHookStateInBothAnswers(t *testing.T) {
	// The bridge dialogs and the VM preflight report the hook alike.
	checks := map[string]func(context.Context, Environment) (*HookStatus, []string){
		"bridge options": firewallChecks,
		"VM preflight":   CheckHook,
	}
	cases := []struct {
		name    string
		mode    os.FileMode // 0: no hook
		want    HookState
		warning string
	}{
		{"missing", 0, HookMissing, " that allows bridged VM traffic is missing"},
		{"not executable", 0o644, HookNotExecutable, "not an executable regular file, so libvirt will not run it"},
		{"installed", 0o755, HookInstalled, ""},
	}
	for checkName, check := range checks {
		for _, tc := range cases {
			t.Run(checkName+"/"+tc.name, func(t *testing.T) {
				assertHookState(t, check, tc.mode, tc.want, tc.warning)
			})
		}
	}
}

// assertHookState checks what check reports for a hook of mode (0: none) when
// the package's copy is there.
func assertHookState(t *testing.T, check func(context.Context, Environment) (*HookStatus, []string), mode os.FileMode, want HookState, warning string) {
	t.Helper()
	env, _, _ := hookTestEnvironment(t)
	if mode != 0 {
		writeTestFile(t, env.LibvirtHookPath, testHookScript, mode)
	}
	status, warnings := check(context.Background(), env)
	if status == nil || *status != (HookStatus{State: want, Restorable: true}) {
		t.Fatalf("status = %#v, want %s and restorable", status, want)
	}
	if warning == "" {
		if len(warnings) != 0 {
			t.Fatalf("warnings = %v", warnings)
		}
		return
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], env.LibvirtHookPath) ||
		!strings.Contains(warnings[0], warning) || strings.Contains(warnings[0], "reinstall") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestHookWithoutThePackageCopyIsNotRestorable(t *testing.T) {
	env, _, _ := hookTestEnvironment(t)
	if err := os.Remove(env.LibvirtHookSource); err != nil {
		t.Fatal(err)
	}
	status, warnings := CheckHook(context.Background(), env)
	if status == nil || *status != (HookStatus{State: HookMissing}) {
		t.Fatalf("status = %#v", status)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], env.LibvirtHookSource+", is missing as well") {
		t.Fatalf("warnings = %v", warnings)
	}
}

// The hook is reported only while the firewall drops bridged traffic.
func TestNoHookStateWhileTheFirewallLetsBridgedTrafficThrough(t *testing.T) {
	env, runner, _ := hookTestEnvironment(t)
	runner.outputs = map[string][]byte{"iptables -S FORWARD": []byte("-P FORWARD ACCEPT\n")}
	if status, warnings := CheckHook(context.Background(), env); status != nil || warnings != nil {
		t.Fatalf("VM preflight = %#v, %v", status, warnings)
	}
	if status, warnings := firewallChecks(context.Background(), env); status != nil || warnings != nil {
		t.Fatalf("bridge options = %#v, %v", status, warnings)
	}
}

// A FORWARD policy that cannot be read is said once, in the bridge dialogs,
// and not on the Virtual Machines page, which asks every few seconds.
func TestUnreadableForwardPolicyWarnsOnlyInBridgeOptions(t *testing.T) {
	env, runner, _ := hookTestEnvironment(t)
	runner.fail("iptables -S FORWARD", errors.New("iptables: not found"))

	status, warnings := firewallChecks(context.Background(), env)
	if status != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "could not be inspected") {
		t.Fatalf("bridge options = %#v, %v", status, warnings)
	}
	if status, warnings := CheckHook(context.Background(), env); status != nil || len(warnings) != 0 {
		t.Fatalf("VM preflight = %#v, %v", status, warnings)
	}
}

func TestRestoreHook(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, hook string){
		"missing folder": func(*testing.T, string) {},
		"missing hook": func(t *testing.T, hook string) {
			if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"non-executable hook": func(t *testing.T, hook string) {
			writeTestFile(t, hook, "edited\n", 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			env, _, restarted := hookTestEnvironment(t)
			setup(t, env.LibvirtHookPath)

			if err := RestoreHook(context.Background(), env); err != nil {
				t.Fatalf("RestoreHook: %v", err)
			}
			assertRestoredHook(t, env.LibvirtHookPath)
			if !slices.Equal(*restarted, libvirtDaemons) {
				t.Fatalf("restarted = %v, want %v", *restarted, libvirtDaemons)
			}
			if status, warnings := CheckHook(context.Background(), env); status == nil || status.State != HookInstalled || len(warnings) != 0 {
				t.Fatalf("after restore = %#v, %v", status, warnings)
			}
		})
	}
}

// assertRestoredHook checks the hook is the package's copy, a regular file
// mode 0755 in a folder mode 0755 that holds nothing else.
func assertRestoredHook(t *testing.T, hook string) {
	t.Helper()
	data, err := os.ReadFile(hook)
	if err != nil || string(data) != testHookScript {
		t.Fatalf("hook = %q, %v", data, err)
	}
	info, err := os.Lstat(hook)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 {
		t.Fatalf("hook mode = %v, %v", info.Mode(), err)
	}
	dir, err := os.Stat(filepath.Dir(hook))
	if err != nil || dir.Mode().Perm() != 0o755 {
		t.Fatalf("hook folder mode = %v, %v", dir.Mode(), err)
	}
	entries, err := os.ReadDir(filepath.Dir(hook))
	if err != nil || len(entries) != 1 {
		t.Fatalf("hook folder holds %v, %v; want the hook alone", entries, err)
	}
}

func TestRestoreHookWithoutThePackageCopy(t *testing.T) {
	env, _, restarted := hookTestEnvironment(t)
	if err := os.Remove(env.LibvirtHookSource); err != nil {
		t.Fatal(err)
	}
	err := RestoreHook(context.Background(), env)
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), env.LibvirtHookSource) {
		t.Fatalf("RestoreHook = %v, want a conflict naming the copy", err)
	}
	if _, statErr := os.Lstat(env.LibvirtHookPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("hook was written: %v", statErr)
	}
	if len(*restarted) != 0 {
		t.Fatalf("restarted = %v", *restarted)
	}
}

// A symlink or a folder at the hook's path was put there by root; the
// restore neither follows nor removes it.
func TestRestoreHookRefusesWhatIsNotAFile(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, hook, target string) error{
		"symlink": func(t *testing.T, hook, target string) error {
			writeTestFile(t, target, "target\n", 0o644)
			return os.Symlink(target, hook)
		},
		"dangling symlink": func(_ *testing.T, hook, target string) error { return os.Symlink(target, hook) },
		"folder":           func(_ *testing.T, hook, _ string) error { return os.Mkdir(hook, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			assertRestoreRefused(t, setup)
		})
	}
}

// assertRestoreRefused checks the restore refuses what setup puts at the
// hook's path, changes nothing there or where it points, and restarts nothing.
func assertRestoreRefused(t *testing.T, setup func(t *testing.T, hook, target string) error) {
	t.Helper()
	env, _, restarted := hookTestEnvironment(t)
	hook := env.LibvirtHookPath
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := setup(t, hook, target); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(hook)
	if err != nil {
		t.Fatal(err)
	}

	err = RestoreHook(context.Background(), env)
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "remove it, then restore the hook") {
		t.Fatalf("RestoreHook = %v, want a conflict", err)
	}
	if after, statErr := os.Lstat(hook); statErr != nil || after.Mode() != before.Mode() {
		t.Fatalf("hook changed from %v (%v)", before.Mode(), statErr)
	}
	if data, readErr := os.ReadFile(target); readErr == nil && string(data) != "target\n" {
		t.Fatalf("symlink target was written: %q", data)
	}
	if len(*restarted) != 0 {
		t.Fatalf("restarted = %v", *restarted)
	}
}

// A restart that fails leaves the restored hook in place and says libvirt
// has not picked it up; the other daemon is still restarted.
func TestRestoreHookReportsARestartFailure(t *testing.T) {
	env, _, _ := hookTestEnvironment(t)
	var restarted []string
	env.TryRestartUnit = func(_ context.Context, name string) error {
		restarted = append(restarted, name)
		if name == "libvirtd.service" {
			return errors.New("try-restart unit libvirtd.service: access denied")
		}
		return nil
	}

	err := RestoreHook(context.Background(), env)
	if err == nil || !strings.Contains(err.Error(), "libvirt hook restored, but restarting libvirt failed") || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("RestoreHook = %v", err)
	}
	if errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("a restart failure is not a client error: %v", err)
	}
	assertRestoredHook(t, env.LibvirtHookPath)
	if !slices.Equal(restarted, libvirtDaemons) {
		t.Fatalf("restarted = %v, want both daemons tried", restarted)
	}
}
