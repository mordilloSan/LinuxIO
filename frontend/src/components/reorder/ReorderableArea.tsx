import { DndContext } from "@dnd-kit/core";
import {
  rectSortingStrategy,
  SortableContext,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import type { ReactNode } from "react";

import type { ReorderableSurface } from "@/hooks/useReorderableSurface";

interface ReorderableAreaProps<TItem> {
  children: ReactNode;
  surface: ReorderableSurface<TItem>;
  /** "grid" for wrapping card layouts, "list" for stacked rows. */
  layout?: "grid" | "list";
}

/**
 * Mounts a surface's drag context around arbitrary markup. Use it for layouts
 * `ReorderableCardGrid` doesn't cover — a virtualized grid, a bespoke column —
 * and wrap each item in a `SortableCard` inside.
 *
 * The context is always mounted, even outside layout mode: dnd-kit has to see
 * the press that opens the mode.
 */
function ReorderableArea<TItem>({
  children,
  layout = "grid",
  surface,
}: ReorderableAreaProps<TItem>) {
  return (
    <DndContext {...surface.dndContextProps}>
      <SortableContext
        items={surface.ids}
        strategy={
          layout === "grid" ? rectSortingStrategy : verticalListSortingStrategy
        }
      >
        {children}
      </SortableContext>
    </DndContext>
  );
}

export default ReorderableArea;
