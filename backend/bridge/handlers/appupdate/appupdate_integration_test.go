package appupdate

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestInstallScriptDryRunWithSystemdSandbox(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_SYSTEMD_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_SYSTEMD_INTEGRATION=1 to run systemd integration test")
	}
	if os.Geteuid() != 0 {
		t.Skip("systemd integration test requires root")
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skipf("systemd-run not available: %v", err)
	}

	scriptPath := installerScriptPath(t)
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read install script: %v", err)
	}

	createUpdaterWritablePaths(t)

	unit := fmt.Sprintf("linuxio-updater-test-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemd-run", buildInstallCommandArgs(unit, "--dry-run")...)
	cmd.Stdin = bytes.NewReader(scriptBytes)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("systemd dry run failed: %v\n%s", err, output)
	}

	out := string(output)
	if !strings.Contains(out, "Dry run completed successfully") {
		t.Fatalf("unexpected dry-run output:\n%s", out)
	}
}

// createUpdaterWritablePaths does what the updater does before systemd-run,
// since a ReadWritePaths entry that does not exist fails the sandbox (exit
// 226), as on a host without LinuxIO. The folders it had to create go again
// when the test ends, so the host is left as it was: without LinuxIO.
func createUpdaterWritablePaths(t *testing.T) {
	t.Helper()

	missing := map[string]bool{}
	for _, entry := range updaterWritablePaths() {
		if entry.optional {
			continue
		}
		for dir := entry.path; dir != "/"; dir = filepath.Dir(dir) {
			if _, err := os.Lstat(dir); err == nil {
				break
			}
			missing[dir] = true
		}
	}
	if err := ensureUpdaterWritablePathDirs(); err != nil {
		t.Fatalf("create updater writable paths: %v", err)
	}
	t.Cleanup(func() {
		created := slices.Collect(maps.Keys(missing))
		// A folder's path is longer than its parent's, so the longest go first.
		slices.SortFunc(created, func(a, b string) int { return len(b) - len(a) })
		for _, dir := range created {
			if err := os.Remove(dir); err != nil {
				t.Errorf("remove %s, which the test created: %v", dir, err)
			}
		}
	})
}

// installerScriptPath is the checkout's install script, or
// LINUXIO_INSTALL_SCRIPT where the test binary runs without the checkout (the
// test VM's updater suite).
func installerScriptPath(t *testing.T) string {
	t.Helper()

	if path := os.Getenv("LINUXIO_INSTALL_SCRIPT"); path != "" {
		return path
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "packaging", "scripts", "install-linuxio-binaries.sh"))
}
