import type { ReactNode } from "react";

import SortableCard from "@/components/cards/SortableCard";
import ReorderableArea from "@/components/reorder/ReorderableArea";
import AppGrid, { type GridSize } from "@/components/ui/AppGrid";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import {
  CARD_LIFT_SHADOW_GUTTER,
  DASHBOARD_CARD_SPACING,
  HOVER_LIFT_HEADROOM,
} from "@/theme/constants";

interface ReorderableCardGridProps<TItem> {
  /** Equal-width columns at each breakpoint. When present, each card spans one column. */
  columns?: GridSize;
  /** Leaves the rendered cards interactive without arming hold-to-reorder. */
  disableReordering?: boolean;
  /**
   * Scroll the cards inside the grid rather than growing the page, the way
   * `fillAvailable` works on AppVirtualGrid and AppDataTable. Set it where the
   * grid is a route's whole surface — a tab panel — so its chrome stays put and
   * the view reads the same as the table it toggles with. Leave it off where
   * the grid is one section stacked among others, which scrolls with the page.
   */
  fillAvailable?: boolean;
  getId: (item: TItem) => string;
  /** Rendered inside the sortable wrapper, one call per item. */
  renderItem: (item: TItem, index: number) => ReactNode;
  size: GridSize;
  surface: ReorderableSurface<TItem>;
  /**
   * Items to render. Defaults to `surface.items`; pass a filtered slice when a
   * search box is in play — the saved order still spans the whole list.
   */
  items?: readonly TItem[];
  spacing?: number;
}

/**
 * A card grid whose cards can be rearranged by holding one of them. The grid is
 * always wrapped in a live `DndContext`: the hold that opens layout mode has to
 * be seen by dnd-kit before the mode exists, so there is no unarmed variant of
 * this component.
 */
function ReorderableCardGrid<TItem>({
  columns,
  disableReordering = false,
  fillAvailable = false,
  getId,
  items,
  renderItem,
  size,
  spacing = DASHBOARD_CARD_SPACING,
  surface,
}: ReorderableCardGridProps<TItem>) {
  const rendered = items ?? surface.items;

  const grid = (
    <AppGrid columns={columns} container spacing={spacing}>
      {rendered.map((item, index) => {
        const id = getId(item);
        return (
          <AppGrid key={id} size={columns ? 1 : size}>
            <SortableCard
              disabled={disableReordering}
              editMode={surface.editMode}
              id={id}
              pending={surface.pendingId === id}
            >
              {renderItem(item, index)}
            </SortableCard>
          </AppGrid>
        );
      })}
    </AppGrid>
  );

  return (
    <ReorderableArea surface={surface}>
      {fillAvailable ? (
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
      )}
    </ReorderableArea>
  );
}

export default ReorderableCardGrid;
