package autoupdate

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	notifyDropInName   = "linuxio-alert.conf"
	aptUpgradeService  = "apt-daily-upgrade.service"
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
	body := fmt.Sprintf("[Service]\nExecStopPost=%s/linuxio alert auto-update --provider %s --policy %s\n", version.BinDir, provider, policy)
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
