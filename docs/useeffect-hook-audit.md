# `useEffect` audit — replacement-candidate evaluation

**Status:** evaluation only (no code changes applied). Generated 2026-06-14.

## Context

The frontend is on **React 19.2.7**, which makes three newer hooks available:
`useLayoutEffect` (mature), `useEffectEvent` (now stable, **already used 64×** here),
and `use()` (not used anywhere yet). This document maps **every `useEffect`** in the
frontend and evaluates whether it could be replaced or refined by one of those hooks.

Scope: **112 `useEffect` calls across ~70 files**, audited in three sweeps
(hooks+contexts, components, pages+layouts). Headline result: most effects are
legitimate external-system sync and should stay; the real wins are a small set of
`useLayoutEffect` conversions plus a few effects that shouldn't be effects at all.

## Headline conclusions

| Hook | Verdict | Why |
|---|---|---|
| **`use()`** | **0 candidates — not applicable** | No Suspense-based data fetching (async is react-query + WebSocket stream-mux), and context is already read through custom hooks. `use()` only helps unwrap a promise under Suspense or read context conditionally; neither pattern exists here. |
| **`useEffectEvent`** | **Refines, never replaces** — a handful of leftover spots | It removes a non-reactive dependency from an effect's dep array; the `useEffect` still exists. Already adopted 64×, so only stragglers remain. The useful ones are prop-callback deps; cleanup-only ones are marginal. |
| **`useLayoutEffect`** | **The only true "replace `useEffect` with X" wins** — ~5 strong, ~6 minor | Effects that scroll/measure/focus after paint cause a visible jump/flash. Converting runs them before paint. |
| **(bonus) no effect at all** | **Highest-value cleanups, though outside the 3 listed hooks** | 1 dead no-op effect to delete + 2–3 "adjust state during render" cases. |

## A — `useLayoutEffect` candidates (currently plain `useEffect`)

**Strong (post-paint scroll → visible jump/flicker):**
- [UpdateDialog.tsx:43](../frontend/src/components/update/UpdateDialog.tsx#L43) — `scrollIntoView` on new output.
- [useLogStream.ts:109](../frontend/src/hooks/useLogStream.ts#L109) — `scrollTop = scrollHeight` to pin logs to bottom.
- [useFileListKeyboardNavigation.ts:136](../frontend/src/hooks/filebrowser/useFileListKeyboardNavigation.ts#L136) — `scrollIntoView` of focused item.
- [UserAccountDetails.tsx:616](../frontend/src/pages/main/accounts/components/UserAccountDetails.tsx#L616) — `scrollIntoView` (line 626) on focused row.
- [DirectoryTree.tsx:175](../frontend/src/components/ui/DirectoryTree.tsx#L175) — `scrollIntoView({ block: "nearest" })`.

**Minor (DOM focus / property mutation before paint — low impact, debatable):**
- Auto-focus: [AppDialog.tsx:131](../frontend/src/components/ui/AppDialog.tsx#L131), [AppFullscreenDialog.tsx:85](../frontend/src/components/ui/AppFullscreenDialog.tsx#L85), [AppMenu.tsx:46](../frontend/src/components/ui/AppMenu.tsx#L46).
- Focus+select-text on rename: [FileCard.tsx:96](../frontend/src/components/cards/FileCard.tsx#L96), [FileListRow.tsx:78](../frontend/src/components/filebrowser/FileListRow.tsx#L78).
- [AppCheckbox.tsx:33](../frontend/src/components/ui/AppCheckbox.tsx#L33) — sets `input.indeterminate` (avoids a one-frame flash of wrong state).

**Already correct — no action** (verified already `useLayoutEffect`): [AppPopover.tsx:214](../frontend/src/components/ui/AppPopover.tsx#L214), [AppSelect.tsx:104](../frontend/src/components/ui/AppSelect.tsx#L104), [UserCard.tsx:58](../frontend/src/components/cards/UserCard.tsx#L58), [AppVirtualDataTable.tsx:449](../frontend/src/components/tables/AppVirtualDataTable.tsx#L449)/[453](../frontend/src/components/tables/AppVirtualDataTable.tsx#L453).

## B — `useEffectEvent` candidates (refine deps; effect remains)

**Worth doing — a prop/callback in the dep array causes needless re-runs:**
- [FileEditor.tsx:168](../frontend/src/components/filebrowser/FileEditor.tsx#L168) — `onDirtyChange` in deps; re-runs on every parent render.
- The repeated **"register create-handler with parent on mount"** pattern, ~10×:
  [UsersTab.tsx:82](../frontend/src/pages/main/accounts/UsersTab.tsx#L82), [GroupsTab.tsx:46](../frontend/src/pages/main/accounts/GroupsTab.tsx#L46), [ImageList.tsx:177](../frontend/src/pages/main/docker/ImageList.tsx#L177), [ComposeStacksPage.tsx:229](../frontend/src/pages/main/docker/ComposeStacksPage.tsx#L229), [NetworkList.tsx:303](../frontend/src/pages/main/docker/NetworkList.tsx#L303), [VolumeList.tsx:158](../frontend/src/pages/main/docker/VolumeList.tsx#L158), [SambaShares.tsx:532](../frontend/src/pages/main/shares/SambaShares.tsx#L532), [NFSShares.tsx:614](../frontend/src/pages/main/shares/NFSShares.tsx#L614), [LVMManagement.tsx:640](../frontend/src/pages/main/storage/LVMManagement.tsx#L640). This is duplicated boilerplate (`onMountCreateHandler(handleCreateX)`) — better addressed once architecturally (a small shared hook / ref handoff) than via 10 mechanical `useEffectEvent` edits.
- [GeneralLogsPage.tsx:534](../frontend/src/pages/main/logs/GeneralLogsPage.tsx#L534) (debounce; `applyIdentifierFilter`), [ContainerList.tsx:160](../frontend/src/pages/main/docker/ContainerList.tsx#L160) (`updateSelectedContainer`), [AppDialog.tsx:82](../frontend/src/components/ui/AppDialog.tsx#L82) (transition callbacks).

**Low value — skip:** cleanup-only effects that merely *read* an already-stable
callback ([useLogStream.ts:166](../frontend/src/hooks/useLogStream.ts#L166), [useTerminalContextMenu.ts:101](../frontend/src/hooks/useTerminalContextMenu.ts#L101), [useIntentPreload.ts:58](../frontend/src/hooks/useIntentPreload.ts#L58), [useLiveStream.ts:85](../frontend/src/hooks/useLiveStream.ts#L85)). No re-run problem to fix.

## Bonus — effects that shouldn't be effects (highest value, outside the 3 hooks)

- **Delete (dead no-op):** [useFileDirectorySizeBase.ts:46](../frontend/src/hooks/filebrowser/useFileDirectorySizeBase.ts#L46) — `useIndexerErrorHandler` body is empty (only comments); indexer availability moved to `AuthContext`. Should be removed along with its call sites.
- **Adjust state during render** instead of an effect: [SidebarContext.tsx:41](../frontend/src/contexts/SidebarContext.tsx#L41) (prev-value compare), [ContainerList.tsx:166](../frontend/src/pages/main/docker/ContainerList.tsx#L166) (clear selection when item leaves list), [UpdateContext.tsx:518](../frontend/src/contexts/UpdateContext.tsx#L518) (mirror path to a ref).

## E — keep as-is (the large majority, ~70+ effects)

Legitimate external-system synchronization with no better hook: WebSocket/job
stream-mux open/close (UpdateContext, ConfigContext, AuthContext, useLogStream,
ComposeOperationDialog, DockerIndexerDialog, useRecoveredJobs), intervals/polling
(dashboard graphs, PowerActionContext, InterfaceClients), `document`/`window`
listeners (Esc/click-outside/clipboard/keyboard nav), `ResizeObserver`/scroll
observers, `localStorage` persistence (theme, ToastContext), and imperative xterm
option sync ([useXtermStreamTerminal.ts:130/140](../frontend/src/hooks/useXtermStreamTerminal.ts#L130) — external-widget sync, **not** derived state).

## Recommended actionable subset (if acting on this audit)

Smallest set with clear, low-risk payoff:
1. **Convert the 5 strong scroll effects to `useLayoutEffect`** (A-strong list) — removes visible scroll jump.
2. **Delete the dead no-op `useIndexerErrorHandler`** ([useFileDirectorySizeBase.ts:46](../frontend/src/hooks/filebrowser/useFileDirectorySizeBase.ts#L46)) and its call sites.
3. **(Optional) `useEffectEvent` for the prop-callback B cases** — [FileEditor.tsx:168](../frontend/src/components/filebrowser/FileEditor.tsx#L168) and consolidate the ~10× parent-handler-registration pattern into one shared helper.

Explicitly **not** recommended: any `use()` work (no fit), and the low-value
cleanup-only `useEffectEvent` rewrites.

## Verification (if changes are approved later)

- `make lint-only` — confirm no `react-hooks/exhaustive-deps` regressions, especially
  around `useEffectEvent` edits and the deleted hook's call sites.
- Manual: open a log viewer / update dialog / file list and confirm new content pins
  to the bottom without a visible scroll flash (the `useLayoutEffect` change).
- `cd frontend && npx vitest run` for touched units — `useFileListKeyboardNavigation.test.tsx`
  already asserts `scrollIntoView`, so the layout-effect conversion must keep it green.