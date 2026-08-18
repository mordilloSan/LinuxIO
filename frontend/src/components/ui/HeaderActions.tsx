import { Fragment, type ReactNode } from "react";

/**
 * The one order route action icons appear in, listed from the right edge of
 * the header inward. Every route reads the same left-to-right, so the icon a
 * given position holds never changes as you move between routes:
 *
 *   … options · maintenance · bulk · refresh · create · view
 *
 * The card/table toggle owns the rightmost slot because it is the control
 * users reach for most and the one every list route has; destructive and
 * bulk work sits furthest from it.
 */
const HEADER_ACTION_SLOTS = [
  /** Display and tooling switches: hidden files, indexing, copy, download. */
  "options",
  /** Cleanup that removes data: prune, clear. */
  "maintenance",
  /** Acts on every listed item at once: start all, stop all, update all. */
  "bulk",
  /** Re-reads the current view: refresh, check for updates. */
  "refresh",
  /** Adds a new item: create, add, mount. */
  "create",
  /** Card/table view toggle. Always the rightmost icon. */
  "view",
] as const;

export type HeaderActionSlot = (typeof HEADER_ACTION_SLOTS)[number];

export type HeaderActionsProps = Partial<Record<HeaderActionSlot, ReactNode>>;

/**
 * Orders a route's header icons. Pass each icon (or a fragment of icons that
 * belong to one slot) by name; the slot decides where it lands, so the call
 * site cannot drift out of the shared order. Renders DOM in visual order, so
 * tabbing through the icons follows what is on screen.
 */
const HeaderActions = (slots: HeaderActionsProps) => (
  <>
    {HEADER_ACTION_SLOTS.map((slot) => (
      <Fragment key={slot}>{slots[slot]}</Fragment>
    ))}
  </>
);

export default HeaderActions;
