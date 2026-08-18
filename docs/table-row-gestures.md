# Table Row Gestures

One row-interaction contract for every data table in the app. It lives in the two
table primitives — [`AppDataTable`](../frontend/src/components/tables/AppDataTable.tsx)
and [`AppVirtualDataTable`](../frontend/src/components/tables/AppVirtualDataTable.tsx) —
so a table declares which gestures it supports and gets the behaviour, the
guards and the animations for free. Do not re-implement any of this in a route
component.

The implementation is shared: the gesture handlers, header, cells and table
chrome live in
[`tableShared.tsx`](../frontend/src/components/tables/tableShared.tsx), and the
event predicates in
[`rowInteraction.ts`](../frontend/src/components/tables/rowInteraction.ts). The
two primitives own only what genuinely differs — body and row rendering.

## The gestures

| Gesture | Does | A table opts in by |
|---|---|---|
| Click | Opens/closes the row's detail panel | passing `renderExpandedContent` |
| Click | Instead runs the table's own row action (open a detail view, select) | passing `onRowClick` |
| Long press | Enters layout mode, then drags to reorder | passing `dnd` |
| Double click | The table's second row action — normally toggling that row's selection | passing `onRowDoubleClick` |
| Ctrl/Cmd-A | Selects every row the filter and sort leave visible | passing `onSelectAll` |
| Escape | Collapses every expanded row | passing `renderExpandedContent` |
| Escape again | Clears the selection | passing `onClearSelection` |

Ctrl/Cmd-A stands down while the user is typing, so a search field keeps its
native select-all (`isTypingTarget`), and it ignores Shift and Alt so combos like
Ctrl+Shift+A stay inert. It reads the visible rows off the table instance and
hands their ids to `onSelectAll`; the page owns the selection.

## Showing selection

Selected rows are tinted, not checkboxed. `selectedRowIds` takes the whole
selected set and `selectedRowId` the single open/focused row; both paint the same
`--app-dt-selected-bg`, which is the theme's `primary.main` at 10% (15% in dark).

The tint is applied as the row's *own* surface rather than an overlay, so zebra
striping stays legible underneath it and hover brightens the tinted surface
instead of replacing it — a selected row still responds to the pointer and still
reads as selected. That derivation is shared with `.app-table-row` and
`.file-row`, so every row in the app responds alike.

A checkbox column is not the way to do this. It costs a column of width on every
row, and its cell has to close over the selection — which makes the column defs
volatile and reintroduces the remount hazard below.

Dropping the checkbox does cost the affordance, though: double click and
Ctrl/Cmd-A leave nothing on screen to find, and the tint is only feedback once
you know the gesture. That is an open tradeoff — a selectable table currently
teaches its gestures nowhere.

## Rules, and why they are rules

**A row has exactly one click action.** `onRowClick` wins over expand-on-click:
a table that gives the click another meaning keeps it, and its chevron stays the
only way to expand. Tables that open a detail view (containers, services, users)
therefore never expand on click, and tables with an inline panel (shares, logs,
compose, update history, groups) always do. Deciding this per table rather than
stacking both keeps the row from having two competing primary actions.

**The whole row is the disclosure control, not just the chevron.** Expandable
rows get `cursor: pointer`, the shared hover brighten, and `aria-expanded`. The
chevron stays because it is the keyboard target and the visible affordance.

**Clicks that land on a control belong to the control.** Buttons, links, inputs,
labels, `role="button"`/`checkbox"`/`menuitem"`/`switch"`, and copy-on-click
cells never trigger a row gesture — see `targetIsRowControl`. A click that ends a
text selection is also left alone, so drag-selecting an ID inside a row does not
collapse the row you are reading.

**A press on a control never arms the reorder hold.** Arming re-renders the
surface into its pending state, and a table that rebuilds its column defs on that
render replaces the pressed control's DOM node — which silently swallows the
click, so a checkbox never toggles. `rowBodyDragListeners` wraps every dnd-kit
sensor activator with the control check to prevent it. The deeper hazard is
unstable column defs; see the note below.

**Layout mode owns the press.** While `dnd.editing` is open, a click neither
expands nor selects — the press belongs to the drag.

**A click waits only when it has to.** When a table binds *both* a click action
and `onRowDoubleClick`, the click is deferred by `ROW_DOUBLE_CLICK_MS` and
cancelled if a double click follows; otherwise the first click of a double click
would open the panel and the second would close it. Tables that bind one gesture
act immediately, so the delay is confined to the tables that need it. The timer
lives on the table, not the row, because rows remount and virtualized rows
unmount as they scroll.

**Escape peels one layer at a time.** Expanded panels first, then the selection,
so two presses return a table to rest. A table with nothing expanded skips
straight to clearing the selection rather than consuming a press on nothing. The
handler stands down when a dialog is open (`OVERLAY_ROOT_SELECTOR`, the same
guard the filebrowser keyboard hooks use) or when the press is already
`defaultPrevented`, and marks the press handled once it acts so nested tables and
page-level handlers leave it alone.

**Binding a double click costs the word-selection gesture.** On a table that
binds `onRowDoubleClick`, the row default-prevents the *second* mousedown of a
double click, which is what would start the browser's word selection — otherwise
every row gesture leaves a stray blue highlight over the row it just acted on.
Suppressing the selection rather than clearing it afterwards means the highlight
never flashes up. Single-click drag-selection is untouched, so copying an id out
of a cell still works, and a press on a control inside the row is exempt. Only
bind the gesture where selection is the table's real second action.

## Column defs must be stable

`renderExpandedContent`, cells and headers render through `flexRender`, which is
`React.createElement` on the value you pass. A `cell` defined inline is a **new
component type every render**, so rebuilding the column array unmounts and
remounts every cell subtree. That breaks in-cell state and animation, and it
breaks any click that is mid-flight when the re-render lands.

Keep the column array stable and pass volatile per-row state (selection, pending
flags, expansion sets) through context, the way
[`ContainerTable`](../frontend/src/routes/_authenticated/docker/-components/ContainerTable.tsx)
does. `meta.getCellRenderKey` then narrows which cells re-render.

## Adding a table

1. Pick the click action: `renderExpandedContent` for an inline panel, or
   `onRowClick` for a detail view. Not both.
2. Add `dnd` only if the row order is the user's to save.
3. Add `onRowDoubleClick` only if the table has a genuine second row action, and
   accept the click delay and the loss of word selection.
4. If rows can be selected, pass `selectedRowIds` so the selection is visible,
   `onSelectAll` for Ctrl/Cmd-A, and `onClearSelection` so Escape can clear it.
5. Define the columns outside the render path, or memoize them, and pass
   per-row state through context.
