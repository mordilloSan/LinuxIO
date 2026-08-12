package docker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestApplyContainerAutoUpdateEnablesNativeTimerAndWritesFiles(t *testing.T) {
	store := testAutoUpdateStore(t)
	ops := newRecordingContainerUpdateOps()
	opts := apischema.DockerContainerAutoUpdateOptions{
		Cleanup:        true,
		ContainerNames: []string{"app.service", "redis"},
		Enabled:        true,
		Mode:           "check_only",
		Time:           "06:15",
	}

	if err := applyContainerAutoUpdate(context.Background(), store, ops.ops(), opts); err != nil {
		t.Fatalf("applyContainerAutoUpdate: %v", err)
	}

	var document dockerUpdateScheduleDocument
	if err := json.Unmarshal([]byte(readTestFile(t, store.configPath)), &document); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if document.Version != dockerUpdateConfigVersion || document.Mode != "check_only" || !document.Cleanup || !reflect.DeepEqual(document.ContainerNames, []string{"app.service", "redis"}) {
		t.Fatalf("config = %+v", document)
	}
	if timer := readTestFile(t, store.timerPath); !strings.Contains(timer, "OnCalendar=*-*-* 06:15:00") || !strings.Contains(timer, "Unit="+dockerUpdateUnitName) {
		t.Fatalf("timer file did not render schedule:\n%s", timer)
	}
	unit := readTestFile(t, store.unitPath)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/linuxio docker-update-runner --config "+DockerUpdateConfigPath) {
		t.Fatalf("unit file does not invoke the LinuxIO runner directly:\n%s", unit)
	}
	if strings.Contains(unit, "/bin/sh") || strings.Contains(strings.ToLower(unit), "watchtower") {
		t.Fatalf("unit file contains a legacy shell or Watchtower path:\n%s", unit)
	}

	wantCalls := []string{"reload", "enable:" + dockerUpdateTimerName, "start:" + dockerUpdateTimerName}
	if !reflect.DeepEqual(ops.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", ops.calls, wantCalls)
	}
}

func TestApplyContainerAutoUpdateDisablesNativeTimer(t *testing.T) {
	store := testAutoUpdateStore(t)
	ops := newRecordingContainerUpdateOps()
	opts := apischema.DockerContainerAutoUpdateOptions{
		Enabled: false,
		Mode:    "update",
		Time:    "04:00",
	}

	if err := applyContainerAutoUpdate(context.Background(), store, ops.ops(), opts); err != nil {
		t.Fatalf("applyContainerAutoUpdate: %v", err)
	}
	var document dockerUpdateScheduleDocument
	if err := json.Unmarshal([]byte(readTestFile(t, store.configPath)), &document); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if len(document.ContainerNames) != 0 {
		t.Fatalf("empty selection = %#v", document.ContainerNames)
	}
	wantCalls := []string{"reload", "stop:" + dockerUpdateTimerName, "disable:" + dockerUpdateTimerName}
	if !reflect.DeepEqual(ops.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", ops.calls, wantCalls)
	}
}

func TestAutoUpdateStoreReadsLegacyWatchtowerSelection(t *testing.T) {
	store := testAutoUpdateStore(t)
	legacyEnv := `WATCHTOWER_MONITOR_ONLY=true
WATCHTOWER_CLEANUP=true
LINUXIO_WATCHTOWER_CONTAINERS=app\\.service redis
`
	if err := os.MkdirAll(filepath.Dir(store.legacyEnvPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(store.legacyEnvPath, []byte(legacyEnv), 0o600); err != nil {
		t.Fatalf("write legacy env: %v", err)
	}
	legacyTimer := `[Timer]
OnCalendar=*-*-* 07:30:00
`
	if err := os.MkdirAll(filepath.Dir(store.legacyTimerPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(store.legacyTimerPath, []byte(legacyTimer), 0o644); err != nil {
		t.Fatalf("write legacy timer: %v", err)
	}

	opts, err := store.readOptions()
	if err != nil {
		t.Fatalf("readOptions: %v", err)
	}
	if opts.Mode != "check_only" || !opts.Cleanup || opts.Time != "07:30" || !reflect.DeepEqual(opts.ContainerNames, []string{"app.service", "redis"}) {
		t.Fatalf("legacy options = %+v", opts)
	}
}

func TestMigrateLegacyContainerUpdateSchedulePreservesScopeAndEnabledState(t *testing.T) {
	store := testAutoUpdateStore(t)
	root := filepath.Dir(filepath.Dir(store.configPath))
	artifacts := legacyDockerUpdateArtifacts{
		binaryPath: filepath.Join(root, "bin", "linuxio-watchtower"),
		envPath:    store.legacyEnvPath,
		timerPath:  store.legacyTimerPath,
		unitPath:   filepath.Join(root, "systemd", "linuxio-watchtower.service"),
	}
	for path, content := range map[string]string{
		artifacts.binaryPath: "legacy binary",
		artifacts.envPath: `WATCHTOWER_MONITOR_ONLY=true
LINUXIO_WATCHTOWER_CONTAINERS=app\\.service redis
`,
		artifacts.timerPath: `[Timer]
OnCalendar=*-*-* 08:45:00
`,
		artifacts.unitPath: "[Service]\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}
	ops := newRecordingContainerUpdateOps()
	ops.unitFileState = "enabled"

	if err := migrateLegacyContainerUpdateSchedule(context.Background(), store, ops.ops(), artifacts); err != nil {
		t.Fatalf("migrateLegacyContainerUpdateSchedule: %v", err)
	}
	opts, err := store.readOptions()
	if err != nil {
		t.Fatalf("read migrated options: %v", err)
	}
	if opts.Mode != "check_only" || opts.Time != "08:45" || !reflect.DeepEqual(opts.ContainerNames, []string{"app.service", "redis"}) {
		t.Fatalf("migrated options = %+v", opts)
	}
	for _, path := range []string{artifacts.binaryPath, artifacts.envPath, artifacts.timerPath, artifacts.unitPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy artifact %s still exists: %v", path, err)
		}
	}
	wantCalls := []string{
		"stop:" + legacyWatchtowerTimerName,
		"disable:" + legacyWatchtowerTimerName,
		"stop:" + legacyWatchtowerUnitName,
		"reload",
		"enable:" + dockerUpdateTimerName,
		"start:" + dockerUpdateTimerName,
		"reload",
	}
	if !reflect.DeepEqual(ops.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", ops.calls, wantCalls)
	}
}

func TestNormalizeContainerAutoUpdateOptionsRejectsInvalidValues(t *testing.T) {
	if _, err := normalizeContainerAutoUpdateOptions(apischema.DockerContainerAutoUpdateOptions{Mode: "daemon", Time: "04:00"}); err == nil {
		t.Fatal("accepted invalid mode")
	}
	if _, err := normalizeContainerAutoUpdateOptions(apischema.DockerContainerAutoUpdateOptions{Mode: "update", Time: "99:00"}); err == nil {
		t.Fatal("accepted invalid time")
	}
}

func TestBuildContainerAutoUpdateTargetsPreservesMissingNames(t *testing.T) {
	containers := []container.Summary{
		{ID: "2", Names: []string{"/redis"}, Image: "redis:7", State: "running"},
		{ID: "1", Names: []string{"/app.service"}, Image: "example/app:1", State: "exited"},
	}
	selected := []string{"missing", "app.service"}

	targets := buildContainerAutoUpdateTargets(containers, selected)
	if len(targets) != 2 || targets[0].Name != "app.service" || !targets[0].Selected || targets[1].Name != "redis" || targets[1].Selected {
		t.Fatalf("targets = %#v", targets)
	}
	missing := missingSelectedContainerNames(containers, selected)
	if want := []string{"missing"}; !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing = %#v, want %#v", missing, want)
	}
}

type recordingContainerUpdateOps struct {
	calls         []string
	unitFileState string
}

func newRecordingContainerUpdateOps() *recordingContainerUpdateOps {
	return &recordingContainerUpdateOps{}
}

func (r *recordingContainerUpdateOps) ops() containerUpdateSystemdOps {
	return containerUpdateSystemdOps{
		daemonReload: func(context.Context) error {
			r.calls = append(r.calls, "reload")
			return nil
		},
		disableUnit: func(_ context.Context, unit string) error {
			r.calls = append(r.calls, "disable:"+unit)
			return nil
		},
		enableUnit: func(_ context.Context, unit string) error {
			r.calls = append(r.calls, "enable:"+unit)
			return nil
		},
		getActiveState: func(context.Context, string) (string, error) {
			return "inactive", nil
		},
		getUnitFileState: func(context.Context, string) (string, error) {
			return r.unitFileState, nil
		},
		startUnit: func(_ context.Context, unit string) error {
			r.calls = append(r.calls, "start:"+unit)
			return nil
		},
		stopUnit: func(_ context.Context, unit string) error {
			r.calls = append(r.calls, "stop:"+unit)
			return nil
		},
	}
}

func testAutoUpdateStore(t *testing.T) containerAutoUpdateStore {
	t.Helper()
	root := t.TempDir()
	return containerAutoUpdateStore{
		configPath:      filepath.Join(root, "etc", "linuxio", "docker-update.json"),
		legacyEnvPath:   filepath.Join(root, "etc", "linuxio", "watchtower.env"),
		legacyTimerPath: filepath.Join(root, "etc", "systemd", "system", "linuxio-watchtower.timer"),
		timerPath:       filepath.Join(root, "etc", "systemd", "system", dockerUpdateTimerName),
		unitPath:        filepath.Join(root, "etc", "systemd", "system", dockerUpdateUnitName),
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
