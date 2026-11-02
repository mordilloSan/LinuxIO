package autoupdate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"regexp"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	notifyDropInName   = "linuxio-alert.conf"
	aptUpgradeService  = "apt-daily-upgrade.service"
	aptRefreshService  = "apt-daily.service"
	mintUpgradeService = "mintupdate-automation-upgrade.service"
)

var (
	allNotifyPolicies = []AutoUpdateNotify{AutoUpdateNotifyNever, AutoUpdateNotifyOnFailure, AutoUpdateNotifyOnChange, AutoUpdateNotifyAlways}
	notifyPolicyRe    = regexp.MustCompile(`--policy (\S+)`)
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

// readNotifyPolicy reads the policy back from disk. Anything missing or
// unrecognised is "never": the drop-in is the authority.
func readNotifyPolicy(host backendHost, service string) AutoUpdateNotify {
	data, err := host.readFile(notifyDropInPath(service))
	if err != nil {
		return AutoUpdateNotifyNever
	}
	m := notifyPolicyRe.FindSubmatch(data)
	if m == nil {
		return AutoUpdateNotifyNever
	}
	for _, p := range allNotifyPolicies {
		if string(m[1]) == string(p) && p != AutoUpdateNotifyNever {
			return p
		}
	}
	return AutoUpdateNotifyNever
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
