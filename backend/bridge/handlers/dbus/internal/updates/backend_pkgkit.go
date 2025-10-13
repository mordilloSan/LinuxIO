package updates

import (
	"context"
	"fmt"
	"os/exec"
)

type pkgkitBackend struct{}

func newPkgKitBackend() Backend     { return &pkgkitBackend{} }
func (*pkgkitBackend) Name() string { return "packagekit" }
func (*pkgkitBackend) Detect() bool { return fileExists("/usr/bin/pkcon") }

// Read returns a minimal state since PackageKit doesn't manage auto-update configuration
// Auto-update config is handled by apt-unattended or dnf-automatic backends
func (*pkgkitBackend) Read() (AutoUpdateState, error) {
	return AutoUpdateState{
		Backend: "packagekit",
		Options: AutoUpdateOptions{
			Enabled:      false,
			Frequency:    "daily",
			Scope:        "security",
			DownloadOnly: false,
			RebootPolicy: "never",
			ExcludePkgs:  []string{},
		},
	}, nil
}

// Apply does nothing - PackageKit doesn't configure automatic updates
// Use apt-unattended or dnf-automatic backends for auto-update configuration
func (*pkgkitBackend) Apply(ctx context.Context, opt AutoUpdateOptions) error {
	_ = ctx
	_ = opt
	return fmt.Errorf("packagekit backend does not support auto-update configuration; use apt-unattended or dnf-automatic")
}

// ApplyOfflineNow schedules updates to be applied on next reboot
// This is the main purpose of the PackageKit backend
func (*pkgkitBackend) ApplyOfflineNow() error {
	// Step 1: Refresh package cache
	if err := exec.Command("/usr/bin/pkcon", "refresh").Run(); err != nil {
		return fmt.Errorf("failed to refresh package cache: %w", err)
	}

	// Step 2: Download updates (best-effort, ignore errors)
	// Some systems may not support --only-download or packages may already be downloaded
	_ = exec.Command("/usr/bin/pkcon", "update", "--only-download").Run()

	// Step 3: Trigger offline update (will apply on next reboot)
	if err := exec.Command("/usr/bin/pkcon", "offline-trigger").Run(); err != nil {
		return fmt.Errorf("failed to trigger offline update: %w", err)
	}

	return nil
}
