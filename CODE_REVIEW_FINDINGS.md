# Code review — uncommitted working tree on `dev/v0.17.0` (2026-07-29)

**Scope:** the ~2870-line working diff — bridge jobs/router admission rework, routed-tab child-route migration (`RoutedTabContainer`/`RoutedTabLayout`/`RoutedTabActions`), package-updater controller + job recovery, docker/updates pages, and the new/updated tests.

**Process note:** 10 finder angles were launched; **7 completed** (reuse, simplification, efficiency, conventions, altitude, wrapper-correctness, removed-behavior audit) and **3 were still running when the review was stopped early** — whatever those angles would have covered is not represented here. Each finding below was verified by its finder against the working tree; the planned cross-finder adversarial verification pass did **not** run. Findings are deduped across finders (several were found independently 2–3 times, noted inline). The conventions/memory-rules check came back clean.

---

> **Resolution update (2026-07-29):** A5, B5, and B6 are closed. The fixes were validated with `make check-frontend` (TypeScript, lint/format, and 496 frontend tests).

## TL;DR — worth addressing before commit

1. **`usePackageUpdater.ts:91`** — every successful update now holds the UI (buttons disabled) an extra fixed **1.5 s** (`ensureMinimumVisible(Date.now())` instead of the captured start time). *(found independently by 2 finders)*
2. **`usePackageUpdater.ts:115`** — the updates-list refresh after an update now depends **solely** on the global job-events stream (`invalidates: [], markHandled: false`); if that stream is down/reconnecting, the list stays stale until the 50 s poll. *(2 finders)*
3. **`usePackageUpdater.ts:81/201`** — every navigation into `/updates` runs an unguarded `jobs.list` recovery scan with `recoveryPending` initialized `true`: a bogus "Preparing… 0%" panel and disabled controls flash on every visit. *(3 finders)*
4. **`RoutedTabContainer.tsx:176`** — `anchorEl` is never cleared when the mobile slot-actions branch unmounts → the menu can auto-reopen mis-anchored to a detached element.
5. The `usePackageUpdater` tests mock a job-stream lifecycle that diverges from the real one (`onSettled` never fired, `onJobStart` never fired on attach) — the hook's settle-cleanup and recovered-job cancel are untested.
6. The tab layout keeps **two parallel copies** of the mobile actions menu (slot vs legacy `rightContent`) and leaks internal wiring props into the public wrapper types.

---

## A. Behavior regressions vs pre-diff code

### A1. Fixed 1.5 s post-success hold — `frontend/src/hooks/usePackageUpdater.ts:91`
`finishSuccess` calls `ensureMinimumVisible(Date.now())`. The old code passed `startedAtMs` captured at `runUpdate` start, so the 1.5 s minimum only padded sub-1.5 s updates; now `elapsed` is always 0, so **every** success (direct or recovered/attached) holds `updatingPackage` non-null 1500 ms longer, keeping `packageOperationPending` true and Update All / Refresh Sources / per-row buttons disabled.
**Fix:** record the start timestamp in a ref at `runUpdate`/`attach` and pass it, restoring top-up-only semantics. *(efficiency + removed-behavior finders)*

### A2. List invalidation single-pointed on the global events stream — `frontend/src/hooks/usePackageUpdater.ts:115`
The hook opts out of local invalidation and ownership (`invalidates: [], markHandled: false`), replacing the removed direct `onComplete → refetch` with the global `JOB_QUERY_INVALIDATIONS["packages.update"]` path in `useRecoveredJobs`. That stream is torn down whenever `streamMuxStatus != 'open'`, a failed `openJobEventsStream()` is only logged with no retry until mux status changes, and backend `eventSubscriber.send` drops events for slow subscribers.
**Failure:** update finishes while the events stream is reconnecting → page shows "Finished" but the list keeps showing the just-updated packages until the 50 s `refetchInterval`; "Update All" would resubmit already-updated IDs.
**Fix:** keep the manifest default (`markHandled: true` + manifest entry) so the attached page invalidates locally with the global path as fallback — worst case is one duplicate invalidation. *(altitude + removed-behavior finders)*

### A3. Recovery scan on every `/updates` mount + dead-controls flash — `frontend/src/hooks/usePackageUpdater.ts:81` and `:201`
`PackageUpdateControllerProvider` (updates/route.tsx:22) mounts `usePackageUpdater` on every entry into the Updates section. `useActiveJobRecovery`'s constant `scanKey` only dedupes within a mount, so each visit fires an uncached `linuxio.jobs.list({status:"active"})` IPC round trip; meanwhile `recoveryPending` initializes `true` and `UpdateStatus` computes `isUpdating = recoveryPending || !!updatingPackage`, so the page renders an in-progress panel ("Preparing… 0%") with Refresh Sources / Update All / row buttons disabled until the scan resolves — on every visit, even with nothing running.
**Fix:** gate `scanKey` the way ComposeOperationDialog does (`open && muxIsOpen ? key : null`) or serve the scan from cached `jobs.list` queryOptions; don't initialize the UI as pending without a hit. *(efficiency + altitude + removed-behavior finders)*

### A4. Mobile tab menu reopens mis-anchored (stale `anchorEl`) — `frontend/src/components/tabbar/RoutedTabContainer.tsx:176`
`TabLayout` never clears `anchorEl` when the mobile slot-actions branch unmounts (all `RoutedTabActions` unregister → `hasSlotActions` false, or `isMobile` flips while open). The layout now persists across child routes, so when a child route registers actions again the branch remounts with `open={Boolean(anchorEl)}` already true: the menu pops open unprompted, anchored to a detached element whose rect is 0×0 (renders clamped to the viewport corner).
**Fix:** clear `anchorEl` when the branch unmounts / `hasSlotActions` drops / `isMobile` changes. *(wrapper-correctness finder)*

### A5. [Resolved] Docker "Stop All" clickable mid-refetch — `frontend/src/routes/_authenticated/docker/-components/DockerDashboardPage.tsx:114`
`containersFetching` was removed from the Start All/Stop All disabled conditions. Stop All is client-driven (captures `runningContainers` at click, stops each by Id), so it can run against a list being replaced by the 5 s poll: a container that just exited yields a thrown `stop_container` and a misleading "Failed to stop 1 of N containers" toast. Narrow race; possibly an intentional UX trade (buttons no longer flicker disabled every poll) — flagging for a deliberate decision. *(removed-behavior finder)*
**Resolution:** restored `containersFetching` to both bulk-action disabled conditions in `DockerDashboardPage.tsx`. `DockerDashboardPage.test.tsx` now verifies that Start All and Stop All remain blocked during a refetch and that Stop All becomes available afterward.

### A6. No cancel affordance during the resume window — `frontend/src/routes/_authenticated/updates/-components/UpdateStatus.tsx:38`
`onCancel` is forced `undefined` while `recoveryPending`. During the recovered-job window (scan found a running update, attach stream not yet open) the panel shows "Resuming update transaction" with no page-level cancel, even though `jobIdRef` is already populated by `onJobStart` and `cancelJob` would work. If attach-open stalls, the only cancel is the navbar background-job chip. *(removed-behavior finder)*

## B. Correctness / UX issues in new code

### B1. User cancellation toasted as a failure — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:451`
The global attach path's `onError` calls `emitPackageUpdateFailure` without seeing `job.state`, so a user-initiated cancel surfaces as an error toast ("operation aborted" → "Package update failed"). The terminal-event branch fires only on `state === 'failed'`, and the neighboring capability-install block covers `'canceled'` too — three per-type blocks with three different terminal-state matrices. **Fix:** consult job state before toasting on the attach-error path. *(altitude finder)*

### B2. Duplicate failure feedback while the page is open — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:92`
`packageUpdateFailureToastedRef` dedupes only across the two **global** paths (attach onError :451, terminal event :556). Nothing suppresses the global toast while the Updates page owns a live stream (`pendingLocalJobKeysRef` never populated for `packages.update`; `markHandled: false` is deliberate), so a failure with the page mounted produces both the inline `finishError` alert and the global toast for the same event. If the toast is meant for detached/unmounted pages only (per the global-transfer pattern), the global paths need a mounted-page suppression signal. *(wrapper-correctness finder)*

### B3. `RoutedTabActions` null-children contract is unmarked and load-bearing — e.g. `frontend/src/routes/_authenticated/accounts/-components/AccountsUsersPage.tsx:56`
`RoutedTabActions` registers in its effect without consulting children, so `<RoutedTabActions>{null}</RoutedTabActions>` still sets `hasSlotActions` and renders a ghost mobile tune-button opening an empty menu. UsersPage and storage/lvm guard with a ternary; AccountsGroups/Docker*/Shares* render unconditionally — copying the unguarded pattern into a page with nullable actions ships the ghost button.
**Fix:** make `RoutedTabActions` skip registering (return null) when `children == null`, then delete the call-site ternaries. *(simplification finder)*

### B4. `keepMounted` on the legacy `rightContent` mobile branch — `frontend/src/components/tabbar/RoutedTabContainer.tsx:207`
The slot branch needs a persistent portal target (pinned by the new test), but the legacy branch renders parent-supplied JSX with nothing to preserve. Sole user is VMPage: its closed mobile menu previously rendered null; now a `display:none` portal lives in `document.body` for the page's lifetime, its `tabActions` reconciled on every VMPage re-render (VM polling, action-pending churn) — and any `rightContent` child relying on remount-per-open (state reset, on-mount fetch) breaks quietly. This semantics change wasn't opted into by VMPage code.
**Fix:** drop `keepMounted` from the legacy branch, or finish migrating VMPage to `RoutedTabActions` and delete the branch. *(efficiency + altitude finders; AppPopover's listeners/focus are properly `open`-gated — the mechanism itself is fine)*

### B5. [Resolved] Actions appear two commits late; breakpoint flip resets action state — `frontend/src/components/tabbar/RoutedTabContainer.tsx:78`
`RoutedTabActions` renders null until the register-effect → `hasSlotActions` → host-div → ref handshake completes (visible pop-in on first mount of e.g. `/accounts`), and because `createPortal` children remount when the container element changes, action-local state resets on desktop/mobile breakpoint flips. Minor. *(wrapper-correctness finder)*
**Resolution:** `RoutedTabLayout` now creates one portal host for its lifetime and reparents that same element between the desktop action bar and the kept-mounted mobile menu. Registration uses a layout effect, removing the passive-effect/ref handshake from the visible render path, while the stable portal target preserves action-local state across breakpoint changes. `RoutedTabContainer.test.tsx` covers the state-preservation regression.

### B6. [Resolved] `deleteTarget` snapshots the whole VM object — `frontend/src/routes/_authenticated/vm/-components/VMMachinesPage.tsx:41`
Only `.name` is load-bearing (mutation at :216). The `vms` list refetches while the snapshot doesn't, so the confirm dialog can show stale state/details (ghost VM deleted elsewhere → "Domain not found", the exact path VMPage.test pins). Holding the name and deriving the dialog row from the live list (closing when it disappears) keeps the dialog honest at the same complexity. Display-only today. *(altitude finder)*
**Resolution:** `VMMachinesPage` now stores only `deleteTargetName` and derives the dialog's VM from the current `vms` list on every render. Refetched details are shown immediately, and the dialog disappears if the VM is no longer present. `VMPage.test.tsx` covers both behaviors.

## C. Test-coverage gaps

### C1. Mock never fires `options.onSettled` — `frontend/src/hooks/usePackageUpdater.test.ts:48`
The real `useActionMutation` spreads options into `useMutation` (react-query.ts:424-426), so React Query fires `onSettled` after every run; the hand-rolled mock never does. `usePackageUpdater`'s entire settle-cleanup (`streamRef`/`jobIdRef`/`cancelledRef` reset, usePackageUpdater.ts:179-185) is dead code in every test — a regression there passes the suite, and in prod a stuck `cancelledRef` would make the next recovered job's `finishSuccess`/`finishError` early-return (page stuck on "Resuming update transaction"). *(wrapper-correctness + reuse finders)*

### C2. Mock `attach` diverges from the real lifecycle — `frontend/src/hooks/usePackageUpdater.test.ts:79`
Real attach routes through the same mutationFn and fires `onJobStart` (react-query.ts:467 — what sets `jobIdRef` for recovered jobs) and short-circuits terminal snapshots (:469-480: completed resolves without a stream, failed throws). The mock does neither and resolves `undefined`. A regression breaking `onJobStart` on attach is invisible: cancel on a recovered update would abort only the local stream, never send `jobs.cancel`, leaving the backend transaction running while the UI says "Update cancelled". *(wrapper-correctness finder)*

### C3. Duplicated hand-cast runtime harness hides type drift — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.test.tsx:65`
Two byte-identical ~30-line runtime/controls/wrapper setups (65-95 and 125-155) ending in `as unknown as BackgroundJobRuntime`. The cast already hides real drift: the literal types `pendingLocalJobKeysRef` as `Map<string, number>` while the real runtime uses a `CountedSet` (`makeCountedSet`). New required fields stay compiler-silent in both copies.
**Fix:** an in-file `makeHarness()` (or compose the real side-effect-free `useBackgroundJobRuntime()` like its own test does). *(reuse + simplification finders)*

## D. Simplification / reuse / API shape

### D1. `startOrQueueJob` error path inlines a `finishJob` clone — `backend/common/ipc/bridge/router.go:407`
The CreateForOwner error path uses two sequential opposite ifs (`if canStart` … `if !canStart`) with two separate `mu.Lock` windows and a hoisted `next`; the `canStart` branch (unmarkActiveLocked + dequeueStartLocked + startTrackedJob) is byte-for-byte `finishJob` (:511). `if canStart { r.finishJob(...) } else { lock; pendingQueued--; unlock }` is one lock window, no clone, and likely drops the `//nolint:gocognit`. Future promotion-logic changes in `finishJob` would otherwise miss this unlabeled clone. *(simplification finder; note the wrapper-correctness finder separately **disproved** any current over-admission here)*

### D2. Fourth copy of the terminal-publish tail — `backend/common/ipc/bridge/jobs.go:383`
`Cancel`'s queued branch inlines signalDone/broadcast/closeSubscribers — alongside markCompleted (:575-577), markFailed (:593-595), markCanceled (:606-608). The lock split is required for the Start/Cancel race; the tail isn't. Extract `publishTerminal(event Event)` called from all four sites, or cancel-while-queued diverges silently when publication grows a step. *(simplification finder)*

### D3. Mobile menu JSX duplicated across slot and legacy branches — `frontend/src/components/tabbar/RoutedTabContainer.tsx:166`
The ~28-line mobile `AppIconButton`+`AppMenu` / desktop-div block is copy-pasted for the slot branch (166-193) vs `rightContent` branch (194-223); copies differ only in `ref={actionHostRef}` vs `{rightContent}`. This diff already paid: `keepMounted` had to be patched into both by hand. One branch with a computed inner div keeps slot pages and VMPage rendering the same menu. *(reuse + simplification + altitude finders)*

### D4. Internal slot props leak into public wrapper types; wrapper is a pass-through — `frontend/src/components/tabbar/RoutedTabContainer.tsx:32` and `:52`
`actionHostRef`/`hasSlotActions` live on the shared `RoutedTabContainerProps`, so legacy `RoutedTabContainer` type-accepts and silently drops them, and `RoutedTabLayout` (Omit only removes `rightContent`) accepts and overrides them — callers compile cleanly and their ref never fires. `RoutedTabContainer` itself is now a zero-logic pass-through.
**Fix:** keep `RoutedTabContainerProps` public-only, declare a private `TabLayoutProps extends` it with the two slot props, and alias `RoutedTabContainer = TabLayout`. *(reuse + simplification + wrapper-correctness finders)*

### D5. Per-type feedback blocks accreting in the global jobs handler — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:556`
Package-update failure feedback is the third per-type special case (after capability-install and the smart-test skip): its own dedupe Set ref (:90), emit callback (:92-105), and insertions in both the attach-error branch (:451) and terminal-event branch (:556-561) — and the once-guard preamble is a mechanical clone of the `installToastedRef` guard. The file already shows the right altitude (manifest-driven `JOB_QUERY_INVALIDATIONS`, colocated `genericLabel` switch): terminal feedback should be one registry (type → onFailed/onCompleted, dedupe owned by the plumbing) with a single insertion point. The three existing blocks already disagree on their terminal-state matrices (see B1). *(altitude + reuse finders)*

### D6. Hand-rolled error extraction instead of `getMutationErrorMessage` — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:96`
`emitPackageUpdateFailure` re-implements `error instanceof Error ? … : typeof error === 'string' ? …` with fallback `'Package update failed'`, while `usePackageUpdater.finishError` formats the identical terminal error with fallback `'Update failed'` — three different strings for the same failure depending on surface, and `LinuxIOError`'s empty-message case (handled by the canonical helper in `frontend/src/utils/mutations.ts`, used by ~15 call sites) is missed. *(reuse finder)*

### D7. Duplicated stream-options literal in the job-stream mock — `frontend/src/hooks/usePackageUpdater.test.ts:59`
The mock builds the same `{ open, onOpen, onProgress }` literal in `run` (59-66) and `attach` (80-87); and the whole mock re-implements the lifecycle mirror VMPage.test.tsx already built (its copy *does* fire `onSettled`/`onMutate` — the two mirrors have diverged from each other and from the real contract). Hoist one shared builder into `frontend/src/test/`. *(simplification + reuse finders; pairs with C1/C2)*

### D8. Duplicated settle-poll loop in router tests — `backend/common/ipc/bridge/router_test.go:143`
Both new tests end with a verbatim ~15-line deadline/poll loop over activeByRoute/queuedByRoute/pendingQueuedByRoute (143-158, 254-268, identical `t.Fatalf` text). jobs_test.go already models this (`waitForState`/`waitForJobEvent` t.Helper()s at :496/:514) — add `waitForRouterSettle(t, router, route)`. *(reuse finder)*

### D9. Five identical tab-layout route wrappers — `frontend/src/routes/_authenticated/accounts/route.tsx:19`
Accounts/Docker/Services/Shares/Storage each add `function XLayout() { return <RoutedTabLayout tabs={X_TABS}><Outlet/></RoutedTabLayout> }` (services adds only `containerStyle`; updates legitimately differs with its provider). A `makeTabLayout(tabs, containerStyle?)` factory next to `RoutedTabLayout` collapses them; drift has already started. Style call — TanStack route files often prefer named components; take or leave. *(simplification finder)*

## E. Minor / pre-existing

- **`useRecoveredJobs.ts:90`** — `packageUpdateFailureToastedRef` (like the pre-existing `installToastedRef`) grows one id per failure for the session, never pruned. Bytes-level; fix alongside D5. *(efficiency finder)*
- **`router.go:393` — pre-existing, not introduced by this diff** — `startsByOwnerRoute` appends a timestamp per admitted job but prunes only inside `checkRateLocked`, which early-returns when `StartRatePerMinuteOwner <= 0` (ActionDefault, transfers) → unbounded per-owner growth for the bridge lifetime, worst on high-frequency filebrowser routes. Skip the append (or prune) when the owner rate limit is disabled. *(efficiency finder)*

## F. Verified non-issues (checked and cleared)

- **Go router over-admission disproven:** every promoted job is `markActiveLocked` inside `dequeueStartLocked` (router.go:540); refusal path decrements exactly once; `finishJob` keys match reservation keys. Balanced reserve/decrement on every create-fail/queue/promote path; context-cancel and EventStarted invariants re-established.
- **`AppPopover`/`AppMenu` `keepMounted` mechanism:** all document/window listeners, autofocus, and repositioning are gated on `open`; nothing leaks or steals events while closed-but-mounted. (The *placement* of keepMounted on the legacy branch is B4 — the mechanism itself is sound.)
- **StrictMode register/unregister/register** batches 1→0→1 with a `Math.max(0, …)` floor; route-to-route action swaps flush in one passive-effect pass — the host never unmounts mid-navigation.
- **`PackageUpdateController` plumbing:** all 10 hook values flow through context; no other `usePackageUpdater` callers; real attach sets `jobIdRef` via `onJobStart`, so cancel works for recovered jobs (in prod — see C2 for the test gap).
- **Conventions/memory rules:** no dead code left by the refactor (all removed-call-site symbols still have users or were deleted), global-transfer pattern followed, no new `common/` placement, no capability-mirror entries touched, new tests restore real timers.
- **Efficiency non-issues:** provider `children` identity keeps `RoutedTabLayout` re-renders bailed out on progress ticks; `setActionHost` churn batches; test sleep-polls bounded.
