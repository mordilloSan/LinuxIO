import { useTheme } from "@mui/material/styles";
import React from "react";

import type { PowerData } from "../types";

import Chip from "@/components/ui/AppChip";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableContainer,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
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
      <AppTableContainer
        className="custom-scrollbar"
        style={{
          maxHeight: 400,
        }}
      >
        <AppTable className="app-table--sticky">
          <AppTableHead>
            <AppTableRow>
              <AppTableCell style={{ fontWeight: 600 }}>State</AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }}>Op</AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }} align="right">
                Max Power
              </AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }}>
                Description
              </AppTableCell>
            </AppTableRow>
          </AppTableHead>
          <AppTableBody>
            {power.states.map((ps) => (
              <AppTableRow
                key={ps.state}
                selected={ps.state === power.currentState}
              >
                <AppTableCell>{ps.state}</AppTableCell>
                <AppTableCell>+</AppTableCell>
                <AppTableCell align="right">{ps.maxPowerW}W</AppTableCell>
                <AppTableCell style={{ fontSize: "0.75rem" }}>
                  {ps.description}
                </AppTableCell>
              </AppTableRow>
            ))}
          </AppTableBody>
        </AppTable>
      </AppTableContainer>
    </>
  );
};
