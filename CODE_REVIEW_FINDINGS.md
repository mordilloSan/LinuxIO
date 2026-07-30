# Code review — uncommitted working tree on `dev/v0.17.0` (2026-07-29, completed 2026-07-30)

**Scope:** the ~2870-line working diff — bridge jobs/router admission rework, routed-tab child-route migration (`RoutedTabContainer`/`RoutedTabLayout`/`RoutedTabActions`), package-updater controller + job recovery, docker/updates pages, and the new/updated tests.

**Process note:** 10 finder angles, all now complete — reuse, simplification, efficiency, conventions, altitude, wrapper-correctness, removed-behavior audit (2026-07-29), plus concurrency, security, and a frontend sweep (2026-07-30). A dedicated verification pass re-verified the contested findings against the current tree, re-anchored line numbers after the fixes, and validated the applied resolutions. Findings are deduped across finders (several were found independently 2–3 times, noted inline). The conventions/memory-rules check and the security angle both came back clean.

---

> **Resolution update (2026-07-29):** A5, B5, and B6 are closed. The fixes were validated with `make check-frontend` (TypeScript, lint/format, and 496 frontend tests).

> **Completion update (2026-07-30):** the remaining three angles and the verification pass ran. Results: the `RoutedTabContainer` rework turned out to close **six** findings, not one — A4, B3, B4, D3, D4 are now verified resolved alongside B5 (details inline). B1 (cancel toasted as failure) was **confirmed end-to-end**. Two new findings were added: **B7** (canceled handler keeps running while its slot is promoted — empirically reproduced, pre-existing mechanism) and **B8** (delete dialog can vanish mid-delete — follow-on to the B6 fix). Security review: clean, and the diff actually *hardens* several paths (see section G).

## TL;DR — open items worth addressing before commit

1. **`router.go:363`** — canceling a running job on a `Timeout>0` policy promotes the next queued job while the canceled handler goroutine is still executing → real concurrent handler executions exceed `MaxActivePerRoute` (**empirically reproduced**: 3 running at cap 2). Mechanism pre-dates this diff, but the reworked cancel→promotion path formalizes it as the fast path. *(B7)*
2. **`usePackageUpdater.ts:91`** — every successful update holds the UI (buttons disabled) an extra fixed **1.5 s** (`ensureMinimumVisible(Date.now())` instead of the captured start time). *(A1, 2 finders)*
3. **`usePackageUpdater.ts:115`** — the updates-list refresh depends **solely** on the global job-events stream (`invalidates: [], markHandled: false`); if that stream is down/reconnecting, the list stays stale until the 50 s poll. *(A2, 2 finders)*
4. **`usePackageUpdater.ts:81/201`** — every navigation into `/updates` runs an unguarded `jobs.list` recovery scan with `recoveryPending` initialized `true`: a bogus "Preparing… 0%" panel and disabled controls flash on every visit. *(A3, 3 finders)*
5. **`useRecoveredJobs.ts:452`** — page-initiated cancel of an update surfaces as an error toast ("operation aborted" → package-update failure); **confirmed end-to-end** — only navbar-initiated cancels are suppressed. *(B1)*
6. The `usePackageUpdater` tests mock a job-stream lifecycle that diverges from the real one (`onSettled` never fired, `onJobStart` never fired on attach) — the hook's settle-cleanup and recovered-job cancel are untested. *(C1/C2)*

**Resolved and verified (2026-07-30):** A4, A5, B3, B4, B5, B6, B8, D3, D4, D9 — plus the A4 residual (`TabSelector.tsx` anchorEl) and the B3 residual (boolean children).

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

### A4. [Resolved] Mobile tab menu reopens mis-anchored (stale `anchorEl`) — `frontend/src/components/tabbar/RoutedTabContainer.tsx`
`TabLayout` never cleared `anchorEl` when the mobile slot-actions branch unmounted, so on remount `open={Boolean(anchorEl)}` was already true: the menu popped open unprompted, anchored to a detached 0×0 element. *(wrapper-correctness finder)*
**Resolution (verified):** `handleMenuTriggerRef` (:139-146), attached to the tune `AppIconButton` (:188, forwardRef to the real button), fires null and clears `anchorEl` whenever the mobile branch unmounts (`hasSlotActions` → false or `isMobile` flip); regression-tested in `RoutedTabContainer.test.tsx:361-371`. **Residual (now also fixed):** the sibling `TabSelector.tsx` carried the same pattern in its `rightContent` mobile-menu branch — no consumer passes `rightContent`, so the prop, the anchorEl state, and the whole branch were deleted as dead code.

### A5. [Resolved] Docker "Stop All" clickable mid-refetch — `frontend/src/routes/_authenticated/docker/-components/DockerDashboardPage.tsx`
`containersFetching` was removed from the Start All/Stop All disabled conditions. Stop All is client-driven (captures `runningContainers` at click, stops each by Id), so it could run against a list being replaced by the 5 s poll: a container that just exited yields a thrown `stop_container` and a misleading "Failed to stop 1 of N containers" toast. *(removed-behavior finder)*
**Resolution (verified):** restored `containersFetching` to both bulk-action disabled conditions (Start All :100, Stop All :118). `DockerDashboardPage.test.tsx:82-98` meaningfully asserts both disabled while `isFetching` and Stop All working once fetching clears. **Trade-off noted:** with `refetchInterval: 5000`, `isFetching` goes true on every poll, so the buttons briefly flicker-disabled every 5 s for the duration of each request — this is the pre-diff behavior, restored deliberately.

### A6. No cancel affordance during the resume window — `frontend/src/routes/_authenticated/updates/-components/UpdateStatus.tsx:38`
`onCancel` is forced `undefined` while `recoveryPending`. During the recovered-job window (scan found a running update, attach stream not yet open) the panel shows "Resuming update transaction" with no page-level cancel, even though `jobIdRef` is already populated by `onJobStart` and `cancelJob` would work. If attach-open stalls, the only cancel is the navbar background-job chip. *(removed-behavior finder)*

## B. Correctness / UX issues in new code

### B1. User cancellation toasted as a failure — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:452` — **CONFIRMED end-to-end**
The global handler attaches to running `packages.update` jobs (events stream → `attachRecoveredJob` :524, generic branch :385; `markHandled: false` means nothing registers a `pendingLocalJobKey` for updates, so the :175 guard never blocks). Cancel from the Updates page (`cancelUpdate`, usePackageUpdater.ts:250, wired UpdatesPage.tsx:129) only aborts the *page's* stream and sends `jobs.cancel`; the backend's `markCanceledLocked` emits `NewError("operation aborted", 499)` (jobs.go:672), delivered to the *global* attach stream as a result-error frame (job_primitives.go:342). `waitForStreamResult` rejects, `onError` fires with its signal **not** aborted (only the navbar cancel in useGenericBackgroundJobs.ts:30 aborts that controller) → `emitPackageUpdateFailure` → `toast.error("operation aborted")`. `cancelledRef` is local to `usePackageUpdater`; the dedupe Set only prevents repeats; the `state === "failed"` check exists only on the events fallback (:557), not the attach path. Cancels from another tab/session toast the same way.
**Fix:** on the attach-error path, check the job's terminal state (or recognize the 499/aborted error) before toasting. *(altitude finder; settled by verification pass)*

### B2. Duplicate failure feedback while the page is open — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:92`
`packageUpdateFailureToastedRef` dedupes only across the two **global** paths (attach onError :452, terminal event :556). Nothing suppresses the global toast while the Updates page owns a live stream (`pendingLocalJobKeysRef` never populated for `packages.update`; `markHandled: false` is deliberate), so a failure with the page mounted produces both the inline `finishError` alert and the global toast for the same event. If the toast is meant for detached/unmounted pages only (per the global-transfer pattern), the global paths need a mounted-page suppression signal. *(wrapper-correctness finder)*

### B3. [Resolved] `RoutedTabActions` null-children contract was unmarked and load-bearing
`RoutedTabActions` registered in its effect without consulting children, so null children still set `hasSlotActions` and rendered a ghost mobile tune-button opening an empty menu; only some call sites guarded with a ternary. *(simplification finder)*
**Resolution (verified):** the register effect (now `RoutedTabContainer.tsx:57-60`) short-circuits both registration and render behind a `hasActions = children != null` guard (:55), with a dedicated test (`RoutedTabContainer.test.tsx:194`). **Residual (now also fixed):** the guard excludes boolean children too (`children != null && typeof children !== "boolean"`), covering the `cond && <Action />` pattern, with its own test.

### B4. [Resolved] `keepMounted` on the legacy `rightContent` mobile branch
The legacy branch kept a hidden portal permanently mounted for VMPage with no state-preservation benefit, and silently changed remount-per-open semantics. *(efficiency + altitude finders)*
**Resolution (verified):** the legacy `rightContent` branch was **deleted outright** (prop removed; VMPage.tsx:169 now uses `RoutedTabActions`). The one remaining `keepMounted` (:197) is on the slot branch and is now load-bearing — it keeps the `.tab-selector__mobile-actions` host attached so the persistent action portal survives while the menu is closed.

### B5. [Resolved] Actions appear two commits late; breakpoint flip resets action state — `frontend/src/components/tabbar/RoutedTabContainer.tsx:78`
`RoutedTabActions` rendered null until the register-effect → `hasSlotActions` → host-div → ref handshake completed (visible pop-in), and `createPortal` children remounted when the container element changed on breakpoint flips. *(wrapper-correctness finder)*
**Resolution (verified):** registration moved to a `useLayoutEffect` through a counted `TabActionSlotContext.registerActions` (:57-60, :85-89) so `hasSlotActions` updates synchronously pre-paint, and a single persistent `actionHost` element (:80-84) is re-appended by `mountActionHost` (:90-97) into whichever breakpoint branch is mounted — the DOM node moves instead of the subtree remounting (state preservation pinned by the breakpoint test). Verified it does not reintroduce A4 and that the design requires the remaining `keepMounted`.

### B6. [Resolved] `deleteTarget` snapshots the whole VM object — `frontend/src/routes/_authenticated/vm/-components/VMMachinesPage.tsx`
Only `.name` was load-bearing; the snapshot could show stale state/details and offer to delete a ghost VM. *(altitude finder)*
**Resolution (verified):** `VMMachinesPage` stores only `deleteTargetName` and derives the dialog's VM live from `vms` (:73); the dialog renders only while that row exists (:212-222) and the mutation uses the row captured at `onDelete` (:188, :217). The old stale path (`deleteOpen` + URL-driven `selectedVM`) is fully removed. **But see B8 — the live derivation opens a new mid-delete window.**

### B7. Canceled handler keeps running while its slot is promoted — `backend/common/ipc/bridge/router.go:363` *(added 2026-07-30, empirically reproduced)*
Canceling a **running** job on a `Timeout > 0` policy frees its `MaxActivePerRoute` slot and promotes the next queued job while the canceled handler is still executing: `routeRunner`'s `case <-runCtx.Done()` returns without joining the detached `runRoute` goroutine (:355), so `run()` marks the job terminal, `Done` closes, and `finishJob` unmarks the slot and dequeues the next job before the abandoned handler exits. Reproduced under cancel stress: **3 concurrent handler executions at cap 2**; with `Timeout=0`'s synchronous path the cap was never exceeded in 30 stress runs, isolating the mechanism.
**Failure:** route with ActionDefault policy (Timeout=120m, MaxActivePerRoute=4): jobs A–D running, E queued; cancel A while its handler is mid-write and slow to observe ctx → `jobs.cancel` returns a "canceled" snapshot, E is promoted, and 5 handlers execute concurrently against a cap of 4. Repeated cancel+resubmit stacks an unbounded number of abandoned still-running handlers behind a bounded slot count.
**Note:** the mechanism **pre-dates this diff** (`routeRunner`'s body is untouched; only the Start call-site changed), but the diff's cancel/promotion hardening formalizes this handoff as the fast path. Fix direction: have the runner join (or reference-count) the detached handler before the job publishes terminal, or gate promotion on handler exit rather than job-terminal. *(concurrency finder)*

### B8. Delete dialog vanishes mid-delete — `frontend/src/routes/_authenticated/vm/-components/VMMachinesPage.tsx:73` *(added 2026-07-30; follow-on to the B6 fix)*
The dialog's existence is derived from the live polled `vms` list, but the backend delete **undefines the domain before managed-disk removal completes** (backend/bridge/handlers/virt/lifecycle.go `deleteVMWithConn`: undefine, then `deleteManagedDisks`), and VMPage polls `virt.list` with `refetchInterval: 5000` — so the dialog can unmount while `deleteMutation.isPending`. The success handler's `setDeleteTargetName(null)` (:92) shows the dialog is expected to stay open until the mutation settles, and the new "keeps the delete dialog synced" test only covers the pre-confirm window.
**Failure:** confirm delete of a VM whose disk removal outlasts the next 5 s poll: after libvirt undefine, the poll returns a list without the VM → `deleteTarget` becomes null → the dialog (spinner showing, Cancel disabled) vanishes mid-operation; if disk deletion then fails, the error arrives as a detached toast after the dialog already disappeared as-if-successful.
**Fix:** keep the dialog mounted while the mutation is pending — e.g. render it when `deleteTarget != null || deleteMutation.isPending`, falling back to the captured name for display. *(frontend-sweep finder)*
**Resolution:** a `pendingDeleteVM` snapshot is captured at confirm time and the dialog target falls back to it while `deleteMutation.isPending`; explicit close and both settle paths clear it, so the pre-confirm live-sync behavior (the point of the B6 fix) is unchanged. New test: "keeps the delete dialog open while the delete job outlives the list row" in `VMPage.test.tsx`.

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
The CreateForOwner error path uses two sequential opposite ifs (`if canStart` … `if !canStart`) with two separate `mu.Lock` windows and a hoisted `next`; the `canStart` branch (unmarkActiveLocked + dequeueStartLocked + startTrackedJob) is byte-for-byte `finishJob` (:511). `if canStart { r.finishJob(...) } else { lock; pendingQueued--; unlock }` is one lock window, no clone, and likely drops the `//nolint:gocognit`. Future promotion-logic changes in `finishJob` would otherwise miss this unlabeled clone. *(simplification finder; over-admission here separately disproven — twice, see F)*

### D2. Fourth copy of the terminal-publish tail — `backend/common/ipc/bridge/jobs.go:383`
`Cancel`'s queued branch inlines signalDone/broadcast/closeSubscribers — alongside markCompleted (:575-577), markFailed (:593-595), markCanceled (:606-608). The lock split is required for the Start/Cancel race; the tail isn't. Extract `publishTerminal(event Event)` called from all four sites, or cancel-while-queued diverges silently when publication grows a step. *(simplification finder)*

### D3. [Resolved] Mobile menu JSX duplicated across slot and legacy branches
The ~28-line mobile menu block was copy-pasted between the slot and `rightContent` branches. *(reuse + simplification + altitude finders)*
**Resolution (verified):** only a single mobile-menu JSX block remains (`RoutedTabContainer.tsx:183-212`) — the duplicated branch was removed along with the `rightContent` prop. The near-identical block in `TabSelector.tsx` was unused (no consumer passed `rightContent`) and has been deleted too — the mobile actions menu now exists in exactly one place.

### D4. [Resolved] Internal slot props leaked into public wrapper types; wrapper was a pass-through
`actionHostRef`/`hasSlotActions` lived on the shared public props type, so wrappers type-accepted and silently dropped/overrode them. *(reuse + simplification + wrapper-correctness finders)*
**Resolution (verified):** public `RoutedTabContainerProps` (:33-37) now carries only `children`/`containerStyle`/`tabs`; the slot internals moved to the unexported `TabLayoutProps` (:39-42), and the default export is re-typed as `ComponentType<RoutedTabContainerProps>` (:230), hiding the internals from consumers.

### D5. Per-type feedback blocks accreting in the global jobs handler — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:556`
Package-update failure feedback is the third per-type special case (after capability-install and the smart-test skip): its own dedupe Set ref (:90), emit callback (:92-105), and insertions in both the attach-error branch (:452) and terminal-event branch (:556-561) — and the once-guard preamble is a mechanical clone of the `installToastedRef` guard. The file already shows the right altitude (manifest-driven `JOB_QUERY_INVALIDATIONS`, colocated `genericLabel` switch): terminal feedback should be one registry (type → onFailed/onCompleted, dedupe owned by the plumbing) with a single insertion point. The three existing blocks already disagree on their terminal-state matrices (see B1). *(altitude + reuse finders)*

### D6. Hand-rolled error extraction instead of `getMutationErrorMessage` — `frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:96`
`emitPackageUpdateFailure` re-implements `error instanceof Error ? … : typeof error === 'string' ? …` with fallback `'Package update failed'`, while `usePackageUpdater.finishError` formats the identical terminal error with fallback `'Update failed'` — three different strings for the same failure depending on surface, and `LinuxIOError`'s empty-message case (handled by the canonical helper in `frontend/src/utils/mutations.ts`, used by ~15 call sites) is missed. *(reuse finder)*

### D7. Duplicated stream-options literal in the job-stream mock — `frontend/src/hooks/usePackageUpdater.test.ts:59`
The mock builds the same `{ open, onOpen, onProgress }` literal in `run` (59-66) and `attach` (80-87); and the whole mock re-implements the lifecycle mirror VMPage.test.tsx already built (its copy *does* fire `onSettled`/`onMutate` — the two mirrors have diverged from each other and from the real contract). Hoist one shared builder into `frontend/src/test/`. *(simplification + reuse finders; pairs with C1/C2)*

### D8. Duplicated settle-poll loop in router tests — `backend/common/ipc/bridge/router_test.go:143`
Both new tests end with a verbatim ~15-line deadline/poll loop over activeByRoute/queuedByRoute/pendingQueuedByRoute (143-158, 254-268, identical `t.Fatalf` text). jobs_test.go already models this (`waitForState`/`waitForJobEvent` t.Helper()s at :496/:514) — add `waitForRouterSettle(t, router, route)`. *(reuse finder)*

### D9. [Resolved] Five identical tab-layout route wrappers — `frontend/src/routes/_authenticated/accounts/route.tsx`
Accounts/Docker/Services/Shares/Storage each added `function XLayout() { return <RoutedTabLayout tabs={X_TABS}><Outlet/></RoutedTabLayout> }`. *(simplification finder)*
**Resolution:** a `makeTabLayout(tabs, containerStyle?)` factory now lives next to `RoutedTabLayout` (`RoutedTabContainer.tsx:116`) and all five route files use it (`component: makeTabLayout(X_TABS)`; services passes its `paddingInline`); updates keeps its provider wrapper. Covered by the "creates a route layout with the supplied container style" test.

## E. Minor / pre-existing

- **`useRecoveredJobs.ts:90`** — `packageUpdateFailureToastedRef` (like the pre-existing `installToastedRef`) grows one id per failure for the session, never pruned. Bytes-level; fix alongside D5. *(efficiency finder)*
- **`router.go:393` — pre-existing, not introduced by this diff** — `startsByOwnerRoute` appends a timestamp per admitted job but prunes only inside `checkRateLocked`, which early-returns when `StartRatePerMinuteOwner <= 0` (ActionDefault, transfers) → unbounded per-owner growth for the bridge lifetime, worst on high-frequency filebrowser routes. Skip the append (or prune) when the owner rate limit is disabled. *(efficiency finder)*

## F. Verified non-issues (checked and cleared)

- **Go router over-admission disproven — twice:** every promoted job is `markActiveLocked` inside `dequeueStartLocked` (router.go:540); refusal path decrements exactly once; `finishJob` keys match reservation keys. The concurrency angle then stress-tested the full admission/queue/promotion/cancel accounting: counters settle to zero, no stranded non-terminal jobs, **race detector clean across ~140 runs**, lock nesting one-directional (`router.mu` → `job.mu.RLock` only), terminal transitions mutually exclusive under `j.mu`, refusal path releases exactly one reservation. (B7 is the one exception that survived, and its mechanism is pre-existing.)
- **`AppPopover`/`AppMenu` `keepMounted` mechanism:** all document/window listeners, autofocus, and repositioning are gated on `open`; nothing leaks or steals events while closed-but-mounted. The remaining `keepMounted` (slot branch) is load-bearing by design.
- **StrictMode register/unregister/register** batches 1→0→1 with a `Math.max(0, …)` floor; action swaps between routes never unmount the host mid-navigation (now via layout effects).
- **`PackageUpdateController` plumbing:** all 10 hook values flow through context; no other `usePackageUpdater` callers; real attach sets `jobIdRef` via `onJobStart`, so cancel works for recovered jobs (in prod — see C2 for the test gap).
- **Conventions/memory rules:** no dead code left by the refactor, global-transfer pattern followed, no new `common/` placement, no capability-mirror entries touched, new tests restore real timers.
- **Efficiency non-issues:** provider `children` identity keeps `RoutedTabLayout` re-renders bailed out on progress ticks; `setActionHost` churn batches; test sleep-polls bounded.

## G. Security review (2026-07-30) — clean

No security findings. Positively verified:

- **Auth/routing:** `_authenticated.tsx` still applies `requireAuthentication` via `beforeLoad` to every migrated child route (services/sockets/timers, storage/lvm, shares, docker/*, updates/history, accounts); docker's `requireAccess` beforeLoad preserved. The `-auth.ts` change is a **hardening fix**: it rejects sanitized redirects whose normalization yields a `//`-prefixed path (e.g. `/..//evil.com` → pathname `//evil.com`), closing a protocol-relative open redirect through `requireGuest`'s `redirect({ href })` sink.
- **Bridge job ownership:** `jobs.get/list/cancel/attach/data` all resolve through `GetForOwner`/`ListForOwner` (`Owner.Matches` by username/UID); `jobs.events` filters per-event by owner — untouched by the diff. The admission changes are race **fixes**: reservation now happens under the router lock before `CreateForOwner`, closing a TOCTOU that previously allowed exceeding `MaxActivePerRoute`/`DuplicateActiveReject`; `Job.Start` now refuses non-queued jobs so a canceled-queued job can no longer run after promotion.
- **Injection/rendering:** recovered-job error strings render via `toast.error(string)` (text-only); no `dangerouslySetInnerHTML`/`innerHTML` sinks anywhere in the diff; `attach(job, vars)` adopts the existing snapshot and never starts a new privileged job.
- **Info exposure:** no new cross-owner payload paths; subscriber-drop logging emits only job id/type at debug level.
