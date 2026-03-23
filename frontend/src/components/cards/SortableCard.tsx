import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import React from "react";

import { useAppTheme } from "@/theme";

import "./frosted-card.css";

import { cardBorderRadius } from "@/theme/constants";

interface SortableCardProps {
  id: string;
  editMode: boolean;
  children: React.ReactNode;
}

const SortableCard: React.FC<SortableCardProps> = ({
  id,
  editMode,
  children,
}) => {
  const theme = useAppTheme();
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id, disabled: !editMode });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    position: "relative",
  };

  return (
    <div ref={setNodeRef} style={style} {...attributes}>
      {editMode && (
        <div
          {...listeners}
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
            } as React.CSSProperties
          }
        >
          <Icon
            icon="mdi:drag"
            width={40}
            height={40}
            style={{ color: theme.palette.text.secondary, opacity: 0.7 }}
          />
        </div>
      )}
      {children}
    </div>
  );
};

export default SortableCard;
