import { DndContext } from "@dnd-kit/core";
import { rectSortingStrategy, SortableContext } from "@dnd-kit/sortable";
import type { ReactNode } from "react";

import SortableCard from "@/components/cards/SortableCard";
import AppVirtualGrid from "@/components/grid/AppVirtualGrid";
import AppGrid, { type GridSize } from "@/components/ui/AppGrid";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import {
  CARD_LIFT_SHADOW_GUTTER,
  DASHBOARD_CARD_SPACING,
  GAP_XS,
  HOVER_LIFT_HEADROOM,
} from "@/theme/constants";

interface ReorderableCardGridProps<TItem> {
  /** Accessible name for the grid. `virtualized` only. */
  ariaLabel?: string;
  /** Equal-width columns at each breakpoint. When present, each card spans one column. */
  columns?: GridSize;
  /**
   * Scroll the cards inside the grid rather than growing the page, the way
   * `fillAvailable` works on AppVirtualGrid and AppDataTable. Set it where the
   * grid is a route's whole surface — a tab panel — so its chrome stays put and
   * the view reads the same as the table it toggles with. Leave it off where
   * the grid is one section stacked among others, which scrolls with the page.
   */
  fillAvailable?: boolean;
  getId: (item: TItem) => string;
  /**
   * Replaces the default flat grid with a caller-built layout — grouped
   * sections, say — while the hold-to-reorder arming stays here: a card is only
   * draggable if it is rendered through the `renderCard` this receives. Chrome
   * the caller adds around or between cards simply isn't draggable. The
   * `fillAvailable` scrollport still applies. Non-virtualized grids only.
   */
  renderBody?: (
    renderCard: (item: TItem, index: number) => ReactNode,
  ) => ReactNode;
  /** Rendered inside the sortable wrapper, one call per item. */
  renderItem: (item: TItem, index: number) => ReactNode;
  /**
   * Overrides the `SortableContext` items when `renderBody` lays out composite
   * sortables — a stack band whose drag id isn't in `surface.ids`. Defaults to
   * `surface.ids`.
   */
  sortableIds?: string[];
  /** Breakpoint spans for each card. Ignored when `virtualized`. */
  size?: GridSize;
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
  ariaLabel,
  columns,
  emptyMessage,
  estimateItemHeight,
  fillAvailable = false,
  getId,
  items,
  minItemWidth,
  renderBody,
  renderItem,
  size,
  sortableIds,
  spacing = DASHBOARD_CARD_SPACING,
  surface,
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
    const grid = renderBody ? (
      renderBody(renderSortableCard)
    ) : (
      <AppGrid columns={columns} container spacing={spacing}>
        {rendered.map((item, index) => (
          <AppGrid key={getId(item)} size={columns ? 1 : size}>
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
          overflow: "auto",
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
        items={sortableIds ?? surface.ids}
        strategy={rectSortingStrategy}
      >
        {body}
      </SortableContext>
    </DndContext>
  );
}

export default ReorderableCardGrid;
