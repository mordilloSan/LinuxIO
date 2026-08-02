# Frontend Shared UI Adoption and Accessibility Plan

Status: focused accessibility closure complete. The initial button/shared-UI
audit, direct-control migration, visible-focus styles, targeted semantic
interaction work, explicit exception ledger, and browser keyboard/focus fixture
have landed and passed the required Make targets. Broad visual/shared-UI
architecture, mass icon-wrapper churn, generic card abstraction, and the
unresolved File Browser composite redesign remain outside this closure.

Plan date: 2026-08-01

## Current implementation re-baseline

The original audit described the tree before commit `a7bc10de`. That follow-up
replaced the straightforward native controls listed in Phase 3, added shared
focus-visible styles, and introduced a source guard for native form controls and
feature-level button roles. Those items must not be treated as unstarted work.

The current targeted batch completes these additional high-value items:

- a semantic `AppLinkButton` replaces the nested release-notes link/button;
- System Health navigation rows use links, in-place rows use buttons, and
  secondary actions are separate siblings;
- File Browser sort controls expose the active field and direction, with an
  indicator available to keyboard focus;
- LVM, Hardware, Docker, and Compose log disclosures use shared buttons with
  `aria-expanded` and valid `aria-controls` relationships;
- a residual audit of all 73 production `AppIconButton` call sites keeps the
  genuine icon-only actions and adds missing programmatic names and state;
- all card/table and card/list presentation switches use one controlled
  `ViewModeToggle` that owns the tooltip, action name, and icons;
- File Browser quick-save and search actions now preserve disabled behavior
  when their owning callback or field is unavailable;
- focused tests cover the new link-button, sorting, System Health interaction,
  File Browser action state, and representative disclosure contracts.

That earlier batch deliberately did not perform the repository-wide Iconify
migration, interactive-card restructuring, or File Browser composite-widget
redesign. The focused closure below now covers the audited card interactions;
the Iconify rewrite and File Browser composite redesign remain non-goals.

## Primary goal: close interaction-accessibility defects with shared controls

Every in-scope frontend interaction must use the existing shared control or a
small, reviewed shared-control extension. Feature code must not hand-roll
buttons, links, disclosures, fields, menus, or equivalent interactive
semantics when the shared system already owns that behavior.

The architectural boundary should be:

```text
routes and feature components
  -> LinuxIO shared UI components
    -> native HTML, Iconify, styling, focus behavior, and accessibility details
```

Native elements and third-party rendering libraries are implementation details
of the shared UI layer. They should not be independently restyled and given
parallel behavior throughout feature code.

Keyboard accessibility and interaction state are the objective of this closure.
Central ownership should keep names, focus, keyboard activation, disabled and
expanded/selected state, and interaction isolation consistent. Broad visual
component consolidation is a separate effort.

## Shared UI ownership policy

### Feature code must use shared components

The policy applies to production code under `frontend/src/routes` and feature
code under `frontend/src/components`, except for the implementation of an
approved shared primitive or specialized composite widget.

Feature code should use the appropriate canonical component, including:

- `AppButton`, `AppIconButton`, and `AppActionIconButton` for actions;
- shared link or link-button components for navigation;
- `FrostedCard`, `AppPaper`, and `AppCardContent` for surfaces, with semantic
  `AppButton` trigger regions kept separate from nested controls;
- `AppTextField`, `AppSearchField`, `AppSelect`, `AppAutocomplete`,
  `AppCheckbox`, `AppSwitch`, and `AppFormControlLabel` for form controls;
- `AppDialog`, `AppFullscreenDialog`, `AppPopover`, `AppMenu`, `AppMenuItem`,
  and `AppTooltip` for overlays;
- `AppAlert`, `AppChip`, `StatusDot`, `AppSkeleton`, `AppCircularProgress`, and
  `AppLinearProgress` for feedback and status;
- `AppTypography`, `AppGrid`, `AppDivider`, and shared table components for
  common presentation;
- `@iconify/react` plus generated local icon collections as the canonical
  icon rendering/bundling boundary. Interactive owners provide names and state;
  decorative icons remain non-interactive and appropriately hidden.

The first implementation phase must document which existing components are
canonical. This does not require a barrel export if one would introduce cycles
or reduce type safety; it requires one clear ownership boundary and catalog.

### Feature code must not hand-roll primitives

Outside the approved shared layer, production feature code should not:

- render raw `<button>` elements or emulate them with clickable divs, spans, or
  list items;
- render raw text, search, select, checkbox, switch, or textarea controls when
  a shared field component can represent the behavior;
- turn an icon into an interactive element or make a decorative icon own the
  accessible name/state that belongs to its parent control;
- create card surfaces, menu items, dialogs, alerts, chips, loaders, or
  tooltips from one-off markup and inline styles;
- duplicate hover, focus, disabled, selected, loading, or responsive behavior
  already owned by a shared component;
- work around a missing shared API repeatedly in feature code.

When the current shared API cannot represent a legitimate design, extend the
shared component or add the smallest coherent primitive first. A feature-local
implementation is not the final solution merely because the shared component
currently lacks one prop.

### Exceptions must be explicit

Some native or specialized implementations are appropriate, but they must be
recorded rather than inferred from existing code. Likely exceptions include
hidden file/color inputs, table-library internals, correct native tab widgets,
drag surfaces, and complex file-manager composites. Iconify and generated local
collections remain the renderer/bundling boundary; a bespoke SVG or
illustration should require a real design need and a documented exception.

Test fixtures may use simple native controls when the native element itself is
not under test. Generated files and shared-component internals are outside the
feature-code restriction.

## Phase 1: Inventory and enforce the shared UI boundary

### 1.1 Build the canonical component catalog

Inventory `frontend/src/components/ui` and the adjacent shared component
families such as cards, dialogs, tables, tab controls, and loaders. For every UI
category, record:

- the canonical component and supported variants;
- whether feature code currently bypasses it;
- missing behavior or styling that causes those bypasses;
- whether the component should remain adjacent or move behind a clearer shared
  UI boundary;
- focused tests that protect its public contract.

At minimum, cover buttons, links, icons, cards, typography, inputs, selection
controls, overlays, menus, status/feedback, loading, layout, tables, tabs, and
interactive rows.

### 1.2 Audit all production feature code

Classify every candidate as an existing-component migration, a shared API gap,
or an approved exception. The audit should include:

- raw `<button>`, `<input>`, `<select>`, `<textarea>`, and action-like `<a>`
  elements;
- `role="button"`, conditional button roles, and manual `tabIndex`/key handlers;
- `onClick` on non-interactive elements and `FrostedCard`;
- focusable or independently interactive icon/SVG markup;
- custom card, dialog, menu, tooltip, chip, alert, loader, and field styling;
- repeated CSS or inline styles that reproduce a shared component state;
- invalid nested interactive markup.

Exclude `frontend/src/components/ui/**`, generated files, tests/fixtures, and
approved specialized composites from automatic violations. Search results must
be reviewed; enforcement must not blindly replace semantically correct native
elements.

Keep an exception ledger in this plan or a dedicated source guard. Each
exception should name the file/pattern, explain why the shared components do not
fit, and state what behavior protects it.

### 1.3 Add regression enforcement

After the inventory is classified, add the narrowest maintainable automated
guard. Prefer lint/source rules that reject unapproved feature-level:

- raw UI primitives;
- action-like clickable non-interactive elements;
- the audited nested-interactive and mouse-only patterns;
- newly introduced feature-level native controls or button roles.

The guard should point developers to the canonical shared component and permit
the reviewed exception list. It should prevent new bypasses while the existing
inventory is migrated incrementally.

### 1.4 Original audit baseline and current residual

The original source audit found:

- 12 application `role="button"` sites across nine files: nine div-backed
  controls, one span, and two list items;
- 11 `FrostedCard` call sites across ten files that attach `onClick` to a card;
- additional mouse-only divs used for status icons, rows, disclosures, and
  section headers;
- straightforward custom controls that duplicate `AppButton`, `AppIconButton`,
  `AppMenuItem`, or `AppSearchField`;
- no general visible keyboard-focus style at that time for `AppButton`,
  `AppIconButton`, or interactive `AppChip`.

After the focused migration, the reviewed feature-level exception ledger
contains two specialized notification list controls in one file. No audited
`FrostedCard` retains an outer `onClick`; card activation now belongs to named
`AppButton` trigger regions and nested controls remain siblings. Shared
focus-visible selectors exist for all three primitives named above. The source
guard rejects unreviewed raw controls and button roles, uses exact documented
exceptions instead of count-based allowances, and protects the audited
mouse-only and nested-interactive regressions. Direct `@iconify/react` imports
are not a closure violation: that renderer and generated local collections are
the canonical rendering/bundling boundary.

Implementation and browser claims below are scoped to the automated evidence
recorded for this closure; excluded composite and visual work remains unverified.

### Focused closure implementation

- Settings color and mount-order cards now leave activation to their semantic
  swatch, icon-button, switch, and add-folder controls.
- Notification peek and recent-toast navigation use one named button/link each;
  the two specialized indexer list controls remain explicit exceptions.
- The remaining audited disclosures and interactive cards use `AppButton`
  trigger regions with nested actions kept outside.
- Primitive tests cover disabled buttons, named/disabled icon buttons, and
  interactive-chip Enter/Space behavior.
- The source guard now uses explicit file/pattern/reason/behavior exceptions and
  protects the audited mouse-only and nested-interactive regressions.
- Required frontend and browser verification passed as recorded below.

### Focused closure verification

Verified on 2026-08-01 after the final implementation diff:

- `make check-frontend`: passed TypeScript, Oxlint/Oxfmt, and all 627 tests in
  122 test files;
- `make test-frontend-browser`: passed the production fixture build and all 11
  Playwright tests, including real Tab focus, computed focus outlines,
  Enter/Space activation, and Space scroll prevention for the shared controls;
- the post-test worktree matched the pre-test worktree, with no generated or
  tooling changes.

## Why keyboard semantics are important

A native `<button>` is more than an element with a button label. The browser
provides a complete interaction contract:

- it enters the keyboard tab order automatically;
- it exposes its role, accessible name, and disabled state to accessibility
  APIs;
- Enter and Space activate it with native button behavior;
- Space does not scroll the page while activating the button;
- `disabled` prevents activation consistently;
- focus can be shown with a shared `:focus-visible` treatment;
- browser, screen-reader, switch-control, and other assistive-technology users
  receive the same control contract.

Adding `role="button"` to a div or span changes how the element may be announced,
but it does not add any of that behavior. `tabIndex={0}` only makes the element
focusable. The application still has to reproduce Enter and Space handling,
prevent unwanted Space scrolling, implement disabled behavior, expose state,
provide a visible focus indicator, and keep an accessible name. Small omissions
make a control mouse-only or make its current state impossible to discover.

Hand-written key handlers can also differ subtly from native activation timing.
For example, the current `SortBar` handlers react on keydown and do not prevent
the default Space action. A real button already has the expected behavior and
does not require each feature component to maintain its own keyboard protocol.

Semantics must also match intent:

- use a button for an in-place action, toggle, disclosure, or dialog trigger;
- use an `<a href>` or TanStack Router `Link` for navigation so URL, history,
  open-in-new-tab, and context-menu behavior remain available;
- expose disclosure state with `aria-expanded` and its relationship with
  `aria-controls`;
- expose sorting through the appropriate table/header semantics and
  `aria-sort`;
- do not wrap buttons, inputs, links, or other actions inside a button-like
  parent. Split a card's primary trigger from its nested actions instead.

These are usability requirements, not cosmetic cleanup. A mouse-only control
blocks keyboard-only users. An invisible focus position makes navigation
unreliable. Incorrect nested semantics can hide or confuse child actions in the
accessibility tree. Centralizing the behavior in shared UI components fixes the
contract once and prevents the same defects from returning in individual
features.

## Implementation principles

1. Feature code uses LinuxIO's shared UI components for all ordinary UI
   primitives.
2. Prefer native HTML semantics inside those shared UI components.
3. ARIA supplements native behavior; it does not replace it.
4. Fix or extend shared primitives before migrating large numbers of call
   sites.
5. Keep `FrostedCard` a visual surface. Do not make every card implicitly
   interactive.
6. Create a new shared primitive only when existing components cannot safely
   represent the interaction or would require repeated style resets.
7. Keep nested controls outside a card's primary semantic trigger.
8. Preserve routing semantics: navigation remains a link, actions remain
   buttons.
9. Preserve responsive layout, loading/disabled states, event propagation, and
   current visual hierarchy during each migration.
10. Implement and verify the work in small, coherent batches.

## Phase 2: Complete and harden the shared UI system

### 2.1 Fill shared component gaps

The inventory is expected to require at least these shared capabilities:

- correct ownership of icon accessibility: `@iconify/react` and generated local
  icon collections remain the canonical renderer/bundling boundary; interactive
  controls supply names/state and decorative icons remain non-interactive and
  appropriately hidden;
- a valid shared link-button treatment for TanStack Router links and external
  anchors;
- a shared disclosure/header trigger;
- semantic `AppButton` card trigger composition for `FrostedCard` surfaces;
- any field, menu, or status variants currently implemented repeatedly in
  feature code.

Add only gaps proven by the inventory. Prefer extending an existing component
when that keeps one clear ownership model without making its API ambiguous.

### 2.2 Visible focus treatment (implemented and browser-verified)

A consistent `:focus-visible` style now exists in:

- `frontend/src/components/ui/app-button.css`
- `frontend/src/components/ui/app-icon-button.css`
- `frontend/src/components/ui/app-chip.css`

The selectors use theme tokens and do not depend on hover. The browser fixture
confirms a computed solid two-pixel focus outline for representative
`AppButton`, `AppIconButton`, and interactive `AppChip` instances in the dark
fixture theme. Full light/dark visual contrast review across every surface is
broader visual QA, not part of this focused closure. Do not remove outlines
without an equivalent visible replacement.

Review other shared interactive primitives for the same contract, especially
`AppMenuItem`, `AppSelect`, autocomplete options, and directory-tree controls.
Keep their existing focus background if it is sufficiently visible; do not make
unrelated visual changes.

### 2.3 Strengthen primitive tests

Extend shared-component coverage to confirm:

- `AppButton` and `AppIconButton` render non-submit native buttons by default;
- accessible names and native attributes pass through;
- disabled and loading controls cannot activate;
- interactive chips remain keyboard-operable;
- focus-visible CSS is covered by a browser test or another test that can
  observe computed focus presentation. A source-only assertion is not enough
  for a runtime visibility claim.

## Phase 3: Migrate direct feature-level interaction bypasses

These sites already map cleanly to existing UI primitives and should not wait
for a new card abstraction.

### 3.1 Keep icon rendering non-interactive and correctly owned

Do not require a repository-wide wrapper or mass migration of `@iconify/react`
imports. Review icon call sites only where they affect interaction semantics:
the owning button/link supplies the accessible name and state, while decorative
icons use the existing hidden/non-interactive conventions. Generated local icon
collections and the Iconify renderer remain the canonical bundling boundary.
Feature code must not make decorative glyphs independently focusable or
interactive.


### 3.2 Replace direct controls

The controls in this table were migrated by `a7bc10de`. The release-notes case
immediately below is completed by the current targeted batch.

| Current site | Target | Required behavior to preserve |
| --- | --- | --- |
| `frontend/src/routes/_authenticated/-components/footer/DevToolsButton.tsx` | `AppButton` with the wrench as `startIcon` | DEV-only rendering, open state, minimum width, hover styling, and panel toggle |
| `frontend/src/components/cards/UnitInfoPanelCard.tsx` | `AppIconButton` | Add an explicit close label |
| `frontend/src/components/dev-tools/DevToolsPanel.tsx` | `AppIconButton` | Add an explicit close label and preserve white/inherited color |
| `frontend/src/components/dev-tools/DevtoolsModal.tsx` | `AppIconButton` | Preserve pointer-event isolation from the drag handle |
| `frontend/src/routes/_authenticated/-components/navbar/NavbarUserDropdown.tsx` | `AppMenu` and `AppMenuItem` | Focus entry, Arrow/Home/End behavior, Escape/outside dismissal, and power confirmation flow |
| `frontend/src/routes/_authenticated/-components/navbar/Navbar.tsx` | `AppSearchField` | Navbar sizing, colors, search icon, accessible name, and responsive visibility |
| `frontend/src/routes/_authenticated/-components/navbar/ThemeColorsSection.tsx` light/dark selector | `AppButton` | Selected visual state and compact pill layout |
| `frontend/src/routes/_authenticated/vm/-components/VMListTable.tsx` name action | `AppButton` or a semantic link if selection becomes navigation | Compact table layout and selection behavior |

Fix `frontend/src/routes/_authenticated/-components/update/UpdateBanner.tsx`
in this phase as well. It currently nests an `AppButton` inside an anchor. Add
the smallest shared link-button API that can render valid external-link markup;
do not retain nested interactive elements and do not convert navigation into an
`onClick` button.

## Phase 4: Add the missing shared interaction patterns

The existing `AppButton` API is suitable for ordinary actions, but it is not a
safe drop-in replacement for every full-width header or card. Repeated local
style resets would simply create another inconsistent convention.

### 4.1 Shared disclosure trigger

Add a small shared disclosure/header trigger built on a native button. It must:

- accept `aria-expanded` and `aria-controls`;
- expose one accessible name;
- render an optional leading/trailing icon without creating a second focus
  stop;
- support full-width, left-aligned content;
- provide shared hover and focus-visible states;
- allow supplementary non-interactive content such as counts or status chips;
- avoid owning collapse state or content rendering.

The feature component should continue to own the state and `AppCollapse`. The
shared trigger should own only the semantic interaction and presentation
contract.

### 4.2 Semantic card action area (implemented by composition)

Use `AppButton` as a semantic card action/trigger rather than making
`FrostedCard` itself interactive. The implemented composition is:

```text
FrostedCard (visual surface)
  -> semantic primary action or disclosure trigger
  -> independent action area containing AppButton/AppIconButton/input controls
  -> related expanded content
```

A simple card with no nested controls may use one full-card `AppButton` trigger.
Cards containing actions or form fields must use a separate trigger region.

The focused migration proves this composition against both simple and
nested-action cards without introducing a generic polymorphic abstraction.

### 4.3 Link-button support

If the inventory shows more than one link styled as a button, add a narrow shared
link-button component or shared style contract for TanStack Router links and
external anchors. Do not make `AppButton` broadly polymorphic unless that
materially simplifies real call sites while retaining type safety.

## Phase 5: Migrate disclosures, sorting, and mouse-only actions

The System Health, Sort Bar, LVM, Hardware, Docker, Compose, settings, and
notification interaction work in this phase is implemented. Verification
status is recorded separately below.

### 5.1 Confirmed accessibility defects

Update `frontend/src/routes/_authenticated/-dashboard/SystemHealth.tsx`:

- status icon: use `AppIconButton` with an accessible name;
- secondary text action: use `AppButton`;
- navigational rows: use semantic links;
- in-place row actions: use semantic buttons;
- ensure a nested secondary action does not also activate the row.

Update `frontend/src/components/filebrowser/SortBar.tsx`:

- use native/shared buttons for all three sort triggers;
- expose the active sort column and direction with appropriate sorting
  semantics;
- ensure both Enter and Space activate exactly once;
- make the sort indicator available on keyboard focus, not only mouse hover;
- preserve grid alignment with file rows at every breakpoint.

### 5.2 Shared disclosure migrations

Use the Phase 4 disclosure trigger in:

- `frontend/src/components/cards/LVMSectionCard.tsx`
- `frontend/src/routes/_authenticated/hardware/-components/HardwarePage.tsx`
- `frontend/src/routes/_authenticated/docker/-components/DockerDashboard.tsx`
- `frontend/src/components/docker/ComposeOperationDialog.tsx`

Remove decorative `AppIconButton` instances that are focusable but do not own
the action. The chevron should be part of the one disclosure trigger. Give each
controlled region a stable id and connect it with `aria-controls`.

### 5.3 Settings and notification interactions

Migrate or restructure:

- the color-swatch div button and clickable color rows in
  `ThemeColorsSection.tsx`;
- the clickable primary-color card in `SettingsDialog.tsx`;
- the mount-order toggle card and add-folder card in
  `DockerFolderSettingsSection.tsx`;
- the notification peek in `NavbarNotificationsDropdown.tsx`.

The two conditional interactive notification list items remain specialized
list controls. Source review confirms their explicit names, tab stops,
Enter/Space handlers, and focus-visible class; the exception ledger locks their
two exact button-role sites. A future full-dropdown integration harness can add
runtime coverage without making a mechanical `AppButton` count the goal.

## Phase 6: Migrate interactive cards in safe groups

### 6.1 Simple action cards

These audited cards now use semantic trigger regions:

- `DockerStatCard.tsx`
- `DriveCard.tsx`, after resolving or separating its overlay action
- `NetworkInterfaceCard.tsx`

`WireguardInterfaceCard.tsx` is handled with the nested-action cards because its
icon actions must remain siblings of the selection trigger. Verify accessible
names, focus order, hover, selected state, and responsive layout.

### 6.2 Cards with nested actions or expanded content

Then migrate:

- `FilesystemCard.tsx`
- `UserCard.tsx`
- `UnitCard.tsx`
- `ContainerCard.tsx`
- `WireguardInterfaceCard.tsx`

These must not become one outer button. Split the primary selection/disclosure
trigger from nested edit, delete, start/stop, browse, text-field, and dialog
actions. Verify that activating any nested action never also selects, opens, or
collapses the parent card.

### 6.3 File-browser rows and cards

Treat `FileCard.tsx` and `FileListRow.tsx` as a separate interaction model. They
support selection, double-click, context menus, marquee behavior, renaming, and
container-level keyboard navigation. Do not replace them directly with
`AppButton`.

First define whether the UI is a listbox, grid, or file-manager-specific
composite widget. Then align roles, selection state, focus ownership, Enter/open
behavior, and renaming with that model. Add or extend a shared file-item
interaction primitive only after this contract is explicit.

## Phase 7: Close focused enforcement gaps

Repeat the interaction audit after the planned migrations. Review native
controls, action-like links, clickable non-interactive elements, nested
interactive markup, disclosure state, accessible names, focus behavior, and
interaction isolation. Direct Iconify imports, broad visual consolidation, and
generic card abstraction are not this phase's acceptance criteria.

For each in-scope result, migrate it, extend a shared control, or record a
narrow approved exception with its technical reason and protected behavior.
Once the residual list contains only reviewed exceptions, make the guard reject
new unapproved interaction bypasses and document the canonical alternative in
each diagnostic.

## Tests and acceptance criteria

Every migrated interaction must satisfy the applicable criteria below.

### Keyboard behavior

- Tab reaches every actionable control exactly once and in a logical order.
- A visible focus indicator appears whenever focus is moved by keyboard.
- Enter and Space activate buttons exactly once.
- Space activation does not scroll the page.
- Disabled or loading controls cannot activate.
- Escape and arrow-key behavior remains correct for menus and composite
  widgets.

### Semantics and state

- Every control has an accessible name.
- Navigation uses a real link; in-place actions use a real button.
- Disclosures expose `aria-expanded` and a valid `aria-controls` relationship.
- Sort controls expose the active column and direction.
- Selected/toggled state is exposed where applicable.
- There are no nested buttons, links, inputs, or button-role parents around
  other interactive controls.

### Interaction isolation

- Nested card actions do not activate the card's primary action.
- Mouse, touch, and keyboard activation produce the same state transition.
- Dialog, popover, and menu triggers return or preserve focus appropriately.
- Responsive action labels and icon-only modes retain accessible names.
- File selection, double-click, context-menu, marquee, and rename behavior do
  not regress.

### Required verification

For each coherent frontend implementation batch:

1. add or update focused component tests;
2. run `make check-frontend`;
3. run `make test-frontend-browser` for claims about real tab order, focus
   visibility, Space scrolling, navigation, chunk loading, or browser behavior;
4. run `make setup-frontend-browser` first only if the repository's browser
   dependency is not already installed;
5. inspect the complete diff and worktree after verification, because tooling
   may update files.

Browser claims in this closure are limited to the fixture behavior exercised by
the passing browser target.

## Recommended implementation batches

1. Focused interaction inventory and reviewed exception ledger.
2. Focus-visible foundation, link-button, disclosure, and primitive tests.
3. Direct button, field, menu, search, close-action, and release-notes link
   migrations.
4. `SystemHealth`, `SortBar`, `ComposeOperationDialog`, and shared disclosure
   migrations.
5. Settings, notification, and simple card interaction migrations.
6. Nested-action card and disclosure isolation migrations.
7. File-browser composite interaction only after its semantic model is agreed.
8. Focused residual audit, explicit exception ledger, and activation of the
   maintainable interaction regression guard.

Each batch should remain independently reviewable and should not combine broad
visual refactoring with semantic migration.

## Definition of done

This plan is complete only when:

- every in-scope action and navigation uses a semantic shared control or a
  reviewed specialized composite;
- interactive controls expose accessible names and applicable state, while
  decorative icons remain non-interactive; `@iconify/react` and generated local
  collections remain the canonical renderer/bundling boundary;
- the remaining settings, notification, card, and disclosure defects are
  migrated or explicitly recorded as reviewed exceptions;
- the exception ledger contains only narrow cases with a technical reason and
  protected behavior;
- shared controls own focus, keyboard activation, disabled/loading, selected or
  expanded state, and accessibility behavior for their category;
- no unreviewed nested interactive markup or clickable non-interactive control
  remains in the focused inventory;
- automated enforcement rejects new unapproved bypasses in the audited
  categories;
- focused unit tests and the required Make targets pass;
- browser-dependent claims are backed by `make test-frontend-browser`.

All focused-scope conditions above are satisfied by the implementation and
verification evidence recorded in this document. The accepted non-goals below
remain separate follow-up work.

## Accepted exceptions and non-goals

An exception is not permission to keep arbitrary bespoke UI. It must be
reviewed and recorded. Likely approved specialized cases include:

- shared UI component internals;
- tab controls with correct tab semantics;
- sidebar backdrop buttons;
- table-library internal sort buttons;
- the draggable/resizable DevTools dialog surface;
- hidden file and color inputs;
- compact inline rename and hexadecimal-color inputs;
- stop-propagation-only wrappers that are not themselves actions;
- the two source-reviewed specialized notification list items recorded in the
  exception ledger.

This plan does not require a visual redesign, replacement of `FrostedCard`, or a
generic abstraction for every clickable surface. The objective is the smallest
coherent shared interaction model that provides correct semantics everywhere it
is used.
