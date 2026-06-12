package watchtower

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckInstallRequiresAllManagedFiles(t *testing.T) {
	paths := testInstallPaths(t)

	writeTestFile(t, paths.binary, "#!/bin/sh\n", 0o755)
	ok, err := checkInstall(paths)
	if ok || err == nil || !strings.Contains(err.Error(), paths.env+" not found") {
		t.Fatalf("binary-only install got ok=%v err=%v, want missing env", ok, err)
	}

	writeTestFile(t, paths.env, "WATCHTOWER_MONITOR_ONLY=false\n", 0o644)
	ok, err = checkInstall(paths)
	if ok || err == nil || !strings.Contains(err.Error(), paths.unit+" not found") {
		t.Fatalf("binary+env install got ok=%v err=%v, want missing unit", ok, err)
	}

	writeTestFile(t, paths.unit, "[Service]\nExecStart=/bin/true\n", 0o644)
	ok, err = checkInstall(paths)
	if ok || err == nil || !strings.Contains(err.Error(), paths.timer+" not found") {
		t.Fatalf("binary+env+unit install got ok=%v err=%v, want missing timer", ok, err)
	}

	writeTestFile(t, paths.timer, "[Timer]\nOnCalendar=*-*-* 04:00:00\n", 0o644)
	ok, err = checkInstall(paths)
	if !ok || err != nil {
		t.Fatalf("full install got ok=%v err=%v, want available", ok, err)
	}
}

func testInstallPaths(t *testing.T) installPaths {
	t.Helper()
	base := t.TempDir()
	return installPaths{
		binary: filepath.Join(base, "usr", "local", "bin", BinaryName),
		env:    filepath.Join(base, "etc", "linuxio", "watchtower.env"),
		unit:   filepath.Join(base, "etc", "systemd", "system", UnitName),
		timer:  filepath.Join(base, "etc", "systemd", "system", TimerName),
	}
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
