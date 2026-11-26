# Stopping containers before shutdown

With live restore on, containers keep running while `docker.service` stops,
so a Docker restart or upgrade does not interrupt them. The same happens when
the host shuts down: dockerd leaves the containers to systemd, which sends
every process SIGTERM and, 90 seconds later, SIGKILL to what is left. A
container whose main process ignores SIGTERM (an image that runs systemd stops
on SIGRTMIN+3) holds every reboot for those 90 seconds, and a container with a
longer `stop_grace_period`, such as a database, is cut short.

The toggle *Stop containers before shutdown* (Settings → Docker, under Live
restore) installs a systemd unit that stops the containers through Docker
before `docker.service` stops, so each gets its own stop signal and grace
period, as it does without live restore. A Docker restart or upgrade still
leaves them running.

## The unit

`linuxio-docker-shutdown.service`, written to `/etc/systemd/system` by
`docker.set_daemon_config` (or the storage change, which saves the same form)
and enabled with `WantedBy=docker.service`. Its text is fixed in
`backend/bridge/handlers/docker/daemon_shutdown_stop.go`, with comments that
explain each step to an administrator reading it on the host. It is plain
shell on the Docker CLI:

- **ExecStop** runs only while `systemctl is-system-running` says
  `stopping` and `docker.service` is active, so stopping or restarting the
  unit by hand leaves the containers alone, and a Docker stopped by hand is
  not started again through `docker.socket` during the shutdown. It saves the
  IDs of the running containers whose restart policy is `unless-stopped` and
  that are not `--rm` to `/var/lib/linuxio/docker-shutdown/running`, then
  stops every running container with one `docker stop`, which stops them in
  parallel. The unit is ordered after `docker.service`, so systemd runs this
  before it stops Docker. `TimeoutStopSec=180`.
- **ExecStart** starts the saved IDs at boot, trying up to five times 30
  seconds apart for a start that waits on a mount or an address, then deletes
  the list. While `docker.service` is not running it fails and keeps the
  list; Docker's next start pulls the unit in and runs it again.

Only `unless-stopped` is saved because it is the only policy `docker stop`
breaks across a reboot: `docker stop` marks a container as stopped by hand,
and dockerd then leaves an `unless-stopped` container down. When dockerd
starts it still starts an `always` container and applies `on-failure` by the
exit code, and a container with no policy stays down, so those behave as they
do without live restore.

The unit is installed only with live restore on: without it dockerd stops the
containers itself, so the form greys the toggle out and turning live restore
off turns it off. A request with the toggle on and live restore off is
refused with 400. On a host whose `docker` is not in `/usr/sbin`, `/sbin`,
`/usr/bin` or `/bin` the save fails before it writes anything, since the unit
would stop nothing. The unit sets `PATH` to those
folders, since the default one starts with `/usr/local`, which a group other
than root can write on some distributions.

The setting is the unit's own state: the form shows it on when systemd
reports the unit enabled, and no LinuxIO file records it. A save writes the
file, reloads systemd, enables the unit and starts it (only an active unit
has its stop step run, and the next shutdown may come before Docker's next
start); it does nothing when the file and its enabled state already match.
Turning it off disables and stops the unit, which leaves the containers
running, and deletes the file. Both log one Info line. The route is
`Privileged()`, as every Docker write is.

## Removal

Removing the package removes the unit, as it does the other units LinuxIO
writes for its own settings ([Removal](./packaging.md#removal)). The DEB's
`prerm` and the RPM's `%preun` stop and disable it, which takes its link out
of `docker.service.wants`; the stop leaves the containers running, since
`ExecStop` acts only while the host shuts down. The DEB's `postrm` and the
RPM's `%postun` then delete the file. `TestPackageRemovalDeletesTheShutdownUnit`
holds both packages to that. Without the unit the host is back to systemd
ending the containers at shutdown, and a new install shows the toggle off.

## Compared with Docker without live restore

| | Live restore off | Live restore on with the unit |
|---|---|---|
| Who stops the containers at shutdown | dockerd | the unit's `docker stop`, before dockerd stops |
| Signal and grace period | each container's own | each container's own |
| Time limit | `docker.service`'s stop timeout | 180 s |
| `always`, `unless-stopped`, `on-failure`, no policy at boot | by the restart policy | the same |
| A Docker restart or upgrade | stops every container | containers keep running |

## Tests

`daemon_shutdown_stop_test.go` runs both commands of the rendered unit in
`sh` against stand-ins for `docker` and `systemctl`: the shutdown gate, the
filter by policy and `--rm`, the list kept while Docker is down, the retries
and the failure after five. It also checks the install, repair and removal
steps against a fake systemd, and that no single `$` or `%` reaches systemd.

## Known limits

- An `unless-stopped` container that still fails to start after the five
  tries stays down at later reboots too, since the next shutdown no longer
  lists it; stock Docker would try it again at each boot. The unit fails and
  shows in `systemctl --failed`, and the shutdown that follows is the slow
  one, since a failed unit runs no stop step.
- A host that loses power writes no list, so its `unless-stopped` containers
  come back by Docker's own rule, as they would without the unit.
