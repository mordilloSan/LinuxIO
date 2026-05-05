package control

import (
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func TestBuildInstallCommandArgsUsesExplicitWritablePaths(t *testing.T) {
	args := buildInstallCommandArgs("linuxio-updater-test")

	var protectSystem string
	var readWritePaths string
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "-p" {
			continue
		}
		switch {
		case strings.HasPrefix(args[i+1], "ProtectSystem="):
			protectSystem = args[i+1]
		case strings.HasPrefix(args[i+1], "ReadWritePaths="):
			readWritePaths = strings.TrimPrefix(args[i+1], "ReadWritePaths=")
		}
	}

	if protectSystem != "ProtectSystem=full" {
		t.Fatalf("ProtectSystem property = %q, want %q", protectSystem, "ProtectSystem=full")
	}
	if readWritePaths == "" {
		t.Fatal("missing ReadWritePaths property")
	}

	expectedPaths := []string{
		version.BinDir,
		"/etc/linuxio",
		"/etc/pam.d",
		"/etc/systemd/system",
		"-/etc/motd.d",
		"/usr/lib/tmpfiles.d",
		"/usr/share/linuxio",
		"/var/lib/linuxIO",
	}
	for _, path := range expectedPaths {
		if !strings.Contains(" "+readWritePaths+" ", " "+path+" ") {
			t.Fatalf("ReadWritePaths missing %q: %q", path, readWritePaths)
		}
	}
	if strings.Contains(" "+readWritePaths+" ", " /etc ") {
		t.Fatalf("ReadWritePaths should use explicit subpaths, got %q", readWritePaths)
	}
	if strings.Contains(" "+readWritePaths+" ", " /etc/motd.d ") {
		t.Fatalf("ReadWritePaths should mark /etc/motd.d optional, got %q", readWritePaths)
	}
	if strings.Contains(" "+readWritePaths+" ", " /etc/pam.d/linuxio ") {
		t.Fatalf("ReadWritePaths should not require the PAM file to already exist, got %q", readWritePaths)
	}
}

func TestBuildInstallCommandArgsAppendsVersion(t *testing.T) {
	version := "v0.9.3"
	args := buildInstallCommandArgs("linuxio-updater-test", version)

	if got := args[len(args)-1]; got != version {
		t.Fatalf("last arg = %q, want %q", got, version)
	}
}
