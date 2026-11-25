package docker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// shutdownStopSystemd stands in for systemd: it holds the unit file state and
// records what the code under test asked of it, in order.
type shutdownStopSystemd struct {
	state string
	calls []string
}

func stubShutdownStopSystemd(t *testing.T, state string) *shutdownStopSystemd {
	t.Helper()
	fake := &shutdownStopSystemd{state: state}
	record := func(call string) func(context.Context, string) error {
		return func(_ context.Context, name string) error {
			require.Equal(t, dockerShutdownStopUnit, name)
			fake.calls = append(fake.calls, call)
			return nil
		}
	}
	origReload, origEnable, origDisable := systemdDaemonReload, systemdEnableUnit, systemdDisableUnit
	origStart, origStop, origState := systemdStartUnitAndWait, systemdStopUnit, systemdUnitFileState
	systemdDaemonReload = func(context.Context) error {
		fake.calls = append(fake.calls, "reload")
		return nil
	}
	systemdEnableUnit = record("enable")
	systemdDisableUnit = record("disable")
	systemdStartUnitAndWait = record("start")
	systemdStopUnit = record("stop")
	systemdUnitFileState = func(_ context.Context, name string) (string, error) {
		require.Equal(t, dockerShutdownStopUnit, name)
		if fake.state == "" {
			return "", systemd.ErrNoUnitFile
		}
		return fake.state, nil
	}
	t.Cleanup(func() {
		systemdDaemonReload, systemdEnableUnit, systemdDisableUnit = origReload, origEnable, origDisable
		systemdStartUnitAndWait, systemdStopUnit, systemdUnitFileState = origStart, origStop, origState
	})
	return fake
}

func TestSyncDockerShutdownStopInstallsEnablesAndStarts(t *testing.T) {
	fake := stubShutdownStopSystemd(t, "")
	path := filepath.Join(t.TempDir(), dockerShutdownStopUnit)

	require.NoError(t, syncDockerShutdownStop(t.Context(), path, "unit text\n"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "unit text\n", string(data))
	require.Equal(t, []string{"reload", "enable", "start"}, fake.calls)
}

func TestSyncDockerShutdownStopChangesNothingWhenTheHostMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), dockerShutdownStopUnit)

	off := stubShutdownStopSystemd(t, "")
	require.NoError(t, syncDockerShutdownStop(t.Context(), path, ""))
	require.Empty(t, off.calls)
	require.NoFileExists(t, path)

	require.NoError(t, os.WriteFile(path, []byte("unit text\n"), 0o644))
	on := stubShutdownStopSystemd(t, "enabled")
	require.NoError(t, syncDockerShutdownStop(t.Context(), path, "unit text\n"))
	require.Empty(t, on.calls)
}

// A unit an administrator disabled by hand, or one an older LinuxIO wrote
// with another text, is written and enabled again.
func TestSyncDockerShutdownStopRepairsADisabledOrOutdatedUnit(t *testing.T) {
	for name, tt := range map[string]struct{ state, onDisk string }{
		"disabled": {state: "disabled", onDisk: "unit text\n"},
		"outdated": {state: "enabled", onDisk: "older text\n"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := stubShutdownStopSystemd(t, tt.state)
			path := filepath.Join(t.TempDir(), dockerShutdownStopUnit)
			require.NoError(t, os.WriteFile(path, []byte(tt.onDisk), 0o644))

			require.NoError(t, syncDockerShutdownStop(t.Context(), path, "unit text\n"))

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "unit text\n", string(data))
			require.Equal(t, []string{"reload", "enable", "start"}, fake.calls)
		})
	}
}

func TestSyncDockerShutdownStopRemovesTheUnit(t *testing.T) {
	fake := stubShutdownStopSystemd(t, "enabled")
	path := filepath.Join(t.TempDir(), dockerShutdownStopUnit)
	require.NoError(t, os.WriteFile(path, []byte("unit text\n"), 0o644))

	require.NoError(t, syncDockerShutdownStop(t.Context(), path, ""))

	require.NoFileExists(t, path)
	require.Equal(t, []string{"disable", "stop", "reload"}, fake.calls)
}

func TestDockerShutdownStopEnabledReadsTheUnitFileState(t *testing.T) {
	for state, want := range map[string]bool{"": false, "disabled": false, "enabled": true} {
		stubShutdownStopSystemd(t, state)
		enabled, err := dockerShutdownStopEnabled(t.Context())
		require.NoError(t, err)
		require.Equal(t, want, enabled, "state %q", state)
	}
}

func stubShutdownStopLookPath(t *testing.T, path string, err error) {
	t.Helper()
	orig := shutdownStopLookPath
	shutdownStopLookPath = func(name string) (string, error) {
		require.Equal(t, "docker", name)
		return path, err
	}
	t.Cleanup(func() { shutdownStopLookPath = orig })
}

func TestDockerShutdownStopUnitFor(t *testing.T) {
	stubShutdownStopLookPath(t, "", exec.ErrNotFound)
	unit, err := dockerShutdownStopUnitFor(apischema.DockerDaemonConfig{})
	require.NoError(t, err)
	require.Empty(t, unit, "the setting is off: nothing is looked up")

	_, err = dockerShutdownStopUnitFor(apischema.DockerDaemonConfig{StopBeforeShutdown: true})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, "the setting needs live restore")

	on := apischema.DockerDaemonConfig{LiveRestore: true, StopBeforeShutdown: true}
	_, err = dockerShutdownStopUnitFor(on)
	require.ErrorIs(t, err, exec.ErrNotFound)

	stubShutdownStopLookPath(t, "/opt/my docker/docker", nil)
	_, err = dockerShutdownStopUnitFor(on)
	require.ErrorContains(t, err, "cannot go into a systemd command line")

	stubShutdownStopLookPath(t, "/usr/bin/docker", nil)
	unit, err = dockerShutdownStopUnitFor(on)
	require.NoError(t, err)
	require.Contains(t, unit, "/usr/bin/docker stop $$ids")
}

// The save is refused before it writes anything on a host that cannot have
// the unit, and a failed save leaves the unit alone.
func TestWithDockerShutdownStopRendersBeforeTheSaveAndSyncsAfterIt(t *testing.T) {
	fake := stubShutdownStopSystemd(t, "")
	stubShutdownStopLookPath(t, "", exec.ErrNotFound)
	saved := false
	save := func() (apischema.DockerDaemonConfigSetResult, error) {
		saved = true
		return apischema.DockerDaemonConfigSetResult{}, nil
	}

	on := apischema.DockerDaemonConfig{LiveRestore: true, StopBeforeShutdown: true}
	_, err := withDockerShutdownStop(t.Context(), on, save)
	require.ErrorIs(t, err, exec.ErrNotFound)
	require.False(t, saved)

	stubShutdownStopLookPath(t, "/usr/bin/docker", nil)
	failed := errors.New("save failed")
	_, err = withDockerShutdownStop(t.Context(), on, func() (apischema.DockerDaemonConfigSetResult, error) {
		return apischema.DockerDaemonConfigSetResult{}, failed
	})
	require.ErrorIs(t, err, failed)
	require.Empty(t, fake.calls)
}

func TestRenderDockerShutdownStopUnit(t *testing.T) {
	unit := renderDockerShutdownStopUnit("/usr/bin/docker", dockerShutdownStopList)

	require.NotContains(t, unit, "@", "a placeholder was left in")
	var settings []string
	for line := range strings.Lines(unit) {
		if !strings.HasPrefix(line, "#") {
			settings = append(settings, strings.TrimSpace(line))
		}
	}
	require.Contains(t, settings, "WantedBy=docker.service")
	require.Contains(t, settings, "StateDirectory=linuxio/docker-shutdown")
	require.Contains(t, settings, "Environment=PATH=/usr/sbin:/sbin:/usr/bin:/bin")
	require.Contains(t, settings, "After=docker.service network-online.target")
	// A Docker restart or upgrade must not stop the unit, and starting the
	// unit must not start a Docker that was stopped.
	for _, line := range settings {
		for _, tie := range []string{"Requires=", "PartOf=", "BindsTo=", "Wants="} {
			require.False(t, strings.HasPrefix(line, tie), "%s ties the unit to docker.service", line)
		}
	}
	// The unit runs without LinuxIO: it names no LinuxIO program.
	require.NotContains(t, strings.Join(settings, "\n"), "/usr/libexec/linuxio")
}

// The unit stays when LinuxIO is removed, as Docker's drop-ins do: no removal
// script stops, disables or deletes it.
func TestPackageRemovalKeepsTheShutdownUnit(t *testing.T) {
	for _, script := range []string{"debian/prerm", "debian/postrm", "rpm/linuxio.spec"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", script))
		require.NoError(t, err)
		require.NotContains(t, string(data), dockerShutdownStopUnit, "packaging/%s", script)
	}
}

// shutdownStopHost is a folder of stand-ins for docker, systemctl and sleep,
// and the two commands of the unit as sh would get them from systemd.
type shutdownStopHost struct {
	t           *testing.T
	dir, list   string
	start, stop string
}

// newShutdownStopHost renders the unit against a fake docker. The fake
// answers "ps -q" and "inspect" from the files ps and inspect, fails "start"
// as many times as the file start-failures says, and logs every call.
func newShutdownStopHost(t *testing.T) *shutdownStopHost {
	t.Helper()
	for _, tool := range []string{"sh", "xargs", "sed"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	dir := t.TempDir()
	write := func(name, script string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755))
	}
	write("docker", `echo "$*" >> "`+dir+`/docker.log"
case "$1" in
ps) cat "`+dir+`/ps" 2>/dev/null ;;
inspect) cat "`+dir+`/inspect" 2>/dev/null ;;
start)
	left=$(cat "`+dir+`/start-failures" 2>/dev/null || echo 0)
	[ "$left" -gt 0 ] || exit 0
	echo $((left - 1)) > "`+dir+`/start-failures"
	exit 1
	;;
esac
`)
	write("systemctl", `case "$1" in
is-system-running) cat "`+dir+`/system-state" ;;
is-active) [ -e "`+dir+`/docker-active" ] ;;
esac
`)
	write("sleep", "exit 0\n")

	host := &shutdownStopHost{t: t, dir: dir, list: filepath.Join(dir, "running")}
	unit := renderDockerShutdownStopUnit(filepath.Join(dir, "docker"), host.list)
	host.start = shutdownStopCommand(t, unit, "ExecStart")
	host.stop = shutdownStopCommand(t, unit, "ExecStop")
	host.set("system-state", "running\n")
	host.set("docker-active", "")
	return host
}

// shutdownStopCommand is the script of one Exec line as systemd hands it to
// sh: the single quotes taken off and every "$$" read as "$".
func shutdownStopCommand(t *testing.T, unit, key string) string {
	t.Helper()
	for line := range strings.Lines(unit) {
		if script, ok := strings.CutPrefix(strings.TrimSpace(line), key+"=/bin/sh -c '"); ok {
			script, ok = strings.CutSuffix(script, "'")
			require.True(t, ok, "%s does not end its quote", key)
			require.NotContains(t, script, "'")
			require.NotContains(t, script, "%", "systemd expands a %% specifier")
			require.NotContains(t, strings.ReplaceAll(script, "$$", ""), "$", "systemd expands a single $")
			return strings.ReplaceAll(script, "$$", "$")
		}
	}
	t.Fatalf("the unit has no %s", key)
	return ""
}

func (h *shutdownStopHost) set(name, content string) {
	h.t.Helper()
	require.NoError(h.t, os.WriteFile(filepath.Join(h.dir, name), []byte(content), 0o644))
}

// dockerStopped makes the fake systemctl report docker.service as not running.
func (h *shutdownStopHost) dockerStopped() {
	h.t.Helper()
	require.NoError(h.t, os.Remove(filepath.Join(h.dir, "docker-active")))
}

// run runs one of the unit's commands and returns its exit status.
func (h *shutdownStopHost) run(script string) int {
	h.t.Helper()
	cmd := exec.CommandContext(h.t.Context(), "sh", "-c", script)
	cmd.Env = []string{"PATH=" + h.dir + ":/usr/sbin:/sbin:/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		h.t.Fatalf("run the command: %v\n%s", err, out)
	}
	return cmd.ProcessState.ExitCode()
}

// dockerCalls is every docker command line the unit ran, in order.
func (h *shutdownStopHost) dockerCalls() []string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "docker.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(h.t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestShutdownStopLeavesContainersAloneUnlessTheHostShutsDown(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("ps", "aaa\nbbb\n")

	require.Zero(t, host.run(host.stop))

	require.Empty(t, host.dockerCalls())
	require.NoFileExists(t, host.list)
}

func TestShutdownStopStopsEveryContainerAndSavesTheUnlessStoppedOnes(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("system-state", "stopping\n")
	host.set("ps", "aaa\nbbb\nccc\nddd\neee\nfff\n")
	host.set("inspect", strings.Join([]string{
		"false/unless-stopped/aaa-long",
		"false/always/bbb-long",
		"false/on-failure/ccc-long",
		"false/no/ddd-long",
		"false//eee-long",
		"true/unless-stopped/fff-long",
		"",
	}, "\n"))

	require.Zero(t, host.run(host.stop))

	saved, err := os.ReadFile(host.list)
	require.NoError(t, err)
	require.Equal(t, "aaa-long\n", string(saved))
	calls := host.dockerCalls()
	require.Len(t, calls, 3)
	require.Equal(t, "ps -q", calls[0])
	require.True(t, strings.HasPrefix(calls[1], "inspect -f "), calls[1])
	require.True(t, strings.HasSuffix(calls[1], " aaa bbb ccc ddd eee fff"), calls[1])
	require.Equal(t, "stop aaa bbb ccc ddd eee fff", calls[2])
}

// A Docker stopped by hand is not started again through its socket.
func TestShutdownStopLeavesAStoppedDockerAlone(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("system-state", "stopping\n")
	host.dockerStopped()

	require.Zero(t, host.run(host.stop))

	require.Empty(t, host.dockerCalls())
}

func TestShutdownStopWritesNoListWithoutContainers(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("system-state", "stopping\n")

	require.Zero(t, host.run(host.stop))

	require.Equal(t, []string{"ps -q"}, host.dockerCalls())
	require.NoFileExists(t, host.list)
}

func TestShutdownStartDoesNothingWithoutAList(t *testing.T) {
	host := newShutdownStopHost(t)

	require.Zero(t, host.run(host.start))

	require.Empty(t, host.dockerCalls())
}

func TestShutdownStartStartsTheListAndDeletesIt(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("running", "aaa-long\nbbb-long\n")

	require.Zero(t, host.run(host.start))

	require.Equal(t, []string{"start aaa-long bbb-long"}, host.dockerCalls())
	require.NoFileExists(t, host.list)
}

// A boot at which Docker did not start keeps the list for Docker's next start.
func TestShutdownStartKeepsTheListWhileDockerIsNotRunning(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("running", "aaa-long\n")
	host.dockerStopped()

	require.NotZero(t, host.run(host.start))

	require.Empty(t, host.dockerCalls())
	require.FileExists(t, host.list)
}

func TestShutdownStartTriesAgainUntilTheContainersStart(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("running", "aaa-long\n")
	host.set("start-failures", "2\n")

	require.Zero(t, host.run(host.start))

	require.Len(t, host.dockerCalls(), 3)
	require.NoFileExists(t, host.list)
}

// The known limit: after five tries the unit fails and the list is gone.
func TestShutdownStartFailsAfterFiveTries(t *testing.T) {
	host := newShutdownStopHost(t)
	host.set("running", "aaa-long\n")
	host.set("start-failures", "99\n")

	require.NotZero(t, host.run(host.start))

	require.Len(t, host.dockerCalls(), 5)
	require.NoFileExists(t, host.list)
}
