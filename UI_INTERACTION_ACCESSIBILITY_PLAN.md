# Frontend Shared UI Adoption and Accessibility Plan

Status: in progress. The initial button/shared-UI audit is complete; the first
direct-control migration, visible-focus styles, and a narrow source guard have
landed. The targeted semantic interaction batch described below is implemented
and passes the required frontend and browser Make targets. Broad card, icon,
and residual shared-UI adoption remain planned.

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

This batch deliberately does not perform the repository-wide Iconify migration,
the interactive-card restructuring, or the File Browser composite-widget
redesign. Those remain inventory-driven follow-up work, not prerequisites for
the confirmed semantic fixes.

## Primary goal: all feature UI uses the shared component system

Every frontend feature and route must compose LinuxIO's shared UI components.
Feature code must not create its own buttons, icon buttons, cards, fields,
selects, switches, menus, dialogs, tooltips, chips, progress indicators,
standard icons, or other reusable UI primitives when the shared system owns or
should own that responsibility.

The architectural boundary should be:

```text
routes and feature components
  -> LinuxIO shared UI components
    -> native HTML, Iconify, styling, focus behavior, and accessibility details
```

Native elements and third-party rendering libraries are implementation details
of the shared UI layer. They should not be independently restyled and given
parallel behavior throughout feature code.

Keyboard accessibility is an important reason for this policy, but it is not
the whole objective. Central ownership also keeps theme usage, responsive
behavior, loading and disabled states, icon sizing, interaction feedback, and
component APIs consistent across the application.

## Shared UI ownership policy

### Feature code must use shared components

The policy applies to production code under `frontend/src/routes` and feature
code under `frontend/src/components`, except for the implementation of an
approved shared primitive or specialized composite widget.

Feature code should use the appropriate canonical component, including:

- `AppButton`, `AppIconButton`, and `AppActionIconButton` for actions;
- shared link or link-button components for navigation;
- `FrostedCard`, `AppPaper`, `AppCardContent`, and the planned semantic card
  action component for surfaces;
- `AppTextField`, `AppSearchField`, `AppSelect`, `AppAutocomplete`,
  `AppCheckbox`, `AppSwitch`, and `AppFormControlLabel` for form controls;
- `AppDialog`, `AppFullscreenDialog`, `AppPopover`, `AppMenu`, `AppMenuItem`,
  and `AppTooltip` for overlays;
- `AppAlert`, `AppChip`, `StatusDot`, `AppSkeleton`, `AppCircularProgress`, and
  `AppLinearProgress` for feedback and status;
- `AppTypography`, `AppGrid`, `AppDivider`, and shared table components for
  common presentation;
- a shared icon component or icon registry for standard application glyphs.

The first implementation phase must document which existing components are
canonical. This does not require a barrel export if one would introduce cycles
or reduce type safety; it requires one clear ownership boundary and catalog.

### Feature code must not hand-roll primitives

Outside the approved shared layer, production feature code should not:

- render raw `<button>` elements or emulate them with clickable divs, spans, or
  list items;
- render raw text, search, select, checkbox, switch, or textarea controls when
  a shared field component can represent the behavior;
- import `@iconify/react` directly for ordinary application icons or duplicate
  icon sizing/color/accessibility rules;
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
drag surfaces, and complex file-manager composites. Standard glyphs should use
the shared icon component; a bespoke SVG or illustration should require a real
design need and a documented exception.

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
- direct `@iconify/react` imports and standard inline SVG/icon markup;
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
- direct standard-icon imports;
- clickable non-interactive elements;
- newly introduced bespoke card/menu/dialog/field patterns.

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

After the first migration, the reviewed feature-level exception ledger contains
four remaining button-role candidates across three files, while the 11
interactive `FrostedCard` call sites across ten files remain. Shared
focus-visible selectors now exist for all three primitives named above. The
current source guard prevents new raw controls and button roles, but it does not
yet enforce direct-icon imports, clickable non-interactive elements, nested
interactive markup, or interactive card usage. Those broader rules must wait
for the residual inventory and reviewed exceptions.

This document is an implementation plan, not evidence that browser behavior has
already been corrected or verified.

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

- a standard `AppIcon` component or icon registry that centralizes Iconify,
  sizes, inherited/theme color, decorative `aria-hidden`, and named-icon use;
- a valid shared link-button treatment for TanStack Router links and external
  anchors;
- a shared disclosure/header trigger;
- a semantic card action area for `FrostedCard` and other card surfaces;
- any field, menu, or status variants currently implemented repeatedly in
  feature code.

Add only gaps proven by the inventory. Prefer extending an existing component
when that keeps one clear ownership model without making its API ambiguous.

### 2.2 Visible focus treatment (implemented; browser verification pending)

A consistent `:focus-visible` style now exists in:

- `frontend/src/components/ui/app-button.css`
- `frontend/src/components/ui/app-icon-button.css`
- `frontend/src/components/ui/app-chip.css`

The selectors use theme tokens and do not depend on hover. Their contrast
against cards, dialogs, and navbar surfaces still requires browser verification
in light and dark modes. Do not remove outlines without an equivalent visible
replacement.

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

## Phase 3: Migrate direct feature-level UI bypasses

These sites already map cleanly to existing UI primitives and should not wait
for a new card abstraction.

### 3.1 Replace direct standard-icon usage

After the shared icon API exists, inventory every production import from
`@iconify/react`. Replace ordinary application glyph rendering with `AppIcon`
or the approved icon registry so feature code no longer owns:

- default icon sizes;
- theme and inherited color behavior;
- decorative versus named-icon accessibility attributes;
- repeated width/height props;
- loading, action, and status icon conventions.

Keep direct third-party rendering only inside the shared icon implementation or
for a documented capability the shared API deliberately does not support.
Bespoke illustrations and logos should use shared assets rather than be
recreated in feature components.

Migrate icons in bounded feature groups so the diff stays reviewable; do not
combine a repository-wide mechanical icon rewrite with unrelated behavior
changes.

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

### 4.2 Shared card action area

Add a semantic card action/trigger pattern rather than making `FrostedCard`
itself always interactive. The preferred composition is:

```text
FrostedCard (visual surface)
  -> semantic primary action or disclosure trigger
  -> independent action area containing AppButton/AppIconButton/input controls
  -> related expanded content
```

A simple card with no nested controls may use one full-card native trigger.
Cards containing actions or form fields must use a separate trigger region.

Before finalizing the API, prove it against one simple card and one card with
nested controls. Avoid a polymorphic abstraction that accepts arbitrary
elements without preserving their types or semantics.

### 4.3 Link-button support

If the inventory shows more than one link styled as a button, add a narrow shared
link-button component or shared style contract for TanStack Router links and
external anchors. Do not make `AppButton` broadly polymorphic unless that
materially simplifies real call sites while retaining type safety.

## Phase 5: Migrate disclosures, sorting, and mouse-only actions

The System Health, Sort Bar, LVM, Hardware, Docker, and Compose disclosure work
in Sections 5.1 and 5.2 is implemented in the current targeted batch and passes
the verification required below. Section 5.3 and the interactive-card phases
remain planned.

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

The two conditional interactive notification list items may remain specialized
list controls if tests confirm their current tab, Enter/Space, focus-visible,
and accessible-name behavior. Do not replace a specialized control solely to
increase `AppButton` usage.

## Phase 6: Migrate interactive cards in safe groups

### 6.1 Simple action cards

Start with cards whose whole content represents one action and which contain no
nested interactive descendants:

- `DockerStatCard.tsx`
- `DriveCard.tsx`, after resolving or separating its overlay action
- `WireguardInterfaceCard.tsx`
- `NetworkInterfaceCard.tsx`

Use the shared card action area and verify accessible names, focus order, hover,
selected state, and responsive layout.

### 6.2 Cards with nested actions or expanded content

Then migrate:

- `FilesystemCard.tsx`
- `UserCard.tsx`
- `UnitCard.tsx`
- `ContainerCard.tsx`

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

## Phase 7: Close the remaining shared-UI adoption gaps

Repeat the Phase 1 audit across every production frontend directory after the
planned migrations. This is broader than checking `role="button"` again. Review
all remaining:

- native controls and action-like links;
- direct icon-library imports and standard inline SVGs;
- custom surfaces, typography, fields, menus, overlays, feedback, loaders, and
  table/tab controls;
- feature-local components that duplicate an existing shared component;
- repeated inline/CSS interaction states;
- temporary compatibility wrappers introduced during migration.

For each remaining result, migrate it, close a demonstrated shared API gap, or
record a narrow approved exception. Do not leave a pattern merely because it
was outside the original button audit.

Once the residual list contains only reviewed exceptions, make the Phase 1
guard fail on new unapproved usage. Document the canonical alternative in each
diagnostic so contributors can fix violations without rediscovering the UI
architecture.

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

Browser behavior must remain described as unverified until the browser target
passes.

## Recommended implementation batches

1. Canonical component catalog, full feature-code inventory, and reviewed
   exception ledger.
2. Shared `AppIcon`, focus-visible foundation, link-button, disclosure, card
   action, and primitive tests.
3. Standard-icon migrations in bounded feature groups.
4. Direct button, field, menu, search, close-action, and release-notes link
   migrations.
5. `SystemHealth`, `SortBar`, `ComposeOperationDialog`, and shared disclosure
   migrations.
6. Simple card migrations.
7. Nested-action card migrations.
8. File-browser composite interaction, only after its semantic model is agreed.
9. Full residual shared-UI audit and activation of the regression guard.

Each batch should remain independently reviewable and should not combine broad
visual refactoring with semantic migration.

## Definition of done

This plan is complete only when:

- all production feature and route code uses canonical LinuxIO UI components
  for buttons, links, icons, cards, fields, selection controls, overlays,
  feedback, loading, typography, layout, tables, tabs, and other ordinary UI;
- no feature code directly implements a standard control or imports the
  standard icon renderer except through a documented exception;
- every demonstrated UI need is covered by an existing shared component, a
  small shared API extension, or a reviewed specialized composite;
- the exception ledger contains only narrow cases with a technical reason and
  protected behavior;
- shared components own theme, responsive, hover, focus, disabled, loading,
  selected, and accessibility behavior for their category;
- no nested interactive markup or clickable non-interactive control remains;
- automated enforcement rejects new unapproved shared-UI bypasses;
- focused unit tests and the required Make targets pass;
- browser-dependent claims are backed by `make test-frontend-browser`.

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
- specialized notification list items that pass the acceptance criteria.

This plan does not require a visual redesign, replacement of `FrostedCard`, or a
generic abstraction for every clickable surface. The objective is the smallest
coherent shared interaction model that provides correct semantics everywhere it
is used.
