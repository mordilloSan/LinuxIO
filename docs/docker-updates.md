# Docker updates

LinuxIO moves a container to the image its reference names in the registry
now: on demand from the Containers page (`docker.update_container`, a job with
progress, `Privileged()` like every Docker write), or in a scheduled pass that
`linuxio-docker-update.timer` starts once a day. A Compose service whose image
is built on the host is rebuilt instead, from the Containers page only
([Rebuilding a built service](#rebuilding-a-built-service)). The engine is
`backend/common/dockerupdate`, shared by the bridge and by the
`linuxio-docker-update` program that the timer's service runs as root.

A standalone container is recreated by LinuxIO from a copy of its
configuration. A Compose service is updated through its project with
`docker compose`, so the stack's files stay the source of its configuration.
Either way the new container must become ready, or the update is undone where
that is safe.

## Checking

A container has an update when the digest the registry gives for its
reference (`DistributionInspect`) is not one of its image's repository
digests. A reference pinned by digest is never updated. A registry that
throttles or does not answer is asked again five times over 155 seconds.

An image built or loaded on the host cannot be compared with a registry, and
how that shows depends on Docker's image store. The classic store gives it no
repository digest, and it is marked uncheckable. The containerd image store
(the default of a new Docker 29 install) gives it one of its own,
`<name>@<image ID>`, so the registry is asked and refuses (the name does not
exist there, or access is denied), and the check records that error. A
refusal is also what a pulled image gets when the registry login expired or
its tag was deleted, so it counts as a sign of a build only when the image's
own labels, `com.docker.compose.project` and `com.docker.compose.service`
(Compose puts them on every image it builds), name the container's project
and service. When an image may be built in either way, Compose manages the
container and its service has a `build:` section, the container is marked
`built` instead (*Built here* on the Containers page), with the service and
project as the reason and no error. Anything else stays uncheckable or keeps
the registry's error, and so does one whose project's files cannot be found
or read. A throttled, timed-out or unreachable registry is never taken for a
refusal. Only these
containers are looked at that way, with one
`docker compose config --format json <services>` per project (per service
when that read fails, as it does for a service no longer in the files), so a
check costs nothing more where every image comes from a registry. Update and the
scheduled pass, which look at each container again before they act, reach
the same verdict for a built one (one `config` for it): nothing to update, no
error, failure or alert, and it stays `built`. What each container's check
found is kept in `/var/lib/linuxio/docker-update-status.json`, which the
Containers page reads.

## One update at a time

Every update, every scheduled pass and every standalone container edit takes
`/run/linuxio-docker-update.lock`. An update from the page waits for the lock
and says so; a scheduled pass that finds it held does no work and is reported
as skipped.

## Standalone containers

An update is refused, before anything is changed, for a container that is
paused or restarting, that uses `--rm` or a container ID file, that Swarm
manages, that has a static address on a network, or that another container
depends on (its network, PID or IPC namespace, or `--volumes-from`). A stopped
container is updated only by a scheduled pass set to update stopped ones.

The image is pulled first; when the tag still names the container's image,
nothing else happens. Otherwise the replacement's configuration is a copy of
the container's, with two changes:

- The values the old image set are dropped (`DropImageDefaults`): an
  inspected container's configuration is its image's merged with what was set
  for the container, and carried as the container's own the old image's
  values would override the new image's through every later update (a new
  entrypoint, `PATH` or version label lost). A value equal to the old image's
  goes: a label or an environment entry, the working folder, user and stop
  signal, an exposed port or a volume the image declares, and each field of
  the health check (its test, timings and retries, as the daemon merges the
  image's into it field by field; a `NONE` test set for the container stays).
  The entrypoint and command follow Docker's rule that the image's command is
  filled in only when the container's entrypoint is empty: an entrypoint set
  for the container keeps both; with the image's entrypoint, the image's
  command goes with it, and a command of the container's own stays, the
  entrypoint going only when that command is not empty. A value set
  differently stays, and so does an exposed port the container publishes.
  When the old image is gone there is nothing to compare with, and the
  configuration is copied as it is.
- A hostname that is the container's short ID, Docker's default, is cleared
  so the replacement gets its own.

An edit from the Containers page recreates the container the same way and
drops the old image's values from the configuration it is given, so the image
the edit names applies its own.

The original is then parked, not removed: a journal
(`/var/lib/linuxio/docker-update-transaction.json`) records the step, the
container is stopped and renamed `linuxio-update-backup-<short id>`, and the
replacement is created under the original name, started and waited for
(readiness, below). Only then is the backup removed and the journal cleared.
Any failure after the original was parked removes the replacement and gives
the original back its name and, if it was running, starts it; the update is
then reported as rolled back. That rollback runs detached from the request,
each step bounded to 30 seconds, so a cancel of the request cannot leave it
half-way. A crash in the middle leaves the journal, and the next update, edit
or pass finishes or undoes the step it names before doing anything else.

## Compose services

A Compose service is updated with the project its labels name: its
configuration files in order, its environment files and its working folder,
with the environment of the `docker compose` process reduced to a fixed
list. The update runs `config --quiet`, `pull <service>` (retried while the
registry throttles) and `up -d --no-deps --no-build <service>`, which builds
nothing whatever the service's pull policy says, then waits for the
service's new container to be ready. A service with more than one replica is
refused, and a scheduled pass does not update a stopped service. A container
whose service label starts with a dash, which `docker compose` would read as
an option, is refused before any Compose command runs, by the check too.

`up` removes the old container, so there is nothing to park. The old image
stays on the host, untagged, and that is what a rollback uses. A rollback
runs only when the new container failed: it exited, turned unhealthy, or
kept restarting. One still coming up when the wait ended (health
`starting`, or running and not yet settled) may already have begun to change
its data, such as a migration at start, so it is reported as failed with
that reason and left on the new image. So is one whose wait ended without a
verdict on it (paused, a state that cannot be read, not found), whose
error says so. When it failed:

1. The old image is the image ID the container ran, read before the pull,
   and what the tag names now is read before any rollback moves a tag.
2. Nothing on the host is changed when the service cannot be rolled back
   (Known limits): `docker compose config --format json <services>` gives the
   services' effective settings (the services are named so that one of a
   profile that is not active is included), and `pull_policy: always`, an
   image pinned by digest, files that now name another image, or a tag that
   still names the old image leave the update reported as failed, with the
   reason. A service with a `build:` section is rolled back like any other;
   one that names no image runs Compose's default name for it
   (`<project>-<service>`), the tag its container was created from.
3. The tag goes back on the old image (`docker image tag <old id> <image>`).
4. `up -d --no-deps --pull never --no-build` brings the services up on it,
   pulling and building nothing whatever their pull policy says.
5. Each restored container is waited for, and must run the old image;
   otherwise the rollback is reported as failed.

When that `up` fails, the tag is put back on the image the update pulled, on
a context of its own bounded to 30 seconds, so a rollback whose time ran out
still tries it; both errors are reported. The rollback is bounded to three
minutes and ten seconds (two readiness waits). When that bound ends the wait
for a restored container, or a read or move of the tag gets no answer within
its 30 seconds, the error says so rather than a bare "timed out".

The rollback runs detached from the request. In the bridge, a cancel of the
update does not stop a rollback in progress; a request canceled while the new
container is still being waited for is not rolled back, since how the
container would have done is unknown. The timer's service is written with
`KillMode=mixed` and `TimeoutStopSec=250` (the rollback's bound and a
minute): a stop signals the program alone, which cancels the pass's work,
while a rollback in progress finishes before systemd kills what is left. A
cancelled `docker compose` command gets SIGTERM, as it did when systemd's
stop reached it directly, so Compose stops what it started, and is killed 10
seconds later if it has not stopped. A schedule saved before these two settings
were added gets them when it is saved again.

A scheduled pass updates the services of one project with one `pull` and one
`up`. A rollback is per tag: every service of the project under the tag a
failed service runs, however each spells it (`nginx` and
`docker.io/library/nginx:latest` are one tag), goes back with it, the ready
ones included. One left on the new image while the tag names the old one
would be moved back by its next `up`, and services that share an image, such
as an application and its worker, are meant to run the same version. None is
rolled back when another service under the tag did not fail itself (it was
still coming up, or its wait gave no verdict), since it may be changing the
data the old image would start on; nor when the services under one tag ran
different images, which one tag cannot take back. Each says why. A service that cannot be
rolled back leaves the others on the new image, where they were ready.

After a rollback the tag names the old image. A rolled-back image is not held
back: the next check finds the update again, and the next pass pulls and tries
it again.

## Rebuilding a built service

A container marked `built` is offered *Rebuild and update* on the Containers
page while it runs: the same `docker.update_container` job, with `rebuild`
set (`RebuildContainerWait`), under the same lock, with the same dialog and
recovery. The scheduled pass never builds an image (Known limits).

The container must be running and stable, Compose must manage it, and its
service must have one replica and a `build:` section; otherwise the job is
refused before anything is built, with 409 (Conflict) when no Compose service
builds the container's image. The rebuild then runs in the project as an
update does:

1. `config --format json <service>` reads the service's settings. It fails
   on an invalid project as `config --quiet` does (seen with Compose 2.40),
   and on a service no longer in the files, so it is the rebuild's
   validation too.
2. The image the container runs gets a second tag,
   `<repository>:linuxio-rebuild-previous`, until the rebuild ends. The build
   moves the service's tag off that image, and with the containerd image
   store an image the build leaves with no name is gone at once, though the
   container still runs it, so a rollback would have nothing to go back to.
   When the rebuild ends the tag is removed, and the image with it when
   nothing else names or runs it. Docker refuses that removal while the tag
   is the image's last name and a container still runs it (the `up` failed,
   the job was cancelled during the wait, or another service under the same
   tag runs it): the tag then stays, and keeps the image. An image already
   gone from the host is not held, and the rebuild goes on without it.
3. `build --pull <service>`, with `--progress=plain` and
   `BUILDX_NO_DEFAULT_ATTESTATIONS=1` in its environment: the base images are
   pulled again, and the rest of the build comes from the cache wherever its
   inputs are unchanged. With the attestations BuildKit adds by default, two
   builds entirely from the cache gave two image IDs for the same layers,
   configuration and creation time (Docker 29, containerd image store);
   without them the ID stays, so a rebuild that changed nothing is seen as
   such. The job's progress stays at the `building` step while each line of
   the build's output goes to the page that follows the job, as Compose's
   own output does for a stack: it is not stored as the job's progress,
   recorded or sent to every session's task list. The dialog shows the
   newest few hundred lines in one scrolling block under that step
   (`--progress=json` would give BuildKit's raw status objects instead of
   lines), and a line longer than 1 MiB is cut to its first MiB. A dialog
   that adopts the job after a reload shows what the job host replays, its
   last 1024 progress events, so after a long build the block may stand
   without the steps before it. A failed build is reported with its last 20
   lines, and nothing else runs. It is not retried: a build's output is
   whatever its steps print, so the throttling text the pull retries on
   would also retry a build that failed for good.
4. The image the build tagged is read: the one the service's `image:` names
   now, or, without one, the one the container was created from (Compose's
   `<project>-<service>`). When it is the image the container runs, the
   build changed nothing: the job ends there, reported as not updated, and
   the container is left as it is. When the files name another image than
   the container was created from, a rollback is refused for that reason.
5. Otherwise `up -d --no-deps --pull never --no-build <service>` brings the
   service up on exactly the image the host now holds, and the new container
   is waited for and, when it fails, rolled back to the image it ran before,
   as an update is.

The container is then marked `built` again, not current, since its image
still has no registry to check.

## Readiness

The new container, standalone or Compose, is read every half second:

- With a health check, it is ready when healthy and fails when unhealthy.
- Without one, it is ready once it runs. Where it replaces a container that
  was running (an update or an edit of a running container, a Compose
  service), it must also have run 10 seconds since its last start
  (`State.StartedAt`, which Docker resets at every restart): the time after
  which Docker's restart policy counts a start as successful, since running
  alone proves nothing about a container that crashes a few seconds in.
  Elsewhere, such as a container created and started from the Containers page
  or a stopped one a pass revives, it may be one-shot work that exits within
  seconds, and running once is enough.
- Created or restarting, it is read again; exited (whatever its exit code),
  dead or being removed, it has failed. Paused, or with no state to read, the
  wait ends at once without a verdict.
- After 90 seconds the wait ends and names the last state it read (status,
  exit code, restart count, health). Three restarts by the restart policy,
  counted from the first read, end it sooner as a failure.

A Compose rollback follows the failures alone (exited, dead, unhealthy,
restarting again and again), not a wait that ran out of time.

## The scheduled pass

The timer runs the service once a day at the configured time (04:00 by
default, `RandomizedDelaySec=1m`, `Persistent=true`). The service runs
`linuxio-docker-update --config /etc/linuxio/docker-update.json` as root
with `ProtectSystem=strict`, writing only `/var/lib/linuxio` and `/run`. The
settings choose the mode (update, or check only), which containers to update
(all, or those named), whether stopped containers are checked, updated and
started, and whether old images no container uses are removed afterwards.

A pass checks every container in its scope, then updates the standalone
containers one by one, then the Compose projects. Its output ends with a
report (`PassReport.Write`): a line of totals, then one line per container it
updated (the image, the versions when both images carry
`org.opencontainers.image.version`, and the short image IDs), failed to update
(the reason), or rolled back (the reason and what it is back on, the old image
or the original container). The history shows the summary of the run, such as
*1 updated, 1 failed, 1 rolled back*; the messages and the exit status are in
[Jobs](./jobs.md#scheduled-runs).

The log lines of the pass about one container end their message with the
container's name, as well as carrying it as the `container` attribute: the
saved output of a run (`journalctl -o short-iso`) shows a line's message and
not its attributes. It is the one exception to the logging rule that keeps
messages constant, and the logging guard allows it only under
`backend/common/dockerupdate` (`containerNamedPaths` in
`backend/common/logguard`), only with the same expression as the name and as
the attribute's value.

Each container whose update failed raises its own alert,
`docker-update/failed-<name>`, titled *Docker update failed* or, when it was
rolled back, *Docker update rolled back*. A replaced container raises
*Docker container updated* when the settings ask for changes; the other
notices and what each setting governs are in [Alerts](./alerts.md).

## Known limits

- A rollback, standalone or Compose, starts the old image on data the new one
  may already have changed, such as a database it migrated at start. A
  Compose service still coming up when its wait ended is not rolled back for
  that reason; one that failed is.
- A Compose service is not rolled back when it has `pull_policy: always`
  (its next `up` would pull the new image again), an image pinned by digest
  (no tag to put back; such a reference is never updated in the first
  place), files that now name another image than the one it ran, or services
  under the same tag that ran different images. The update is reported as
  failed with the reason, and the service stays on the new image.
- The scheduled pass never builds an image: a built service is rebuilt only
  when an administrator asks from the Containers page and watches it (why is
  under Rejected in the [backlog](./TODO/backlog.md)).
- A rebuild pulls new base images, but a step the cache answers does not run
  again: a package or plugin a cached `RUN` step fetched stays as it was
  until that step reruns, because the step itself or one before it changed.
- The build reads its context as root, so whoever can write the context
  folder or the Dockerfile decides what is built and then run.
- An image pulled from a registry that Compose built elsewhere under the
  same project and service names, as a CI job using the same files would,
  and whose service also has a `build:` section, is marked `built` when the
  registry refuses its lookup, and the refusal is not reported.
- A rebuild with nothing changed still recreates the container when the
  image it runs has another ID than the build gives: once after an image
  built outside LinuxIO with BuildKit's default attestations (such as
  `docker compose up --build`), and every time when the service's files ask
  for provenance or SBOM attestations or the build cache was pruned, so that
  steps run again.
- A rebuild that failed and was not rolled back removes the image the
  container ran along with its `linuxio-rebuild-previous` tag, when nothing
  else names or runs it. The tag stays when a container still runs that
  image (step 2 above), and after a crash, until the service's next rebuild
  moves it.
- An update or a rebuild from the page brings up its own service alone:
  other services under the same tag keep the image they ran until their own
  next `up` or recreation, which puts them on the new one.
- A Compose update whose `pull` or `up` itself fails is reported as failed and
  not rolled back; after a failed `up` the tag may name the new image.
- A Compose container without a health check that exits, with status 0
  included, counts as failed: a short job that runs and ends at pass time is
  rolled back to its old image.
- Three restarts fail an update even when the container would have settled:
  the wait stops at the third restart counted from its first read and does
  not look again, so a container that crashes three times while something it
  needs comes up, and would then run, is reported as failed and rolled back
  where it can be. Chosen over waiting the full 90 seconds on every restart
  loop.
- A rollback leaves the tag on the old image for the whole host: containers
  of other projects and standalone containers already updated from the same
  tag keep the new image until their own next `up` or recreation, which puts
  them on the old one, until the next pass pulls again.
- A value set for a standalone container equal to the one its old image sets
  cannot be told apart from the image's and is dropped with it. This applies
  to an edit that changes the image too: a value the form showed from the old
  image is not carried to the new one. A container
  an earlier LinuxIO replaced still carries the values of the image it had
  then where they have changed since; recreating it from its image clears
  them.
- The job host that runs an update from the page has its own stop timeout (2
  minutes), shorter than a Compose rollback's bound: a stop of it during a
  rollback can end the rollback's `docker compose up`.
