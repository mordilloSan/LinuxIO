import {
  Box,
  Chip,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import React from "react";

import type { PowerData } from "../types";

interface PowerStatesTabProps {
  power: PowerData;
}

export const PowerStatesTab: React.FC<PowerStatesTabProps> = ({ power }) => {
  return (
    <>
      <Box sx={{ mb: 3 }}>
        <Typography variant="subtitle2" gutterBottom>
          Current State
        </Typography>
        <Box display="flex" gap={2} alignItems="center">
          <Chip label={`Power State ${power.currentState}`} color="primary" />
          <Typography variant="body2" color="text.secondary">
            Estimated Power: ~{power.estimatedW.toFixed(2)}W
          </Typography>
        </Box>
      </Box>

      <Typography variant="subtitle2" gutterBottom>
        Supported Power States
      </Typography>
      <TableContainer className="custom-scrollbar" sx={{ maxHeight: 400 }}>
        <Table
          size="small"
          stickyHeader
          sx={{ "& .MuiTableCell-root": { borderColor: "divider" } }}
        >
          <TableHead>
            <TableRow>
              <TableCell sx={{ fontWeight: 600 }}>State</TableCell>
              <TableCell sx={{ fontWeight: 600 }}>Op</TableCell>
              <TableCell sx={{ fontWeight: 600 }} align="right">
                Max Power
              </TableCell>
              <TableCell sx={{ fontWeight: 600 }}>Description</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {power.states.map((ps) => (
              <TableRow
                key={ps.state}
                selected={ps.state === power.currentState}
              >
                <TableCell>{ps.state}</TableCell>
                <TableCell>+</TableCell>
                <TableCell align="right">{ps.maxPowerW}W</TableCell>
                <TableCell sx={{ fontSize: "0.75rem" }}>
                  {ps.description}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </>
  );
};
