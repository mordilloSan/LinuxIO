import {
  ArrowDownward as ArrowDownwardIcon,
  ArrowUpward as ArrowUpwardIcon,
} from "@mui/icons-material";
import { Box, Typography, alpha, useTheme } from "@mui/material";
import React from "react";

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";

export interface SortBarProps {
  sortOrder: SortOrder;
  onSortChange: (field: SortField) => void;
}

const SortBar: React.FC<SortBarProps> = ({ sortOrder, onSortChange }) => {
  const theme = useTheme();
  const [hoveredField, setHoveredField] = React.useState<SortField | null>(
    null,
  );

  const renderSortIcon = (field: SortField) => {
    const isHovered = hoveredField === field;

    // Only show icons on hover
    if (!isHovered) return null;

    const iconStyles = {
      fontSize: "1.15rem",
      ml: 1,
      transition: "opacity 0.2s ease",
      opacity: 0.8,
    };

    // If hovering show current sort direction
    return sortOrder === "asc" ? (
      <ArrowUpwardIcon sx={iconStyles} />
    ) : (
      <ArrowDownwardIcon sx={iconStyles} />
    );
  };

  const columnStyle = {
    display: "flex",
    alignItems: "center",
    cursor: "pointer",
    userSelect: "none" as const,
    py: 3,
    px: 4,
    transition: "background-color 0.2s ease",
  };

  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: "1fr 150px 200px",
        backgroundColor: theme.palette.mode === "dark" ? "#20292f" : "#ffffff",
        border: `0.1px solid ${alpha(theme.palette.divider, theme.palette.mode === "dark" ? 0.15 : 0.1)}`,
        borderRadius: 2,
      }}
    >
      <Box
        sx={columnStyle}
        onClick={() => onSortChange("name")}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            onSortChange("name");
          }
        }}
        onMouseEnter={() => setHoveredField("name")}
        onMouseLeave={() => setHoveredField(null)}
      >
        <Typography
          variant="h6"
          sx={{ display: "flex", alignItems: "center", fontSize: "0.9rem" }}
        >
          Name
          {renderSortIcon("name")}
        </Typography>
      </Box>

      <Box
        sx={columnStyle}
        onClick={() => onSortChange("size")}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            onSortChange("size");
          }
        }}
        onMouseEnter={() => setHoveredField("size")}
        onMouseLeave={() => setHoveredField(null)}
      >
        <Typography
          variant="h6"
          sx={{ display: "flex", alignItems: "center", fontSize: "0.9rem" }}
        >
          Size
          {renderSortIcon("size")}
        </Typography>
      </Box>

      <Box
        sx={columnStyle}
        onClick={() => onSortChange("modTime")}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            onSortChange("modTime");
          }
        }}
        onMouseEnter={() => setHoveredField("modTime")}
        onMouseLeave={() => setHoveredField(null)}
      >
        <Typography
          variant="h6"
          sx={{ display: "flex", alignItems: "center", fontSize: "0.9rem" }}
        >
          Last modified
          {renderSortIcon("modTime")}
        </Typography>
      </Box>
    </Box>
  );
};

export default SortBar;
