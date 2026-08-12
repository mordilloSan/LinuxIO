package appupdate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"uuid"

	"github.com/mordilloSan/LinuxIO/backend/common/durabletask"
)

func TestInstallScriptDryRunWithSystemdSandbox(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_SYSTEMD_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_SYSTEMD_INTEGRATION=1 to run systemd integration test")
	}
	if os.Geteuid() != 0 {
		t.Skip("systemd integration test requires root")
	}
	scriptPath := installerScriptPath(t)
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read install script: %v", err)
	}

	operationID := uuid.New().String()
	store := durabletask.NewStore(filepath.Join(t.TempDir(), "operations"))
	storedScript, err := store.WriteArtifact(operationID, appUpdateInstallerName, scriptBytes, 0o700)
	if err != nil {
		t.Fatalf("write installer: %v", err)
	}
	resultPath, err := store.ExecutorResultPath(operationID)
	if err != nil {
		t.Fatalf("result path: %v", err)
	}
	unit := appUpdateUnitName(operationID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	executor := systemdUpdaterExecutor{}
	if err := executor.Start(ctx, updaterLaunch{
		OperationID:   operationID,
		UID:           0,
		Unit:          unit,
		Description:   appUpdateUnitDescription(operationID, 0),
		ScriptPath:    storedScript,
		ResultPath:    resultPath,
		InstallerArgs: []string{"--dry-run"},
	}); err != nil {
		t.Fatalf("start transient dry-run updater: %v", err)
	}
	defer executor.Collect(context.Background(), unit)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, resultErr := store.ReadExecutorResult(operationID)
		if resultErr == nil {
			if result.State != durabletask.StateCompleted || result.ExitCode != 0 {
				t.Fatalf("dry-run result = %+v", result)
			}
			break
		}
		select {
		case <-ctx.Done():
			state, _ := executor.Inspect(context.Background(), unit, appUpdateUnitDescription(operationID, 0))
			t.Fatalf("timed out waiting for dry-run result: %v (unit=%+v)", resultErr, state)
		case <-ticker.C:
		}
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
