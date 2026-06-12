package watchtower

import (
	"strings"
	"testing"
)

func TestDefaultEnvFileUsesSafeOneShotDefaults(t *testing.T) {
	gotBytes, err := DefaultEnvFile()
	if err != nil {
		t.Fatalf("DefaultEnvFile: %v", err)
	}
	got := string(gotBytes)
	wantLines := []string{
		"WATCHTOWER_NO_STARTUP_MESSAGE=true",
		"LINUXIO_WATCHTOWER_CONTAINERS=" + NoContainersID,
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("env file missing %q:\n%s", line, got)
		}
	}
	if strings.Contains(got, "WATCHTOWER_HTTP_API") {
		t.Fatalf("env file should not enable Watchtower HTTP API:\n%s", got)
	}
	if strings.Contains(got, "WATCHTOWER_CLEANUP=false") || strings.Contains(got, "WATCHTOWER_MONITOR_ONLY=false") {
		t.Fatalf("env file should omit false Watchtower options:\n%s", got)
	}
}

func TestUnitFileUsesManagedBinaryAndEnv(t *testing.T) {
	gotBytes, err := UnitFile()
	if err != nil {
		t.Fatalf("UnitFile: %v", err)
	}
	got := string(gotBytes)
	wantLines := []string{
		"Type=oneshot",
		"EnvironmentFile=" + EnvPath,
		"Environment=HOME=/root",
		"Environment=DOCKER_CONFIG=/root/.docker",
		"Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		`ExecCondition=/bin/sh -c 'set -- $$LINUXIO_WATCHTOWER_CONTAINERS; [ "$$#" -gt 0 ] && [ "$$1" != "__linuxio_no_containers_selected__" ]'`,
		`ExecStart=/bin/sh -c 'set -- $$LINUXIO_WATCHTOWER_CONTAINERS; exec /usr/bin/flock -w 600 /run/linuxio-watchtower.lock /usr/local/bin/linuxio-watchtower --run-once --porcelain v1 "$$@"'`,
		"Requires=docker.service",
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("unit file missing %q:\n%s", line, got)
		}
	}
	// The unit must stay in sync with the Go constants it duplicates: the
	// empty-list guard falls back to the sentinel, and the flock target must
	// be the same lock the bridge runner takes.
	if !strings.Contains(got, NoContainersID) {
		t.Fatalf("unit file does not use sentinel %q:\n%s", NoContainersID, got)
	}
	if !strings.Contains(got, LockPath) {
		t.Fatalf("unit file does not use lock %q:\n%s", LockPath, got)
	}
}

func TestTimerFileUsesManagedSchedule(t *testing.T) {
	gotBytes, err := TimerFile()
	if err != nil {
		t.Fatalf("TimerFile: %v", err)
	}
	got := string(gotBytes)
	wantLines := []string{
		"OnCalendar=*-*-* 04:00:00",
		"Persistent=true",
		"Unit=" + UnitName,
		"WantedBy=timers.target",
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line+"\n") {
			t.Fatalf("timer file missing %q:\n%s", line, got)
		}
	}
}
