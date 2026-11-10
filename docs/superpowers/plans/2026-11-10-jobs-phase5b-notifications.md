# Jobs Phase 5b: Configurable Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The admin chooses, per kind of work, what LinuxIO reports besides failures, and in a grid of level by channel where each report goes; each schedule can override its kind's choice; and a notice at sign-in says what happened while the user was away.

**Architecture:** The choices live in the existing root-only `notifications.json` (`backend/common/alerts`), which every producer of alerts already reads or can read as root. The single least-severity setting becomes a grid (`info`, `warning`, `error` by browser, email, webhook) applied at delivery and in `alerts.list`; a per-kind "report" value decides whether a producer raises a notice for a run that did not fail. The frontend rebuilds the Notifications settings card around those two parts and adds the notice.

**Tech Stack:** Go (`backend/common/alerts`, `backend/common/scheduledrun`, `backend/bridge/internal/jobshost`, `backend/btrfs-timer`, `backend/common/btrfs`, `backend/common/dockerupdate`, `backend/cli`, `backend/bridge/handlers/{alerts,schedules,packages,config}`), generated TypeScript contract, React with TanStack Query, Playwright against the test VM.

**Spec:** `docs/TODO/jobs.md` section 4.5 (the decisions taken with the user on 2026-11-10) and `docs/alerts.md` (the canonical description of alerts today). Read both before any task. This plan follows `docs/superpowers/plans/2026-11-10-jobs-phase5a-history-and-banner.md`: it links alerts to the `/history` page built there.

## Global Constraints

- Never create, amend or push a commit. Each task ends by proposing a commit message; the user commits.
- Use Make targets only; never two Make runs at once in one worktree.
- **Levels** are the three stored severities, shown as: `info` Notices, `warning` Warnings, `error` Alerts. **Channels** are browser, email and webhook.
- **Failures are always reported.** No choice in the UI silences a failure, and alerts always reach the browser.
- **Report values:** `failures` (the default), `changes`, `every_run`, and for jobs started in the browser `long_jobs`. A kind offers only the values that can differ for it:

  | Kind | Key | Values |
  |---|---|---|
  | System updates | (the provider's drop-in, as today) | `on_failure`, `on_change`, `always` |
  | Docker updates | `docker_updates` | `failures`, `changes`, `every_run` |
  | Scheduled scripts | `scheduled_scripts` | `failures`, `every_run` |
  | Snapshots and replication | `snapshots` | `failures`, `changes`, `every_run` |
  | Pool resync | `resync` | `failures`, `changes` |
  | Jobs started in the browser | `browser_jobs` | `failures`, `long_jobs` |

- A job is long when it ran five minutes or more (`alerts.LongJob`).
- What a result is: a failure is an alert (`error`); an interruption is a warning; a run that completed, or changed something, is a notice (`info`). A cancelled job reports nothing.
- Storage health (missing disks, errors, low space, an unmounted pool) has no setting and is always raised.
- A schedule's override is stored in `notifications.json`, keyed by schedule ID, never in the schedule's definition file: a new field there changes every schedule's script path and unit text and makes an older build refuse to list schedules.
- `notifications.json` stays readable by an older build: new members are added, none changes meaning.
- No behaviour a user has today is silenced by the migration: a pool that is whole again keeps being reported unless the admin chooses otherwise.
- Logging follows AGENTS.md. `frontend/src/api/generated/*` changes only through `make generate`. UI copy is British English.

## Review Focus

1. An existing `notifications.json` (with `minSeverity` and `dockerUpdates`) must load into the same behaviour it had: same emails and webhooks, same Docker reports (Task 1, the migration table test).
2. Two writers of `notifications.json` (the settings page and a schedule's dialog) must not lose each other's change (Task 1, `TestUpdateNotificationsKeepsTheOtherWritersChange` and `TestSettingsSaveKeepsScriptOverrides`).
3. An alert of a level whose browser tick is off must neither show nor count as unseen, and an `error` must always show (Task 2).
4. A script deleted while its override exists, and an override for an ID that is no schedule, must not break saving or loading (Task 6).
5. A host whose system updates were set to "Never" reports no failure until the admin acts: the page must say so plainly and fix it in one click (Task 5 and Task 7).

---

### Task 1: The settings model

**Files:**
- Modify: `backend/common/alerts/settings.go`, `backend/common/alerts/settings_test.go`, `backend/bridge/apischema/alerts.go`, `backend/bridge/handlers/alerts/operations.go`, `backend/bridge/handlers/alerts/handlers_test.go` (or the operations test beside it)
- Generated: `frontend/src/api/generated/*`

**Interfaces:**
- Produces:

```go
// package alerts
const (
	ReportFailures = "failures"
	ReportChanges  = "changes"
	ReportEveryRun = "every_run"
	ReportLongJobs = "long_jobs"

	KindDockerUpdates = "docker_updates"
	KindScripts       = "scheduled_scripts"
	KindSnapshots     = "snapshots"
	KindResync        = "resync"
	KindBrowserJobs   = "browser_jobs"

	LongJob = 5 * time.Minute
)

// Channels says where the alerts of one level go.
type Channels struct {
	Browser bool `json:"browser"`
	Email   bool `json:"email"`
	Webhook bool `json:"webhook"`
}

// NotificationSettings gains
Report  map[string]string   `json:"report,omitempty"`  // kind -> report; a missing kind reports failures
Scripts map[string]string   `json:"scripts,omitempty"` // schedule ID -> report, overriding the scheduled scripts' line
Levels  map[string]Channels `json:"levels,omitempty"`  // severity -> channels

func (n NotificationSettings) ReportFor(kind string) string
func (n NotificationSettings) ReportForScript(scheduleID string) string
func (n NotificationSettings) ChannelsFor(severity string) Channels

// UpdateNotifications loads the settings, applies fn and saves, under one lock.
func UpdateNotifications(ctx context.Context, fn func(*NotificationSettings) error) (NotificationSettings, error)
```

`MinSeverity` and `DockerUpdates` stay in the struct, read for migration and still written with the values an older build would act on.

- [ ] **Step 1: Write the failing tests** (`settings_test.go`):
  - `TestLoadMigratesTheLeastSeverity`, a table: a file with `minSeverity` `""`, `error`, `warning`, `info` and no `levels` loads `Levels` in which every level has `Browser: true` and `Email`/`Webhook` true exactly for the levels at or above that severity (none for `""`).
  - `TestLoadMigratesTheDockerPolicy`: `dockerUpdates` `never` and `on_failure` give `ReportFor("docker_updates") == "failures"`, `on_change` gives `changes`.
  - `TestMissingFileHasTheDefaults`: every level reaches the browser, `error` also email and webhook; every kind reports `failures` except `resync`, which reports `changes`.
  - `TestStoredLevelsWinOverTheOldSetting`: a file with both `levels` and `minSeverity` uses `levels`.
  - `TestErrorsAlwaysReachTheBrowser`: saving `Levels["error"].Browser == false` stores it as true.
  - `TestValidateRejectsUnknownReports`: a value not offered for its kind (`scheduled_scripts: changes`, `browser_jobs: every_run`, an unknown kind, a level that is no severity) is an error.
  - `TestReportForScript`: an override wins; without one the kind's line applies; an override must be `failures` or `every_run`.
  - `TestSaveWritesWhatAnOlderBuildReads`: after saving levels in which email is ticked for `warning` and `error` only, the file's `minSeverity` is `warning`; with nothing ticked for email or webhook it is `""`; `dockerUpdates` is `on_change` for `changes` or `every_run`, else `on_failure`.
  - `TestUpdateNotificationsKeepsTheOtherWritersChange`: two `UpdateNotifications` calls, one setting `Scripts["a"]`, one setting `WebhookURL`, leave both in the file.
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS=./common/alerts/... GO_TEST_FLAGS='-run "Migrat|Defaults|Levels|Report|UpdateNotifications|OlderBuild|AlwaysReach"'`
Expected: FAIL, unknown fields and functions.

- [ ] **Step 3: Implement.** `UpdateNotifications` uses `filelock.RunExclusive` on a `.lock` beside the file, as the alert store does; `SaveNotifications` goes through it. The wire types `apischema.NotificationSettings` and `NotificationSettingsView` gain `report` and `levels` (not `scripts`: a schedule's override travels with its schedule, Task 6); the two mappers copy them, and a save from the settings page keeps the stored `Scripts`. Add `TestSettingsSaveKeepsScriptOverrides` beside the alerts handlers. Then `make generate`.
- [ ] **Step 4: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/alerts/... ./bridge/handlers/alerts/... ./bridge/apischema/..."`
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(alerts): store what each kind reports and where each level goes`.

---

### Task 2: The grid at delivery and in the bell

**Files:**
- Modify: `backend/common/alerts/deliver.go`, `backend/common/alerts/deliver_test.go`, `backend/bridge/handlers/alerts/operations.go` and its test

**Interfaces:**
- Consumes: `ChannelsFor` (Task 1).
- Produces: an alert is posted to the webhook only when its level's `Webhook` is ticked and a URL is set, emailed only when its level's `Email` is ticked and SMTP is configured; `alerts.list` returns, and counts as unseen, only alerts whose level's `Browser` is ticked.

- [ ] **Step 1: Write the failing tests:**
  - `TestDeliveryFollowsTheGrid`, a table over level and ticks: a `warning` with only `Email` ticked sends one email and no webhook; an `info` with nothing ticked sends nothing; an `error` with both sends both.
  - `TestTestNotificationIgnoresTheGrid`: the test notification still goes to every configured channel.
  - `TestAlertListHidesLevelsWithoutTheBrowser`: with `info` unticked for the browser, an `info` alert is neither listed nor counted in `unseen`; an `error` is always listed.
  - `TestMarkAllSeenLeavesHiddenAlertsUnseen`: marking all seen marks only what the list shows.
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./common/alerts/... ./bridge/handlers/alerts/..." GO_TEST_FLAGS='-run "Grid|HidesLevels|HiddenAlerts"'`
Expected: FAIL.

- [ ] **Step 3: Implement.** `notify` replaces `wants` with `ChannelsFor`; `Send` takes the channels to use. The webhook body and the email format do not change.
- [ ] **Step 4: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/alerts/... ./bridge/handlers/alerts/..."`
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(alerts): deliver and show each level where the grid says`.

---

### Task 3: Jobs and scheduled scripts

**Files:**
- Modify: `backend/bridge/internal/jobshost/host.go` (`alert`, `alertInterrupted`, `Config`), `backend/bridge/internal/jobshost/recorder.go` (`finish`), `backend/bridge/internal/jobshost/lifecycle.go`, `backend/bridge/cmd/jobs_host.go`, `backend/common/scheduledrun/hooks.go` (`alertFor`, `Hooks`), `backend/common/scheduledrun/liveness.go`
- Test: `backend/bridge/internal/jobshost/host_test.go`, `backend/common/scheduledrun/hooks_test.go`, `liveness_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces:

```go
// package jobshost, in Config
// Notifications returns the notification settings; nil means the defaults.
Notifications func() alerts.NotificationSettings

// package scheduledrun, in Hooks and in Liveness
// Settings returns the notification settings; nil means the defaults.
Settings func() alerts.NotificationSettings
```

- [ ] **Step 1: Write the failing job host tests** (root host with a recording `Alert`):
  - `TestFailedJobAlertNamesTheJob`: a failed job whose metadata label is `Scrubbing btrfs filesystem` raises `Severity: error`, `Title: "Scrubbing btrfs filesystem failed"`, `Link: "/history?job=<id>"`; a job with no label uses its route as before.
  - `TestInterruptedJobIsAWarning`: an interrupted job raises `Severity: warning`, title `<name> was interrupted`.
  - `TestLongJobThatFinishesIsANotice`: with `browser_jobs: long_jobs`, a completed job that ran 5 minutes raises `Severity: info`, `Title: "<name> finished"`, `Message: "It ran for 5 min."`; one that ran 4 min 59 s raises nothing; with `failures` neither does.
  - `TestCanceledJobReportsNothing`.
- [ ] **Step 2: Write the failing scheduled-run tests:**
  - `TestEndReportsEveryRunWhenAsked`: with `scheduled_scripts: every_run`, a completed script raises `Severity: info`, `Title: "Nightly backup finished"`, link to its schedule; with `failures` it raises nothing.
  - `TestEndFollowsTheScriptsOverride`: the kind says `failures`, the override for this schedule says `every_run`: a notice is raised; the reverse raises none.
  - `TestInterruptedScriptIsAWarning` (in `End` and in `Liveness`).
  - `TestCompletedSnapshotUnitRaisesNothingHere`: units that are not scripts still raise nothing from `End`.
- [ ] **Step 3: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./bridge/internal/jobshost/... ./common/scheduledrun/..." GO_TEST_FLAGS='-run "Alert|Warning|Notice|Reports|Override"'`
Expected: FAIL.

- [ ] **Step 4: Implement.** The durations in messages use one helper (`It ran for 12 s.`, `…5 min.`, `…3 h 12 min.`). `jobs_host.go` wires `Notifications` to `alerts.LoadNotifications` (a load error is logged and the defaults apply) for uid 0 only; `DefaultHooks` does the same. Alert keys, sources and occurrence handling stay as they are.
- [ ] **Step 5: Run the tests**

Run: `make test-go GO_TEST_PKGS="./bridge/internal/jobshost/... ./common/scheduledrun/... ./bridge/cmd/... ./cli/..."`
Expected: PASS.

- [ ] **Step 6: Propose the commit message** `feat(alerts): report long jobs and script runs when asked`.

---

### Task 4: Snapshots, replication and resync

**Files:**
- Modify: `backend/btrfs-timer/snapshot.go`, `backend/common/btrfs/replication_run.go` (`RunReplication`), `backend/common/btrfs/degraded.go` (`refreshResyncAlerts`), their callers
- Test: `backend/btrfs-timer/snapshot_test.go`, `backend/common/btrfs/replication_run_test.go`, `backend/common/btrfs/degraded_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces: `func RunReplication(ctx context.Context, jobs []BtrfsReplicationJob, report func(done, total int)) (sent int, errs []error)`, where `sent` counts the jobs that sent a snapshot (not the ones already up to date).

- [ ] **Step 1: Write the failing tests:**
  - btrfs: `TestRunReplicationCountsWhatItSent`: two enabled jobs, one sends and one is up to date: `sent == 1`.
  - btrfs-timer, with fakes for the two passes and a recording alert seam:
    - `TestSnapshotPassReportsAChange`: `snapshots: changes`, the pass took 2 snapshots and sent 1: one notice, source `btrfs`, key `snapshot-run`, `Severity: info`, `Title: "Snapshots: 2 taken, 1 sent"`, link `/storage/subvolumes?view=snapshots`; a pass that took and sent nothing raises none.
    - `TestSnapshotPassReportsEveryRun`: `every_run` and nothing to do gives `Title: "Snapshots: nothing to do"`.
    - `TestSnapshotPassReportsOnlyFailuresByDefault`: `failures` raises no notice (the per-rule failure alerts are unchanged).
  - btrfs: `TestWholeAgainFollowsTheSetting`: after a successful resync, `resync: changes` raises `resynced-<uuid>` as today and `failures` does not.
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./btrfs-timer/... ./common/btrfs/..." GO_TEST_FLAGS='-run "CountsWhatItSent|SnapshotPassReports|WholeAgain"'`
Expected: FAIL.

- [ ] **Step 3: Implement.** Each run of the snapshot unit is a new occurrence of the one `snapshot-run` alert (occurrence: the run's time). The settings are loaded once per pass; a load error is logged and the defaults apply. No ioctl code changes.
- [ ] **Step 4: Run the tests**

Run: `make test-go GO_TEST_PKGS="./btrfs-timer/... ./common/btrfs/... ./bridge/handlers/storage/..."`
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(alerts): report snapshot runs and resyncs as the settings ask`.

---

### Task 5: Docker and system updates

**Files:**
- Modify: `backend/common/dockerupdate/alerts.go`, `backend/common/dockerupdate/scheduled.go`, `backend/cli/alert.go` (`autoUpdateAlert`), `backend/bridge/handlers/packages/internal/autoupdate/` (the three backends' `notify_policies`, `notify.go`)
- Test: `backend/common/dockerupdate/alerts_test.go` (or the test beside it), `backend/cli/alert_test.go`, `backend/bridge/handlers/packages/internal/autoupdate/backend_*_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces: no `never` among the offered system policies; `AutoUpdateState` (or the support block the page reads) says when the stored policy is `never`.

- [ ] **Step 1: Write the failing tests:**
  - Docker: `TestDockerFailureIsAlwaysReported` (no setting silences `raiseDockerUpdateAlert`); `TestDockerUpdatedFollowsTheReport` (`changes` and `every_run` raise "Docker container updated", `failures` does not); `TestDockerEveryRunReportsAQuietPass` (`every_run` and a pass that updated nothing: one notice, key `run`, `Severity: info`, `Title: "Docker updates: nothing to update"`).
  - System: `TestOfferedPoliciesHaveNoNever` (APT offers `on_failure`, `on_change`, `always`; DNF and Mint offer `on_failure` and `always`, because only the service result is known there); `TestAStoredNeverIsReported` (a host with no drop-in reads `never`, and the state says failures are not reported); `TestAlwaysWithoutPackageListSaysItRan` (for a provider that cannot tell what was installed, the `always` notice is titled `Automatic updates ran`, not "found nothing to install").
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./common/dockerupdate/... ./cli/... ./bridge/handlers/packages/..." GO_TEST_FLAGS='-run "Docker.*Report|DockerFailure|EveryRun|Policies|StoredNever|SaysItRan"'`
Expected: FAIL.

- [ ] **Step 3: Implement.** The Docker worker reads `ReportFor("docker_updates")`. `never` stays a value the code can read and write (an older host has it), but nothing offers it. Do not change when the pre-update snapshot hook is written.
- [ ] **Step 4: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/dockerupdate/... ./docker-update/... ./cli/... ./bridge/handlers/packages/..."`, then `make generate` if a contract type changed.
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(alerts): always report update failures and offer no "never"`.

---

### Task 6: A schedule's own choice

**Files:**
- Modify: `backend/bridge/apischema/schedules.go`, `backend/bridge/handlers/schedules/manager.go`, `native.go` (`hostOps`), `handlers.go`, `frontend/src/routes/_authenticated/services/-components/SchedulesTab.tsx`
- Test: `backend/bridge/handlers/schedules/schedules_test.go`, `frontend/src/routes/_authenticated/services/-components/SchedulesTab.test.tsx`
- Generated: `frontend/src/api/generated/*`

**Interfaces:**
- Consumes: `UpdateNotifications`, `ReportForScript` (Task 1).
- Produces: `ScheduleStatus.Notify string` (`json:"notify"`; `""` means the kind's line applies); `ScheduleCreateRequest.Notify string` and `ScheduleUpdateRequest.Notify *string` (`json:"notify,omitzero"`; absent leaves it as it is). Values: `""`, `failures`, `every_run`. `ScheduleOptions` and `ScheduleDefinition` do not change.

- [ ] **Step 1: Write the failing backend tests:**
  - `TestCreateStoresTheNotifyChoice` and `TestGetReturnsTheNotifyChoice`.
  - `TestUpdateOfOnlyTheNotifyChoiceLeavesTheUnitAlone`: an update whose options equal the stored ones and whose `notify` differs calls no `stop`, `disable`, `reload` or file write of the unit, and stores the choice; it also works while the script runs.
  - `TestUpdateWithoutNotifyKeepsTheChoice`.
  - `TestDeleteRemovesTheNotifyChoice`.
  - `TestNotifyChoiceMustBeKnown`: `changes` is `ErrInvalidArgs`.
  - `TestAnOverrideForAMissingScheduleIsHarmless`: a stored override whose ID is no schedule changes nothing in `schedules.list`, and saving the settings keeps working.
  - `TestScheduleDefinitionFileIsUnchanged`: the definition written for a schedule with an override is byte-identical to the one written without, and `renderService` is too.
- [ ] **Step 2: Write the failing dialog tests:** the dialog has a select `Notify me` with `Use the default`, `Failures only`, `After every run`; editing a schedule shows its stored choice; saving sends `notify` beside `options`.
- [ ] **Step 3: Run and see them fail**

Run: `make test-go GO_TEST_PKGS=./bridge/handlers/schedules/... GO_TEST_FLAGS='-run "Notify|DefinitionFileIsUnchanged"'` and `make test-frontend VITEST_FILE=src/routes/_authenticated/services/-components/SchedulesTab.test.tsx`
Expected: FAIL.

- [ ] **Step 4: Implement.** The manager reaches the settings through two new `hostOps` functions (read one schedule's choice, write or clear it) so tests need no real file. `make generate` after the Go change.
- [ ] **Step 5: Run the tests**, then `make check-frontend-quiet`.
Expected: PASS.

- [ ] **Step 6: Propose the commit message** `feat(schedules): let a task choose what it reports`.

---

### Task 7: The settings page

**Files:**
- Modify: `frontend/src/routes/_authenticated/settings/-components/NotificationSettingsSection.tsx`, `NotificationSettingsSection.test.tsx`

**Interfaces:**
- Consumes: the generated `NotificationSettings` with `report` and `levels` (Task 1), the system update state with its "never" flag (Task 5).

- [ ] **Step 1: Write the failing tests:**
  - A card `What to hear about` has one select per kind with exactly the values of the Global Constraints table, labelled: `Failures only`, `Also when something changed`, `Every run`; for jobs started in the browser `Failures only` and `Also long jobs that finish`; for pool resync `Failures only` and `Also when a pool is whole again`. A line of text says storage health is always reported.
  - The two "updates are waiting" checkboxes are still there and behave as before.
  - A card `Where each level goes` is a grid with rows `Notices`, `Warnings`, `Alerts` and columns `Browser`, `Email`, `Webhook`; the `Alerts`/`Browser` box is ticked and disabled; ticking `Warnings`/`Email` and saving sends `levels.warning.email: true` and leaves the rest as loaded.
  - The old `Send alerts` select is gone.
  - When the system update state says failures are not reported, a warning reads `System update failures are not reported on this machine.` with a button `Report failures`; pressing it saves the policy `on_failure` through `updates.set_auto_updates`.
  - System choices still save as they change; the others save with `Save`.
- [ ] **Step 2: Run and see them fail**

Run: `make test-frontend VITEST_FILE=src/routes/_authenticated/settings/-components/NotificationSettingsSection.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement.** Keep the draft pattern the section already uses (`toDraft` field by field, `fromDraft`). The `Delivery` card keeps the webhook and email fields and the test button.
- [ ] **Step 4: Run the test**, then `make check-frontend-quiet`.
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(settings): choose what to hear about and where each level goes`.

---

### Task 8: "While you were away"

**Files:**
- Create: `frontend/src/routes/_authenticated/-components/useAwayNotice.ts`, `useAwayNotice.test.tsx`, `frontend/src/routes/_authenticated/-components/footer/notificationsPanel.ts`
- Modify: `frontend/src/contexts/AuthProvider.tsx` (mark a fresh sign-in), the authenticated layout that mounts the hook, `frontend/src/routes/_authenticated/-components/footer/NotificationsBell.tsx` and its test, `frontend/src/routes/_authenticated/history/-components/HistoryPage.tsx`, `backend/bridge/internal/userconfig/types.go`, `backend/bridge/apischema/config.go`, `backend/bridge/handlers/config/apply_dismissals.go` and its test, `frontend/src/api/config-types.ts`
- Generated: `frontend/src/api/generated/*`

**Interfaces:**
- Consumes: `alerts.list` and its `unseen` count and `tasks.history` (plan 5a).
- Produces: `openNotificationsPanel(): void`, which opens the bell's tray from anywhere (`notificationsPanel.ts` is a small external store read with `useSyncExternalStore`; the bell subscribes to it), and `PersistedDismissals.HistorySeenAt string` (RFC 3339; `apischema.Dismissals.historySeenAt`), the time the user last looked at their history.

- [ ] **Step 1: Write the failing tests:**
  - backend: `TestDismissalsStoreWhenHistoryWasSeen` (a valid time is stored; an invalid one is `ErrInvalidArgs`).
  - hook, admin: right after a sign-in with 3 unseen alerts, one toast `While you were away: 3 new notifications` with an action `Show` that opens the bell; with none, no toast; after a page reload (no fresh sign-in), no toast; it shows once per sign-in.
  - hook, other user: right after a sign-in, with two history entries that failed or were interrupted after `historySeenAt` and one before, one toast `While you were away: 2 of your jobs did not finish` with an action `Show` that goes to `/history`; with none, no toast.
  - history page: opening it stores `historySeenAt` as now.
  - bell: `openNotificationsPanel()` opens the tray.
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS=./bridge/handlers/config/... GO_TEST_FLAGS='-run TestDismissalsStoreWhenHistoryWasSeen'` and the hook's test file with `make test-frontend VITEST_FILE=…`.
Expected: FAIL.

- [ ] **Step 3: Implement.** `AuthProvider.signIn` already knows a session just started (it shows the welcome toast there); record that in `sessionStorage` and let the hook consume it. The admin path needs no new stored state: alerts already keep a per-user seen count, and the grid of Task 2 already limits them to what reaches the browser.
- [ ] **Step 4: Run the tests**, then `make generate` and `make check-frontend-quiet`.
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(alerts): tell the user at sign-in what happened while away`.

---

### Task 9: Real-machine checks, documents and gates

**Files:**
- Create: `frontend/src/test/vm/jobs-phase5b.spec.ts`
- Modify: `docs/alerts.md` (the settings, the grid, every producer's row, the "Not in this slice" list), `docs/TODO/jobs.md` (status table, a new section 3.10, section 4.5), `docs/api-contract.md` (the changed types)

- [ ] **Step 1: Write the VM spec.**
  1. *A script reports every run when asked.* Set scheduled scripts to `Every run` on the settings page, run a script that succeeds: the bell shows `<name> finished`. Set it back to `Failures only`, run it again: no new notice.
  2. *A schedule's own choice wins.* Kind `Failures only`, the schedule's dialog `After every run`: its run raises the notice, another schedule's run does not; the schedule's unit file is unchanged by saving the choice.
  3. *The grid hides a level from the bell.* Untick `Notices`/`Browser`, run the reporting script: no notice in the bell and the badge does not count it; tick it again: the next run shows.
  4. *The grid picks the channel.* With a webhook pointing at a listener started in the VM over SSH and only `Alerts`/`Webhook` ticked: a failing script posts once, a notice does not.
  5. *An interrupted script is a warning.* Reboot the VM during a run: the alert that follows has the warning icon and is delivered to the channels ticked for warnings only.
  6. *An older settings file keeps its behaviour.* Write a `notifications.json` holding only `minSeverity: "warning"`, a webhook URL and `dockerUpdates: "on_change"` over SSH, open the settings page: warnings and alerts are ticked for webhook, notices are not, Docker updates reads `Also when something changed`.
  7. *The notice at sign-in.* With one unseen alert, sign out and in: the toast `While you were away: 1 new notification` appears and `Show` opens the bell.
- [ ] **Step 2: Run the full gate**

Run: `make test-quiet`
Expected: PASS.

- [ ] **Step 3: Run the VM checks**

Run, one after the other: `make vm-reset`, `make vm-install`, `make test-vm-browser PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5b.spec.ts'`, then `make vm-test-btrfs-integration` (Task 4 changed how `RunReplication` returns).
Expected: all pass.

- [ ] **Step 4: Ask for the privilege review.** Dispatch the `privilege-boundary-reviewer` agent on the whole diff: a privileged route gains fields, and root programs read new settings.
- [ ] **Step 5: Reconcile the documents.** `docs/alerts.md` is the canonical description: every producer's row says what it raises at which level and which setting governs it; the delivery section describes the grid; the limits (what DNF and Mint cannot tell, what a stored "Never" means) are stated. Section 3.10 of `docs/TODO/jobs.md` lists what was built, each target run and its result, and what was only inferred.
- [ ] **Step 6: Propose the commit message** `test(alerts): check configurable notifications in the test VM`.
