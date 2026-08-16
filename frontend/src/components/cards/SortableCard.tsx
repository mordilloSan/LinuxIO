import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import type { CSSProperties, ReactNode } from "react";

import { REORDER_HOLD_MS } from "@/constants/reorder";
import { useAppTheme } from "@/theme";
import { cardBorderRadius } from "@/theme/constants";

import "./FrostedCard.css";
import "../reorder/reorder.css";

interface SortableCardProps {
  children: ReactNode;
  /** Layout mode is open: the card shows its drag affordance and eats clicks. */
  editMode: boolean;
  id: string;
  /** Unarms the card entirely — no hold, no drag. */
  disabled?: boolean;
  /** This card is being held, waiting for the hold to complete. */
  pending?: boolean;
}

const SortableCard = ({
  children,
  disabled = false,
  editMode,
  id,
  pending = false,
}: SortableCardProps) => {
  const theme = useAppTheme();
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id, disabled });

  const holding = pending && !editMode;

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    position: "relative",
    borderRadius: cardBorderRadius,
    // This wrapper sits between a stretched grid cell and a card that sizes
    // itself with height:100%. Without its own height it collapses to the
    // card's content and every card in the row stops matching its neighbours.
    height: "100%",
    // Declared on the wrapper rather than on the ring so that whatever the card
    // chooses to animate for the hold — the ring, its own accent line — reads
    // the same timing and colour by inheritance.
    "--reorder-hold-color": theme.palette.primary.main,
    "--reorder-hold-ms": `${REORDER_HOLD_MS}ms`,
  } as CSSProperties;

  return (
    // The listeners live on the card itself, not just on the edit-mode overlay:
    // holding anywhere on a card is what opens layout mode in the first place.
    <div
      className={holding ? "sc-hold" : undefined}
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
    >
      {holding && <div className="reorder-hold-ring" />}
      {editMode && (
        <div
          className="sc-drag-overlay"
          style={
            {
              position: "absolute",
              top: 0,
              left: 0,
              right: 0,
              bottom: 0,
              zIndex: 10,
              cursor: isDragging ? "grabbing" : "grab",
              touchAction: "none",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              borderRadius: cardBorderRadius,
              "--sc-hover-bg": theme.palette.action.hover,
            } as CSSProperties
          }
        >
          <Icon
            height={40}
            icon="mdi:drag"
            style={{ color: theme.palette.text.secondary, opacity: 0.7 }}
            width={40}
          />
        </div>
      )}
      {children}
    </div>
  );
};

export default SortableCard;
