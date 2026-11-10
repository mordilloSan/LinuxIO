# Jobs Phase 5a: History and Update Banner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A page that lists ended jobs and scheduled runs, each schedule's past runs with their saved output, and a banner that says how many running jobs a LinuxIO update is waiting for.

**Architecture:** The job host is the only process that can read job records, so two new built-in task routes (`tasks.history`, `tasks.output`) travel the path `tasks.list` already takes: browser, bridge task service, relay, job host, store. A third (`tasks.host`) reports whether the bridge's job host belongs to an older build; in that state the bridge reads the running records itself, so the banner can count and name the jobs being waited for. The frontend adds one page, one shared output viewer and one banner.

**Tech Stack:** Go (`backend/common/jobs`, `backend/bridge/internal/bridgeipc`, `backend/bridge/internal/jobshost`, `backend/bridge/cmd`), generated TypeScript contract, React with TanStack Router and Query, Playwright against the test VM.

**Spec:** `docs/TODO/jobs.md` sections 2.12 (updates), 2.16 (frontend) and 4.5 (the decisions taken with the user on 2026-11-10). Read 4.5 before any task.

## Global Constraints

- Never create, amend or push a commit. Each task ends by proposing a commit message; the user commits.
- Use Make targets only (`make test-go GO_TEST_PKGS=… GO_TEST_FLAGS='-run …'`, `make test-frontend VITEST_FILE=<one file>`, `make generate`). Never two Make runs at once in one worktree; wait for a running one.
- Admins see every ended job and every scheduled run. Other users see only the jobs of their own job host. Nothing in this plan lets a non-admin read a scheduled run or a saved output.
- A job that completed in under one second leaves no record and therefore no history entry. Do not change that.
- A saved output is raw journal text of at most 10 MiB with control characters and possibly invalid UTF-8. It is shown as plain text, never as HTML, and never sent in one piece: a relay frame holds at most 16 MiB after JSON escaping.
- `tasks.*` is a reserved namespace: new routes are declared in `backend/bridge/handlers/tasks/handlers.go` and implemented in `TaskService.RegisterRoutes` (`backend/bridge/internal/bridgeipc/task_primitives.go`). The contract types in `backend/bridge/apischema/tasks.go` mirror the private ones by hand.
- `frontend/src/api/generated/*` and `frontend/src/routeTree.gen.ts` change only through `make generate` and the router plugin.
- The update banner has no button that forces the update (decision in 4.5).
- Logging follows AGENTS.md: return or log, never both; lowercase constant messages; the key is `error`.
- UI copy is British English.

## Review Focus

1. A non-admin must get nothing from `tasks.output` and no scheduled run from `tasks.history`, also when they guess a run's ID (Task 3, `TestUserHostAnswersNoOutput`; Task 2, `TestUserHostHistoryHasNoScheduledRuns`).
2. An output full of escape sequences or invalid UTF-8 must reach the page as inert text and must not break the JSON frame (Task 3, `TestReadOutputReplacesInvalidUTF8`; Task 5, the viewer test with `<script>` and `\x1b[31m`).
3. Records written by older builds have no metadata and must still list with a readable title (Task 5, `taskTitle` fallback tests).
4. While the job host is an older build, the banner must still name the jobs being waited for, `tasks.list` must stay as it is (nothing can watch a job on an older host) and history must not error (Task 4, `TestRelayStatusWhileAnUpdateWaits`, `TestRelayListIsEmptyWhileAnUpdateWaits`).
5. A store with thousands of scheduled records must answer a first page quickly and page correctly at equal timestamps (Task 2, `TestHistoryPagesThroughRecordsWithEqualTimes`).

---

### Task 1: Keep a browser job's label in its record

**Files:**
- Modify: `backend/bridge/internal/jobshost/recorder.go` (`newRecord`)
- Test: `backend/bridge/internal/jobshost/host_test.go`

**Interfaces:**
- Produces: for a job whose route declares task metadata, `jobs.Record.Metadata` holds the JSON of its `bridgeipc.TaskMetadata` (the public projection: `label`, `path`, `action`, …). `snapshotFromRecord` already decodes it.

- [ ] **Step 1: Write the failing test** `TestRecordKeepsTheTasksPublicMetadata`: start a job on a test route registered with `bridgeipc.WithTaskMetadata` returning `TaskMetadata{Label: "Scrubbing btrfs filesystem", Identity: []string{"pool-1"}}`; when it has ended, read the record from the store: `Metadata` decodes to that label and identity, and `Target == []string{"pool-1"}` as before. A second case: a route with no metadata builder leaves `Metadata` empty.
- [ ] **Step 2: Run it and see it fail**

Run: `make test-go GO_TEST_PKGS=./bridge/internal/jobshost/... GO_TEST_FLAGS='-run TestRecordKeepsTheTasksPublicMetadata'`
Expected: FAIL, `Metadata` is empty.

- [ ] **Step 3: Implement** in `newRecord`: marshal `task.Snapshot().Metadata` when it is not nil. The request itself stays in `Request` and is never returned.
- [ ] **Step 4: Run the package tests**

Run: `make test-go GO_TEST_PKGS=./bridge/internal/jobshost/...`
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(jobs): keep a job's public metadata in its record`.

---

### Task 2: `tasks.history`

**Files:**
- Modify: `backend/common/jobs/store.go`, `backend/bridge/internal/bridgeipc/task_primitives.go`, `backend/bridge/apischema/tasks.go`, `backend/bridge/handlers/tasks/handlers.go`, `backend/bridge/internal/jobshost/host.go`, `backend/bridge/internal/jobshost/scheduled.go`, `backend/bridge/cmd/jobs_relay.go`
- Test: `backend/common/jobs/store_test.go`, `backend/bridge/internal/bridgeipc/task_primitives_test.go`, `backend/bridge/internal/jobshost/scheduled_test.go`, `backend/bridge/cmd/jobs_relay_test.go`, `backend/bridge/handlers/tasks/handlers_test.go`
- Generated: `frontend/src/api/generated/*`

**Interfaces:**
- Consumes: Task 1.
- Produces:

```go
// package jobs
const (
	DefaultHistoryLimit = 100
	MaxHistoryLimit     = 500
)

// HistoryQuery selects ended records, newest first.
type HistoryQuery struct {
	Before time.Time // only records that ended before it; zero means no bound
	Limit  int       // 0 means DefaultHistoryLimit; capped at MaxHistoryLimit
	Target string    // only records whose first target is it; "" means all
}

// History returns ended records by the time they ended, newest first, and
// whether older ones remain.
func (s *Store) History(ctx context.Context, q HistoryQuery) ([]Record, bool, error)

// package bridgeipc
type TaskHistoryRequest struct {
	Before *time.Time `json:"before,omitzero"`
	Limit  int        `json:"limit,omitzero"`
	Unit   string     `json:"unit,omitzero"` // only the runs of this systemd unit
}

type TaskHistoryEntry struct {
	Task   TaskSnapshot `json:"task"`
	Reason string       `json:"reason,omitzero"` // shutdown, reboot or crash for an interrupted job
	Unit   string       `json:"unit,omitzero"`   // the unit of a scheduled run
	Output bool         `json:"output,omitzero"` // a saved output exists
}

type TaskHistory struct {
	Entries []TaskHistoryEntry `json:"entries"`
	More    bool               `json:"more"`
}

// RemoteTasks gains
History(ctx context.Context, req TaskHistoryRequest) (TaskHistory, error)
```

Route: `tasks.history`, a retry-safe Call, not privileged (a user's host answers with that user's jobs).

- [ ] **Step 1: Write the failing store tests:**
  - `TestHistoryListsEndedRecordsNewestFirst`: three ended records and one running; the result holds the three, ordered by `FinishedAt` descending, and `more` is false.
  - `TestHistoryStopsAtTheLimit`: five ended records, `Limit: 2`: the two newest and `more == true`; `Limit: 0` uses 100; `Limit: 9999` is capped at 500.
  - `TestHistoryBeforeATime`: `Before` set to the second record's end time returns only older ones.
  - `TestHistoryOfATarget`: `Target: "a.service"` returns only records whose `Target[0]` is it.
  - `TestHistoryPagesThroughRecordsWithEqualTimes`: four records that ended at the same instant, paged with `Limit: 2` and `Before` taken from each page's last record: every record is returned exactly once. (Ties are ordered by ID; say in a comment how the second page avoids repeating the first.)
- [ ] **Step 2: Write the failing service and host tests:**
  - bridgeipc: `TestTasksHistoryAsksTheRemote` (the fake remote's entries come back; no remote gives an empty list and `more == false`); `TestTasksHistoryRejectsABadRequest` (`limit: -1` is `ErrInvalidArgs`).
  - jobshost: `TestTasksHistoryListsEndedJobsNewestFirst` (two ended host jobs, each entry's `task.state`, `task.metadata.label` and times); `TestTasksHistoryIncludesScheduledRunsOnTheRootHost` (one host job and one scheduled run interleaved by end time; the scheduled entry has `unit`, and `output == true` only when its `.log` exists); `TestTasksHistoryOfAUnit` (`unit` returns that unit's runs only, never a host job); `TestTasksHistoryCarriesTheReason` (an interrupted record gives `reason: "reboot"`); `TestTasksHistoryHidesRecordsOfOtherExecutors` (a record whose `Executor.Kind` is neither `host` nor `unit`, as the network handoff writes, is not listed); `TestUserHostHistoryHasNoScheduledRuns` (`Config.Scheduled == nil`: only its own jobs, and `unit` gives an empty list).
  - cmd: `TestRelayForwardsHistory` (the relay calls the host's `tasks.history` and returns its answer; with no host connected it returns an empty history and no error).
  - handlers/tasks: extend `TestRoutesAreRegisteredByTaskService` with `tasks.history`.
- [ ] **Step 3: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./common/jobs/... ./bridge/internal/bridgeipc/... ./bridge/internal/jobshost/... ./bridge/cmd/... ./bridge/handlers/tasks/..." GO_TEST_FLAGS='-run "History"'`
Expected: FAIL, undefined `History`, `TaskHistoryRequest`.

- [ ] **Step 4: Implement.** `Store.History` reads the record files once, keeps terminal ones and sorts by end time, then ID. The host's `storedJobs.History` lists its own store (executor kind `host` only) and, when `Scheduled` is set, merges the scheduled store's history into one newest-first list cut at the limit; with `Unit` it asks only the scheduled store with `Target: unit`. Each entry is `snapshotFromRecord` plus `Reason`, `Unit` and whether the output file exists. The bridge's handler only forwards to the remote; the relay uses `Client.Call(ctx, "tasks.history", req)`. Add the contract route and mirror types, then `make generate`.
- [ ] **Step 5: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/jobs/... ./bridge/internal/bridgeipc/... ./bridge/internal/jobshost/... ./bridge/cmd/... ./bridge/handlers/tasks/... ./bridge/apischema/..."`
Expected: PASS.

- [ ] **Step 6: Propose the commit message** `feat(jobs): list ended jobs and scheduled runs through tasks.history`.

---

### Task 3: `tasks.output`

**Files:**
- Modify: `backend/common/jobs/scheduled.go`, `backend/bridge/internal/bridgeipc/task_primitives.go`, `backend/bridge/apischema/tasks.go`, `backend/bridge/handlers/tasks/handlers.go`, `backend/bridge/internal/jobshost/scheduled.go`, `backend/bridge/cmd/jobs_relay.go`
- Test: `backend/common/jobs/scheduled_test.go`, `backend/bridge/internal/jobshost/scheduled_test.go`, `backend/bridge/internal/bridgeipc/task_primitives_test.go`, `backend/bridge/cmd/jobs_relay_test.go`
- Generated: `frontend/src/api/generated/*`

**Interfaces:**
- Produces:

```go
// package jobs
const (
	DefaultOutputChunk = 256 << 10
	MaxOutputChunk     = 1 << 20
)

// ReadOutput returns up to limit bytes of the saved output of id starting at
// offset, and the output's size. A negative offset means the last limit bytes.
func (s *Store) ReadOutput(id string, offset int64, limit int) (data []byte, start, size int64, err error)

// package bridgeipc
type TaskOutputRequest struct {
	TaskID string `json:"taskId"`
	Offset *int64 `json:"offset,omitzero"` // absent: the end of the output
	Limit  int    `json:"limit,omitzero"`
}

type TaskOutput struct {
	Content string `json:"content"` // valid UTF-8; invalid bytes are replaced
	Offset  int64  `json:"offset"`  // where Content starts in the output
	Size    int64  `json:"size"`
}

// RemoteTasks gains
Output(ctx context.Context, req TaskOutputRequest) (TaskOutput, error)
```

Route: `tasks.output`, a retry-safe Call, `Privileged` (declared with `apischema.Privileged()` in the contract and `Privileged: true` on the built-in route).

- [ ] **Step 1: Write the failing tests:**
  - jobs: `TestReadOutputReturnsTheEndByDefault` (a 600 KiB file, offset -1, limit 0: the last 256 KiB, `start == size-262144`); `TestReadOutputFromAnOffset` (offset 10, limit 5 returns bytes 10 to 14); `TestReadOutputCapsTheChunk` (limit 5 MiB returns 1 MiB); `TestReadOutputPastTheEndIsEmpty`; `TestReadOutputOfAMissingFileIsNotFound` (`jobs.ErrNotFound`); `TestReadOutputRefusesALink` (a symlink at the output path is an error, not followed); `TestReadOutputRejectsABadID`.
  - jobshost: `TestTasksOutputReturnsAChunk`; `TestReadOutputReplacesInvalidUTF8` (bytes `a\xffb\x1b[31m` come back as `a�b\x1b[31m`: invalid bytes replaced, control characters kept); `TestTasksOutputOfAJobWithoutOneIs404`; `TestUserHostAnswersNoOutput` (`Config.Scheduled == nil`: 404 for any ID).
  - bridgeipc: `TestTasksOutputIsPrivileged` (an unprivileged session gets 403 before the remote is asked); `TestTasksOutputRejectsABadRequest` (empty `taskId`, `limit: -1`).
  - cmd: `TestRelayForwardsOutput`.
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./common/jobs/... ./bridge/internal/bridgeipc/... ./bridge/internal/jobshost/... ./bridge/cmd/..." GO_TEST_FLAGS='-run "Output"'`
Expected: FAIL, undefined `ReadOutput`, `TaskOutputRequest`.

- [ ] **Step 3: Implement.** `ReadOutput` opens the file the way `readRegularFile` in `store.go` does (inside the store directory, regular file only, no link followed) and reads one chunk with `ReadAt`. The host answers only from the scheduled store; `strings.ToValidUTF8(…, "�")` makes the content valid. Add the contract route, then `make generate`.
- [ ] **Step 4: Run the tests**

Run: `make test-go GO_TEST_PKGS="./common/jobs/... ./bridge/internal/bridgeipc/... ./bridge/internal/jobshost/... ./bridge/cmd/... ./bridge/handlers/tasks/... ./bridge/apischema/..."`
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(jobs): serve a scheduled run's saved output in chunks`.

---

### Task 4: Know when an update waits, and for how many jobs

**Files:**
- Modify: `backend/bridge/cmd/jobs_relay.go`, `backend/bridge/internal/bridgeipc/task_primitives.go`, `backend/bridge/apischema/tasks.go`, `backend/bridge/handlers/tasks/handlers.go`, `backend/bridge/internal/jobshost/host.go`, `backend/bridge/internal/jobshost/scheduled.go`
- Test: `backend/bridge/cmd/jobs_relay_test.go`, `backend/bridge/internal/bridgeipc/task_primitives_test.go`, `backend/bridge/internal/jobshost/host_test.go`
- Generated: `frontend/src/api/generated/*`

**Interfaces:**
- Produces:

```go
// package bridgeipc
type TaskHostStatus struct {
	UpdatePending bool           `json:"update_pending"` // the job host is of an older build
	Running       int            `json:"running"`        // jobs it still runs
	Jobs          []TaskSnapshot `json:"jobs,omitzero"`  // those jobs, only while an update waits
}

// RemoteTasks gains
Status(ctx context.Context) (TaskHostStatus, error)

// package jobshost
// RunningSnapshot is the snapshot of a record whose job still runs.
func RunningSnapshot(r jobs.Record, progress jsontext.Value) bridgeipc.TaskSnapshot
```

Route: `tasks.host`, a retry-safe Call, not privileged. With no remote it answers `{false, 0}`.

Background: an older host refuses the bridge's hello with 426, `jobsclient` turns that into `ErrUpdatePending`, and the relay then has no client: today its `List` returns nothing and a new mutation gets `409 update_pending: LinuxIO update waits for running jobs`, with no count. The older host cannot be asked, but its records can be read: the bridge runs as the same uid as its host.

`tasks.list` stays as it is while an update waits (it returns nothing). The bell's tray is fed by `tasks.events` and each of its rows by `tasks.watch`, and pages that find a job through `tasks.list` go on to watch it; an older host can serve neither stream. So the jobs being waited for travel in the answer of `tasks.host`, and the banner names them itself (Task 7).

- [ ] **Step 1: Write the failing tests** (cmd, with a temp store standing for the host's and a fake that makes the hello fail with 426):
  - `TestRelayStatusWhileAnUpdateWaits`: two `running` records of executor kind `host` whose process is alive (use the test's own pid and start time) and one whose process is gone: `Status` is `{UpdatePending: true, Running: 2}` and `Jobs` holds the two live records as running snapshots with their metadata and last progress.
  - `TestRelayStatusWithACurrentHost`: `{false, n}` as the host reports, and no `Jobs`.
  - `TestRelayStatusWaitsForTheFirstConnection`: asked while the relay is still connecting, `Status` answers only once the host is connected or has refused the hello; when neither happens within the host wait it answers `{false, 0}` and no error.
  - `TestRelayListIsEmptyWhileAnUpdateWaits`: `List` still returns nothing.
  - `TestUpdatePendingMessageCarriesTheCount`: a forwarded mutation is refused with code 409 and the message `update_pending: LinuxIO update waits for 2 running jobs`; with one, `…for 1 running job`; with none readable, `…for running jobs`.
  - `TestRelayHistoryWhileAnUpdateWaits`: `History` returns an empty history at once and no error.
  - `TestRelayHistoryWaitsForTheHost`: asked while the relay is still connecting, `History` answers from the host once it is connected; when no host appears within the host wait it answers 503 (this replaces Task 2's empty answer without a host, which looked the same as "no history").
  - jobshost: `TestHostStatusCountsRunningJobs`.
  - bridgeipc: `TestTasksHostWithoutARemote`.
- [ ] **Step 2: Run and see them fail**

Run: `make test-go GO_TEST_PKGS="./bridge/cmd/... ./bridge/internal/bridgeipc/... ./bridge/internal/jobshost/..." GO_TEST_FLAGS='-run "Status|WhileAnUpdateWaits|UpdatePending|TasksHost"'`
Expected: FAIL, undefined `Status`, `TaskHostStatus`.

- [ ] **Step 3: Implement.** The relay learns the store's two directories where it learns the socket path (`version.DataDir + "/jobs/<uid>"`, `version.RunDir + "/jobs/<uid>"`). While its last connect failed with `ErrUpdatePending`, `Status` reads `ListActive`, keeps records of executor kind `host` whose process `jobs.ProcessAlive` confirms, and builds snapshots with `jobshost.RunningSnapshot` (move the function `scheduled.go` already uses for a running scheduled record into one exported place); the refusal of a mutation takes its count from the same read. `Status` and `History` wait, up to the host wait, for the first connection attempt to end in a client or in that refusal. The existing prefix `update_pending:` and code 409 stay, so `TestBridgeReportsUpdatePending` keeps passing.
- [ ] **Step 4: Run the tests**

Run: `make test-go GO_TEST_PKGS="./bridge/cmd/... ./bridge/internal/bridgeipc/... ./bridge/internal/jobshost/... ./bridge/handlers/tasks/... ./bridge/apischema/..."` then `make generate`.
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(jobs): count the jobs a LinuxIO update waits for`.

---

### Task 5: The history page and the bell's link

**Files:**
- Create: `frontend/src/tasks/taskTitles.ts`, `frontend/src/tasks/taskTitles.test.ts`, `frontend/src/components/tasks/SavedOutput.tsx`, `frontend/src/components/tasks/SavedOutput.test.tsx`, `frontend/src/routes/_authenticated/history/route.tsx`, `frontend/src/routes/_authenticated/history/-components/HistoryPage.tsx`, `frontend/src/routes/_authenticated/history/-components/HistoryPage.test.tsx`
- Modify: `frontend/src/routes/_authenticated/-components/footer/NotificationsBell.tsx`, `frontend/src/routes/_authenticated/-components/footer/NotificationsBell.test.tsx`

**Interfaces:**
- Consumes: `tasks.history` (Task 2) and `tasks.output` (Task 3).
- Produces: `<SavedOutput taskId={string} />`, which shows the end of a saved output and loads earlier parts on request, and

```ts
/** A readable name for a job or scheduled run, for lists of ended work. */
export function taskTitle(task: TaskSnapshot): string;
/** "Completed", "Failed", "Cancelled", "Interrupted (reboot)". */
export function taskOutcome(entry: TaskHistoryEntry): string;
```

Route `/history`: any signed-in user; not in the sidebar (no `staticData.navigation`); search `{ job?: string }` selects an entry.

- [ ] **Step 1: Write the failing title tests** (`taskTitles.test.ts`):
  - a scheduled run uses its label: `{type: "scheduled.script", metadata: {label: "Nightly backup"}}` gives `Nightly backup`;
  - a job with a stored label and path: `{type: "filebrowser.compress", metadata: {label: "backup.zip"}}` gives `backup.zip`;
  - a job with no metadata falls back to the route: `storage.btrfs_scrub_start` gives `Storage: btrfs scrub start`, `docker.update_container` gives `Docker: update container`;
  - outcomes: completed, failed, `canceled` shown as `Cancelled`, interrupted with reason `reboot` shown as `Interrupted (reboot)`, interrupted with no reason as `Interrupted`.
- [ ] **Step 2: Write the failing viewer tests** (`SavedOutput.test.tsx`):
  - it asks `tasks.output` with only the `taskId` and shows the returned text in a `LogViewport`;
  - when the first chunk starts after byte 0 it shows `Show earlier`; pressing it asks with `offset` = max(0, start − 262144) and `limit` = start − that offset, and puts the text before what is shown; at offset 0 the button is gone;
  - text is inert: content `<script>alert(1)</script>\x1b[31mred` appears as those characters and creates no element;
  - a 404 shows `No output was saved for this run.`
- [ ] **Step 3: Write the failing page tests** (`HistoryPage.test.tsx`, the query mocked at `linuxio.tasks.history`):
  - rows show title, outcome, who started it (`task.owner.username`), when it ended and how long it ran, newest first as received;
  - an empty history shows `Nothing has ended yet.`;
  - `more: true` shows a `Load older` button that asks again with `before` set to the last entry's end time and appends;
  - selecting a failed entry shows its error message; a scheduled run with `output: true` shows the saved output for a privileged session and no output section for an unprivileged one; a scheduled run shows an `Open` link to its owning page (`scheduledRunLink`).
- [ ] **Step 4: Write the failing bell tests:** the tray has a link named `History` to `/history`, also when there are no notifications; following it closes the tray.
- [ ] **Step 5: Run and see them fail**

Run each: `make test-frontend VITEST_FILE=src/tasks/taskTitles.test.ts`, `make test-frontend VITEST_FILE=src/components/tasks/SavedOutput.test.tsx`, `make test-frontend VITEST_FILE=src/routes/_authenticated/history/-components/HistoryPage.test.tsx`, `make test-frontend VITEST_FILE=src/routes/_authenticated/-components/footer/NotificationsBell.test.tsx`
Expected: FAIL, missing modules; link not found.

- [ ] **Step 6: Implement.** The viewer keeps its loaded chunks in component state keyed by `taskId` (a new run resets them through `key`). The page is an `AppVirtualTable` (`fillAvailable`) with the columns above and a details area under it, in the pattern of `SchedulesTab`. The route follows `services/schedules.tsx` (`validateSearch` with `optionalString`, `loadRouteQueries`); add its endpoint to `ROUTE_CONTEXT_OBSERVER_ENDPOINTS` in `frontend/src/test/guards/suspense-loader-coverage.test.ts` if it reads the query from route context. The bell's link is an `AppRouterLinkButton` in a footer row inside the tray's dialog, after the list or the empty block, with `onClick={handleClose}`.
- [ ] **Step 7: Run the tests**, the four commands of Step 5, then `make check-frontend-quiet`.
Expected: PASS.

- [ ] **Step 8: Propose the commit message** `feat(jobs): add a history page for ended jobs and scheduled runs`.

---

### Task 6: A schedule's past runs

**Files:**
- Modify: `frontend/src/routes/_authenticated/services/-components/SchedulesTab.tsx`, `frontend/src/routes/_authenticated/services/-components/SchedulesTab.test.tsx`

**Interfaces:**
- Consumes: `tasks.history` with `unit` (Task 2), `SavedOutput` and `taskOutcome` (Task 5).

- [ ] **Step 1: Write the failing Schedules tests:**
  - the details of a selected schedule list its past runs from `tasks.history` asked with `unit: status.unit_name`: when each ended, its outcome and how long it ran;
  - no past runs shows `This task has not run yet.`;
  - selecting a past run shows its error message when it failed and its `SavedOutput`; the live card `Output of the latest run` is still there for the current invocation.
- [ ] **Step 2: Run and see it fail**

Run: `make test-frontend VITEST_FILE=src/routes/_authenticated/services/-components/SchedulesTab.test.tsx`
Expected: FAIL, list not found.

- [ ] **Step 3: Implement.** The past-runs query polls with the page's existing 5 s interval only while the schedule is selected. No effect mirrors query data into state.
- [ ] **Step 4: Run the test** of Step 2, then `make check-frontend-quiet`.
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(schedules): list a task's past runs with their saved output`.

---

### Task 7: The update banner

**Files:**
- Create: `frontend/src/routes/_authenticated/-components/app-update/JobsUpdateBanner.tsx`, `JobsUpdateBanner.test.tsx`, `useJobHostStatus.ts`, `useJobHostStatus.test.ts`
- Modify: `frontend/src/routes/_authenticated/-components/MainLayout.tsx` (or wherever `AppUpdateBanner` is rendered)

**Interfaces:**
- Consumes: `tasks.host` (Task 4).
- Consumes: `taskTitle` (Task 5).
- Produces: `useJobHostStatus(): { updatePending: boolean; running: number; jobs: TaskSnapshot[] }`.

- [ ] **Step 1: Write the failing tests:**
  - hook: it asks `tasks.host` once when the stream is open; while `update_pending` is true it asks again every 15 s, and stops when it turns false; a mutation that fails with code 409 and a message starting `update_pending:` makes it ask at once.
  - banner: with `{update_pending: true, running: 2}` it reads `LinuxIO was updated. New jobs wait for 2 running jobs to finish.`; with 1, `…for 1 running job…`; with 0, `LinuxIO was updated. New jobs can start shortly.` and no button; with jobs it has one button, `Show running jobs`, that unfolds a list under the text naming each job (`taskTitle`), who started it and when, and then reads `Hide running jobs`; it has no other action; with `update_pending: false` it renders nothing.
- [ ] **Step 2: Run and see them fail**

Run each test file with `make test-frontend VITEST_FILE=…`.
Expected: FAIL, missing modules.

- [ ] **Step 3: Implement.** The banner is an `AppAlert` of info severity next to `AppUpdateBanner`, shown to every signed-in user (a user's own job host can be the older one). The list is local state of the banner; the bell's tray is not involved, because it cannot show a job on an older host (Task 4). The mutation path reports an `update_pending` failure to the hook from the one place mutation errors are turned into toasts (`frontend/src/api/call-react-query.ts`); the toast itself stays.
- [ ] **Step 4: Run the tests**, then `make check-frontend-quiet`.
Expected: PASS.

- [ ] **Step 5: Propose the commit message** `feat(jobs): show how many running jobs an update waits for`.

---

### Task 8: Real-machine checks, documents and gates

**Files:**
- Create: `frontend/src/test/vm/jobs-phase5a.spec.ts`
- Modify: `frontend/src/test/vm/vm.ts` if a helper is missing, `docs/TODO/jobs.md` (status table, new section 3.9, sections 2.12, 2.16 and 4.5), `docs/api-contract.md` (the three routes, their types, who may call them), `docs/process-systemd-architecture.md` (what the bridge reads while an update waits)

- [ ] **Step 1: Write the VM spec.**
  1. *A finished job shows in history with its name.* Start a scrub of the test pool, wait for it to end, open the bell, follow `History`: the first row names the scrub, says `Completed`, and names the test user.
  2. *A failed scheduled script shows with its error and its output.* A schedule whose script is `echo boom; exit 3`, run once: its history row says `Failed`; selecting it shows `exited with status 3` and an output containing `boom`.
  3. *A schedule lists its past runs.* Run a script twice: the Schedules page lists two past runs for it, and selecting the older shows its saved output.
  4. *A long output opens at its end and loads earlier parts.* A script printing 600 numbered lines of 1,000 characters: the viewer shows the last line, `Show earlier` is present, and pressing it until it disappears brings line 1 into view.
  5. *A user who is not an admin sees only their own jobs.* Sign in as a non-sudo user created over SSH for the test, copy a file large enough to last over a second in the file browser, open `/history`: one row, theirs, and no scheduled run; `tasks.output` for the script run's ID answers 403.
  6. *The update banner counts the waiting jobs.* Start a throttled scrub, then make the root job host look older to a new bridge without stopping it (the simplest honest way: install a build whose `jobs.Protocol` is one higher with `make vm-install`, as the phase 3 spec did for its update test; if that test used another way, use the same), sign in again: the banner reads `…wait for 1 running job…`, `Show running jobs` names the scrub under it, a new mutation is refused with the count in its message, and when the scrub ends the banner goes within 30 s.
- [ ] **Step 2: Run the full gate**

Run: `make test-quiet`
Expected: PASS. On a failure read `.cache/test-logs/` before any rerun.

- [ ] **Step 3: Run the VM checks**

Run, one after the other: `make vm-reset`, `make vm-install`, `make test-vm-browser PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5a.spec.ts'`.
Expected: all pass.

- [ ] **Step 4: Ask for the privilege review.** Dispatch the `privilege-boundary-reviewer` agent on the whole diff: two new routes read root-owned records and one is privileged. Fix what it finds and rerun the affected gates.
- [ ] **Step 5: Reconcile the documents.** Section 3.9 lists what was built, each target run and its result, and what was only inferred. Section 2.12 says what "reads work" means while an update waits. The status row of phase 5 names this plan as done and the notifications plan as open.
- [ ] **Step 6: Propose the commit message** `test(jobs): check history and the update banner in the test VM`.
