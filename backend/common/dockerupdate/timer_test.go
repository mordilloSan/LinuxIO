package dockerupdate

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func TestApplyContainerAutoUpdateEnablesNativeTimerAndWritesFiles(t *testing.T) {
	store := testAutoUpdateStore(t)
	ops := newRecordingContainerUpdateOps()
	opts := DockerContainerAutoUpdateOptions{
		Cleanup:        true,
		ContainerNames: []string{"app.service", "redis"},
		IncludeStopped: true,
		Mode:           "check_only",
		ReviveStopped:  true,
		Time:           "06:15",
		UpdateStopped:  true,
		Enabled:        true,
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
	if timer := readTestFile(t, store.timerPath); !strings.Contains(timer, "OnCalendar=*-*-* 06:15:00") || !strings.Contains(timer, "RandomizedDelaySec=1m\n") || !strings.Contains(timer, "Unit="+dockerUpdateUnitName) {
		t.Fatalf("timer file did not render schedule:\n%s", timer)
	}
	// The service is a package unit: a save writes the timer alone.
	if _, err := os.Lstat(filepath.Join(filepath.Dir(store.timerPath), dockerUpdateUnitName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a save wrote %s beside the timer: %v", dockerUpdateUnitName, err)
	}

	wantCalls := []string{"reload", "enable:" + dockerUpdateTimerName, "start:" + dockerUpdateTimerName}
	if !reflect.DeepEqual(ops.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", ops.calls, wantCalls)
	}
}

func TestApplyContainerAutoUpdateDisablesNativeTimer(t *testing.T) {
	store := testAutoUpdateStore(t)
	ops := newRecordingContainerUpdateOps()
	opts := DockerContainerAutoUpdateOptions{
		Mode:    "update",
		Time:    "04:00",
		Enabled: false,
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
	if _, err := normalizeContainerAutoUpdateOptions(DockerContainerAutoUpdateOptions{Mode: "daemon", Time: "04:00"}); err == nil {
		t.Fatal("accepted invalid mode")
	}
	if _, err := normalizeContainerAutoUpdateOptions(DockerContainerAutoUpdateOptions{Mode: "update", Time: "99:00"}); err == nil {
		t.Fatal("accepted invalid time")
	}
	if _, err := normalizeContainerAutoUpdateOptions(DockerContainerAutoUpdateOptions{Mode: "update", Time: "04:00", ReviveStopped: true}); err == nil {
		t.Fatal("accepted revive-stopped without stopped-container updates")
	}
	for _, minFree := range []int{-1, maxMinFreeGiB + 1} {
		settings := DockerContainerAutoUpdateSettings{Mode: "update", Time: "04:00", MinFreeGiB: minFree}
		_, err := normalizeContainerAutoUpdateOptions(DockerContainerAutoUpdateOptions{DockerContainerAutoUpdateSettings: settings})
		if !errors.Is(err, ErrInvalidSettings) {
			t.Fatalf("free-space minimum %d: error = %v, want ErrInvalidSettings", minFree, err)
		}
	}
	if _, err := normalizeContainerAutoUpdateOptions(DockerContainerAutoUpdateOptions{Mode: "daemon"}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidSettings", err)
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
	byName := make(map[string]DockerContainerAutoUpdateTarget, len(targets))
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

// A run of a Docker job is never a target, and is no replica of the service
// whose labels it carries.
func TestBuildContainerAutoUpdateTargetsRefusesDockerJobRuns(t *testing.T) {
	labels := map[string]string{
		"com.docker.compose.project": "media",
		"com.docker.compose.service": "web",
	}
	jobRun := map[string]string{
		"com.docker.compose.project": "media",
		"com.docker.compose.service": "web",
		dockerJobLabel:               "nightly",
	}
	targets := buildContainerAutoUpdateTargets([]container.Summary{
		{ID: "web", Names: []string{"/web"}, State: "running", Labels: labels},
		{ID: "run", Names: []string{"/media-nightly-1"}, State: "running", Labels: jobRun},
	}, nil)
	byName := make(map[string]DockerContainerAutoUpdateTarget, len(targets))
	for _, target := range targets {
		byName[target.Name] = target
	}
	if target := byName["web"]; !target.MutationAllowed {
		t.Fatalf("service eligibility = %+v, want allowed beside a job run", target)
	}
	if target := byName["media-nightly-1"]; target.MutationAllowed || target.MutationReason == nil || !strings.Contains(*target.MutationReason, "job") {
		t.Fatalf("job run eligibility = %+v, want refused", target)
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

// The service is the package unit packaging/systemd/linuxio-docker-update.service.
// It runs the updater this package names with the configuration it saves,
// writes only where the updater keeps its state, and gives a rollback in
// progress the time it needs when the pass is stopped.
func TestPackagedDockerUpdateUnit(t *testing.T) {
	unit := readTestFile(t, filepath.Join("..", "..", "..", "packaging", "systemd", dockerUpdateUnitName))
	for _, want := range []string{
		"After=docker.service network-online.target\n",
		"Requires=docker.service\n",
		"Wants=network-online.target\n",
		"ExecStart=" + RunnerPath + " --config " + ConfigPath + "\n",
		"NoNewPrivileges=true\n",
		"PrivateTmp=true\n",
		"ProtectHome=read-only\n",
		"ProtectSystem=strict\n",
		"ReadWritePaths=" + version.DataDir + " /run\n",
		"KillMode=mixed\n",
		fmt.Sprintf("TimeoutStopSec=%d\n", int(dockerUpdateStopTimeout.Seconds())),
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit file is missing %q:\n%s", want, unit)
		}
	}
	if dockerUpdateStopTimeout <= composeRollbackTimeout+composeTagTimeout {
		t.Errorf("stop timeout %v leaves no room for a rollback of %v", dockerUpdateStopTimeout, composeRollbackTimeout)
	}
	// The scheduled run hooks live in the packaged drop-in
	// (linuxio-run-record.conf), never in the unit.
	if strings.Contains(unit, "scheduled-run") {
		t.Errorf("unit carries a run hook:\n%s", unit)
	}
}

func TestScheduledRunKnowsTheDockerUpdateUnit(t *testing.T) {
	if kind, ok := scheduledrun.KindOf(dockerUpdateUnitName, ""); !ok || kind.Route != scheduledrun.RouteDockerUpdate {
		t.Errorf("KindOf(%q) = %+v, %v", dockerUpdateUnitName, kind, ok)
	}
}

// Removing the package deletes the Docker update timer, which LinuxIO wrote while it ran:
// each path is in the removal lists of both packages.
func TestPackageRemovalDeletesTheTimer(t *testing.T) {
	for _, script := range []string{"debian/postrm", "rpm/linuxio.spec"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", script))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{dockerUpdateTimerPath} {
			if !slices.Contains(strings.Fields(string(data)), path) {
				t.Errorf("packaging/%s does not delete %s", script, path)
			}
		}
	}
}
