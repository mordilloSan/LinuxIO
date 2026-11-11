package autoupdate

import (
	"strings"
	"testing"
)

const aptAlertDropIn = "/etc/systemd/system/apt-daily-upgrade.service.d/linuxio-alert.conf"

func TestWriteNotifyDropInContent(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	if err := writeNotifyDropIn(host, "apt-daily-upgrade.service", "apt", AutoUpdateNotifyOnChange); err != nil {
		t.Fatal(err)
	}
	got := string(fake.files[aptAlertDropIn])
	want := "[Service]\nExecStartPre=-/usr/local/bin/linuxio-btrfs-timer snapshot pre-update\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider apt --policy on_change\n"
	if got != want {
		t.Fatalf("drop-in = %q, want %q", got, want)
	}
	if got := readNotifyPolicy(host, "apt-daily-upgrade.service", allNotifyPolicies); got != AutoUpdateNotifyOnChange {
		t.Fatalf("readNotifyPolicy = %q, want on_change", got)
	}
}

func TestWriteNotifyDropInNeverRemoves(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	fake.files[aptAlertDropIn] = []byte("stale")
	if err := writeNotifyDropIn(host, "apt-daily-upgrade.service", "apt", AutoUpdateNotifyNever); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.files[aptAlertDropIn]; ok {
		t.Fatal("drop-in not removed")
	}
	// Removing an absent file is fine.
	if err := writeNotifyDropIn(host, "apt-daily-upgrade.service", "apt", ""); err != nil {
		t.Fatal(err)
	}
}

func TestReadNotifyPolicy(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	svc := "dnf-automatic.service"
	path := "/etc/systemd/system/dnf-automatic.service.d/linuxio-alert.conf"
	if got := readNotifyPolicy(host, svc, allNotifyPolicies); got != AutoUpdateNotifyNever {
		t.Fatalf("missing drop-in = %q", got)
	}
	fake.files[path] = []byte("[Service]\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider dnf --policy always\n")
	if got := readNotifyPolicy(host, svc, allNotifyPolicies); got != AutoUpdateNotifyAlways {
		t.Fatalf("policy = %q", got)
	}
	fake.files[path] = []byte("[Service]\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider dnf --policy sometimes\n")
	if got := readNotifyPolicy(host, svc, allNotifyPolicies); got != AutoUpdateNotifyNever {
		t.Fatalf("invalid policy read as %q, want never", got)
	}
}

// Only on_change, which an older build offered where only the service result
// is known, reads as on_failure. Any other stored policy a backend does not
// offer reads as stored, so a save cannot quietly narrow it.
func TestReadNotifyPolicyFallsBackOnlyForOnChange(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	offered := []AutoUpdateNotify{AutoUpdateNotifyOnFailure}
	for stored, want := range map[AutoUpdateNotify]AutoUpdateNotify{
		AutoUpdateNotifyOnChange:  AutoUpdateNotifyOnFailure,
		AutoUpdateNotifyAlways:    AutoUpdateNotifyAlways,
		AutoUpdateNotifyOnFailure: AutoUpdateNotifyOnFailure,
	} {
		fake.files[aptAlertDropIn] = []byte("[Service]\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider apt --policy " + string(stored) + "\n")
		if got := readNotifyPolicy(host, aptUpgradeService, offered); got != want {
			t.Errorf("stored %q read as %q, want %q", stored, got, want)
		}
	}
	delete(fake.files, aptAlertDropIn)
	if got := readNotifyPolicy(host, aptUpgradeService, offered); got != AutoUpdateNotifyNever {
		t.Errorf("no drop-in read as %q, want never", got)
	}
}

func TestValidateOptionsRejectsUnsupportedNotify(t *testing.T) {
	support := AutoUpdateOptionSupport{
		Frequencies:    []AutoUpdateFrequency{"daily"},
		Scopes:         []AutoUpdateScope{"all"},
		RebootPolicies: []AutoUpdateRebootPolicy{"never"},
		NotifyPolicies: allNotifyPolicies,
	}
	base := AutoUpdateOptions{Frequency: "daily", Scope: "all", RebootPolicy: "never"}
	bad := base
	bad.Notify = "sometimes"
	if err := validateOptions(bad, support); err == nil || !strings.Contains(err.Error(), "notify") {
		t.Fatalf("err = %v", err)
	}
	support.NotifyPolicies = nil
	bad.Notify = AutoUpdateNotifyOnChange
	if err := validateOptions(bad, support); err == nil {
		t.Fatal("notify accepted by a backend without support")
	}
	if err := validateOptions(base, support); err != nil {
		t.Fatalf("empty notify should be allowed without support: %v", err)
	}
}

func TestWritePendingDropIn(t *testing.T) {
	fake, host := newFakeAutoUpdateHost()
	path := "/etc/systemd/system/apt-daily.service.d/linuxio-alert.conf"
	if err := writePendingDropIn(host, aptRefreshService, "apt", true); err != nil {
		t.Fatal(err)
	}
	want := "[Service]\nExecStopPost=/usr/local/bin/linuxio alert pending-updates --provider apt\n"
	if got := string(fake.files[path]); got != want {
		t.Fatalf("drop-in = %q, want %q", got, want)
	}
	if !readPendingEnabled(host, aptRefreshService) {
		t.Fatal("readPendingEnabled = false after write")
	}
	if err := writePendingDropIn(host, aptRefreshService, "apt", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.files[path]; ok || readPendingEnabled(host, aptRefreshService) {
		t.Fatal("drop-in not removed")
	}
}

func TestAptPeriodicRefreshesListsForPendingAlerts(t *testing.T) {
	update, download, unattended := aptPeriodicValues(AutoUpdateOptions{Enabled: false, NotifyAvailable: true})
	if update != "1" || download != "0" || unattended != "0" {
		t.Fatalf("values = %s %s %s", update, download, unattended)
	}
	if update, _, _ := aptPeriodicValues(AutoUpdateOptions{Enabled: false}); update != "0" {
		t.Fatalf("disabled without pending alerts refreshes lists: %s", update)
	}
}

func TestValidateOptionsRejectsPendingWithoutSupport(t *testing.T) {
	support := AutoUpdateOptionSupport{
		Frequencies:    []AutoUpdateFrequency{"daily"},
		Scopes:         []AutoUpdateScope{"all"},
		RebootPolicies: []AutoUpdateRebootPolicy{"never"},
	}
	opts := AutoUpdateOptions{Frequency: "daily", Scope: "all", RebootPolicy: "never", NotifyAvailable: true}
	if err := validateOptions(opts, support); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("err = %v", err)
	}
	support.NotifyAvailable = true
	if err := validateOptions(opts, support); err != nil {
		t.Fatal(err)
	}
}
