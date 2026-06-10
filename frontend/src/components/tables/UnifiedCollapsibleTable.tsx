import { Icon } from "@iconify/react";
import { motion } from "framer-motion";
import React, { useState } from "react";

import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableContainer,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTypography from "@/components/ui/AppTypography";
import { shadowSm } from "@/constants";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

export interface UnifiedTableColumn {
  align?: "left" | "center" | "right";
  className?: string;
  field: string;
  headerName: string;
  style?: React.CSSProperties;
  width?: string | number;
}

interface UnifiedCollapsibleTableProps<T> {
  columns: UnifiedTableColumn[];
  data: T[];
  emptyMessage?: string;
  getRowKey: (row: T, index: number) => string | number;
  onRowClick?: (row: T, index: number) => void;
  onRowDoubleClick?: (row: T, index: number) => void;
  renderExpandedContent?: (row: T, index: number) => React.ReactNode;
  renderFirstCell?: (row: T, index: number) => React.ReactNode;
  renderHeaderFirstCell?: () => React.ReactNode;
  renderMainRow: (row: T, index: number) => React.ReactNode;
  selectedKey?: string | number | null;
}

function UnifiedCollapsibleTable<T>({
  data,
  columns,
  getRowKey,
  renderMainRow,
  renderExpandedContent,
  renderFirstCell,
  renderHeaderFirstCell,
  onRowClick,
  onRowDoubleClick,
  selectedKey,
  emptyMessage = "No data available.",
}: UnifiedCollapsibleTableProps<T>) {
  const [expanded, setExpanded] = useState<string | number | null>(null);
  const theme = useAppTheme();
  const isDark = theme.palette.mode === "dark";

  const headRowBg = alpha(theme.palette.text.primary, 0.08);
  const selectedBg = alpha(theme.palette.primary.main, isDark ? 0.15 : 0.1);
  const altBg = alpha(theme.palette.text.primary, isDark ? 0.04 : 0.05);
  const hoverBg = alpha(theme.palette.primary.main, 0.08);
  const isInteractive = !!onRowClick || !!onRowDoubleClick;

  return (
    <div>
      <AppTableContainer>
        <AppTable
          style={
            {
              "--uct-hover-bg": hoverBg,
              boxShadow: shadowSm,
            } as React.CSSProperties
          }
        >
          <AppTableHead>
            <AppTableRow style={{ backgroundColor: headRowBg }}>
              {renderFirstCell && (
                <AppTableCell
                  component="th"
                  style={{ width: 40, padding: "8px 4px" }}
                >
                  {renderHeaderFirstCell?.()}
                </AppTableCell>
              )}
              {columns.map((column) => (
                <AppTableCell
                  align={column.align || "left"}
                  className={column.className}
                  component="th"
                  key={column.field}
                  style={{ width: column.width, ...column.style }}
                >
                  {column.headerName}
                </AppTableCell>
              ))}
              {renderExpandedContent && (
                <AppTableCell component="th" style={{ width: 40 }} />
              )}
            </AppTableRow>
          </AppTableHead>
          <AppTableBody>
            {data.map((row, index) => {
              const rowKey = getRowKey(row, index);
              const isExpanded = expanded === rowKey;
              const isSelected = rowKey === selectedKey;

              return (
                <React.Fragment key={rowKey}>
                  <AppTableRow
                    className={
                      isInteractive ? "app-table-row--interactive" : ""
                    }
                    onClick={() => onRowClick?.(row, index)}
                    onDoubleClick={() => onRowDoubleClick?.(row, index)}
                    style={{
                      backgroundColor: isSelected
                        ? selectedBg
                        : index % 2 === 0
                          ? "transparent"
                          : altBg,
                    }}
                  >
                    {renderFirstCell && (
                      <AppTableCell style={{ width: 40, padding: "8px 4px" }}>
                        {renderFirstCell(row, index)}
                      </AppTableCell>
                    )}
                    {renderMainRow(row, index)}
                    {renderExpandedContent && (
                      <AppTableCell>
                        <AppIconButton
                          onClick={(e) => {
                            e.stopPropagation();
                            setExpanded(isExpanded ? null : rowKey);
                          }}
                          size="small"
                        >
                          <Icon
                            height={22}
                            icon="mdi:chevron-down"
                            style={{
                              transform: isExpanded
                                ? "rotate(180deg)"
                                : "rotate(0deg)",
                              transition: "0.2s",
                            }}
                            width={22}
                          />
                        </AppIconButton>
                      </AppTableCell>
                    )}
                  </AppTableRow>
                  {renderExpandedContent && (
                    <AppTableRow style={{ backgroundColor: "transparent" }}>
                      <AppTableCell
                        colSpan={columns.length + (renderFirstCell ? 2 : 1)}
                        style={{ paddingBottom: 0, paddingTop: 0 }}
                      >
                        <AppCollapse
                          in={isExpanded}
                          timeout="auto"
                          unmountOnExit
                        >
                          <motion.div
                            animate={{ opacity: 1, y: 0 }}
                            initial={{ opacity: 0, y: -10 }}
                          >
                            <div
                              style={{
                                overflowX: "auto",
                                padding: "10px 0",
                              }}
                            >
                              {renderExpandedContent(row, index)}
                            </div>
                          </motion.div>
                        </AppCollapse>
                      </AppTableCell>
                    </AppTableRow>
                  )}
                </React.Fragment>
              );
            })}
          </AppTableBody>
        </AppTable>
      </AppTableContainer>
      {data.length === 0 && (
        <div
          style={{
            textAlign: "center",
            paddingTop: 32,
            paddingBottom: 32,
          }}
        >
          <AppTypography color="text.secondary" variant="body2">
            {emptyMessage}
          </AppTypography>
        </div>
      )}
    </div>
  );
}

export default UnifiedCollapsibleTable;
