import { Icon } from "@iconify/react";
import { AnimatePresence, motion } from "motion/react";
import { useState, type CSSProperties } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { getSubtleDividerColor } from "@/theme/surfaces";

export type SortField = "name" | "size" | "modTime";
export type SortOrder = "asc" | "desc";

export interface SortBarProps {
  onSortChange: (field: SortField) => void;
  sortField: SortField;
  sortOrder: SortOrder;
}

const SortBar = ({ sortField, sortOrder, onSortChange }: SortBarProps) => {
  const [hoveredField, setHoveredField] = useState<SortField | null>(null);
  const [focusedField, setFocusedField] = useState<SortField | null>(null);
  // Allow numeric columns to shrink on smaller widths while keeping alignment in sync with rows
  const columnTemplate =
    "minmax(0, 1fr) clamp(80px, 16vw, 140px) clamp(120px, 22vw, 200px)";

  const renderSortIcon = (field: SortField) => {
    const isVisible =
      hoveredField === field || focusedField === field || sortField === field;

    // Inactive fields start ascending; active field reflects the current order.
    const direction = sortField === field ? sortOrder : "asc";
    return (
      <AnimatePresence initial={false} mode="popLayout">
        {isVisible && (
          <motion.span
            animate={{ opacity: 1, scale: 1, filter: "blur(0px)" }}
            exit={{ opacity: 0, scale: 0.25, filter: "blur(4px)" }}
            initial={{ opacity: 0, scale: 0.25, filter: "blur(4px)" }}
            key={direction}
            style={{ display: "inline-flex", marginLeft: 4 }}
            transition={{ type: "spring", duration: 0.3, bounce: 0 }}
          >
            <Icon
              aria-hidden
              data-testid={`sort-icon-${field}`}
              height={18}
              icon={direction === "asc" ? "mdi:arrow-up" : "mdi:arrow-down"}
              width={18}
            />
          </motion.span>
        )}
      </AnimatePresence>
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
    transition: "background-color 150ms ease",
  };

  const renderSortButton = (
    field: SortField,
    label: string,
    style?: CSSProperties,
  ) => (
    <AppButton
      aria-label={`Sort by ${label}${
        sortField === field
          ? `, ${sortOrder === "asc" ? "ascending" : "descending"}`
          : ""
      }`}
      aria-pressed={sortField === field}
      color="inherit"
      onBlur={() => setFocusedField(null)}
      onClick={() => onSortChange(field)}
      onFocus={() => setFocusedField(field)}
      onMouseEnter={() => setHoveredField(field)}
      onMouseLeave={() => setHoveredField(null)}
      style={{ ...columnStyle, ...style }}
    >
      <AppTypography
        fontWeight={500}
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: style?.justifyContent,
          width: "100%",
        }}
        variant="body2"
      >
        {label}
        {renderSortIcon(field)}
      </AppTypography>
    </AppButton>
  );

  return (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: columnTemplate,
        backgroundColor: "var(--app-file-browser-surface)",
        border: `1px solid ${getSubtleDividerColor()}`,
        borderRadius: "var(--app-radius-md)",
      }}
    >
      {renderSortButton("name", "Name")}
      {renderSortButton("size", "Size")}
      {renderSortButton("modTime", "Last modified", {
        justifyContent: "center",
        textAlign: "center",
      })}
    </div>
  );
};

export default SortBar;
