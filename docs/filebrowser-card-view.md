# File Browser Card View

The file browser shows a folder's items, and the Trash's, as cards or as a
list; the header's view button switches between the two. In card view a
slider beside that button sets the card size, the way a file manager's zoom
does: each step makes a card wider, its icon bigger and the card taller
together. The model is FileBrowser Quantum's card view, whose column width and
icon size grow with one size setting while the card's height follows the
icon; LinuxIO keeps its own card (icon on the left, name, size and date beside
it) and its own seven steps.

## Size steps

`cardGeometry` (`-cardSize.ts`) maps a step to what it draws. The width is the
narrowest a card may get: as many cards as fit that width share a row, and
they share the row's spare width. The card's height is its icon plus its 8px
padding and 3px border on each side; rows are 12px apart.

| Step | Narrowest card | Icon | Card height |
|------|----------------|------|-------------|
| 1 | 180px | 64px | 86px |
| 2 | 220px | 72px | 94px |
| 3 (default) | 260px | 80px | 102px |
| 4 | 300px | 88px | 110px |
| 5 | 340px | 96px | 118px |
| 6 | 380px | 104px | 126px |
| 7 | 420px | 112px | 134px |

The smallest icon is as tall as the most text a card holds: four lines, the
name, a location (a Trash item's original folder, a search result's folder),
the size and the date. So the text never makes a card taller than its step,
and the virtual list (`VirtualItems` in `VirtualDirectoryItems.tsx`) lays
every row out at its exact height before the row is measured: rows do not
overlap, and the list's height does not change as rows scroll into view. The
list hands the step's icon size to its cards (`CardIconSizeContext`, read by
`FileCard`), so the row heights and the icons come from one step. A step
measures every row again, and the item the keyboard last moved to stays in
view, as it does when a resize changes the columns.

## The slider

The slider (`CardSizeSlider.tsx`) is a native range input named "Card size":
the arrow keys move a step, Home and End go to the smallest and the largest.
It sits in the header's view slot, before the view button, in a folder's
header (`FileBrowserHeader.tsx`) and in the Trash's (`TrashPlace.tsx`). On a
narrow screen a folder's header keeps it in the Actions menu, where a step
leaves the menu open, the slider keeps its own keys, and Up and Down move
through the menu's stops. The header does not scroll with the list, and the
slider is there in an empty folder and an empty Trash too. On a desktop
header that is short of room, the actions' column grows and the search moves
over rather than lying under them. The list view has no slider and is
unchanged by the size.

The slider is offered only where a step can change something: in card view,
when the area the list fills is wide enough for two cards of the smallest
step side by side, 380px (two 180px cards, the 12px gap and the list's 4px
padding on each side; `cardSizeAdjustable`). Narrower, every step would show
one card a row, so the slider is hidden and the cards show at the default
step. `useShownCardSize` measures that area for `FileBrowserContent.tsx` and
`TrashPlace.tsx` before the first paint and on every resize, so the rule
holds whether or not the folder has items.

## The saved size

The step is per-browser state, kept in localStorage under
`linuxio.filebrowserCardSize` beside the card/list choice
(`useFileViewState.ts`); the folders and the Trash share it. A stored value
that is not a whole step from 1 to 7 is ignored, and the default step is used.
Where the slider is hidden, the saved step is kept for a wider window.

## Known limits

- With a browser font size above the default, the four lines of a Trash or
  search-result card are taller than the icon of the smallest steps; those
  cards grow past their step and their rows are measured as they render.
- The rule measures the area the list fills, scrollbar included: in a list
  area from 380px to 388px wide that scrolls, one card fills a row at every
  step and the slider still changes the icon and the height.
