import { Icon } from "@iconify/react";
import React from "react";

import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { getSubtleDividerColor } from "@/theme/surfaces";

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";

export interface SortBarProps {
  sortOrder: SortOrder;
  onSortChange: (field: SortField) => void;
}

const SortBar: React.FC<SortBarProps> = ({ sortOrder, onSortChange }) => {
  const theme = useAppTheme();
  const [hoveredField, setHoveredField] = React.useState<SortField | null>(
    null,
  );
  // Allow numeric columns to shrink on smaller widths while keeping alignment in sync with rows
  const columnTemplate =
    "minmax(0, 1fr) clamp(80px, 16vw, 140px) clamp(120px, 22vw, 200px)";

  const renderSortIcon = (field: SortField) => {
    const isHovered = hoveredField === field;

    // Only show icons on hover
    if (!isHovered) return null;

    // If hovering show current sort direction
    return sortOrder === "asc" ? (
      <Icon
        icon="mdi:arrow-up"
        width={18}
        height={18}
        style={{ marginLeft: 4, transition: "opacity 0.2s ease", opacity: 0.8 }}
      />
    ) : (
      <Icon
        icon="mdi:arrow-down"
        width={18}
        height={18}
        style={{ marginLeft: 4, transition: "opacity 0.2s ease", opacity: 0.8 }}
      />
    );
  };

  const columnStyle: React.CSSProperties = {
    display: "flex",
    alignItems: "center",
    cursor: "pointer",
    userSelect: "none",
    paddingBlock: 12,
    paddingInline: 8,
    transition: "background-color 0.2s ease",
  };

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: columnTemplate,
        backgroundColor: theme.fileBrowser.surface,
        border: `1px solid ${getSubtleDividerColor(theme)}`,
        borderRadius: 8,
      }}
    >
      <div
        style={columnStyle}
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
        <AppTypography
          variant="h6"
          style={{ display: "flex", alignItems: "center", fontSize: "0.9rem" }}
        >
          Name
          {renderSortIcon("name")}
        </AppTypography>
      </div>
      <div
        style={columnStyle}
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
        <AppTypography
          variant="h6"
          style={{ display: "flex", alignItems: "center", fontSize: "0.9rem" }}
        >
          Size
          {renderSortIcon("size")}
        </AppTypography>
      </div>
      <div
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
        style={{
          ...columnStyle,
          justifyContent: "center",
          textAlign: "center",
        }}
      >
        <AppTypography
          variant="h6"
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: "0.9rem",
            width: "100%",
          }}
        >
          Last modified
          {renderSortIcon("modTime")}
        </AppTypography>
      </div>
    </div>
  );
};

export default SortBar;
