import { Icon } from "@iconify/react";
import { useState, type CSSProperties } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { getSubtleDividerColor } from "@/theme/surfaces";

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";

export interface SortBarProps {
  onSortChange: (field: SortField) => void;
  sortOrder: SortOrder;
}

const SortBar = ({ sortOrder, onSortChange }: SortBarProps) => {
  const theme = useAppTheme();
  const [hoveredField, setHoveredField] = useState<SortField | null>(null);
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
        height={18}
        icon="mdi:arrow-up"
        style={{ marginLeft: 4, transition: "opacity 0.2s ease", opacity: 0.8 }}
        width={18}
      />
    ) : (
      <Icon
        height={18}
        icon="mdi:arrow-down"
        style={{ marginLeft: 4, transition: "opacity 0.2s ease", opacity: 0.8 }}
        width={18}
      />
    );
  };

  const columnStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    cursor: "pointer",
    justifyContent: "flex-start",
    minWidth: 0,
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
      <AppButton
        color="inherit"
        onClick={() => onSortChange("name")}
        onMouseEnter={() => setHoveredField("name")}
        onMouseLeave={() => setHoveredField(null)}
        style={columnStyle}
      >
        <AppTypography
          style={{ display: "flex", alignItems: "center", fontSize: "0.9rem" }}
          variant="h6"
        >
          Name
          {renderSortIcon("name")}
        </AppTypography>
      </AppButton>
      <AppButton
        color="inherit"
        onClick={() => onSortChange("size")}
        onMouseEnter={() => setHoveredField("size")}
        onMouseLeave={() => setHoveredField(null)}
        style={columnStyle}
      >
        <AppTypography
          style={{ display: "flex", alignItems: "center", fontSize: "0.9rem" }}
          variant="h6"
        >
          Size
          {renderSortIcon("size")}
        </AppTypography>
      </AppButton>
      <AppButton
        color="inherit"
        onClick={() => onSortChange("modTime")}
        onMouseEnter={() => setHoveredField("modTime")}
        onMouseLeave={() => setHoveredField(null)}
        style={{
          ...columnStyle,
          justifyContent: "center",
          textAlign: "center",
        }}
      >
        <AppTypography
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: "0.9rem",
            width: "100%",
          }}
          variant="h6"
        >
          Last modified
          {renderSortIcon("modTime")}
        </AppTypography>
      </AppButton>
    </div>
  );
};

export default SortBar;
