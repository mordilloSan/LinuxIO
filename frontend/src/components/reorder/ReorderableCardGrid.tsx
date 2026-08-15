import type { ReactNode } from "react";

import SortableCard from "@/components/cards/SortableCard";
import ReorderableArea from "@/components/reorder/ReorderableArea";
import AppGrid, { type GridSize } from "@/components/ui/AppGrid";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import { DASHBOARD_CARD_SPACING } from "@/theme/constants";

interface ReorderableCardGridProps<TItem> {
  /** Equal-width columns at each breakpoint. When present, each card spans one column. */
  columns?: GridSize;
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
  getId,
  items,
  renderItem,
  size,
  spacing = DASHBOARD_CARD_SPACING,
  surface,
}: ReorderableCardGridProps<TItem>) {
  const rendered = items ?? surface.items;

  return (
    <ReorderableArea surface={surface}>
      <AppGrid columns={columns} container spacing={spacing}>
        {rendered.map((item, index) => {
          const id = getId(item);
          return (
            <AppGrid key={id} size={columns ? 1 : size}>
              <SortableCard
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
    </ReorderableArea>
  );
}

export default ReorderableCardGrid;
