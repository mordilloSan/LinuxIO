import { DndContext, DragOverlay, useDndMonitor } from "@dnd-kit/core";
import { rectSortingStrategy, SortableContext } from "@dnd-kit/sortable";
import { motion } from "motion/react";
import { useState, type ReactNode } from "react";

import SortableCard from "@/components/cards/SortableCard";
import AppVirtualGrid from "@/components/grid/AppVirtualGrid";
import AppGrid, { type GridSize } from "@/components/ui/AppGrid";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import {
  CARD_LIFT_SHADOW_GUTTER,
  DASHBOARD_CARD_SPACING,
  EASING_STANDARD,
  GAP_XS,
  HOVER_LIFT_HEADROOM,
  TRANSITION_DURATION_STANDARD_MS,
} from "@/theme/constants";

const LAYOUT_TRANSITION = {
  duration: TRANSITION_DURATION_STANDARD_MS / 500,
  ease: EASING_STANDARD,
};

// In overlay mode the caller previews a drag by re-rendering the grid with a
// provisional order, so the resting items' slots are shown by real layout —
// at their real widths — and strategy transforms would only fight that
// reflow with stale drag-start rects.
const staticSortingStrategy = () => null;

// Rendered inside the DndContext so it can watch the active drag. The overlay
// is a fixed-size copy of the dragged item that follows the pointer, leaving
// the source card in the grid as the dimmed ghost at wherever the layout puts
// it — the pairing dnd-kit expects when a drag is previewed by re-rendering
// rather than by strategy transforms.
function CardGridDragOverlay({
  render,
}: {
  render: (activeId: string) => ReactNode;
}) {
  const [activeId, setActiveId] = useState<string | null>(null);
  useDndMonitor({
    onDragStart: (event) => setActiveId(String(event.active.id)),
    onDragEnd: () => setActiveId(null),
    onDragCancel: () => setActiveId(null),
  });
  return <DragOverlay>{activeId ? render(activeId) : null}</DragOverlay>;
}

interface ReorderableCardGridProps<TItem> {
  /**
   * Slide resting cards to their new slots (framer FLIP) whenever the grid
   * repacks — a stack collapsing, or the mid-drag reflow. Only for grids that
   * preview drags by reflowing (`renderDragOverlay`): under strategy
   * transforms the same movement is already animated by dnd-kit, and the two
   * would fight.
   */
  animateLayout?: boolean;
  /** Accessible name for the grid. `virtualized` only. */
  ariaLabel?: string;
  /** Equal-width columns at each breakpoint. When present, each card spans one column. */
  columns?: GridSize;
  /**
   * Backfill the row gap a wide item (a stack band) leaves with the items that
   * follow it, via `grid-auto-flow: dense`. A no-op while every item is
   * card-sized. Ignored when `virtualized`.
   */
  dense?: boolean;
  /**
   * Scroll the cards inside the grid rather than growing the page, the way
   * `fillAvailable` works on AppVirtualGrid and AppVirtualTable. Set it where the
   * grid is a route's whole surface — a tab panel — so its chrome stays put and
   * the view reads the same as the table it toggles with. Leave it off where
   * the grid is one section stacked among others, which scrolls with the page.
   */
  fillAvailable?: boolean;
  getId: (item: TItem) => string;
  /**
   * Per-item span, overriding `size` — how one grid mixes card-sized items
   * with wider ones (stack bands). Mixed sizes are also why SortableCard
   * applies only the translation half of its drag transform. Ignored when
   * `virtualized` or `columns` is set.
   */
  getItemSize?: (item: TItem, index: number) => GridSize;
  /**
   * A fixed-size copy of the dragged item, rendered in a `DragOverlay` that
   * follows the pointer. Pair it with a live `onDragOver` reorder on the
   * surface's dnd props: the grid then previews the drag by reflowing for
   * real, which is how variable-width items keep their true widths mid-drag.
   * Must be presentational — no sortable hooks.
   */
  renderDragOverlay?: (activeId: string) => ReactNode;
  /** Rendered inside the sortable wrapper, one call per item. */
  renderItem: (item: TItem, index: number) => ReactNode;
  /** Breakpoint spans for each card. Ignored when `virtualized`. */
  size?: GridSize;
  /**
   * Park the `animateLayout` FLIP without unmounting the motion cells (the
   * cells must never remount mid-drag — that kills the drag). Set while
   * layout mode is open: a drag commits a reflow per over-change, and
   * animations restarting that fast read as jumping, their mid-flight
   * transforms poking past the scrollport. Toggles still animate — they
   * happen outside layout mode.
   */
  suspendLayoutAnimation?: boolean;
  surface: ReorderableSurface<TItem>;
  /**
   * Items to render. Defaults to `surface.items`; pass a filtered slice when a
   * search box is in play — the saved order still spans the whole list.
   */
  items?: readonly TItem[];
  spacing?: number;
  /** Shown in place of the cards when there are none. `virtualized` only. */
  emptyMessage?: string;
  /** Row-height hint for the virtualizer. `virtualized` only. */
  estimateItemHeight?: number;
  /** Column width floor. `virtualized` only. */
  minItemWidth?: number;
  /**
   * Render through `AppVirtualGrid` instead of `AppGrid`, for lists long enough
   * that mounting every card costs — the services list is the case. The trade
   * is that only the cards the virtualizer has mounted are drop targets, so a
   * long list is rearranged in steps, scrolling between drags. Cards are then
   * sized by `minItemWidth`/`estimateItemHeight` rather than `size`/`columns`,
   * and the virtualizer owns the scrollport, so `fillAvailable` passes straight
   * through to it.
   */
  virtualized?: boolean;
}

/**
 * A card grid whose cards can be rearranged by holding one of them. The grid is
 * always wrapped in a live `DndContext`: the hold that opens layout mode has to
 * be seen by dnd-kit before the mode exists, so there is no unarmed variant of
 * this component.
 */
function ReorderableCardGrid<TItem>({
  animateLayout = false,
  ariaLabel,
  columns,
  dense = false,
  emptyMessage,
  estimateItemHeight,
  fillAvailable = false,
  getId,
  getItemSize,
  items,
  minItemWidth,
  renderDragOverlay,
  renderItem,
  size,
  spacing = DASHBOARD_CARD_SPACING,
  surface,
  suspendLayoutAnimation = false,
  virtualized = false,
}: ReorderableCardGridProps<TItem>) {
  const rendered = items ?? surface.items;

  // The one place a card is armed for the hold gesture. Both layouts go through
  // it, so a route never wires SortableCard itself.
  const renderSortableCard = (item: TItem, index: number) => (
    <SortableCard
      editMode={surface.editMode}
      id={getId(item)}
      pending={surface.pendingId === getId(item)}
    >
      {renderItem(item, index)}
    </SortableCard>
  );

  let body: ReactNode;
  if (virtualized) {
    body = (
      <AppVirtualGrid
        ariaLabel={ariaLabel}
        emptyMessage={emptyMessage}
        estimateItemHeight={estimateItemHeight}
        fillAvailable={fillAvailable}
        gap={spacing * GAP_XS}
        getItemKey={(item) => getId(item)}
        items={rendered as TItem[]}
        minItemWidth={minItemWidth}
        padding={0}
        renderItem={renderSortableCard}
      />
    );
  } else {
    const grid = (
      <AppGrid
        columns={columns}
        container
        spacing={spacing}
        style={dense ? { gridAutoFlow: "row dense" } : undefined}
      >
        {rendered.map((item, index) => (
          <AppGrid
            key={getId(item)}
            size={columns ? 1 : (getItemSize?.(item, index) ?? size)}
            {...(animateLayout && {
              component: motion.div,
              layout: suspendLayoutAnimation ? false : "position",
              transition: LAYOUT_TRANSITION,
            })}
          >
            {renderSortableCard(item, index)}
          </AppGrid>
        ))}
      </AppGrid>
    );
    body = fillAvailable ? (
      <div
        className="custom-scrollbar"
        style={{
          flex: "1 1 0",
          minHeight: 0,
          minWidth: 0,
          // Never sideways: the cards are fluid-width, so any horizontal
          // overflow is transient animation spill, and the scrollbar it
          // summons would itself shift the layout.
          overflowX: "hidden",
          overflowY: "auto",
          /*
            This scrollport clips, so the room a hover-lifted card needs is
            reserved inside it and handed straight back by the matching
            negative margin — the cards land exactly where they did before,
            and the space they rise into now travels with them. Same trade
            AppVirtualGrid makes; the top headroom is the gap the tab strip
            leaves below itself (--tab-strip-headroom).
          */
          paddingTop: HOVER_LIFT_HEADROOM,
          marginTop: -HOVER_LIFT_HEADROOM,
          paddingInline: CARD_LIFT_SHADOW_GUTTER,
          marginInline: -CARD_LIFT_SHADOW_GUTTER,
          // Not reclaimed: the last row's shadow falls below the cards, and
          // scrolling a little past the end is what a list should do anyway.
          paddingBottom: CARD_LIFT_SHADOW_GUTTER,
        }}
      >
        {grid}
      </div>
    ) : (
      grid
    );
  }

  return (
    <DndContext {...surface.dndContextProps}>
      <SortableContext
        items={surface.ids}
        strategy={
          renderDragOverlay ? staticSortingStrategy : rectSortingStrategy
        }
      >
        {body}
      </SortableContext>
      {renderDragOverlay && <CardGridDragOverlay render={renderDragOverlay} />}
    </DndContext>
  );
}

export default ReorderableCardGrid;
