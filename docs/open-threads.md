# Open threads checklist

Working checklist across all open threads as of 2026-08-17. We work these one
by one; after each item is done the checklist is updated and Miguel runs the
listed verification targets.

Legend: `[ ]` open · `[x]` done · **(decision)** = needs Miguel's call before
or instead of code.

---

## Thread A — card/table coherence refactor (staged, uncommitted)

- [x] **A1. Adopt the card padding scale.** *(done 2026-08-17)*
  24 body-inset sites across 22 card files now use `CARD_PADDING_SM/MD/LG`,
  including insets hoisted onto full-bleed inner elements (DriveCard's body
  button, DockerStatCard's button, DockerResourceListCard's header/footer).
  Zero visual change — every conversion maps the identical pixel value.
  Deliberately left raw because they're off-scale, with the policy recorded in
  the constants.ts comment: 6px insets (`GAP_SM` paddings in LVMMetricCard /
  DockerSectionCard / LVMSectionCard, FileCard's `theme.spacing(1.5)`), 16px
  empty/error states (SensorEmptyCard, UnitLogsCard), and sub-element
  refinements (chip labels, row paddings, UserCard's footer `paddingTop: 8`).
  *Verified: `make check-frontend` passed (173 files / 833 tests).*

- [ ] **A2. (decision) Grid-density divergence.**
  Now explicit in constants, values untouched: docker images/networks/volumes
  use `CARD_GRID_SIZE_STANDARD` (lg:3, 4-across) while containers/compose use
  `CARD_GRID_SIZE_DENSE` (lg:2, 6-across); shares' folder cards disagree with
  NFS mounts one tab over. Decide intent per route, then unify or document.
  *Verify: `make check-frontend`, visual pass over docker + shares tabs*

- [ ] **A3. (decision) Prune or keep the reorder escape hatches.**
  Two primitives now have zero production consumers:
  `disableReordering` prop (`ReorderableCardGrid.tsx:21`) and the
  `ReorderableArea` component (only `ReorderableCardGrid` itself consumes it;
  the network route mention is just a comment). Prune both, keep both as
  documented escape hatches, or split the decision.
  *Verify: `make check-frontend`*

- [ ] **A4. (decision) `color-mix` normalisation — parked on request.**
  ~25 files, two inverted argument shapes (`X 10%, transparent` = 10% opacity
  vs `X, transparent 60%` = 40%). Each conversion must be recomputed by hand —
  pattern substitution silently changes colours. `mixWithTransparency` is now
  exported from `theme/surfaces.ts` so TSX sites can route through it; CSS
  sites can only pick one argument order. Scope options: TSX half only
  (recommended last session), both, or neither.
  *Verify: `make check-frontend`, visual spot-check of touched surfaces*

- [ ] **A5. (decision) Pull the off-scale 6px cards onto the padding scale.**
  Deliberate visual change (6px → 8px body inset): LVMMetricCard,
  DockerSectionCard and LVMSectionCard pad with `GAP_SM` (= 6), FileCard with
  `theme.spacing(1.5)` (= 6 on the 4px unit). Converting them to
  `CARD_PADDING_SM` unifies the scale but visibly loosens those cards —
  Miguel's call, then a small sweep.
  *Verify: `make check-frontend`, visual pass over LVM, docker dashboard,
  file browser card view*

- [ ] **A6. (decision) View-mode default divergence.**
  Docker stacks *do* use the normal accent card (`ComposeStackCard` →
  `FrostedCard accent hoverLift` in a `ReorderableCardGrid`) — but only in
  card view, and `useViewMode("docker.stacks", "table")` defaults to the
  table. Across the app, `docker.containers` is the *only* surface defaulting
  to `"card"`; stacks/images/networks/volumes, accounts, shares, services,
  sockets and timers all default to `"table"`. Decide the intended default per
  surface (or one app-wide rule) and align. Pairs naturally with A2, which
  covers the card-density split between the same tabs.
  *Verify: `make check-frontend`, visual pass over the docker tabs*

## Thread B — dock focus artifacts (simplified 2026-08-17; history in
`docs/focus-artifact-investigation-2026-08-16.md`)

Decision (2026-08-17): delete the per-element focus-origin system and adopt
native `:focus-visible` semantics everywhere. Keyboard activity revealing
focus on the currently focused control is accepted platform behavior, not
fought.

- [ ] **B1. Headed re-verification — once, against the finished state.**
  Real switch-away/return under a window manager (Xephyr `:99` + Muffin):
  confirm no stepped user-agent outline appears; keyboard focus shows the
  tile ring with the label as its caption; hover labels stay hidden after
  returning until the pointer actually moves over the dock; the mobile
  `Actions` ring after search close is the clean designed ring.
  *Verify: manual headed run*

- [x] **B2–B5 collapsed into one simplification.** *(done 2026-08-17)*
  Deleted `utils/inputModality.ts` and its tests, `data-pointer-focus`
  marking, the global outline kill rule in `variables.css`, the
  `:not([data-pointer-focus])` clauses (dock, section-header,
  container-table), and the mobile search's marker restoration — the
  `trigger.focus()` restore itself stays, and the ring it paints is
  intentional, which settles B3. B2 (body-marking bug) skipped: the code it
  lived in is gone. B4 resolved: `.app-dock-link:focus-visible { outline:
  none }` stays, and a deliberate two-colour tile-local ring (white outline
  over a dark separator, lengths authored ×1.6 for the scaled layer) is the
  focus indicator; the label now shows on plain `:focus-visible` as its
  caption. B5 resolved: only the hover-liveness gate survives, moved into the
  dock as `useDockPointerLiveness` — one owner arms `data-dock-pointer` on
  `.app-dock` (non-touch pointers only) and feeds magnification from the same
  handler, and resets both on pointerleave, window blur, or document hiding.
  `AppTooltip` asks the platform directly (`matches(":focus-visible")`,
  text-entry controls excluded); jsdom never matches `:focus-visible`, so the
  Chromium heuristic (keyboard, pointer, programmatic restoration, text-entry
  exclusion) is covered in the browser suite instead.
  *Verified: `make check-frontend` (172 files / 822 tests) and
  `make test-frontend-browser` (20 tests) pass, 2026-08-17.*

## Thread C — sensors (hp_wmi investigation, 2026-08-17)

- [ ] **C1. (decision) Honest rendering for dead fan channels.**
  `hp_wmi` on the HP 15s exposes `fan1_input`/`fan2_input` that always read 0;
  `sensorGroupHelpers.ts:34` maps 0 RPM → "Off" and `SensorGroupCard.tsx:187`
  greys the icon, making "driver reports nothing" indistinguishable from "fan
  stopped". Options: show `—`/"n/a" for RPM channels that have never read
  above 0 since boot (generalises to other broken drivers), or suppress the
  reading on `hp-isa-*` adapters. Pick one, implement.
  *Verify: `make check-frontend`, visual check on the hardware page*

---

## Closed since last session (no action)

- Git identity — now set (`Miguel Mariz` / `miguelgalizamariz@gmail.com`).
- Linux Mint auto-update backend work — committed (`8bdc411a`).
- `UpdateSettings.tsx` typecheck vs stale generated types — resolved;
  `make check-frontend` passes (173 files / 833 tests, 2026-08-17).
- `scripts/compiler-coverage.mjs` formatting — file no longer exists.
