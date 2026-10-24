import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { CSSProperties, KeyboardEvent } from "react";

import { REORDER_HOLD_MS } from "@/components/reorder/constants";

/**
 * Sortable wiring for one navigation entry, shared by the sidebar and the dock. The listeners sit on the `<li>` so
 * holding anywhere on the entry picks it up, while the link inside keeps its
 * link semantics instead of taking dnd-kit's button role and extra tab stop.
 */
export function useSortableNavItem(id: string, disabled: boolean) {
  const {
    attributes,
    isDragging,
    listeners,
    setNodeRef,
    transform,
    transition,
  } = useSortable({ id, disabled });

  return {
    /** dnd-kit's keyboard instructions, for the link to describe itself with. */
    describedBy: attributes["aria-describedby"],
    itemProps: {
      ...listeners,
      // Enter has to keep following the link, so only Space starts a keyboard drag.
      onKeyDown: (event: KeyboardEvent<HTMLLIElement>) => {
        if (event.key === " ") listeners?.onKeyDown?.(event);
      },
      ref: setNodeRef,
      style: {
        transform: CSS.Translate.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : undefined,
        position: "relative",
        zIndex: isDragging ? 5 : undefined,
        "--reorder-hold-color": "var(--app-palette-primary-main)",
        "--reorder-hold-ms": `${REORDER_HOLD_MS}ms`,
      } as CSSProperties,
    },
  };
}
