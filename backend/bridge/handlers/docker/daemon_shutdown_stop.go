package docker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// The unit that stops containers through Docker before the host shuts down.
// See docs/docker-shutdown.md.
const (
	dockerShutdownStopUnit     = "linuxio-docker-shutdown.service"
	dockerShutdownStopUnitPath = "/etc/systemd/system/" + dockerShutdownStopUnit
	// The folder is the unit's StateDirectory, so systemd makes it.
	dockerShutdownStopState = "linuxio/docker-shutdown"
	dockerShutdownStopList  = "/var/lib/" + dockerShutdownStopState + "/running"
)

var (
	systemdEnableUnit       = systemd.EnableUnit
	systemdDisableUnit      = systemd.DisableUnit
	systemdStartUnitAndWait = systemd.StartUnitAndWait
	systemdStopUnit         = systemd.StopUnit
	systemdUnitFileState    = systemd.GetUnitFileState
	shutdownStopLookPath    = lookSystemPath
)

// dockerShutdownStopUnitText is the whole unit, plain shell on the Docker
// CLI. Removing the package disables and deletes it (packaging/debian/prerm
// and postrm, the RPM's %preun and %postun). @DOCKER@, @LIST@, @STATE@ and @PATH@ are filled in by
// renderDockerShutdownStopUnit; a "$$" is systemd's way to write one "$".
const dockerShutdownStopUnitText = `# Written by LinuxIO: Settings, Docker, "Stop containers before shutdown". Removing LinuxIO
# disables and deletes it.
#
# Stops running containers through Docker before shutdown, and at boot starts those of them whose
# restart policy is unless-stopped.
# Why: live-restore leaves containers running when docker.service stops, so systemd ends them:
# SIGTERM to every process and, for one that ignores it, SIGKILL 90 s later (an image that runs
# systemd stops on SIGRTMIN+3 and ignores SIGTERM). docker stop uses each container's own stop
# signal and grace period instead.
# docker stop marks a container as manually stopped, so restart: unless-stopped would leave it
# down after the reboot: the IDs of the running unless-stopped containers are saved and started
# again by ExecStart.
# Only unless-stopped is saved, because it is the only policy docker stop breaks across a reboot:
# when the daemon starts, Docker itself starts an always container and applies on-failure by the
# exit code, manually stopped or not. A container with no restart policy (restart: no) stays down
# after the reboot. All three behave as they do on Docker without live-restore.
# A Docker restart or upgrade does not stop this unit (no Requires/PartOf), so live-restore still
# keeps containers running then. ExecStop acts only while the system is shutting down, so a manual
# restart or stop of this unit leaves the containers alone.
# One docker stop gets every ID: the CLI already stops them in parallel (up to 50 at once).
# Containers started with --rm are stopped but not saved: docker stop deletes them, so their
# IDs could not start again. The list is made before the stop, not from what still exists after
# it, because the daemon removes them just after docker stop returns.
# The list: docker inspect prints "<AutoRemove>/<restart policy>/<ID>" for each container, and
# sed keeps the ID of the lines that start with "false/unless-stopped/".
# ExecStop does nothing while docker.service is not running: docker ps would start it through
# docker.socket in the middle of the shutdown.
# ExecStart leaves the list alone and fails while docker.service is not running, so a boot at
# which Docker did not start loses nothing: docker.service wants this unit, and its next start
# runs ExecStart again.
# ExecStart tries the whole list up to five times, 30 s apart, for a start that fails because
# something is not there yet (a mount, an address). Starting a running container changes nothing.
# Known limit: an unless-stopped container that still fails after those tries stays down at later
# reboots too, because docker stop marked it manually stopped and the next shutdown no longer
# lists it. Stock Docker would retry it at each boot. The unit then fails, so it shows up in
# systemctl --failed. A failed unit runs no ExecStop, so that one shutdown is the old slow one;
# the boot after it finds no list and the unit is active again.
# PATH names the system folders only: the default one starts with /usr/local, which a group other
# than root can write on some distributions.
[Unit]
Description=Stop Docker containers before shutdown and start them again at boot
After=docker.service network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
StateDirectory=@STATE@
Environment=PATH=@PATH@
ExecStart=/bin/sh -c 'f=@LIST@; [ -s "$$f" ] || exit 0; systemctl is-active --quiet docker.service || exit 1; n=0; rc=0; until xargs -r @DOCKER@ start < "$$f"; do n=$$((n+1)); if [ "$$n" -ge 5 ]; then rc=1; break; fi; sleep 30; done; rm -f "$$f"; exit $$rc'
ExecStop=/bin/sh -c '[ "$$(systemctl is-system-running)" = stopping ] || exit 0; systemctl is-active --quiet docker.service || exit 0; f=@LIST@; ids=$$(@DOCKER@ ps -q); [ -n "$$ids" ] || exit 0; @DOCKER@ inspect -f "{{.HostConfig.AutoRemove}}/{{.HostConfig.RestartPolicy.Name}}/{{.Id}}" $$ids | sed -n "s,^false/unless-stopped/,,p" > "$$f"; @DOCKER@ stop $$ids'
TimeoutStopSec=180

[Install]
WantedBy=docker.service
`

// withDockerShutdownStop runs save, which writes the daemon settings of req,
// and then installs or removes the shutdown unit as req asks. The unit is
// rendered first, so a host that cannot have it is refused before save writes
// anything. A cancel does not reach the unit: by then save is done, and for a
// storage change Docker has already restarted.
func withDockerShutdownStop(ctx context.Context, req apischema.DockerDaemonConfig, save func() (apischema.DockerDaemonConfigSetResult, error)) (apischema.DockerDaemonConfigSetResult, error) {
	unit, err := dockerShutdownStopUnitFor(req)
	if err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	result, err := save()
	if err != nil {
		return result, err
	}
	if err := syncDockerShutdownStop(context.WithoutCancel(ctx), dockerShutdownStopUnitPath, unit); err != nil {
		return apischema.DockerDaemonConfigSetResult{}, err
	}
	result.Config.StopBeforeShutdown = req.StopBeforeShutdown
	return result, nil
}

// dockerShutdownStopUnitFor returns the unit the request asks for, or "" for
// none. It fails before anything is written on a host without the Docker CLI,
// where the unit would stop nothing and say nothing.
func dockerShutdownStopUnitFor(req apischema.DockerDaemonConfig) (string, error) {
	if !req.StopBeforeShutdown {
		return "", nil
	}
	// Without live restore dockerd stops the containers itself, with their
	// own signals and grace periods.
	if !req.LiveRestore {
		return "", fmt.Errorf("%w: stopping containers before shutdown needs live restore", bridgeipc.ErrInvalidArgs)
	}
	docker, err := shutdownStopLookPath("docker")
	if err != nil {
		return "", fmt.Errorf("find the docker CLI for the shutdown unit: %w", err)
	}
	if !hostIPWaitToolPath.MatchString(docker) {
		return "", fmt.Errorf("docker is at %q, which cannot go into a systemd command line", docker)
	}
	return renderDockerShutdownStopUnit(docker, dockerShutdownStopList), nil
}

func renderDockerShutdownStopUnit(docker, list string) string {
	return strings.NewReplacer(
		"@DOCKER@", docker,
		"@LIST@", list,
		"@STATE@", dockerShutdownStopState,
		"@PATH@", strings.Join(hostcmd.SearchDirs, ":"),
	).Replace(dockerShutdownStopUnitText)
}

// dockerShutdownStopEnabled reports whether systemd starts the unit with
// Docker. That is the setting: no LinuxIO file records it.
func dockerShutdownStopEnabled(ctx context.Context) (bool, error) {
	state, err := systemdUnitFileState(ctx, dockerShutdownStopUnit)
	if errors.Is(err, systemd.ErrNoUnitFile) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return state == "enabled", nil
}

// syncDockerShutdownStop makes the host match unit: the text to install and
// enable, or "" to remove it. It changes nothing when the host already matches.
func syncDockerShutdownStop(ctx context.Context, path, unit string) error {
	enabled, err := dockerShutdownStopEnabled(ctx)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read the docker shutdown unit: %w", err)
	}
	if unit == "" {
		if !exists {
			return nil
		}
		return removeDockerShutdownStop(ctx, path)
	}
	if enabled && string(current) == unit {
		return nil
	}
	return installDockerShutdownStop(ctx, path, unit)
}

func installDockerShutdownStop(ctx context.Context, path, unit string) error {
	if err := atomicfile.Write(path, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write the docker shutdown unit: %w", err)
	}
	if err := systemdDaemonReload(ctx); err != nil {
		return err
	}
	if err := systemdEnableUnit(ctx, dockerShutdownStopUnit); err != nil {
		return err
	}
	// Started now: only an active unit has its ExecStop run, and the next
	// shutdown may come before Docker's next start.
	if err := systemdStartUnitAndWait(ctx, dockerShutdownStopUnit); err != nil {
		return err
	}
	slog.Info("enabled stopping docker containers before shutdown", "component", "docker", "unit", dockerShutdownStopUnit)
	return nil
}

func removeDockerShutdownStop(ctx context.Context, path string) error {
	if err := systemdDisableUnit(ctx, dockerShutdownStopUnit); err != nil {
		return err
	}
	// The stop leaves the containers running: ExecStop acts only while the
	// host shuts down.
	if err := systemdStopUnit(ctx, dockerShutdownStopUnit); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the docker shutdown unit: %w", err)
	}
	if err := systemdDaemonReload(ctx); err != nil {
		return err
	}
	slog.Info("disabled stopping docker containers before shutdown", "component", "docker", "unit", dockerShutdownStopUnit)
	return nil
}
