import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import type { PowerData } from "../types";

import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
interface PowerStatesTabProps {
  power: PowerData;
}
export const PowerStatesTab: React.FC<PowerStatesTabProps> = ({ power }) => {
  const theme = useTheme();
  return (
    <>
      <div
        style={{
          marginBottom: theme.spacing(3),
        }}
      >
        <AppTypography variant="subtitle2" gutterBottom>
          Current State
        </AppTypography>
        <div
          style={{
            display: "flex",
            gap: theme.spacing(2),
            alignItems: "center",
          }}
        >
          <Chip
            label={`Power State ${power.currentState}`}
            color="primary"
            variant="soft"
          />
          <AppTypography variant="body2" color="text.secondary">
            Estimated Power: ~{power.estimatedW.toFixed(2)}W
          </AppTypography>
        </div>
      </div>

      <AppTypography variant="subtitle2" gutterBottom>
        Supported Power States
      </AppTypography>
      <TableContainer
        className="custom-scrollbar"
        sx={{
          maxHeight: 400,
        }}
      >
        <Table
          size="small"
          stickyHeader
          sx={{
            "& .MuiTableCell-root": {
              borderColor: "divider",
            },
          }}
        >
          <TableHead>
            <TableRow>
              <TableCell
                sx={{
                  fontWeight: 600,
                }}
              >
                State
              </TableCell>
              <TableCell
                sx={{
                  fontWeight: 600,
                }}
              >
                Op
              </TableCell>
              <TableCell
                sx={{
                  fontWeight: 600,
                }}
                align="right"
              >
                Max Power
              </TableCell>
              <TableCell
                sx={{
                  fontWeight: 600,
                }}
              >
                Description
              </TableCell>
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
                <TableCell
                  sx={{
                    fontSize: "0.75rem",
                  }}
                >
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
