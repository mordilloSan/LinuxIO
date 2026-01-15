import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import {
  Box,
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
  renderExpandedContent: (row: T, index: number) => React.ReactNode;
  renderFirstCell?: (row: T, index: number) => React.ReactNode;
  emptyMessage?: string;
}

function UnifiedCollapsibleTable<T>({
  data,
  columns,
  getRowKey,
  renderMainRow,
  renderExpandedContent,
  renderFirstCell,
  emptyMessage = "No data available.",
}: UnifiedCollapsibleTableProps<T>) {
  const [expanded, setExpanded] = useState<string | number | null>(null);

  return (
    <Box>
      <TableContainer
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
                backgroundColor:
                  theme.palette.mode === "dark"
                    ? "rgba(255,255,255,0.08)"
                    : "rgba(0,0,0,0.08)",
                borderRadius: "6px",
                boxShadow: "none",
              })}
            >
              {renderFirstCell && (
                <TableCell
                  width="40px"
                  sx={{ padding: "8px 4px 8px 4px" }}
                ></TableCell>
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
              <TableCell width="40px" />
            </TableRow>
          </TableHead>
          <TableBody>
            {data.map((row, index) => {
              const rowKey = getRowKey(row, index);
              const isExpanded = expanded === rowKey;

              return (
                <React.Fragment key={rowKey}>
                  <TableRow
                    sx={(theme) => ({
                      "& .MuiTableCell-root": { borderBottom: "none" },
                      backgroundColor:
                        index % 2 === 0
                          ? "transparent"
                          : theme.palette.mode === "dark"
                            ? "rgba(255,255,255,0.04)"
                            : "rgba(0,0,0,0.05)",
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
                    <TableCell>
                      <IconButton
                        size="small"
                        onClick={() => setExpanded(isExpanded ? null : rowKey)}
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
                  </TableRow>
                  <TableRow
                    sx={(theme) => ({
                      "& .MuiTableCell-root": { borderBottom: "none" },
                      backgroundColor:
                        index % 2 === 0
                          ? "transparent"
                          : theme.palette.mode === "dark"
                            ? "rgba(255,255,255,0.08)"
                            : "rgba(0,0,0,0.05)",
                    })}
                  >
                    <TableCell
                      style={{ paddingBottom: 0, paddingTop: 0 }}
                      colSpan={columns.length + (renderFirstCell ? 2 : 1)}
                    >
                      <Collapse in={isExpanded} timeout="auto" unmountOnExit>
                        <Box
                          component={motion.div}
                          initial={{ opacity: 0, y: -10 }}
                          animate={{ opacity: 1, y: 0 }}
                          sx={{
                            margin: 2,
                            borderRadius: 2,
                            p: 2,
                            bgcolor: (theme) =>
                              theme.palette.mode === "dark"
                                ? "rgba(255,255,255,0.05)"
                                : "rgba(0,0,0,0.03)",
                            overflowX: "auto",
                            "@media (max-width: 600px)": {
                              margin: 1,
                              padding: 1,
                              fontSize: "0.85rem",
                            },
                          }}
                        >
                          {renderExpandedContent(row, index)}
                        </Box>
                      </Collapse>
                    </TableCell>
                  </TableRow>
                </React.Fragment>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
      {data.length === 0 && (
        <Box textAlign="center" py={4}>
          <Typography variant="body2" color="text.secondary">
            {emptyMessage}
          </Typography>
        </Box>
      )}
    </Box>
  );
}

export default UnifiedCollapsibleTable;
