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
func (*pkgkitBackend) Read(ctx context.Context) (AutoUpdateState, error) {
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
func (*pkgkitBackend) Apply(ctx context.Context, _ AutoUpdateOptions) error {
	return fmt.Errorf("packagekit backend does not support auto-update configuration; use apt-unattended or dnf-automatic")
}

// ApplyOfflineNow schedules updates to be applied on next reboot
// This is the main purpose of the PackageKit backend
func (*pkgkitBackend) ApplyOfflineNow(ctx context.Context) error {
	// Step 1: Refresh package cache
	if err := exec.CommandContext(ctx, "/usr/bin/pkcon", "refresh").Run(); err != nil {
		return fmt.Errorf("failed to refresh package cache: %w", err)
	}

	// Step 2: Download updates (best-effort, don't fail if this errors)
	if err := exec.CommandContext(ctx, "/usr/bin/pkcon", "update", "--only-download").Run(); err != nil {
		// Log warning but continue - some systems may not support --only-download
		// Or packages may already be downloaded
	}

	// Step 3: Trigger offline update (will apply on next reboot)
	if err := exec.CommandContext(ctx, "/usr/bin/pkcon", "offline-trigger").Run(); err != nil {
		return fmt.Errorf("failed to trigger offline update: %w", err)
	}

	return nil
}
