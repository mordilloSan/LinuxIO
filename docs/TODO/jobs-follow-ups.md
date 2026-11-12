# Jobs: follow-ups

What is still open about [jobs](../jobs.md), the mutations that run in a
per-uid job host and the timer runs recorded beside them. Each item was
checked against the code; an item that could not be settled from the source
says "not verified".

## Deferred

- **Periodic scrub checkpoints.** A scrub is checkpointed only by its
  `Suspend` hook at a host stop, so after a power loss it rescans from the
  start (`backend/bridge/handlers/storage/btrfs_device_operations.go`).
- **Cancel for device removal and replace.** The Devices card has a cancel
  for a running balance only; a removal or a replace cannot be cancelled from
  the page (`frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsDevicesCard.tsx`).
  Whether the tray lists their jobs with a cancel: not verified.
- **The notice at sign-in during an update wait.** A user without sudo who
  signs in while an update waits loses that sign-in's notice: the history
  answers empty and the notice takes the sign-in mark anyway
  (`frontend/src/routes/_authenticated/-components/useAwayNotice.ts`).
- **Release notes.** The notes of the release that ships jobs should state
  three behaviour changes: a stored Docker `never` no longer silences
  failures; an interruption by a shutdown or a reboot is a warning, so a host
  that had "Errors only" no longer emails it unless *Warnings* is ticked for
  *Email*; and a save from the settings page over an unreadable
  `notifications.json` drops every schedule's choice and the SMTP password and
  keeps the old file as `notifications.json.unreadable`
  ([Alerts](../alerts.md#notification-settings)). Not verified whether they
  are written: release notes are not kept in the repository.

## Small findings from the reviews

- The scrub banner prints the percentage unrounded, such as
  `Checking for errors · 18.986434994665448%`
  (`frontend/src/routes/_authenticated/storage/-components/btrfs/BtrfsHealthBanner.tsx`;
  the backend sends a float from `backend/common/btrfs/inventory.go`).
- The job host's recorder writes progress files while it holds its mutex
  (`backend/bridge/internal/jobshost/recorder.go`, `writeProgressLocked`).
- `Store.Finish` on a record that is already terminal rewrites the file
  unchanged (`backend/common/jobs/store.go`, `mutate`).
- Store reads check a file with `Lstat` and then open it, in two steps
  (`backend/common/jobs/store.go`, `openRegularFile`); the directories belong
  to root or to the uid itself.
- A user's host records the owner's username from the bridge's hello rather
  than its own account (`backend/bridge/internal/jobshost/host.go`,
  `recordOwner`).
- A resend is compared byte for byte after compaction, so a client that
  reorders a request's keys gets a `409`; the browser resends identical bytes
  (`backend/bridge/internal/jobshost/host.go`, `sameJSON`).
- A schedule deleted while `notifications.json` does not parse logs "could not
  clear the notification choice" even when it had none
  (`backend/bridge/handlers/schedules/manager.go`, `forgetNotifyChoice`).
- An update of only a schedule's notification choice skips the re-apply that
  repairs an incomplete schedule; a second save repairs it. `changeLocked`
  takes six positional parameters (`backend/bridge/handlers/schedules/manager.go`).
- Mark all seen lists the alerts and marks them under separate locks, and
  `alerts.mark_seen` with explicit IDs is not filtered by the levels grid
  (`backend/bridge/handlers/alerts/operations.go`, `markSeenForUID`).
- The Docker update alerts share one source: a container named `updated-<x>`
  shares its failure key with the "updated" notice of a container named `<x>`,
  and one named `linuxio-update-pass` would share the pass notice's key
  (`backend/common/dockerupdate/alerts.go`).
- The snapshot pass's summary line can read `replicated=0 sent=1` when
  recording a job's outcome fails after its send
  (`backend/btrfs-timer/snapshot.go`).
- A bare `\r` in an alert message is not normalised in the email body
  (`backend/common/alerts/deliver.go`).
- On the notification settings page, the *Scheduled scripts* help line is not
  tied to its select for assistive technology, and the page's warnings use
  `role="alert"`, which `AppAlert` always sets
  (`frontend/src/routes/_authenticated/settings/-components/NotificationSettingsSection.tsx`,
  `frontend/src/components/ui/AppAlert.tsx`).

## Tests

- The design asked for a relay performance test (a no-op mutation through the
  relay many times, with a bound on the median) and for a checked inventory of
  every Mutation with its `Resumable` and `BrowserBound` flags; neither exists.
  `TestRetrySafeRoutesAreExplicitCalls` checks two lists of names only
  (`backend/bridge/apischema/schema_test.go`).
- The relay tests in `backend/bridge/cmd/jobs_relay_test.go` skip when run as
  root, and a privileged login skipping the host start has no hermetic test
  (`backend/auth/linuxio-auth-pam_test.c`).
- Not run in the test VM: a real update from a release that has job hosts
  (the check uses a protocol-bumped build of the same tree), more than one
  waiting job, a user's host as the older host, `tasks.watch`, `tasks.cancel`
  and `tasks.output` during the wait, and uploads during it
  (`frontend/src/test/vm/jobs-phase5a.spec.ts`).

## Not verified

Seen once in a review or a test VM run and not settled from the source:

- A progress frame published between a stream's subscribe and its first
  snapshot may be skipped
  (`backend/bridge/internal/bridgeipc/tasks.go`, `subscribeWithReplayStatus`).
- An abort while the client waits for the stream mux after its job started
  may not reach the server (`frontend/src/api/linuxio-core.ts`).
- After a failed first history load, a non-admin's notice observer refetches
  on focus and ignores the result
  (`frontend/src/routes/_authenticated/-components/useAwayNotice.ts`).
- The bridge of a user without sudo logged `forbidden: privileged route
  monitoring.get_cpu_history requires elevated bridge` (and the network and
  disk histories, and `filebrowser.subfolders`) on the dashboard and in the
  file browser; the dashboard's CPU graph calls that route with no privilege
  check at its call site
  (`frontend/src/routes/_authenticated/-dashboard/ProcessorGraph.tsx`).
- A session bridge logged `route failed: write frame: i/o deadline reached`
  while a scheduled script ran.
- A Compose stack saved by a sudo user got a stack folder owned by root, mode
  `0700`, inside that user's home (`backend/bridge/handlers/docker/background_operations.go`).
- The journal field `LINUXIO_REASON` held the reason JSON-quoted (`"reboot"`).
