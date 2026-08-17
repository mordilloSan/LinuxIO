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

- [x] **A2. Grid-density divergence — decided and applied.** *(done 2026-08-17)*
  Decisions: docker keeps the split deliberately — containers/compose stay
  DENSE (busiest surfaces, compact cards), images/networks/volumes stay
  STANDARD (wider text: tags, subnets); shares unify to DENSE, so NFS mounts
  moved from `CARD_GRID_SIZE_STANDARD` to `CARD_GRID_SIZE_DENSE` to match the
  folder-shares tab (only visual change). Intent is now documented in the
  `constants.ts` comment instead of "no documented reason".
  *Verify: `make check-frontend`, visual pass over docker + shares tabs*

- [x] **A3. Reorder escape hatches — pruned both.** *(done 2026-08-17)*
  Decision: prune. `disableReordering` deleted (zero callers; `SortableCard`'s
  own `disabled` default covers it, and its removal makes the docblock's
  "there is no unarmed variant" literally true). `ReorderableArea.tsx` deleted
  — its only consumer was `ReorderableCardGrid` itself and its `layout="list"`
  branch was dead too; the grid now wraps its body once in
  `DndContext`/`SortableContext` directly (`rectSortingStrategy`). Test mock
  removed (the real contexts render in jsdom); the stale hand-roll comment in
  `NetworkInterfaceList.tsx` reworded. Reintroducible from git if a bespoke
  layout ever needs the escape hatch.
  *Verify: `make check-frontend`*

- [x] **A4. `color-mix` normalisation — TSX half done, CSS half declined.**
  *(done 2026-08-17)* Decision: TSX half only. Nine transparency-mix sites now
  route through `mixWithTransparency`, each opacity hand-recomputed and
  exactly representable (zero visual change): SelectionBox (0.1), FileListRow
  (0.15 / 0.1), CompressFormatDialog (0.1, was the inverted `10%, transparent`
  shape), LVMManagement ×2 (0.12, inverted shape), MultiFileDetail (0.4 /
  0.05), and surfaces.ts's own `getFileEntryBackground` (0.4 / 0.5). Left raw
  on purpose: two-colour mixes the helper can't express
  (`getFileEntryHoverBackground`, `utils/color.ts` `mixWith`) and all 18 CSS
  files — the CSS half keeps both argument shapes, per the scope decision.
  *Verify: `make check-frontend`, visual spot-check of file browser
  (selection box, hidden/selected rows, size chips), compress dialog, LVM*

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
  `.app-dock` from non-touch pointer movement or presses and feeds
  magnification from the same handler. Touch takeover, pointerleave, window
  blur, document hiding, and either magnification-breakpoint transition reset
  both systems.
  `AppTooltip` asks the platform directly (`matches(":focus-visible")`,
  text-entry controls excluded); jsdom never matches `:focus-visible`, so the
  Chromium heuristic (keyboard, pointer, programmatic restoration, text-entry
  exclusion) is covered in the browser suite instead. Direct and Chromium
  coverage also exercise touch takeover, pointerleave, blur, document hiding,
  breakpoint changes, magnification returning to rest, and the focus outline
  in forced-colors mode.
  *Verified: `make check-frontend` (172 files / 827 tests) and
  `make test-frontend-browser` (24 tests) pass, 2026-08-17.*

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
