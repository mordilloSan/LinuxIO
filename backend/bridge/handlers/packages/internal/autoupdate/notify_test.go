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
	want := "[Service]\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider apt --policy on_change\n"
	if got != want {
		t.Fatalf("drop-in = %q, want %q", got, want)
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
	if got := readNotifyPolicy(host, svc); got != AutoUpdateNotifyNever {
		t.Fatalf("missing drop-in = %q", got)
	}
	fake.files[path] = []byte("[Service]\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider dnf --policy always\n")
	if got := readNotifyPolicy(host, svc); got != AutoUpdateNotifyAlways {
		t.Fatalf("policy = %q", got)
	}
	fake.files[path] = []byte("[Service]\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider dnf --policy sometimes\n")
	if got := readNotifyPolicy(host, svc); got != AutoUpdateNotifyNever {
		t.Fatalf("invalid policy read as %q, want never", got)
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
