import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import HistoryIcon from "@mui/icons-material/History";
import {
  Typography,
  Box,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  Collapse,
  Chip,
} from "@mui/material";
import { motion } from "framer-motion";
import React, { useState } from "react";

import type { UpdateHistoryRow } from "@/api/linuxio-types";
import linuxio from "@/api/react-query";
import {
  getTableHeaderStyles,
  getTableRowStyles,
  getExpandedRowStyles,
  getExpandedContentStyles,
  tableContainerStyles,
  responsiveTextStyles,
} from "@/styles/tableStyles";

const chunkArray = <T,>(array: T[], chunkSize: number): T[][] => {
  const result: T[][] = [];
  for (let i = 0; i < array.length; i += chunkSize) {
    result.push(array.slice(i, i + chunkSize));
  }
  return result;
};

const UpdateHistoryCard: React.FC = () => {
  const { data: rows = [] } = linuxio.dbus.GetUpdateHistory.useQuery();
  const [expanded, setExpanded] = useState<number | null>(null);

  return (
    <Box>
      <TableContainer sx={tableContainerStyles}>
        <Table size="small" sx={{ borderRadius: 3, boxShadow: 2 }}>
          <TableHead>
            <TableRow sx={getTableHeaderStyles}>
              <TableCell width="40px"></TableCell>
              <TableCell>Date</TableCell>
              <TableCell align="center">Packages Updated</TableCell>
              <TableCell width="40px" />
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((row, index) => {
              const rowStyles = (theme: any) => getTableRowStyles(theme, index);
              const expandedRowStyles = (theme: any) =>
                getExpandedRowStyles(theme, index);
              return (
                <React.Fragment key={index}>
                  <TableRow sx={rowStyles}>
                  <TableCell>
                    <Box
                      sx={{
                        display: "flex",
                        alignItems: "center",
                        color: "primary.main",
                      }}
                    >
                      <HistoryIcon fontSize="small" />
                    </Box>
                  </TableCell>
                  <TableCell>
                    <Typography
                      variant="body2"
                      fontWeight="medium"
                      sx={responsiveTextStyles}
                    >
                      {row.date}
                    </Typography>
                  </TableCell>
                  <TableCell align="center">
                    <Chip
                      label={row.upgrades.length}
                      size="small"
                      color="success"
                      sx={{ minWidth: 40 }}
                    />
                  </TableCell>
                  <TableCell>
                    <IconButton
                      size="small"
                      onClick={() =>
                        setExpanded(expanded === index ? null : index)
                      }
                    >
                      <ExpandMoreIcon
                        style={{
                          transform:
                            expanded === index
                              ? "rotate(180deg)"
                              : "rotate(0deg)",
                          transition: "0.2s",
                        }}
                      />
                    </IconButton>
                  </TableCell>
                </TableRow>
                <TableRow sx={expandedRowStyles}>
                  <TableCell
                    style={{ paddingBottom: 0, paddingTop: 0 }}
                    colSpan={4}
                  >
                    <Collapse
                      in={expanded === index}
                      timeout="auto"
                      unmountOnExit
                    >
                      <Box
                        component={motion.div}
                        initial={{ opacity: 0, y: -10 }}
                        animate={{ opacity: 1, y: 0 }}
                        sx={getExpandedContentStyles}
                      >
                        <Typography variant="subtitle2" gutterBottom>
                          <b>Packages Installed:</b>
                        </Typography>
                        <Table
                          size="small"
                          sx={{
                            borderCollapse: "collapse",
                            "& .MuiTableCell-root": { border: "none" },
                            overflowX: "auto",
                            display: "block",
                          }}
                        >
                          <TableBody>
                            {chunkArray(row.upgrades, 5).map((group, i) => (
                              <TableRow key={i}>
                                {group.map((pkg, j) => (
                                  <TableCell
                                    key={j}
                                    sx={{
                                      width: "20%",
                                      padding: "8px 12px",
                                      color: "text.secondary",
                                      fontFamily: "monospace",
                                      fontSize: "0.85rem",
                                      ...responsiveTextStyles,
                                    }}
                                  >
                                    {pkg.package}
                                  </TableCell>
                                ))}
                                {group.length < 5 &&
                                  [...Array(5 - group.length)].map((_, j) => (
                                    <TableCell
                                      key={`empty-${j}`}
                                      sx={{ width: "20%", border: "none" }}
                                    />
                                  ))}
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
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
      {rows.length === 0 && (
        <Box textAlign="center" py={4}>
          <Typography variant="body2" color="text.secondary">
            No update history available.
          </Typography>
        </Box>
      )}
    </Box>
  );
};

export default UpdateHistoryCard;
