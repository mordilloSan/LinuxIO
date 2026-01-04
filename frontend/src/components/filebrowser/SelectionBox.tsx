import { useTheme } from "@mui/material/styles";
import React from "react";

interface SelectionBoxProps {
  left: number;
  top: number;
  width: number;
  height: number;
}

/**
 * Visual overlay for marquee selection box
 */
const SelectionBox: React.FC<SelectionBoxProps> = ({
  left,
  top,
  width,
  height,
}) => {
  const theme = useTheme();

  return (
    <div
      style={{
        position: "absolute",
        left: `${left}px`,
        top: `${top}px`,
        width: `${width}px`,
        height: `${height}px`,
        border: `2px solid ${theme.palette.primary.main}`,
        backgroundColor:
          "color-mix(in srgb, var(--mui-palette-primary-main), transparent 90%)",
        pointerEvents: "none",
        zIndex: 1000,
        borderRadius: "4px",
      }}
    />
  );
};

export default SelectionBox;
