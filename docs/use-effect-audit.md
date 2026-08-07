# React `useEffect` Audit

Audit date: 2026-08-07

## Result

The repository contains **98 actual `useEffect` calls across 69 files**:

| Area | Calls | Files |
|---|---:|---:|
| Routes | 32 | 23 |
| Components | 31 | 20 |
| Hooks | 23 | 17 |
| Contexts | 9 | 6 |
| Router and theme | 3 | 3 |

That is **95 production effects and 3 test-only effects**. No aliased or
`React.useEffect` calls were found, and no `useEffect` calls exist outside
`frontend/src`.

## Replacement Verdict

### `useEffectEvent`

`useEffectEvent` does not replace an Effect's subscription, timer, connection,
or cleanup. It is already used appropriately for many stream, timer, and global
event callbacks. React limits it to non-reactive logic called from Effects or
other Effect Events. See the
[React documentation](https://react.dev/reference/react/useEffectEvent).

No additional migration is warranted merely to remove a dependency. Values
that should cause an Effect to re-synchronize must remain dependencies.

### `useId`

There are no candidates. None of the audited Effects exist to generate DOM or
accessibility identifiers. Existing `useId` calls already handle identifier
generation outside Effects. See the
[React documentation](https://react.dev/reference/react/useId).

### `useImperativeHandle`

There is one architectural candidate:
[useRegisterCreateHandler.ts](../frontend/src/hooks/useRegisterCreateHandler.ts#L30).
Its Effect registers a child-owned create action with a parent toolbar. A ref
that exposes an `openCreateDialog()` method would remove the registration Effect
and would be cleared automatically on unmount. Lifting dialog state to the
parent would be more declarative still.

This affects several callers and is not a mechanical hook substitution. The
current Effect remains valid until that API is deliberately redesigned. See the
[React documentation](https://react.dev/reference/react/useImperativeHandle).

### `useSyncExternalStore`

There are no new candidates. Router, TanStack Query, stream status, media query,
toast history, and live-chart stores already use their appropriate subscription
hooks. The remaining browser listeners report actions rather than expose a
stable snapshot. See the
[React documentation](https://react.dev/reference/react/useSyncExternalStore).

### `useLayoutEffect`

There are no mandatory conversions. The only plausible conversion is
[AppCheckbox.tsx](../frontend/src/components/ui/AppCheckbox.tsx#L37), where
`indeterminate` is a visible DOM property. Convert it only if browser testing
shows a one-frame incorrect checkbox state. `useLayoutEffect` blocks painting,
so the existing `useEffect` is preferable otherwise. See the
[React documentation](https://react.dev/reference/react/useLayoutEffect).

The audited animation effects that intentionally wait for a paint, such as
`AppCollapse`, must not be converted to `useLayoutEffect`.

## Important Findings

### 1. Capability manager Strict Mode lifecycle bug

[CapabilityManagerSection.tsx](../frontend/src/routes/_authenticated/-components/navbar/CapabilityManagerSection.tsx#L180)
sets `mountedRef.current = false` during cleanup, but its setup never restores
the ref to `true`. Because [index.tsx](../frontend/src/index.tsx#L17) enables
Strict Mode, development setup/cleanup replay can leave the ref false and
suppress subsequent asynchronous state updates.

Set the ref true during setup or replace the mounted flag with cancellation
owned by each asynchronous operation.

### 2. Notification peek timer cleanup

[NavbarNotificationsDropdown.tsx](../frontend/src/routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx#L298)
clears its zero-delay `openTimer`, but does not clear the longer hide timer in
`peekTimerRef` when the Effect cleans up.

### 3. Dialog body scroll locking

[AppDialog.tsx](../frontend/src/components/ui/AppDialog.tsx#L78)
unconditionally restores `document.body.style.overflow = ""`. Mixed or nested
dialogs can therefore unlock scrolling while another overlay remains open.
[AppFullscreenDialog.tsx](../frontend/src/components/ui/AppFullscreenDialog.tsx#L39)
maintains a separate lock mechanism. Both components should eventually use one
shared, reference-counted body lock that restores the original value when the
last owner releases it.

### 4. Effects that look derived but own synchronization

Two Effects initially resemble redundant derived state, but should remain in
the current architecture:

- [ContainerList.tsx](../frontend/src/routes/_authenticated/docker/-components/ContainerList.tsx#L183)
  canonicalizes stale router URL state when a polled container disappears.
- [useContainerAutoUpdateState.ts](../frontend/src/routes/_authenticated/docker/-components/useContainerAutoUpdateState.ts#L77)
  maintains a confirmed server baseline separate from optimistic queued state.

Neither is a `useSyncExternalStore`, `useId`, or `useImperativeHandle`
replacement candidate.

## Complete Inventory

Every entry below remains a valid `useEffect` under the current architecture.

```text
components — 31
frontend/src/components/cards/FileCard.tsx:105
frontend/src/components/charts/HistoryAreaChart.tsx:156
frontend/src/components/charts/LiveChartHover.tsx:43
frontend/src/components/charts/useLiveSeries.ts:44
frontend/src/components/docker/ComposeOperationDialog.tsx:89,94
frontend/src/components/docker/ComposeValidationFeedback.tsx:40
frontend/src/components/filebrowser/DirectoryListing.tsx:194
frontend/src/components/filebrowser/FileEditor.tsx:198,203
frontend/src/components/filebrowser/FileListRow.tsx:87
frontend/src/components/loaders/BootstrapLoaderReady.test.tsx:58
frontend/src/components/tables/AppVirtualDataTable.tsx:523,556
frontend/src/components/ui/AppCheckbox.tsx:37
frontend/src/components/ui/AppCollapse.tsx:23
frontend/src/components/ui/AppDialog.tsx:78,106,144,156
frontend/src/components/ui/AppFullscreenDialog.tsx:39,80,91
frontend/src/components/ui/AppMenu.tsx:60
frontend/src/components/ui/AppPopover.tsx:217,231
frontend/src/components/ui/AppSelect.tsx:117
frontend/src/components/ui/AppTooltip.tsx:209,227,248
frontend/src/components/ui/DirectoryTree.tsx:257

contexts — 9
frontend/src/contexts/AuthContext.tsx:159,264,269,289
frontend/src/contexts/ConfigProvider.tsx:344
frontend/src/contexts/PowerActionProvider.tsx:25
frontend/src/contexts/ToastProvider.tsx:22
frontend/src/contexts/UpdateProvider.tsx:421
frontend/src/contexts/composeProviders.test.tsx:11

hooks — 23
frontend/src/hooks/backgroundJobs/useActiveJobRecovery.ts:53
frontend/src/hooks/backgroundJobs/useAnimatedIndexerStats.ts:115
frontend/src/hooks/backgroundJobs/useRecoveredJobs.ts:441
frontend/src/hooks/filebrowser/useFileBrowserClipboardShortcuts.ts:55
frontend/src/hooks/filebrowser/useFileBrowserNavigation.ts:41
frontend/src/hooks/filebrowser/useFileListKeyboardNavigation.ts:121
frontend/src/hooks/filebrowser/useFileMarqueeSelection.ts:175
frontend/src/hooks/useDebouncedValue.ts:10
frontend/src/hooks/useDismissibleLayer.ts:27
frontend/src/hooks/useGlobalContextMenuGuard.ts:8
frontend/src/hooks/useLiveStream.ts:109
frontend/src/hooks/useLogStream.ts:157,167,188,192
frontend/src/hooks/usePackageUpdateTransaction.ts:187
frontend/src/hooks/useRegisterCreateHandler.ts:30
frontend/src/hooks/useStreamMessageChannel.ts:47
frontend/src/hooks/useTerminalContextMenu.ts:91,101
frontend/src/hooks/useXtermStreamTerminal.ts:130,140,156

router and theme — 3
frontend/src/router/provider.tsx:30
frontend/src/router/query-client.test.tsx:25
frontend/src/theme/index.ts:691

routes — 32
frontend/src/routes/-components/RouteError.tsx:13
frontend/src/routes/sign-in/-components/Login.tsx:45
frontend/src/routes/_authenticated/-components/navbar/CapabilityManagerSection.tsx:180,187
frontend/src/routes/_authenticated/-components/navbar/DockerFolderSettingsSection.tsx:159
frontend/src/routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx:267,298,363
frontend/src/routes/_authenticated/-components/sidebar/useCloseMobileSidebarOnNavigate.ts:16
frontend/src/routes/_authenticated/-components/update/UpdateDialog.tsx:46
frontend/src/routes/_authenticated/-components/update/useUpdateInfo.ts:89
frontend/src/routes/_authenticated/-dashboard/DriveGraph.tsx:60
frontend/src/routes/_authenticated/-dashboard/NetworkGraph.tsx:72
frontend/src/routes/_authenticated/-dashboard/ProcessorGraph.tsx:48
frontend/src/routes/_authenticated/accounts/-components/UsersTab.tsx:74
frontend/src/routes/_authenticated/accounts/-components/components/UserAccountDetails.tsx:577
frontend/src/routes/_authenticated/docker/-components/ContainerList.tsx:183,189
frontend/src/routes/_authenticated/docker/-components/useContainerAutoUpdateState.ts:77,87,100
frontend/src/routes/_authenticated/logs/-components/GeneralLogsPage.tsx:747,860,995
frontend/src/routes/_authenticated/network/-components/NetworkInterfaceList.tsx:190
frontend/src/routes/_authenticated/network/-components/NetworkTrafficGraph.tsx:39
frontend/src/routes/_authenticated/services/-components/UnitListTab.tsx:85
frontend/src/routes/_authenticated/storage/-components/DiskOverview/index.tsx:161,388
frontend/src/routes/_authenticated/updates/-components/UpdateList.tsx:47
frontend/src/routes/_authenticated/vm/-components/ConsoleDialog.tsx:62
frontend/src/routes/_authenticated/wireguard/-components/WireguardDashboard.tsx:103
```

## Verification Scope

The inventory came from a repository-wide, multiline-aware source search for
actual calls:

```sh
rg -n --glob '*.{ts,tsx,js,jsx}' \
  '(?:\buseEffect|React\.useEffect)\s*\(' .
```

The conclusions are source-verified. They do not claim browser-observed timing,
layout, focus, or flicker behavior. Browser verification is required before
promoting any Effect to `useLayoutEffect`.
