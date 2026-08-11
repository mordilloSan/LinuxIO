# Mutation Feedback Audit and Handoff

Status: audit complete; implementation not started.

Snapshot date: 2026-08-11.

This document is the portable handoff for restoring visible working state to
frontend mutations before the persistent alert phase begins. It records the
current behavior, the confirmed gaps, the intended UI contract, and an
implementation order that can be resumed on another machine without replaying
the investigation.

## Decision

Mutation feedback is a frontend presentation concern. It does not require a
database, another Task type, a global mutation registry, or a Suspense boundary.

`useCallMutation`, `useTaskAction`, and `useTaskStreamAction` expose TanStack
Query mutation state, including `isPending`. That state does not render
anything automatically. The component that owns the user action must connect
it to a visible control or progress surface.

The required distinction is:

| Operation | Required presentation |
|-----------|-----------------------|
| Bounded Call behind an icon button | Replace that action's icon with a spinner and disable conflicting actions. |
| Bounded Call behind a text button | Disable conflicting controls and show an active verb such as `Saving...` or `Stopping...`; a spinner is optional. |
| Entity-scoped row/card action | Show pending state only on the affected entity and action. |
| Task with meaningful progress | Render the common percentage, phase, and message locally or through the existing background-Task surface. |
| Optimistic mutation | Apply the optimistic state immediately, prevent unsafe duplication, and retain failure feedback. |
| Self-severing host action | Replace local feedback with the existing global reboot, shutdown, or update overlay. |

Suspense remains responsible for query and lazy-component loading. It is not a
mutation progress mechanism.

## Audit Scope

The audit covered production TypeScript under `frontend/src`, excluding tests
and generated API files. It found 132 mutation-hook instances across 50
feature, component, context, and hook files:

- 124 `useCallMutation` instances; and
- 8 `useTaskAction` or `useTaskStreamAction` instances.

The count is a call-site count, not a count of unique backend routes. The same
Docker lifecycle route is intentionally owned by several UI surfaces.

The shared mutation lifecycle is implemented in
[`frontend/src/api/call-react-query.ts`](../frontend/src/api/call-react-query.ts).
It awaits mapped query invalidations before the mutation settles, so
`isPending` can correctly cover both the request and the subsequent cache
convergence. Task hooks reuse the same lifecycle in
[`frontend/src/api/task-react-query.ts`](../frontend/src/api/task-react-query.ts).

`AppActionIconButton` already has the desired primitive: `loading` disables the
button and replaces its icon with `AppCircularProgress`. `AppButton` has no
built-in `loading` prop; existing dialogs use explicit active-verb labels and,
where useful, a `startIcon` spinner.

## Confirmed Inventory

### Docker

| Surface | Mutations | Current behavior | Assessment |
|---------|-----------|------------------|------------|
| `ContainerCard` | start, stop, restart, remove, update | Combines each `isPending`, disables actions, and renders a card overlay spinner. | Complete. |
| Docker page actions | Start All, Stop All, Prune All, Check Updates | Uses action-button spinners; Stop All also tracks the affected container IDs. | Complete. |
| Network, image, and volume dialogs | create/delete | Keeps the dialog open, disables controls, and changes the action label. | Complete. |
| Auto-update toggle | save per-container selection | Optimistic state plus a per-container action spinner. | Complete. |
| Compose stack operation | up, down, stop, restart | Dedicated Task dialog renders progress and logs. | Complete. |
| Compose setup/editor/delete | validate, resolve path, save, delete | Dialog-owned saving/validating state or the operation dialog. | Complete. |
| `ContainerTable` row actions | start, stop, restart, remove | Each hook discards `isPending`. The row `pending` prop represents Stop All only. Compact menus close and leave no feedback. | Defect. |
| Dashboard Docker mini-icons | start, stop, restart, remove | Context menu closes immediately and no pending target remains visible. | Defect. |
| Compose expanded containers | start, stop, restart, remove | Hooks discard pending state. | Defect. |
| Compose expanded container update | update | A shared pending flag disables some parent controls, but the affected row/action has no spinner. | Incomplete. |

Primary source locations:

- [`frontend/src/components/cards/ContainerCard.tsx`](../frontend/src/components/cards/ContainerCard.tsx)
- [`frontend/src/routes/_authenticated/docker/-components/ContainerTable.tsx`](../frontend/src/routes/_authenticated/docker/-components/ContainerTable.tsx)
- [`frontend/src/routes/_authenticated/-dashboard/Docker.tsx`](../frontend/src/routes/_authenticated/-dashboard/Docker.tsx)
- [`frontend/src/routes/_authenticated/docker/-components/ComposeList.tsx`](../frontend/src/routes/_authenticated/docker/-components/ComposeList.tsx)
- [`frontend/src/routes/_authenticated/docker/-components/DockerDashboardPage.tsx`](../frontend/src/routes/_authenticated/docker/-components/DockerDashboardPage.tsx)

### Virtual Machines

| Surface | Mutations | Current behavior | Assessment |
|---------|-----------|------------------|------------|
| VM creation | create Task and ISO-folder creation | Typed message/percentage, linear progress, and action spinner. | Complete. |
| VM deletion | delete | Confirmation dialog remains mounted and renders a spinner. | Complete. |
| VM table lifecycle | start, shutdown, reboot, force off, suspend, resume | A single aggregate flag disables lifecycle actions for every VM, but no control identifies the running action or target VM. | Incomplete. |

The lifecycle repair belongs in
[`frontend/src/routes/_authenticated/vm/-components/VMMachinesLayout.tsx`](../frontend/src/routes/_authenticated/vm/-components/VMMachinesLayout.tsx)
and
[`frontend/src/routes/_authenticated/vm/-components/VMListTable.tsx`](../frontend/src/routes/_authenticated/vm/-components/VMListTable.tsx).

### Accounts and System Alerts

| Surface | Mutations | Current behavior | Assessment |
|---------|-----------|------------------|------------|
| Account dialogs | create, edit, delete, password, group membership | Explicit `Creating...`, `Saving...`, `Deleting...`, or `Changing...` state. | Complete. |
| Session termination | terminate session | Confirmation remains open with `Terminating...`. | Complete. |
| User lock/unlock | lock, unlock | All lock buttons disable, but the affected user and operation have no visible working state. | Incomplete. |
| Failed-login dialog | dismiss alert | Shows `Dismissing...`. | Complete. |
| Health-card quick dismiss | dismiss failed-login/unclean-shutdown alert | The icon merely disables. | Incomplete, low priority. |
| Account-detail automatic dismissal | dismiss alert on routed focus | Background convergence with no direct user action. | Intentional. |

### Storage and Shares

| Surface | Mutations | Current behavior | Assessment |
|---------|-----------|------------------|------------|
| SMART self-test | Task start/watch | Spinner, percentage, message, and recovery. | Complete. |
| Filesystem actions | unmount, create Btrfs subvolume | Explicit active-verb labels. | Complete. |
| LVM dialogs | create, resize, delete | Explicit active-verb labels. | Complete. |
| Share create/edit/delete dialogs | SMB and NFS lifecycle | Explicit active-verb labels. | Complete. |
| NFS/CIFS create, edit, remove dialogs | mount/remount/remove | Explicit active-verb labels. | Complete. |
| Existing NFS entry mount | mount | Tracks the mountpoint and disables the action; only the tooltip changes to `Mounting...`. | Incomplete. |
| Existing NFS entry unmount | unmount | Hook pending state is discarded. | Defect. |
| Existing CIFS entry mount/unmount | mount, unmount | Hook pending state is discarded. | Defect. |

### Services, Network, Power, and Settings

| Surface | Mutations | Current behavior | Assessment |
|---------|-----------|------------------|------------|
| systemd unit actions | start, stop, restart, reload, enable, disable, mask, unmask, reset failed | Each action renders its own spinner and disables conflicting actions. | Complete; use this as the row-action reference. |
| Network address save | automatic/manual IPv4 | Explicit `Saving...`. | Complete. |
| Network enable/disable | connection switch | Switch merely disables while pending. | Incomplete. |
| Power profile | set profile | Explicit `Applying...`. | Complete. |
| TuneD start/disable | power icon | Section becomes busy, but the action icon does not show progress. | Incomplete. |
| Indexer and monitoring settings | save/restart/timer | Explicit saving/restarting state. | Complete. |
| Docker folder settings | validate/create/save | One manual `isSaving` flow covers the complete sequence. | Complete. |
| Date/time settings | several sequenced Calls | One manual `isSaving` flow covers the complete sequence. | Complete. |
| Hostname | set hostname | Save button merely disables. | Incomplete, low priority. |
| Capability installation | install Task | Spinner, status, and percentage. | Complete. |
| Reboot and power-off | control Calls | Existing global host-action overlay appears immediately. | Intentional. |
| Configuration persistence | config set | Optimistic local state and failure toast. | Intentional. |

The systemd implementation in
[`frontend/src/routes/_authenticated/services/-components/UnitViews.tsx`](../frontend/src/routes/_authenticated/services/-components/UnitViews.tsx)
is the best existing reference for action-specific spinners.

### WireGuard

| Surface | Mutations | Current behavior | Assessment |
|---------|-----------|------------------|------------|
| Interface creation | add interface | Creation dialog shows `Creating...`. | Complete. |
| Interface lifecycle | remove, up, down, enable, disable | Pending state is not retained or rendered. | Defect. |
| Peer lifecycle | add/remove peer | Pending state is not retained or rendered. | Defect. |
| Peer config download | generate/download config | No pending state remains visible after activation. | Defect. |

The affected owners are
[`frontend/src/routes/_authenticated/wireguard/-components/WireguardDashboard.tsx`](../frontend/src/routes/_authenticated/wireguard/-components/WireguardDashboard.tsx)
and
[`frontend/src/routes/_authenticated/wireguard/-components/InterfaceClients.tsx`](../frontend/src/routes/_authenticated/wireguard/-components/InterfaceClients.tsx).

### File Browser and Background Work

| Surface | Mutations | Current behavior | Assessment |
|---------|-----------|------------------|------------|
| Transfer operations | upload, download, copy, move, compress, extract | Existing background-Task provider and navbar show progress independently of the page. | Complete. |
| Indexer | index Task | Dedicated dialog and global state show progress. | Complete. |
| File/folder creation | resource post | Input dialog closes immediately; completion is visible only through a later toast/list refresh. | Defect. |
| Batch delete | delete Task | Confirmation closes immediately and the Task's progress is not connected to the global background surface or a local dialog. | Defect. |
| Batch permissions | chmod Task | Permissions dialog closes before the awaited Task settles; progress is invisible. | Defect. |
| Rename | rename Task | Inline input remains until settlement, but has no busy indicator or explicit duplicate-submit protection. | Incomplete. |

The mutation facade in
[`frontend/src/hooks/filebrowser/useFileMutations.ts`](../frontend/src/hooks/filebrowser/useFileMutations.ts)
currently returns action functions but strips all mutation state. The dialog
handlers in
[`frontend/src/hooks/filebrowser/useFileBrowserItemActions.ts`](../frontend/src/hooks/filebrowser/useFileBrowserItemActions.ts)
also close several surfaces before completion. This area needs a coherent
local-versus-background ownership decision, not just scattered spinner props.

### Updates

Package refresh, package installation, cancellation, recovery, application
update, and automatic-update settings already have explicit progress or saving
surfaces. The package cancel Call intentionally removes cancelability while the
main Task remains visible; the Task's terminal frame remains authoritative.

## Root Causes

The gaps come from four repeated patterns:

1. A component destructures only `mutate` or `mutateAsync` and discards
   `isPending`.
2. A parent computes one aggregate boolean and disables an entire table without
   retaining the action or entity identity.
3. A menu or confirmation dialog closes immediately, leaving no mounted owner
   capable of presenting pending state.
4. A helper exposes only command functions and hides the underlying mutation
   state from its UI owner.

The transport migration preserved several of these pre-existing presentation
gaps; it did not create an automatic regression mechanism. The new typed hooks
make the state available, but the affected components still have to render it.

## Implementation Plan

Implement one independently reviewable frontend batch at a time.

### Batch 1: Docker action surfaces

- Connect each `ContainerTable` lifecycle mutation to the matching action's
  `loading` prop.
- Derive the row busy state from all four local mutations, not from Stop All.
- On compact layouts, keep a visible row-level spinner after the menu closes.
- Retain the target container and action for dashboard mini-icons.
- Add entity/action-scoped state to Compose expanded-container actions.
- Preserve the existing container-card and bulk-action behavior.
- Add delayed-mutation tests that assert the correct spinner and disabled
  controls while the promise is unsettled.

### Batch 2: Other entity-scoped actions

- VM lifecycle actions.
- WireGuard interface and peer actions.
- NFS and CIFS inline mount/unmount actions.
- Account lock/unlock.

For a mutation owner that permits only one in-flight action, the mutation's
`variables` can identify the affected entity. If parallel actions are an
intentional requirement, retain an explicit `Set` keyed by entity ID instead
of replacing one pending target with another.

### Batch 3: File Browser lifecycle

- Expose bounded create/rename pending state to the owning surface.
- Keep bounded-action dialogs open until success or failure.
- Decide whether batch delete and chmod remain local progress dialogs or join
  the existing global background-Task surface. They must not be represented in
  both places as separate work.
- Preserve path-precise invalidation and existing collision handling.
- Add tests for double-submit prevention, failure retention, success closure,
  and Task progress ownership.

### Batch 4: Small disabled-only controls

- Network enable/disable.
- TuneD start/disable.
- Hostname save.
- Health-card quick dismissal.

These are lower priority because they already prevent duplicate activation and
are usually short, but they should converge on the same visible contract.

### Batch 5: Consistency and regression coverage

- Consider an opt-in `loading` prop for `AppButton` only if it materially
  reduces repeated markup. Do not require it merely to avoid active-verb text.
- Add `aria-busy` or an appropriate live status where visual spinner changes
  are otherwise silent to assistive technology.
- Test reusable presentation patterns and every repaired ownership boundary.
  Do not add a brittle source guard that assumes every mutation must use the
  same UI; optimistic and global-overlay exceptions are valid.

## Acceptance Criteria

- Every directly activated mutation immediately produces a visible working
  state that remains until the action and mapped invalidations settle.
- The affected action and entity are identifiable; unrelated rows do not show
  a spinner.
- Conflicting actions are disabled without unnecessarily blocking unrelated
  entities.
- A closed menu or dialog never leaves a long-running mutation with no visible
  owner.
- Tasks with useful progress render the common envelope; bounded Calls do not
  become Tasks merely to obtain a spinner.
- Optimistic configuration and self-severing host actions retain their existing
  specialized feedback.
- Error and success toasts remain single-owned.
- Each frontend batch passes `make check-frontend` before handoff.

## Test Baseline and Gaps

Existing progress tests cover important update and VM-create flows, but the
affected Docker row/menu, VM lifecycle, WireGuard, mount, account lock, and File
Browser pending states have little or no direct UI coverage. A passing mutation
hook test is insufficient: the regression is specifically whether pending
state reaches a mounted user-visible control.

No implementation or test command was run as part of this audit. The working
tree already contained the API reliability and generic-progress changes when
the audit was written; preserve those changes when moving or committing this
handoff.

## Explicit Non-Goals

- Do not introduce a notification database for mutation spinners.
- Do not turn bounded Calls into Tasks solely for presentation.
- Do not use Suspense as a mutation boundary.
- Do not add a global spinner for every mutation in the application.
- Do not hide entity identity behind one page-wide `isPending` flag.
- Do not duplicate Task progress in both a local dialog and the navbar without
  a deliberate single-owner presentation rule.

