package docker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/watchtower"
)

func TestApplyContainerAutoUpdateEnablesTimerAndWritesFiles(t *testing.T) {
	store := testAutoUpdateStore(t)
	ops := newRecordingWatchtowerOps()
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

	env := readTestFile(t, store.envPath)
	for _, want := range []string{
		"WATCHTOWER_NO_STARTUP_MESSAGE=true",
		"WATCHTOWER_MONITOR_ONLY=true",
		"WATCHTOWER_CLEANUP=true",
		`LINUXIO_WATCHTOWER_CONTAINERS=app\\.service redis`,
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q:\n%s", want, env)
		}
	}
	if timer := readTestFile(t, store.timerPath); !strings.Contains(timer, "OnCalendar=*-*-* 06:15:00") {
		t.Fatalf("timer file did not render schedule:\n%s", timer)
	}
	if unit := readTestFile(t, store.unitPath); !strings.Contains(unit, "ExecCondition=") {
		t.Fatalf("unit file missing ExecCondition:\n%s", unit)
	}

	wantCalls := []string{"reload", "enable:" + watchtower.TimerName, "start:" + watchtower.TimerName}
	if !reflect.DeepEqual(ops.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", ops.calls, wantCalls)
	}
}

func TestApplyContainerAutoUpdateDisablesTimer(t *testing.T) {
	store := testAutoUpdateStore(t)
	ops := newRecordingWatchtowerOps()
	opts := apischema.DockerContainerAutoUpdateOptions{
		ContainerNames: []string{},
		Enabled:        false,
		Mode:           "update",
		Time:           "04:00",
	}

	if err := applyContainerAutoUpdate(context.Background(), store, ops.ops(), opts); err != nil {
		t.Fatalf("applyContainerAutoUpdate: %v", err)
	}
	if env := readTestFile(t, store.envPath); !strings.Contains(env, "LINUXIO_WATCHTOWER_CONTAINERS="+watchtower.NoContainersID) {
		t.Fatalf("empty selection did not write sentinel:\n%s", env)
	}
	wantCalls := []string{"reload", "stop:" + watchtower.TimerName, "disable:" + watchtower.TimerName}
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

type recordingWatchtowerOps struct {
	calls []string
}

func newRecordingWatchtowerOps() *recordingWatchtowerOps {
	return &recordingWatchtowerOps{}
}

func (r *recordingWatchtowerOps) ops() watchtowerSystemdOps {
	return watchtowerSystemdOps{
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
			return "disabled", nil
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

func testAutoUpdateStore(t *testing.T) watchtowerAutoUpdateStore {
	t.Helper()
	root := t.TempDir()
	return watchtowerAutoUpdateStore{
		envPath:   filepath.Join(root, "etc", "linuxio", "watchtower.env"),
		timerPath: filepath.Join(root, "etc", "systemd", "system", watchtower.TimerName),
		unitPath:  filepath.Join(root, "etc", "systemd", "system", watchtower.UnitName),
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
