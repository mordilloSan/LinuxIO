package autoupdate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	notifyDropInName   = "linuxio-alert.conf"
	aptUpgradeService  = "apt-daily-upgrade.service"
	aptRefreshService  = "apt-daily.service"
	mintUpgradeService = "mintupdate-automation-upgrade.service"
)

var (
	// allNotifyPolicies are the policies a drop-in can name. "never" is no
	// drop-in; no backend offers it, since failures are always reported, but
	// it is still saved when an older page sends it, and then reads back as
	// on_failure.
	allNotifyPolicies = []AutoUpdateNotify{AutoUpdateNotifyOnFailure, AutoUpdateNotifyOnChange, AutoUpdateNotifyAlways}
	// resultOnlyNotifyPolicies are offered where only the service result is
	// known (DNF, Mint): a run that installed something looks like any other.
	resultOnlyNotifyPolicies = []AutoUpdateNotify{AutoUpdateNotifyOnFailure, AutoUpdateNotifyAlways}
	notifyPolicyRe           = regexp.MustCompile(`--policy (\S+)`)
)

func notifyDropInPath(service string) string {
	return "/etc/systemd/system/" + service + ".d/" + notifyDropInName
}

// writeNotifyDropIn installs or removes the ExecStopPost hook that records an
// alert after each run. The caller runs daemon-reload afterwards.
func writeNotifyDropIn(host backendHost, service, provider string, policy AutoUpdateNotify) error {
	path := notifyDropInPath(service)
	if policy == "" || policy == AutoUpdateNotifyNever {
		if err := host.removeFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	}
	// ExecStartPre only exists while the drop-in does, so "never" keeps removing the whole file.
	body := fmt.Sprintf("[Service]\nExecStartPre=-%[1]s/linuxio-btrfs-timer snapshot pre-update\nExecStopPost=%[1]s/linuxio alert auto-update --provider %[2]s --policy %[3]s\n", version.BinDir, provider, policy)
	if err := host.writeFileAtomic(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// readNotifyPolicy reads the policy back from disk for a backend that offers
// offered: the drop-in is the authority. Anything missing or unrecognised
// reads as on_failure, which is what the host runs as without the drop-in:
// the run-record hook on the update service reports a failed run where this
// hook is not installed (scheduledrun). An on_change the backend does not
// offer, which an older build wrote where only the service result is known,
// reads as on_failure too; any other policy reads as stored, so a save cannot
// quietly narrow it.
func readNotifyPolicy(host backendHost, service string, offered []AutoUpdateNotify) AutoUpdateNotify {
	data, err := host.readFile(notifyDropInPath(service))
	if err != nil {
		return AutoUpdateNotifyOnFailure
	}
	m := notifyPolicyRe.FindSubmatch(data)
	if m == nil {
		return AutoUpdateNotifyOnFailure
	}
	switch policy := AutoUpdateNotify(m[1]); {
	case policy == AutoUpdateNotifyOnChange && !slices.Contains(offered, policy):
		return AutoUpdateNotifyOnFailure
	case slices.Contains(allNotifyPolicies, policy):
		return policy
	}
	return AutoUpdateNotifyOnFailure
}

// writePendingDropIn installs or removes the ExecStopPost hook on the
// package-list refresh service that reports packages waiting to be
// installed. The caller runs daemon-reload afterwards.
func writePendingDropIn(host backendHost, service, provider string, enabled bool) error {
	path := notifyDropInPath(service)
	if !enabled {
		if err := host.removeFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	}
	body := fmt.Sprintf("[Service]\nExecStopPost=%s/linuxio alert pending-updates --provider %s\n", version.BinDir, provider)
	if err := host.writeFileAtomic(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// readPendingEnabled reads the setting back from disk: the drop-in is the authority.
func readPendingEnabled(host backendHost, service string) bool {
	data, err := host.readFile(notifyDropInPath(service))
	return err == nil && bytes.Contains(data, []byte("alert pending-updates"))
}
