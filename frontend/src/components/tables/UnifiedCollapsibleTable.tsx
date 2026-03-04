import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  Collapse,
  Typography,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import { motion } from "framer-motion";
import React, { useState } from "react";

export interface UnifiedTableColumn {
  field: string;
  headerName: string;
  align?: "left" | "center" | "right";
  width?: string | number;
  sx?: object;
}

interface UnifiedCollapsibleTableProps<T> {
  data: T[];
  columns: UnifiedTableColumn[];
  getRowKey: (row: T, index: number) => string | number;
  renderMainRow: (row: T, index: number) => React.ReactNode;
  renderExpandedContent?: (row: T, index: number) => React.ReactNode;
  renderFirstCell?: (row: T, index: number) => React.ReactNode;
  renderHeaderFirstCell?: () => React.ReactNode;
  onRowClick?: (row: T, index: number) => void;
  onRowDoubleClick?: (row: T, index: number) => void;
  selectedKey?: string | number | null;
  emptyMessage?: string;
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

  return (
    <div>
      <TableContainer
        className="custom-scrollbar"
        sx={{
          overflowX: "auto",
          "@media (max-width: 600px)": {
            "& .MuiTable-root": {
              minWidth: "100%",
            },
          },
        }}
      >
        <Table size="small" sx={{ borderRadius: 3, boxShadow: 2 }}>
          <TableHead>
            <TableRow
              sx={(theme) => ({
                "& .MuiTableCell-root": { borderBottom: "none" },
                backgroundColor: alpha(theme.palette.text.primary, 0.08),
                borderRadius: "6px",
                boxShadow: "none",
              })}
            >
              {renderFirstCell && (
                <TableCell width="40px" sx={{ padding: "8px 4px 8px 4px" }}>
                  {renderHeaderFirstCell?.()}
                </TableCell>
              )}
              {columns.map((column) => (
                <TableCell
                  key={column.field}
                  align={column.align || "left"}
                  width={column.width}
                  sx={{
                    "@media (max-width: 600px)": {
                      fontSize: "0.75rem",
                      padding: "8px 4px",
                    },
                    ...column.sx,
                  }}
                >
                  {column.headerName}
                </TableCell>
              ))}
              {renderExpandedContent && <TableCell width="40px" />}
            </TableRow>
          </TableHead>
          <TableBody>
            {data.map((row, index) => {
              const rowKey = getRowKey(row, index);
              const isExpanded = expanded === rowKey;

              return (
                <React.Fragment key={rowKey}>
                  <TableRow
                    onClick={() => onRowClick?.(row, index)}
                    onDoubleClick={() => onRowDoubleClick?.(row, index)}
                    sx={(theme) => ({
                      "& .MuiTableCell-root": { borderBottom: "none" },
                      cursor:
                        onRowClick || onRowDoubleClick ? "pointer" : "default",
                      backgroundColor:
                        rowKey === selectedKey
                          ? alpha(
                              theme.palette.primary.main,
                              theme.palette.mode === "dark" ? 0.15 : 0.1,
                            )
                          : index % 2 === 0
                            ? "transparent"
                            : alpha(
                                theme.palette.text.primary,
                                theme.palette.mode === "dark" ? 0.04 : 0.05,
                              ),
                      "&:hover": onRowClick
                        ? {
                            backgroundColor: alpha(
                              theme.palette.primary.main,
                              0.08,
                            ),
                          }
                        : undefined,
                      "@media (max-width: 600px)": {
                        "& .MuiTableCell-root": {
                          fontSize: "0.75rem",
                          padding: "8px 4px",
                        },
                      },
                    })}
                  >
                    {renderFirstCell && (
                      <TableCell
                        width="40px"
                        sx={{ padding: "8px 4px 8px 4px" }}
                      >
                        {renderFirstCell(row, index)}
                      </TableCell>
                    )}
                    {renderMainRow(row, index)}
                    {renderExpandedContent && (
                      <TableCell>
                        <IconButton
                          size="small"
                          onClick={(e) => {
                            e.stopPropagation();
                            setExpanded(isExpanded ? null : rowKey);
                          }}
                        >
                          <ExpandMoreIcon
                            style={{
                              transform: isExpanded
                                ? "rotate(180deg)"
                                : "rotate(0deg)",
                              transition: "0.2s",
                            }}
                          />
                        </IconButton>
                      </TableCell>
                    )}
                  </TableRow>
                  {renderExpandedContent && (
                    <TableRow
                      sx={{
                        "& .MuiTableCell-root": { borderBottom: "none" },
                        backgroundColor: "transparent",
                      }}
                    >
                      <TableCell
                        style={{ paddingBottom: 0, paddingTop: 0 }}
                        colSpan={columns.length + (renderFirstCell ? 2 : 1)}
                      >
                        <Collapse in={isExpanded} timeout="auto" unmountOnExit>
                          <motion.div
                            initial={{ opacity: 0, y: -10 }}
                            animate={{ opacity: 1, y: 0 }}
                          >
                            <div
                              style={{
                                margin: 16,
                                borderRadius: 16,
                                padding: 16,
                                overflowX: "auto",
                              }}
                            >
                              {renderExpandedContent(row, index)}
                            </div>
                          </motion.div>
                        </Collapse>
                      </TableCell>
                    </TableRow>
                  )}
                </React.Fragment>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
      {data.length === 0 && (
        <div
          style={{
            textAlign: "center",
            paddingTop: 32,
            paddingBottom: 32,
          }}
        >
          <Typography variant="body2" color="text.secondary">
            {emptyMessage}
          </Typography>
        </div>
      )}
    </div>
  );
}

export default UnifiedCollapsibleTable;
