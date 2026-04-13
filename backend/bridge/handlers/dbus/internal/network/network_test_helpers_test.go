package network

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls    []string
	failures map[string]error
	outputs  map[string][]byte
}

func (r *fakeRunner) LookPath(name string) (string, error) {
	return name, nil
}

func (r *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	call := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	r.calls = append(r.calls, call)
	if out, ok := r.outputs[call]; ok {
		if err, failed := r.failures[call]; failed {
			return out, err
		}
		return out, nil
	}
	if err, ok := r.failures[call]; ok {
		return nil, err
	}
	return nil, nil
}

func (r *fakeRunner) fail(call string, err error) {
	if r.failures == nil {
		r.failures = map[string]error{}
	}
	r.failures[call] = err
}

func testEnv(t *testing.T) (Environment, *fakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	runner := &fakeRunner{}
	env := Environment{
		NetplanDir:      filepath.Join(root, "etc", "netplan"),
		NMConnectionDir: filepath.Join(root, "etc", "NetworkManager", "system-connections"),
		NetworkdDir:     filepath.Join(root, "etc", "systemd", "network"),
		IfupdownMain:    filepath.Join(root, "etc", "network", "interfaces"),
		IfupdownDir:     filepath.Join(root, "etc", "network", "interfaces.d"),
		IfcfgDir:        filepath.Join(root, "etc", "sysconfig", "network-scripts"),
		Runner:          runner,
		WriteFile: func(path string, data []byte, mode fs.FileMode) error {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, data, mode)
		},
	}
	for _, dir := range []string{
		env.NetplanDir,
		env.NMConnectionDir,
		env.NetworkdDir,
		env.IfupdownDir,
		env.IfcfgDir,
		filepath.Dir(env.IfupdownMain),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return env, runner, root
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireCalls(t *testing.T, runner *fakeRunner, expected ...string) {
	t.Helper()
	if len(runner.calls) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %#v", len(expected), len(runner.calls), runner.calls)
	}
	for i, call := range expected {
		if runner.calls[i] != call {
			t.Fatalf("call %d: expected %q, got %q", i, call, runner.calls[i])
		}
	}
}

func errBoom() error {
	return errors.New("boom")
}
