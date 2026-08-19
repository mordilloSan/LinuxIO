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
		IncludeStopped: true,
		Mode:           "check_only",
		ReviveStopped:  true,
		Time:           "06:15",
		UpdateStopped:  true,
	}

	if err := applyContainerAutoUpdate(context.Background(), store, ops.ops(), opts); err != nil {
		t.Fatalf("applyContainerAutoUpdate: %v", err)
	}

	var document dockerUpdateScheduleDocument
	if err := json.Unmarshal([]byte(readTestFile(t, store.configPath)), &document); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if document.Version != dockerUpdateConfigVersion || document.Mode != "check_only" || !document.Cleanup || !document.IncludeStopped || !document.UpdateStopped || !document.ReviveStopped || !reflect.DeepEqual(document.ContainerNames, []string{"app.service", "redis"}) {
		t.Fatalf("config = %+v", document)
	}
	if timer := readTestFile(t, store.timerPath); !strings.Contains(timer, "OnCalendar=*-*-* 06:15:00") || !strings.Contains(timer, "Unit="+dockerUpdateUnitName) {
		t.Fatalf("timer file did not render schedule:\n%s", timer)
	}
	unit := readTestFile(t, store.unitPath)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/linuxio-docker-update run --config "+DockerUpdateConfigPath) {
		t.Fatalf("unit file does not invoke the LinuxIO runner directly:\n%s", unit)
	}
	if strings.Contains(unit, "/bin/sh") {
		t.Fatalf("unit file contains a legacy shell:\n%s", unit)
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

func TestAutoUpdateStoreUsesDefaultsWithoutNativeConfig(t *testing.T) {
	opts, err := testAutoUpdateStore(t).readOptions()
	if err != nil {
		t.Fatalf("readOptions: %v", err)
	}
	if opts.Mode != "update" || opts.Time != defaultDockerUpdateTime || opts.Enabled || opts.Cleanup || len(opts.ContainerNames) != 0 {
		t.Fatalf("default options = %+v", opts)
	}
}

func TestNormalizeContainerAutoUpdateOptionsRejectsInvalidValues(t *testing.T) {
	if _, err := normalizeContainerAutoUpdateOptions(apischema.DockerContainerAutoUpdateOptions{Mode: "daemon", Time: "04:00"}); err == nil {
		t.Fatal("accepted invalid mode")
	}
	if _, err := normalizeContainerAutoUpdateOptions(apischema.DockerContainerAutoUpdateOptions{Mode: "update", Time: "99:00"}); err == nil {
		t.Fatal("accepted invalid time")
	}
	if _, err := normalizeContainerAutoUpdateOptions(apischema.DockerContainerAutoUpdateOptions{Mode: "update", Time: "04:00", ReviveStopped: true}); err == nil {
		t.Fatal("accepted revive-stopped without stopped-container updates")
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

func TestBuildContainerAutoUpdateTargetsMarksMutationEligibility(t *testing.T) {
	composeLabels := map[string]string{
		"com.docker.compose.project": "media",
		"com.docker.compose.service": "web",
	}
	targets := buildContainerAutoUpdateTargets([]container.Summary{
		{ID: "replica-1", Names: []string{"/web-1"}, State: "running", Labels: composeLabels},
		{ID: "replica-2", Names: []string{"/web-2"}, State: "running", Labels: composeLabels},
		{ID: "stopped", Names: []string{"/stopped"}, State: "exited"},
		{ID: "stopped-compose", Names: []string{"/stopped-compose"}, State: "exited", Labels: map[string]string{
			"com.docker.compose.project": "stopped-project",
			"com.docker.compose.service": "web",
		}},
		{ID: "standalone", Names: []string{"/standalone"}, State: "running"},
	}, nil)
	if len(targets) != 5 {
		t.Fatalf("targets = %#v", targets)
	}
	byName := make(map[string]apischema.DockerContainerAutoUpdateTarget, len(targets))
	for _, target := range targets {
		byName[target.Name] = target
	}
	for _, name := range []string{"web-1", "web-2"} {
		target := byName[name]
		if target.MutationAllowed ||
			target.MutationReason == nil ||
			!strings.Contains(*target.MutationReason, "media/web") ||
			!strings.Contains(*target.MutationReason, "2 replicas") {
			t.Fatalf("Compose target %s eligibility = %+v", name, target)
		}
	}
	if target := byName["stopped"]; !target.MutationAllowed || target.MutationReason != nil {
		t.Fatalf("stopped target eligibility = %+v", target)
	}
	if target := byName["stopped-compose"]; target.MutationAllowed || target.MutationReason == nil || !strings.Contains(*target.MutationReason, "Stopped Compose") {
		t.Fatalf("stopped Compose target eligibility = %+v", target)
	}
	if target := byName["standalone"]; !target.MutationAllowed || target.MutationReason != nil {
		t.Fatalf("standalone target eligibility = %+v", target)
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
		configPath: filepath.Join(root, "etc", "linuxio", "docker-update.json"),
		timerPath:  filepath.Join(root, "etc", "systemd", "system", dockerUpdateTimerName),
		unitPath:   filepath.Join(root, "etc", "systemd", "system", dockerUpdateUnitName),
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
