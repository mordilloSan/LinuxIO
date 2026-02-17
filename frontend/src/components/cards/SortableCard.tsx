import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { DragIndicator } from "@mui/icons-material";
import { Box } from "@mui/material";
import React from "react";

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
    <Box ref={setNodeRef} style={style} {...attributes}>
      {editMode && (
        <Box
          {...listeners}
          sx={{
            position: "absolute",
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            zIndex: 10,
            cursor: isDragging ? "grabbing" : "grab",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            borderRadius: 1,
            bgcolor: "rgba(0,0,0,0.05)",
            "&:hover": {
              bgcolor: "rgba(0,0,0,0.1)",
            },
          }}
        >
          <DragIndicator
            sx={{ fontSize: 40, color: "text.secondary", opacity: 0.7 }}
          />
        </Box>
      )}
      {children}
    </Box>
  );
};

export default SortableCard;
