# Card Query Render Boundaries Plan

## Status

Proposed. A working implementation was explored on top of `3cfd870f`, but the
worktree is intentionally being discarded. Reimplement this plan from a clean
checkout; do not recover or blindly reapply the discarded diff.

The dashboard baseline is commit `7ae83016` (`performance: - reduced cards
re-renders on data pull`). That commit established the desired pattern for the
dashboard cards. This plan extends the same ownership rule to other frequently
polled cards and adds a source-level regression guard.

## Problem

Several cards subscribe to volatile TanStack Query results in the same React
component that creates the card shell. Every polling update therefore invokes
the component that returns `FrostedCard` or `DashboardCard`, even when only a
small value such as RX/TX throughput changed. React Scan then reports the card
shell and its provider subtree as rerendered.

This was reproduced manually in both development and production builds. The
production-only React Compiler is therefore not the root cause and must not be
treated as the fix. Memoizing the card or adding another context layer is also
not the intended solution: the query subscription needs a narrower owner.

## Goals

- Keep stable card chrome mounted while polled values update.
- Keep exactly one polling observer for each query key and view.
- Let small live slots subscribe to the existing cache without starting their
  own polling cadence.
- Preserve current loading, error, interaction, layout, and invalidation
  behavior.
- Run imported production components through the same React Compiler transform
  in Vitest that the production Vite build uses.
- Add a component-aware source guard that prevents query hooks from moving back
  into protected card shells.

## Non-goals

- Do not add a behavioral QueryClient/render-count test for Network. The
  component-aware source guard is the requested regression layer.
- Do not add blanket `memo`, custom equality functions, or a card-data context.
- Do not split non-polled cards solely for uniformity.
- Do not change polling intervals, query keys, invalidation, or API contracts.
- Do not compile the Vite development server with the production compiler as
  part of this work. The issue is present with and without the compiler.

## Required component pattern

For a list of cards backed by one polled endpoint:

1. The list or view component owns the only `refetchInterval` observer.
2. Its selector is hoisted and returns stable identity/layout data only, such as
   a name, type, raw index, or reading count.
3. TanStack Query structural sharing keeps that selected result referentially
   stable when only volatile values change.
4. The component that returns `FrostedCard` receives only identity, layout, and
   event props. It must not call a query hook.
5. A nested live component observes the same query key with a selector for one
   entity. It has no `refetchInterval` and uses `refetchOnMount: false`.
6. Prefer `useQuery` for the live observer when an already-mounted parent
   suspense query guarantees the cache entry. This avoids creating a new
   suspense-loader ownership site.

For a standalone polled card, keep the shell query-free and place the existing
polling query in a nested live body. Do not create a second polling observer.

The desired shape is:

~~~tsx
const selectIdentities = (rows: Row[]) =>
  rows.map(({ id, type }) => ({ id, type }));

const CardList = () => {
  const { data: identities } = useSuspenseQuery(
    endpoint.queryOptions({
      refetchInterval: EXISTING_INTERVAL,
      select: selectIdentities,
    }),
  );

  return identities.map((identity) => (
    <StableCardShell key={identity.id} {...identity} />
  ));
};

const StableCardShell = ({ id, type }: Identity) => (
  <FrostedCard>
    <StableHeader id={id} type={type} />
    <LiveCardValues id={id} />
  </FrostedCard>
);

const selectRow = (id: string) => (rows: Row[]) =>
  rows.find((row) => row.id === id) ?? null;

const LiveCardValues = ({ id }: { id: string }) => {
  const { data } = useQuery(
    endpoint.queryOptions({
      refetchOnMount: false,
      select: selectRow(id),
    }),
  );

  return data ? <Values data={data} /> : null;
};
~~~

## Implementation scope

### React Compiler parity

Create `frontend/config/oxc-react-compiler.ts` and move the existing Oxc React
Compiler Vite plugin into it without changing its production options or error
handling.

- `frontend/config/vite.config.ts` imports the helper and still installs it only
  for production builds.
- `frontend/config/vitest.config.ts` installs the same pre-transform.
- The Vitest instance should skip test/spec modules and `src/test/**`, while
  still compiling every imported production module. Test fixtures remain easy
  to inspect, and the production components under test use the production
  compiler.
- Preserve the current `oxc-transform` version note, React target `19`, ES2022
  target, hard-error handling, compiler-panic fallback, and sourcemap behavior.

### Network interface cards

Files:

- `frontend/src/routes/_authenticated/network/-components/NetworkInterfaceList.tsx`
- `frontend/src/components/cards/NetworkInterfaceCard.tsx`

Changes:

- Hoist `selectNetworkInterfaceIdentities`; retain name/type and filter `veth`
  interfaces exactly as today.
- Keep the existing 1-second poll only in `NetworkInterfaceList`.
- Make `NetworkInterfaceCard` the query-free `FrostedCard` owner.
- Add `NetworkInterfaceCardContent`, selected by interface name, for status,
  addresses, link data, RX/TX, and editor data.
- Let the expanded traffic graph subscribe to the same cache by interface name,
  without another interval.
- Keep navigation callbacks stable and preserve the existing keyboard/button
  semantics.

### WireGuard peer cards

Files:

- `frontend/src/routes/_authenticated/wireguard/-components/InterfaceClients.tsx`
- `frontend/src/components/cards/WireguardPeerCard.tsx`

Changes:

- Hoist `selectPeerIdentities` and keep the only 3-second `list_peers` poll in
  `InterfaceClients`.
- Pass peer and interface names into a query-free `WireguardPeerCard` shell.
- Add a selected `usePeer` cache observer backed by a hoisted `selectPeer`.
- Put the online chip and statistics in live children that call `usePeer`.
- Retain the QR-code query as a separate dialog-driven query; it is not polling.
- Preserve delete, download, and QR callbacks by passing the peer name from the
  shell.
- Use one module-level external-store clock for the three-minute online status,
  starting one 3-second timer for the first subscriber and stopping it after the
  last subscriber. Do not create one timer per peer.

### Hardware sensor cards

Files:

- `frontend/src/routes/_authenticated/hardware/-components/HardwarePage.tsx`
- `frontend/src/components/cards/SensorGroupCard.tsx`

Changes:

- Hoist `selectVisibleSensorGroupIdentities` and keep the only 5-second sensor
  poll in `SensorReadings`.
- Each identity contains adapter, the raw source-array index, and visible
  reading count. The raw index is important: filtering out an empty group must
  not shift the selector onto a different source group.
- Make `SensorGroupCardShell` own the card/header and reading-count subtitle.
- Put volatile readings in `SensorGroupCardLive`, selected by adapter plus raw
  group index.
- A change in reading count may rerender the shell because it changes card
  layout/header data; value-only updates must not.

### Hardware history cards

File:

- `frontend/src/routes/_authenticated/hardware/-components/HardwareHistoryCards.tsx`

Changes:

- Keep `HistoryCardShell` query-free and responsible for card chrome, title,
  icon, range selector, and a child region.
- Move the existing queries into `CPUHistoryLive`, `MemoryHistoryLive`,
  `DiskIOLive`, and `NetworkHistoryLive`.
- Preserve each range-dependent interval, placeholder data, capability gate,
  loading/error/empty message, series calculation, and synchronized hover.
- Exported `*HistoryCard` wrappers render both the shared shell and the matching
  live body.

### NFS mount cards

Files:

- `frontend/src/routes/_authenticated/shares/-components/NFSMounts.tsx`
- `frontend/src/components/cards/NFSMountCard.tsx`

Changes:

- The active view owns the only 10-second polling observer.
- Card mode mounts `NFSMountCardGrid`, whose hoisted selector returns mountpoint
  identities.
- Table mode mounts `NFSMountTable`, which observes the full rows because the
  table itself displays volatile values.
- Never mount both polling observers at once.
- Make `NFSMountCard` a query-free shell and put the selected mount data and
  actions in `NFSMountCardLiveContent`.
- Preserve all dialogs, capability warnings, mutation behavior, columns,
  expanded rows, and current filtering semantics.

### Unit information panel

File:

- `frontend/src/components/cards/UnitInfoPanelCard.tsx`

Changes:

- Keep `UnitInfoPanel` as the query-free `FrostedCard`, title, and close button.
- Move the existing 2-second `get_unit_info` query and all rows into
  `UnitInfoPanelLive`.

### Dashboard card owners

The component guard must cover the existing dashboard owners established by
`7ae83016`:

- Query-free owners: `SystemHealth`, `DockerInfo`, `NetworkInterfacesCard`,
  `MotherBoardInfo`, `Processor`, `MemoryUsage`, `FsInfoCard`, and `GpuInfo`.
- Allowed stable selector owners: `Drive` with `hasAnyDrive`, and
  `SystemOverview` with `selectPlatform`.
- Also protect the shared `DashboardCard` itself as query-free.

Do not revive discarded memo-only experiments in Docker or storage. A broad
`DriveCard`/`DiskOverview` split needs a coherent owner for all sibling polling
queries; otherwise the surrounding grid still rerenders and the churn buys
nothing. Reassess it separately with runtime measurements if it remains visible
after the scoped work above.

## Component-aware regression guard

Add `frontend/src/components/cards/cardQueryOwnership.test.ts`.

Use the TypeScript compiler AST rather than regex-balanced function bodies:

- Parse TSX source files with `ts.createSourceFile`.
- Find named function declarations and arrow/function initializers.
- Count direct `useQuery`, `useQueries`, `useSuspenseQuery`, and
  `useSuspenseQueries` calls while excluding nested function bodies.
- Declare exact query-free or allowed query counts per named component.
- For stable-selector owners, inspect the direct query call text and require the
  named `select:` function there, rather than matching an unrelated property in
  the whole component.
- For WireGuard live children, require one direct `usePeer` call, and separately
  require `usePeer` to own one query using `selectPeer`.
- Record shell-to-live and parent-to-card JSX relationships and assert the
  expected child component is actually rendered. This prevents a dead shell or
  detached live helper from satisfying the manifest.
- Include both card/list owners and the live slots in the manifest so renamed or
  removed components make the test fail explicitly.

This is intentionally a structural guard. It prevents the known ownership
mistake but does not claim to prove runtime render counts or TanStack Query
structural-sharing behavior.

The existing keyboard test in
`frontend/src/components/cards/InteractiveCards.test.tsx` will need a small
`useQuery` mock returning its existing `networkInterface` fixture after the
Network live slot is introduced. This keeps the existing accessibility test; it
must not become the rejected behavioral QueryClient/render-count test.

## Verification

After implementation is quiescent, use a fresh Luna medium/Fast test worker and
run these targets sequentially:

~~~text
make check-frontend
make compiler-coverage
make test-frontend-browser
~~~

Do not run Make verification concurrently with implementation or another Make
target. Sol must inspect the complete post-test diff and worktree.

An interrupted exploratory run produced this evidence:

- `make compiler-coverage` passed: 320 memoized, 152 nothing-to-memoize, and 8
  existing compiler skips.
- `make test-frontend-browser` passed a production build and 11/11 Chromium
  tests.
- The first `make check-frontend` reached 654/656 passing unit tests and exposed
  three integration fixes: an optional AST function body, the Network keyboard
  fixture, and three new shared suspense sites. The intended resolution is to
  guard the optional body, mock the narrow live query in the keyboard fixture,
  and use non-suspending `useQuery` in live slots whose parent already owns a
  suspense query.
- A complete rerun after those fixes was started but interrupted for shutdown.

These are not final results because source changed afterward. All three targets
must pass again from the clean reimplementation before handoff. For the browser
target, confirm the actual Playwright count and result rather than relying only
on the Make exit code.

Finally, use React Scan in both development and production against live polling:

- RX/TX, peer statistics, sensor values, history charts, NFS usage, and unit
  information continue updating.
- The corresponding `FrostedCard`/`DashboardCard` shell is not invoked on a
  value-only poll.
- Identity, visibility, selection, range, or layout changes still update the
  shell when appropriate.

## Acceptance checklist

- [ ] Vitest compiles imported production modules with the shared production
      Oxc React Compiler transform.
- [ ] Development Vite behavior is unchanged.
- [ ] Every refactored query key has one polling observer per active view.
- [ ] Live observers share the same query key and do not poll or refetch on
      mount.
- [ ] Protected card shells contain no direct React Query hook.
- [ ] Hoisted identity selectors contain no volatile values.
- [ ] The AST source guard covers dashboard owners, refactored shells, live
      slots, selector names, and JSX ownership relationships.
- [ ] No behavioral QueryClient/render-count test was added.
- [ ] `make check-frontend` passes.
- [ ] `make compiler-coverage` passes after the final source change.
- [ ] `make test-frontend-browser` passes with an explicit Playwright result.
- [ ] React Scan confirms the live values update without invoking the shell in
      both development and production.
