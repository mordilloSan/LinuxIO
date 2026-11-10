# Jobs Phase 4: Scheduled Runs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Work that runs on a timer (scheduled scripts, btrfs snapshots and replication, pool resync, Docker container updates) shows in the job list while it runs, reports progress or live output, and leaves a record with a copy of its output.

**Architecture:** Scheduled work keeps running in its own systemd unit. Two hooks on each unit (`ExecStartPre`, `ExecStopPost`, in packaged drop-ins) call `linuxio scheduled-run begin|end`, which writes the run's record into a store of its own. The root job host reads that store to add scheduled runs to `tasks.list` and `tasks.events`, and asks systemd whether a `running` record is still true.

**Tech Stack:** Go (`backend/common/jobs`, new `backend/common/scheduledrun`, `backend/cli`, `backend/bridge/internal/jobshost`), systemd drop-ins, bash installer, React and TanStack Query, Playwright against the test VM.

**Spec:** `docs/TODO/jobs.md` section 2.14 and decision D8. Read both before any task.

## Global Constraints

- Never create, amend or push a commit. Each task ends by proposing a commit message; the user commits.
- Use Make targets only (`make test-go GO_TEST_PKGS=… GO_TEST_FLAGS='-run …'`, `make test-frontend VITEST_FILE=<one file>`, `make generate`). Never two Make runs at once in one worktree.
- Nothing outside the browser starts a job, and `linuxio-bridge` gains no verb.
- The text `renderService` (schedules), `btrfs.RenderUnit` and `renderDockerUpdateUnit` produce stays byte-identical. Hooks live only in packaged files.
- Hook lines are exactly `ExecStartPre=-+/usr/local/bin/linuxio scheduled-run begin --unit %n` and `ExecStopPost=-+/usr/local/bin/linuxio scheduled-run end --unit %n`. A hook takes no other input than `--unit` and systemd's environment.
- Store: `/var/lib/linuxio/jobs/scheduled`, progress in `/run/linuxio/jobs/scheduled`, directories `0700`, files `0600`, root.
- Limits: newest 50 ended runs per unit, none older than 30 days, output at most 10 MiB (first 5 MiB and last 5 MiB of a longer one).
- Routes: `scheduled.script`, `scheduled.btrfs_snapshot`, `scheduled.btrfs_resync`, `scheduled.docker_update`. Labels: the schedule's name (fallback `Scheduled task`), `Snapshots and replication`, `Pool resync`, `Container updates`.
- Logging follows AGENTS.md: return or log, never both; lowercase constant messages; the key is `error`.
- No new bridge route. `frontend/src/api/generated/*` changes only through `make generate`.
- UI copy is British English.

## Review Focus

1. The last lines a script prints before it exits must be in the saved output: the journal may not have them yet when `end` runs (Task 9, VM test 6).
2. A hook that fails (missing binary, unwritable store) must neither block nor fail the run (Task 9, VM test 7).
3. A schedule created before this update must still run with "Run now" and no re-save (Task 3, `TestScheduleUnitCarriesNoRunHooks`).
4. A slow or huge output copy must not leave the record `running`: `ExecStopPost` is cut after `TimeoutStopSec=15s` (Task 2, `TestEndFinishesTheRecordBeforeASlowOutputCopy`).
5. Two invocations of one unit, one from an earlier boot and one running now, must be told apart (Task 5, `TestRunningTellsApartTwoInvocationsOfOneUnit`).

## Working beside phase 5

Phase 5 builds on two things from here: `jobs.Record.Metadata` (Task 1) and `snapshotFromRecord` carrying it (Task 5). If the phases run in parallel, land Task 1 and the `snapshotFromRecord` step of Task 5 first. Phase 5 then owns the history route, the history view, the login notice and the update banner; it does not edit `backend/common/scheduledrun`, `backend/cli`, the packaged units or `useRecoveredTasks.ts`.

---

### Task 1: The scheduled store

**Files:**
- Modify: `backend/common/jobs/record.go`, `backend/common/jobs/store.go`
- Create: `backend/common/jobs/scheduled.go`, `backend/common/jobs/scheduled_test.go`

**Interfaces:**
- Produces:

```go
const (
	MaxScheduledRunsPerUnit = 50
	MaxOutputBytes          = 10 << 20
)

// Record gains, after Request:
// Metadata is the public projection shown with the job. Unlike Request it is
// returned by the API.
Metadata jsontext.Value `json:"metadata,omitzero"`

func NewScheduledStore() *Store // version.DataDir+"/jobs/scheduled", version.RunDir+"/jobs/scheduled"
func UnitRunID(invocationID string) (string, error)
func (s *Store) OutputPath(id string) string // <stateDir>/<id>.log
func (s *Store) PrunePerTarget(ctx context.Context, now time.Time, keep int) error
func (s *Store) ModTime() (time.Time, error) // mtime of stateDir; zero time and nil when it does not exist
```

- [ ] **Step 1: Write the failing tests** in `scheduled_test.go`:
  - `TestUnitRunID`: `"1234567890abcdef1234567890abcdef"` gives `"12345678-90ab-cdef-1234-567890abcdef"` and passes `ValidateID`; `""`, a 31-character value, `"1234567890ABCDEF1234567890ABCDEF"` and 32 zeros each return an error.
  - `TestRecordMetadataRoundTrips`: a record created with `Metadata: jsontext.Value(`{"label":"x"}`)` comes back from `Get` with the same JSON.
  - `TestPrunePerTargetKeepsNewestPerUnit`: three terminal records with `Target: []string{"a.service"}` finished 3 h, 2 h and 1 h ago, one terminal with `Target: []string{"b.service"}`, one `running` for `a.service`, each with an output file and a progress file; `PrunePerTarget(ctx, now, 2)` removes only the 3 h record, its `.log` and its `.progress`.
  - `TestPrunePerTargetDropsOldRuns`: a lone terminal record finished 31 days ago is removed.
  - `TestModTimeChangesWhenARecordIsWritten`: `ModTime` of a missing store is the zero time; after `Create` it is non-zero; after a later `Update` it is not earlier.
- [ ] **Step 2: Run them and see them fail**

Run: `make test-go GO_TEST_PKGS=./common/jobs/... GO_TEST_FLAGS='-run "TestUnitRunID|TestRecordMetadata|TestPrunePerTarget|TestModTime"'`
Expected: FAIL, undefined `UnitRunID`, `PrunePerTarget`, `ModTime`, `Metadata`.

- [ ] **Step 3: Implement.** `removeRecord` also removes `OutputPath(id)`. `PrunePerTarget` runs under the store lock, groups terminal records by `Target[0]` (records without a target form one group), keeps the newest `keep` of each group by `finishedOrCreated`, and removes any older than `TerminalRetention`. `UnitRunID` accepts exactly 32 lowercase hex characters that are not all zero.
- [ ] **Step 4: Run the package tests**

Run: `make test-go GO_TEST_PKGS=./common/jobs/...`
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(jobs): add a store for scheduled runs with a limit per unit`.

---

### Task 2: Begin, end and the output copy

**Files:**
- Create: `backend/common/scheduledrun/kind.go`, `hooks.go`, `output.go`, and `kind_test.go`, `hooks_test.go`, `output_test.go`
- Modify: `backend/common/systemd/systemd.go` (add `SystemStopping`)
- Test: `backend/common/btrfs/unit_test.go`, `backend/common/dockerupdate/timer_test.go`, `backend/bridge/handlers/schedules/schedules_test.go` (one guard each)

**Interfaces:**
- Consumes: Task 1.
- Produces:

```go
package scheduledrun

const (
	RouteScript       = "scheduled.script"
	RouteSnapshot     = "scheduled.btrfs_snapshot"
	RouteResync       = "scheduled.btrfs_resync"
	RouteDockerUpdate = "scheduled.docker_update"
	AlertSource       = "jobs"
)

type Kind struct {
	Route    string
	Label    string
	Instance string // schedule ID or pool UUID; "" for a single unit
	Link     string // the page that owns it
	Alerts   bool   // a failed or interrupted run raises a jobs alert
}

// KindOf names the scheduled work unit runs. scheduleDir holds the schedule
// definitions a script's label is read from.
func KindOf(unit, scheduleDir string) (Kind, bool)

// Result is systemd's $SERVICE_RESULT, $EXIT_CODE and $EXIT_STATUS.
type Result struct{ Service, Code, Status string }

type Hooks struct {
	Store       *jobs.Store
	ScheduleDir string
	Now         func() time.Time
	BootID      func() (string, error)
	Output      func(ctx context.Context, invocationID string, w io.Writer) error
	Stopping    func(ctx context.Context) bool
	Alert       func(ctx context.Context, obs alerts.Observation) error
}

func DefaultHooks() Hooks
func (h Hooks) Begin(ctx context.Context, unit, invocationID string) error
func (h Hooks) End(ctx context.Context, unit, invocationID string, result Result) error
```

```go
// package systemd
// SystemStopping reports whether the manager's SystemState is "stopping";
// false when it cannot be read.
func SystemStopping(ctx context.Context) bool
```

- [ ] **Step 1: Write the failing kind tests** (`kind_test.go`):
  - `TestKindOf`, a table with schedule ID `3f0c2f0e-7a1b-4c9d-8e2f-1a2b3c4d5e6f` and pool `0a1b2c3d-1111-2222-3333-444455556666`:

    | Unit | Route | Label | Instance | Link | Alerts |
    |---|---|---|---|---|---|
    | `linuxio-schedule-<id>.service` | `scheduled.script` | `Scheduled task` | the ID | `/services/schedules?schedule=<id>` | true |
    | `linuxio-btrfs-snapshot.service` | `scheduled.btrfs_snapshot` | `Snapshots and replication` | `""` | `/storage/subvolumes?view=snapshots` | false |
    | `linuxio-btrfs-resync@<uuid>.service` | `scheduled.btrfs_resync` | `Pool resync` | the UUID | `/storage?pool=<uuid>` | false |
    | `linuxio-docker-update.service` | `scheduled.docker_update` | `Container updates` | `""` | `/docker/containers` | false |

    and `ok == false` for `linuxio-btrfs-health.service`, `ssh.service`, `linuxio-schedule-not-a-uuid.service`, `linuxio-btrfs-resync@.service`.
  - `TestScriptLabelIsTheScheduleName`: with `<scheduleDir>/<id>.json` holding `{"version":1,"definition":{"id":"<id>","options":{"name":"Nightly backup"}}}` the label is `Nightly backup`.
  - `TestScriptLabelFallsBackWhenTheDefinitionIsUnreadable`: a missing file, invalid JSON, an empty name and a file larger than 512 KiB each give `Scheduled task`.
- [ ] **Step 2: Write the failing hook tests** (`hooks_test.go`, a temp store, fixed clock, fakes for `Output`, `Stopping`, `Alert`):
  - `TestBeginCreatesTheRunningRecord`: ID is `UnitRunID(invocation)`; `Route`, `Target == []string{unit}`, `State == running`, `CreatedAt == StartedAt == now`, `Owner == {UID: 0, Username: "root"}`, `Metadata` is `{"label":"Nightly backup","identity":["<id>"]}`, `Executor == {Kind: "unit", Unit: unit, InvocationID: invocation, BootID: boot}`, `Protocol == jobs.Protocol`.
  - `TestBeginTwiceKeepsTheFirstRecord`: a second `Begin` returns nil and changes nothing.
  - `TestBeginRefusesAnUnknownUnit`: `ssh.service` returns an error and writes nothing.
  - `TestEndOutcomes`, a table:

    | Result (Service, Code, Status) | cancel_requested | stopping | State | Reason | Error |
    |---|---|---|---|---|---|
    | `success`, `exited`, `0` | no | no | `completed` | | none |
    | `success`, `exited`, `0` | yes | no | `completed` | | none |
    | `exit-code`, `exited`, `3` | no | no | `failed` | | `exited with status 3`, code 500 |
    | `signal`, `killed`, `KILL` | no | no | `failed` | | `killed by signal KILL` |
    | `core-dump`, `dumped`, `SEGV` | no | no | `failed` | | `killed by signal SEGV` |
    | `timeout`, `killed`, `TERM` | no | no | `failed` | | `timed out` |
    | `oom-kill`, `killed`, `KILL` | no | no | `failed` | | `killed for using too much memory` |
    | `start-limit-hit`, ``, `` | no | no | `failed` | | `failed: start-limit-hit` |
    | `signal`, `killed`, `TERM` | yes | no | `canceled` | | `operation aborted`, code 499 |
    | `signal`, `killed`, `TERM` | yes | yes | `canceled` | | `operation aborted`, code 499 |
    | `signal`, `killed`, `TERM` | no | yes | `interrupted` | `shutdown` | none |

    Each row also asserts `FinishedAt == now`.
  - `TestEndWithoutBeginStillRecordsTheRun`: no record before `End`; afterwards one terminal record with `CreatedAt == now`.
  - `TestEndKeepsARunShorterThanASecond`: a run begun and ended at the same instant with `success` still has its record.
  - `TestEndIsHarmlessOnATerminalRecord`: a second `End` with another result changes nothing and calls `Alert` no second time.
  - `TestEndAlertsOnlyForScripts`: a failed script calls `Alert` once with `Source: "jobs"`, `Key: <record id>`, `Severity: alerts.SeverityError`, `Title: "Nightly backup failed"`, `Message: "exited with status 3"`, `Link: "/services/schedules?schedule=<id>"`; an interrupted script gets `Title: "Nightly backup was interrupted"`; a canceled or completed script and a failed `linuxio-btrfs-snapshot.service` get none.
  - `TestEndPrunesTheUnitsOlderRuns`: with 50 ended records of the unit already stored, `End` leaves 50 and the oldest is gone.
  - `TestEndFinishesTheRecordBeforeASlowOutputCopy`: `Output` blocks until its context ends; `End` called with a 50 ms deadline returns an error and the record is terminal.
- [ ] **Step 3: Write the failing output tests** (`output_test.go`):
  - `TestEndSavesTheOutput`: `Output` writes `"first\nlast\n"`; `Store.OutputPath(id)` holds exactly that, mode `0600`.
  - `TestEndWithNoOutputWritesNoFile`.
  - `TestEndKeepsTheStartAndEndOfALongOutput`: `Output` writes 5 MiB of `A`, 2 MiB of `M`, 5 MiB of `Z`. The file starts with 5 MiB of `A`, ends with 5 MiB of `Z`, and between them holds exactly `\n[… 2097152 bytes left out …]\n`.
- [ ] **Step 4: Write the guard tests** in the three owning packages:
  - `backend/common/btrfs`: `TestScheduledRunKnowsTheBtrfsUnits`: `KindOf(SnapshotServiceName, "")` has route `scheduled.btrfs_snapshot`; `KindOf(ResyncUnitName(uuid), "")` has route `scheduled.btrfs_resync` and `Instance == uuid`.
  - `backend/common/dockerupdate`: `TestScheduledRunKnowsTheDockerUpdateUnit` with `dockerUpdateUnitName`.
  - `backend/bridge/handlers/schedules`: `TestScheduledRunReadsTheScheduleName`: after `store.save` of a definition named `Nightly backup`, `KindOf(serviceName(id), store.configDir)` has that label.
- [ ] **Step 5: Run them and see them fail**

Run: `make test-go GO_TEST_PKGS=./common/scheduledrun/...`
Expected: FAIL, package does not build.

- [ ] **Step 6: Implement.**
  - `End` order: decide the outcome and write the terminal record with `Store.Update` (not `Finish`, which deletes short runs); then copy the output; then alert; then `PrunePerTarget(ctx, now, jobs.MaxScheduledRunsPerUnit)`. It returns the joined errors of the later steps.
  - Outcome order: `success` first, then `cancel_requested`, then `Stopping`, then failed.
  - The output goes through a writer that keeps the first and last `jobs.MaxOutputBytes/2` bytes and counts the rest, then is written with `atomicfile.Write`.
  - `DefaultHooks`: `Store` is `jobs.NewScheduledStore()`, `ScheduleDir` is `version.ConfigDir + "/schedules"`, `Alert` is `alerts.Raise`, `Stopping` is `systemd.SystemStopping`, and `Output` runs `journalctl --sync` and then `journalctl --no-pager -o cat _SYSTEMD_INVOCATION_ID=<invocation>` with `exec.CommandContext`, separate arguments, stderr in the returned error.
  - `scheduledrun` does not import `backend/common/btrfs`, `dockerupdate` or anything under `backend/bridge`; the guard tests keep its unit names in step.
- [ ] **Step 7: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/scheduledrun/... ./common/systemd/... ./common/btrfs/... ./common/dockerupdate/... ./bridge/handlers/schedules/..."`
Expected: PASS.

- [ ] **Step 8: Propose the commit message** `feat(jobs): record scheduled runs and keep a copy of their output`.

---

### Task 3: The command, the drop-ins and the installer

**Files:**
- Create: `backend/cli/scheduled_run.go`, `backend/cli/scheduled_run_test.go`, `packaging/systemd/linuxio-run-record.conf`
- Modify: `backend/cli/main.go`, `packaging/systemd/linuxio-btrfs-resync@.service`, `packaging/scripts/install-linuxio-binaries.sh` (the unit list near line 100, the install loop near line 600, the removal near line 836), `packaging/scripts/tests/installer-fixtures.sh`
- Test: `backend/bridge/handlers/schedules/schedules_test.go`

**Interfaces:**
- Consumes: `scheduledrun.Hooks` (Task 2).
- Produces: `linuxio scheduled-run begin|end --unit <name>`, and

```go
func runScheduledRun(ctx context.Context, args []string, getenv func(string) string, hooks scheduledrun.Hooks, stderr io.Writer) int
```

- [ ] **Step 1: Write the failing CLI tests** (`scheduled_run_test.go`, hooks over a temp store):
  - `TestScheduledRunBegin`: `["begin", "--unit", "linuxio-docker-update.service"]` with `INVOCATION_ID=1234567890abcdef1234567890abcdef` returns 0 and the store holds the running record.
  - `TestScheduledRunEnd`: after a begin, `["end", "--unit", …]` with `SERVICE_RESULT=exit-code`, `EXIT_CODE=exited`, `EXIT_STATUS=3` returns 0 and the record is `failed` with `exited with status 3`.
  - `TestScheduledRunUsage`: no arguments, an unknown verb and a missing `--unit` return 2 and print `Usage: linuxio scheduled-run begin|end --unit <unit>`.
  - `TestScheduledRunNeedsAnInvocation`: an unset `INVOCATION_ID` returns 1 and prints `INVOCATION_ID is not set`.
  - `TestScheduledRunRefusesAnUnknownUnit`: `--unit ssh.service` returns 1 and writes nothing.
- [ ] **Step 2: Write the failing guard** `TestScheduleUnitCarriesNoRunHooks` in the schedules package: `renderService` output does not contain `scheduled-run`, with a comment naming `ensureInstalled`.
- [ ] **Step 3: Extend `installer-fixtures.sh`** to fail unless, after an install: `linuxio-schedule-.service.d/10-linuxio-run.conf`, `linuxio-btrfs-snapshot.service.d/10-linuxio-run.conf` and `linuxio-docker-update.service.d/10-linuxio-run.conf` exist under the systemd directory and each holds both hook lines; `linuxio-btrfs-resync@.service` holds both hook lines; and after an uninstall the three directories are gone.
- [ ] **Step 4: Run and see them fail**

Run: `make test-go GO_TEST_PKGS=./cli/...` then `make test-installation-scripts`
Expected: FAIL, undefined `runScheduledRun`; the fixture reports the missing drop-ins.

- [ ] **Step 5: Implement.**
  - `main.go`: `case "scheduled-run"` calls `runScheduledRun(ctx, args, os.Getenv, scheduledrun.DefaultHooks(), os.Stderr)`; the help text gets `scheduled-run begin|end   Record a scheduled run (used by systemd hooks; needs root)`.
  - A hook error is printed to stderr and returns 1; the `-` prefix makes systemd ignore it.
  - `linuxio-run-record.conf` holds `[Service]` and the two hook lines from Global Constraints. The installer copies it to the three drop-in directories as `10-linuxio-run.conf`, mode `0644`, before its `daemon-reload`.
  - `linuxio-btrfs-resync@.service` gets the two lines in its `[Service]` section.
- [ ] **Step 6: Run the checks**

Run: `make test-go GO_TEST_PKGS="./cli/... ./bridge/handlers/schedules/..."`, then `make test-installation-scripts`, then `make check-systemd`, then `shellcheck packaging/scripts/install-linuxio-binaries.sh packaging/scripts/tests/installer-fixtures.sh && shfmt -d packaging/scripts/install-linuxio-binaries.sh packaging/scripts/tests/installer-fixtures.sh`
Expected: all pass, no diff from `shfmt`.

- [ ] **Step 7: Propose the commit message** `feat(jobs): hook scheduled units so every run writes its record`.

---

### Task 4: Progress from our own programs

**Files:**
- Create: `backend/common/scheduledrun/reporter.go`, `reporter_test.go`
- Modify: `backend/common/btrfs/replication_run.go` (`RunReplication`), `backend/common/btrfs/degraded.go` (`Resync`), every caller of the two, `backend/btrfs-timer/snapshot.go`, `backend/btrfs-timer/degraded.go`, `backend/common/dockerupdate/scheduled.go` (`RunScheduled`), `backend/docker-update/main.go`
- Test: `backend/btrfs-timer/snapshot_test.go`, `backend/btrfs-timer/health_test.go`, `backend/common/dockerupdate/scheduled_test.go`, the existing tests of `RunReplication` and `Resync`

**Interfaces:**
- Produces:

```go
// package scheduledrun
type Reporter struct{ /* store, id */ }

// ReporterFromEnv is the reporter of the scheduled run this process belongs
// to, found through $INVOCATION_ID; nil when there is none.
func ReporterFromEnv(store *jobs.Store) *Reporter

// Report writes the run's progress. A nil reporter does nothing. A negative
// percentage means unknown.
func (r *Reporter) Report(percentage int, message string)
```

```go
// package btrfs
func RunReplication(ctx context.Context, jobs []BtrfsReplicationJob, report func(done, total int)) []error
func Resync(ctx context.Context, uuid string, report func(percent float64)) error

// package dockerupdate
func RunScheduled(ctx context.Context, configPath string, report func(message string)) error
```

A nil `report` is allowed in all three.

- [ ] **Step 1: Write the failing reporter tests:**
  - `TestReporterWritesTheRunsProgress`: after `Hooks.Begin`, with `INVOCATION_ID` set, `Report(43, "Scrubbing")` makes `Store.ReadProgress(id)` return `{"percentage":43,"message":"Scrubbing"}`.
  - `TestReporterOmitsAnUnknownPercentage`: `Report(-1, "Taking snapshots")` writes `{"message":"Taking snapshots"}`.
  - `TestReporterWithoutARunDoesNothing`: an unset `INVOCATION_ID`, and one with no record, both give a nil reporter whose `Report` does not panic.
- [ ] **Step 2: Write the failing caller tests:**
  - `backend/btrfs-timer`: `TestSnapshotPassReportsItsPhases`: with the `runSnapshots` and `runReplication` fakes (the replication fake calls `report(0, 2)` and `report(1, 2)`) and a recording `reportProgress` seam, the messages are `Taking snapshots`, `Replicating 1 of 2`, `Replicating 2 of 2`, all with percentage -1.
  - `backend/btrfs-timer`: `TestResyncReportsTheScrubPercent`: the `resyncPool` fake calls `report(42.6)`; the seam records `(42, "Scrubbing")`.
  - `backend/common/btrfs`: `TestRunReplicationReportsEachEnabledJob`: two enabled jobs and one disabled give `report(0, 2)` then `report(1, 2)`.
  - `backend/common/dockerupdate`: `TestScheduledPassReportsItsSteps`: with fake operations and two selected containers `web` and `db`, the messages are `Checking for updates`, `Updating web (1 of 2)`, `Updating db (2 of 2)`.
- [ ] **Step 3: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./common/scheduledrun/... ./btrfs-timer/... ./common/btrfs/... ./common/dockerupdate/..." GO_TEST_FLAGS='-run "Reporter|Reports"'`
Expected: FAIL, undefined `ReporterFromEnv`; too many arguments in the calls.

- [ ] **Step 4: Implement.** `Resync` passes `report` to the scrub it runs, as the browser scrub job does. `btrfs-timer` and `docker-update` build one reporter at start with `ReporterFromEnv(jobs.NewScheduledStore())` and turn the callbacks into `Report` calls behind a package variable `reportProgress` that tests replace. A failed progress write is logged at Debug and never fails the run. Other callers of `RunReplication` and `Resync` pass nil.
- [ ] **Step 5: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/scheduledrun/... ./btrfs-timer/... ./common/btrfs/... ./common/dockerupdate/... ./docker-update/... ./bridge/handlers/storage/..."`
Expected: PASS.

- [ ] **Step 6: Propose the commit message** `feat(jobs): report progress from snapshot, resync and Docker update runs`.

---

### Task 5: Scheduled runs in the root host's job list

**Files:**
- Create: `backend/common/scheduledrun/liveness.go`, `liveness_test.go`, `backend/bridge/internal/jobshost/scheduled.go`, `scheduled_test.go`
- Modify: `backend/bridge/internal/jobshost/host.go` (`Config`, `storedJobs`, `snapshotFromRecord`), `backend/bridge/internal/jobshost/lifecycle.go` (`reconcile`), `backend/bridge/cmd/jobs_host.go`, `backend/bridge/internal/bridgeipc/task_primitives.go` (export the event writer), `backend/common/systemd/systemd.go`

**Interfaces:**
- Consumes: Tasks 1, 2 and 4.
- Produces:

```go
// package scheduledrun
type Liveness struct {
	Store  *jobs.Store
	BootID string
	// Active reports whether unit still runs invocationID.
	Active func(ctx context.Context, unit, invocationID string) (bool, error)
	Alert  func(ctx context.Context, obs alerts.Observation) error // nil raises none
	Now    func() time.Time
}

// Running returns the scheduled runs that still run, after marking the
// others interrupted.
func (l Liveness) Running(ctx context.Context) ([]jobs.Record, error)

// package systemd
// UnitRunsInvocation reports whether unit's current invocation is
// invocationID and its ActiveState is activating, active, reloading or
// deactivating.
func UnitRunsInvocation(ctx context.Context, unit, invocationID string) (bool, error)

// package jobshost, in Config
// Scheduled is the scheduled runs' store; nil lists none.
Scheduled *jobs.Store
// UnitActive answers for Scheduled's running records; nil means
// systemd.UnitRunsInvocation.
UnitActive func(ctx context.Context, unit, invocationID string) (bool, error)

// package bridgeipc
func WriteTaskEvent(stream net.Conn, event TaskEvent) bool
```

- [ ] **Step 1: Write the failing liveness tests:**
  - `TestRunningKeepsARunSystemdStillRuns`.
  - `TestRunningMarksARunFromAnEarlierBootInterrupted`: `Executor.BootID` differs from `BootID`; the record becomes `interrupted`, reason `reboot`, `FinishedAt == now`, `Active` is not asked, and a script gets the alert `Nightly backup was interrupted`.
  - `TestRunningMarksAGoneRunCrashed`: same boot, `Active` says false: `interrupted`, reason `crash`.
  - `TestRunningLeavesARunAloneWhenSystemdCannotBeAsked`: `Active` returns an error; the record stays `running` and is returned.
  - `TestRunningTellsApartTwoInvocationsOfOneUnit`: one record of an earlier boot and one of this boot for the same unit; only the second is returned, the first is `interrupted/reboot`.
  - `TestRunningDoesNotAlertForUnitsWithTheirOwnAlerts`: an interrupted `linuxio-btrfs-snapshot.service` run raises none.
- [ ] **Step 2: Write the failing host tests** (`scheduled_test.go`, the existing host harness, `Scheduled` on a temp store, `UnitActive` a fake, `scheduledPoll` set to 10 ms):
  - `TestSnapshotFromRecordCarriesMetadata`: a record with `Metadata` `{"label":"x","identity":["a"]}` gives a snapshot whose `Metadata.Label == "x"` and `Metadata.Identity == []string{"a"}`.
  - `TestTasksListIncludesRunningScheduledRuns`: one running script record with progress `{"message":"x"}` and one completed; `tasks.list` holds exactly the running one with `type == "scheduled.script"`, `state == "running"`, `metadata.label == "Nightly backup"`, `progress.message == "x"`.
  - `TestTasksEventsFollowAScheduledRun`: the stream starts with a `task.snapshot` for the running run; a new progress file gives a `task.progress` whose `task.progress.message` is the new one; a run begun after the stream opened gives a `task.snapshot`; ending a run `failed` gives a `task.error` whose `task.state == "failed"` and `task.error.message` is the record's.
  - `TestTasksEventsEndWithCompletedAndCanceled`: `completed` gives `task.result`, `canceled` gives `task.canceled`, `interrupted` gives `task.error` with code 409.
  - `TestTasksGetAnswersForAScheduledRun`: running and ended.
  - `TestTasksCancelRefusesAScheduledRun`: 409, message `a scheduled run is stopped on its page`.
  - `TestTasksWatchRefusesARunningScheduledRun`: 409, message `a running scheduled run is followed through tasks.events`; an ended one is answered like any stored job.
  - `TestHostWithoutAScheduledStoreListsNone`.
  - `TestHostStartMarksScheduledRunsOfAnEarlierBoot`.
- [ ] **Step 3: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./common/scheduledrun/... ./bridge/internal/jobshost/..." GO_TEST_FLAGS='-run "Running|Scheduled|Metadata"'`
Expected: FAIL, undefined `Liveness`, `Config.Scheduled`.

- [ ] **Step 4: Implement.**
  - `Liveness.Running` reads the store's active records; one whose boot differs is marked without asking systemd; an `Active` error leaves the record as it is. Whether a run alerts and where its alert links come from `KindOf(record.Executor.Unit, "")`; the alert's title uses the label stored in the record's `Metadata`.
  - `storedJobs.List` returns the snapshots of `Running`, each with the store's progress file decoded into `Progress`.
  - `storedJobs.Events` is one loop that ends with its context: every `scheduledPoll` (2 s) it re-reads progress files of the runs it knows; it re-lists the store only when `Store.ModTime` changed or 30 s passed since the last listing; it writes events with `bridgeipc.WriteTaskEvent`.
  - `storedJobs.record` looks in `Scheduled` when the host's own store has no such ID.
  - `Host.reconcile` calls `Running` once at start when `Scheduled` is set.
  - `jobs_host.go` sets `cfg.Scheduled = jobs.NewScheduledStore()` only for uid 0.
  - The idle rule is unchanged: a scheduled run does not keep the host up.
- [ ] **Step 5: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/scheduledrun/... ./common/systemd/... ./bridge/internal/jobshost/... ./bridge/internal/bridgeipc/... ./bridge/cmd/..."`
Expected: PASS.

- [ ] **Step 6: Propose the commit message** `feat(jobs): list scheduled runs in the root job host`.

---

### Task 6: A stop marks the run canceled

**Files:**
- Modify: `backend/common/scheduledrun/hooks.go`, `backend/bridge/handlers/schedules/native.go` (`hostOps`), `backend/bridge/handlers/schedules/manager.go` (`stop`), the handler of `storage.btrfs_scrub_cancel` under `backend/bridge/handlers/storage`
- Test: `backend/common/scheduledrun/hooks_test.go`, `backend/bridge/handlers/schedules/schedules_test.go`, the scrub cancel handler's test file

**Interfaces:**
- Produces:

```go
// RequestCancel marks the running record of unit as canceled by request, so
// End records the run as canceled. No running record is not an error.
func RequestCancel(ctx context.Context, store *jobs.Store, unit string) error
```

and in `hostOps`: `markCanceled func(ctx context.Context, unit string) error`.

- [ ] **Step 1: Write the failing tests:**
  - `TestRequestCancelMarksTheUnitsRunningRecord`: the unit's running record gets `CancelRequested`; an ended record of the same unit and a running record of another unit are untouched.
  - `TestRequestCancelWithoutARunIsNil`.
  - schedules, `TestStopMarksTheRunCanceledFirst`: the recorded calls end with `mark:linuxio-schedule-<id>.service` then `stop-invocation:<id>:<invocation>`; a `markCanceled` error does not prevent the stop.
  - storage, `TestScrubCancelMarksATimersResync`: the cancel calls the mark seam with `btrfs.ResyncUnitName(uuid)` before the kernel cancel.
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./common/scheduledrun/... ./bridge/handlers/schedules/... ./bridge/handlers/storage/..." GO_TEST_FLAGS='-run "RequestCancel|StopMarks|ScrubCancelMarks"'`
Expected: FAIL, undefined `RequestCancel`, `markCanceled`.

- [ ] **Step 3: Implement.** A failed mark is logged at Warn and the stop goes on. Both routes already declare `Privileged()`.
- [ ] **Step 4: Run the package tests**

Run: `make test-go GO_TEST_PKGS="./common/scheduledrun/... ./bridge/handlers/schedules/... ./bridge/handlers/storage/..."`
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(jobs): record a stopped scheduled run as canceled`.

---

### Task 7: Live output on the Schedules page

**Files:**
- Modify: `backend/bridge/apischema/logs.go` (`ServiceLogsFollowRequest`), `backend/bridge/handlers/logs/service.go`, `frontend/src/routes/_authenticated/services/-components/SchedulesTab.tsx`
- Test: the service log request test in `backend/bridge/handlers/logs`, `frontend/src/routes/_authenticated/services/-components/SchedulesTab.test.tsx` (create it if missing)
- Generated: `frontend/src/api/generated/*` through `make generate`

**Interfaces:**
- Produces: `ServiceLogsFollowRequest.InvocationID *string` with tag `json:"invocationId,omitzero"`. With it, `logs.service.follow` shows only what the unit's own processes logged in that run.

- [ ] **Step 1: Write the failing backend tests:**
  - `TestServiceLogsScopeToAnInvocation`: unit `linuxio-schedule-x.service`, invocation `1234567890abcdef1234567890abcdef`, lines `200` build the arguments `_SYSTEMD_UNIT=linuxio-schedule-x.service _SYSTEMD_INVOCATION_ID=1234567890abcdef1234567890abcdef -n 200 -f --no-pager -o short-iso`, with no `-u`.
  - `TestServiceLogsRejectABadInvocation`: `"nope"` returns an error wrapping `bridgeipc.ErrInvalidArgs`.
  - Without an invocation the arguments are unchanged (extend the existing test).
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS=./bridge/handlers/logs/... GO_TEST_FLAGS='-run TestServiceLogs'`
Expected: FAIL, unknown field `InvocationID`.

- [ ] **Step 3: Implement the backend**, reusing `invocationIDMatch`, then run `make generate`.
- [ ] **Step 4: Write the failing frontend test** in `SchedulesTab.test.tsx`: with a selected schedule whose status has `unit_name: "linuxio-schedule-x.service"` and `invocation_id: "1234567890abcdef1234567890abcdef"`, the details show a card titled `Output of the latest run` and `openChannel` was called with `"logs.service.follow"` and an object containing `serviceName: "linuxio-schedule-x.service"` and `invocationId: "1234567890abcdef1234567890abcdef"`; with an empty `invocation_id` there is no such card.
- [ ] **Step 5: Run and see it fail**

Run: `make test-frontend VITEST_FILE=src/routes/_authenticated/services/-components/SchedulesTab.test.tsx`
Expected: FAIL, the card is not found.

- [ ] **Step 6: Implement.** In `Details`, render `UnitLogsCard` with `title="Output of the latest run"`, a `key` of the invocation ID so a new run restarts the stream, and a `createStream` that opens `logs.service.follow` with the unit, the invocation and the card's `tail`. The "Logs" and "Latest run logs" buttons stay.
- [ ] **Step 7: Run the tests**

Run: `make test-go GO_TEST_PKGS=./bridge/handlers/logs/...` then `make test-frontend VITEST_FILE=src/routes/_authenticated/services/-components/SchedulesTab.test.tsx`
Expected: PASS.

- [ ] **Step 8: Propose the commit message** `feat(schedules): show the latest run's output live on the Schedules page`.

---

### Task 8: Scheduled runs in the bell

**Files:**
- Create: `frontend/src/tasks/scheduledRuns.ts`, `frontend/src/tasks/scheduledRuns.test.ts`
- Modify: `frontend/src/tasks/types.ts` (`BackgroundTask`), `frontend/src/tasks/engine/useRecoveredTasks.ts`, `frontend/src/routes/_authenticated/-components/footer/NotificationsBell.tsx`
- Test: `frontend/src/tasks/engine/useRecoveredTasks.test.tsx`, `frontend/src/routes/_authenticated/-components/footer/NotificationsBell.test.tsx`

**Interfaces:**
- Produces:

```ts
export const isScheduledRun: (type: string) => boolean;
/** "Label", or "Label: message" once the run reports progress. */
export function scheduledRunLabel(task: TaskSnapshot): string;
/** The page that owns the run. */
export function scheduledRunLink(task: TaskSnapshot): { to: string; search?: Record<string, string> };

// BackgroundTask gains
/** A scheduled run: no cancel here; the row opens this page. */
link?: { to: string; search?: Record<string, string> };
```

- [ ] **Step 1: Write the failing unit tests** (`scheduledRuns.test.ts`):
  - `isScheduledRun` is true for the four routes and false for `docker.update_container`.
  - Labels: type `scheduled.btrfs_snapshot`, `metadata.label` `Snapshots and replication`, `progress.message` `Replicating 2 of 5` gives `Snapshots and replication: Replicating 2 of 5`; a script named `Nightly backup` with no progress gives `Nightly backup`; no label gives `Scheduled run`.
  - Links: a script with `metadata.identity` `["<id>"]` gives `{ to: "/services/schedules", search: { schedule: "<id>" } }`; a resync with `["<uuid>"]` gives `{ to: "/storage", search: { pool: "<uuid>" } }`; a snapshot run gives `{ to: "/storage/subvolumes", search: { view: "snapshots" } }`; a Docker update gives `{ to: "/docker/containers" }`.
- [ ] **Step 2: Write the failing hook tests** (`useRecoveredTasks.test.tsx`):
  - A `task.snapshot` event of a running `scheduled.script` adds one row with the label, `indeterminate: true`, the link, and `openTaskWatchStream` was not called.
  - A `task.progress` event with `progress: { percentage: 43, message: "Scrubbing" }` for a `scheduled.btrfs_resync` row sets `progress: 43`, `indeterminate: false` and the label `Pool resync: Scrubbing`.
  - A terminal event removes the row and shows no toast, whatever the state.
  - A second snapshot event of the same run adds no second row.
- [ ] **Step 3: Write the failing bell tests** (`NotificationsBell.test.tsx`): a row with `link` has no `Cancel task` button and is a link to that page; a row with `cancelRequested` and `link` shows `Cancelling…`.
- [ ] **Step 4: Run and see them fail**

Run each: `make test-frontend VITEST_FILE=src/tasks/scheduledRuns.test.ts`, `make test-frontend VITEST_FILE=src/tasks/engine/useRecoveredTasks.test.tsx`, `make test-frontend VITEST_FILE=src/routes/_authenticated/-components/footer/NotificationsBell.test.tsx`
Expected: FAIL, missing module; rows not found.

- [ ] **Step 5: Implement.** In the events handler of `useRecoveredTasks`, a scheduled run is handled before `watchRecoveredTask` and returns: it upserts or removes the row from the event's `task`, opens no watch stream, emits no terminal feedback and invalidates no queries. A failed script reaches the user through its alert, which the bell already shows.
- [ ] **Step 6: Run the tests**, the three commands of Step 4.
Expected: PASS.

- [ ] **Step 7: Propose the commit message** `feat(jobs): show running scheduled runs in the bell`.

---

### Task 9: Real-machine checks, documents and gates

**Files:**
- Create: `frontend/src/test/vm/jobs-phase4.spec.ts`
- Modify: `frontend/src/test/vm/vm.ts` (add `scheduledRecords()` and `scheduledOutput(id)` reading `/var/lib/linuxio/jobs/scheduled`), `docs/TODO/jobs.md` (status table, new section 3.8, section 4.4), `docs/api-contract.md` (tasks section: the four types, `metadata`, the two 409s, `invocationId` on `logs.service.follow`), `docs/process-systemd-architecture.md` (the hooks, the store, the command), `docs/alerts.md` (the script failure alert)

- [ ] **Step 1: Write the VM spec.** Each test creates its schedule on the Schedules page with a calendar that never fires on its own and starts it with "Run now".
  1. *A running script shows in the bell and on its page, and leaves its output.* Script `echo first; sleep 20; echo last >&2`. While it runs: the bell lists the schedule's name with no `Cancel task`; clicking the row opens `/services/schedules?schedule=<id>`; the page's output card shows `first`. After it ends: one record with route `scheduled.script`, state `completed`; its saved output is exactly `first\nlast\n`.
  2. *A failing script raises an alert.* Script `echo boom; exit 3`: state `failed`, error `exited with status 3`; the bell shows the alert `<name> failed`.
  3. *A script stopped on its page ends canceled.* Script `sleep 300`; "Stop": state `canceled`, `cancel_requested` true, no alert.
  4. *A script cut by a power loss is marked after the boot.* Script `sleep 300`; `rig(["power-cut"])`; after the boot and a sign-in: state `interrupted`, reason `reboot`; the alert `<name> was interrupted`.
  5. *A long output keeps its start and its end.* Script `for i in $(seq 300); do head -c 45000 /dev/zero | tr '\0' x; echo; done`: the saved output is at most 10 MiB plus 64 bytes and contains `bytes left out`.
  6. *The last line before the exit is saved.* Script `echo almost; echo last`, run 10 times in a row: every saved output ends with `last\n`.
  7. *A broken hook does not stop the run.* Move `/usr/local/bin/linuxio` aside over SSH, run a script that writes `/tmp/phase4-ran`, restore the binary: the file exists and the unit's result is `success`.
  8. *A drop-in alone creates no unit.* Before any snapshot rule exists, `systemctl show -p LoadState --value linuxio-btrfs-snapshot.service` over SSH prints `not-found`.
  9. *The snapshot unit records itself.* Save a snapshot rule for the test pool on the Storage page, then `sudo systemctl start linuxio-btrfs-snapshot.service` over SSH: one record with route `scheduled.btrfs_snapshot`, state `completed`.
- [ ] **Step 2: Run the full gate**

Run: `make test-quiet`
Expected: PASS. On a failure read `.cache/test-logs/` before any rerun.

- [ ] **Step 3: Run the VM checks**

Run, one after the other: `make vm-reset`, `make vm-install`, `make test-vm-browser PLAYWRIGHT_ARGS='login.spec.ts jobs-phase4.spec.ts'`, then `make vm-test-btrfs-integration` (Task 4 changed the `Resync` and `RunReplication` call paths).
Expected: all pass. If test 6 fails, the journal is behind `end`: fix it in `Hooks.Output`, not in the test.

- [ ] **Step 4: Check an upgrade by hand in the VM.** Install the previous release, create a schedule, install this build over it, press "Run now" without saving: the run starts and leaves a record. Note the result in section 3.8.
- [ ] **Step 5: Ask for the privilege review.** Dispatch the `privilege-boundary-reviewer` agent on the whole diff: two hooks run as root on units whose main process runs as a chosen user. Fix what it finds and rerun the affected gates.
- [ ] **Step 6: Reconcile the documents.** Section 3.8 of `docs/TODO/jobs.md` lists what was built, the targets run and their results, and what was only inferred (the Docker update unit's hooks, unless Docker runs in the VM). The status row of phase 4 becomes `done`.
- [ ] **Step 7: Propose the commit message** `test(jobs): check scheduled runs in the test VM and document phase 4`.
