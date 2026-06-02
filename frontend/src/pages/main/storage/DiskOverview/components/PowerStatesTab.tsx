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
import { useAppTheme } from "@/theme";

interface PowerStatesTabProps {
  power: PowerData;
}
export const PowerStatesTab: React.FC<PowerStatesTabProps> = ({ power }) => {
  const theme = useAppTheme();
  return (
    <>
      <div
        style={{
          marginBottom: theme.spacing(3),
        }}
      >
        <AppTypography gutterBottom variant="subtitle2">
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
            color="primary"
            label={`Power State ${power.currentState}`}
            variant="soft"
          />
          <AppTypography color="text.secondary" variant="body2">
            Estimated Power: ~{power.estimatedW.toFixed(2)}W
          </AppTypography>
        </div>
      </div>

      <AppTypography gutterBottom variant="subtitle2">
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
              <AppTableCell align="right" style={{ fontWeight: 600 }}>
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
