package control

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func installerScriptPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "packaging", "scripts", "install-linuxio-binaries.sh"))
}
