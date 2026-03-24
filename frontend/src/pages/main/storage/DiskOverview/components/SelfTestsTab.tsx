import React from "react";

import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
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
interface SelfTestsTabProps {
  startPending: "short" | "long" | null;
  onRunTest: (testType: "short" | "long") => void;
  selfTestLog?: {
    standard?: {
      table?: unknown[];
    };
  };
  nvmeSelfTestLog?: {
    table?: unknown[];
  };
  smartmontoolsAvailable: boolean;
  smartmontoolsReason?: string;
}
export const SelfTestsTab: React.FC<SelfTestsTabProps> = ({
  startPending,
  onRunTest,
  selfTestLog,
  nvmeSelfTestLog,
  smartmontoolsAvailable,
  smartmontoolsReason,
}) => {
  const theme = useAppTheme();
  const testActionsDisabled = startPending !== null || !smartmontoolsAvailable;
  return (
    <>
      <div
        style={{
          marginBottom: theme.spacing(3),
        }}
      >
        <AppTypography variant="subtitle2" gutterBottom>
          Run SMART Self-Test
        </AppTypography>
        <div
          style={{
            display: "flex",
            gap: theme.spacing(2),
            alignItems: "center",
          }}
        >
          <AppButton
            variant="outlined"
            size="small"
            disabled={testActionsDisabled}
            onClick={(e) => {
              e.stopPropagation();
              onRunTest("short");
            }}
            startIcon={
              startPending === "short" ? (
                <AppCircularProgress size={16} />
              ) : undefined
            }
          >
            {startPending === "short" ? "Starting..." : "Short Test"}
          </AppButton>
          <AppButton
            variant="outlined"
            size="small"
            disabled={testActionsDisabled}
            onClick={(e) => {
              e.stopPropagation();
              onRunTest("long");
            }}
            startIcon={
              startPending === "long" ? (
                <AppCircularProgress size={16} />
              ) : undefined
            }
          >
            {startPending === "long" ? "Starting..." : "Extended Test"}
          </AppButton>
        </div>
        <AppTypography
          variant="caption"
          color="text.secondary"
          style={{
            marginTop: 4,
            display: "block",
          }}
        >
          {smartmontoolsAvailable
            ? "Short test takes ~2 minutes. Extended test can take hours depending on drive size."
            : smartmontoolsReason ||
              "SMART self-tests are disabled because smartmontools is unavailable."}
        </AppTypography>
      </div>

      <AppTypography variant="subtitle2" gutterBottom>
        Self-Test History
      </AppTypography>
      {selfTestLog?.standard?.table &&
      (selfTestLog.standard.table as unknown[]).length > 0 ? (
        <AppTableContainer
          className="custom-scrollbar"
          style={{ maxHeight: 400 }}
        >
          <AppTable className="app-table--sticky">
            <AppTableHead>
              <AppTableRow>
                <AppTableCell style={{ fontWeight: 600 }}>#</AppTableCell>
                <AppTableCell style={{ fontWeight: 600 }}>Type</AppTableCell>
                <AppTableCell style={{ fontWeight: 600 }}>Status</AppTableCell>
                <AppTableCell style={{ fontWeight: 600 }} align="right">
                  Lifetime Hours
                </AppTableCell>
              </AppTableRow>
            </AppTableHead>
            <AppTableBody>
              {(
                selfTestLog.standard.table as {
                  num?: number;
                  type?: {
                    string?: string;
                  };
                  status?: {
                    string?: string;
                    passed?: boolean;
                  };
                  lifetime_hours?: number;
                }[]
              ).map((entry, idx) => (
                <AppTableRow key={idx}>
                  <AppTableCell>{entry.num ?? idx + 1}</AppTableCell>
                  <AppTableCell>{entry.type?.string || "Unknown"}</AppTableCell>
                  <AppTableCell
                    style={{
                      color: entry.status?.passed
                        ? "var(--mui-palette-success-main)"
                        : "var(--mui-palette-error-main)",
                    }}
                  >
                    {entry.status?.string || "Unknown"}
                  </AppTableCell>
                  <AppTableCell align="right">
                    {entry.lifetime_hours?.toLocaleString() || "N/A"}
                  </AppTableCell>
                </AppTableRow>
              ))}
            </AppTableBody>
          </AppTable>
        </AppTableContainer>
      ) : nvmeSelfTestLog?.table &&
        (nvmeSelfTestLog.table as unknown[]).length > 0 ? (
        <AppTableContainer
          className="custom-scrollbar"
          style={{ maxHeight: 400 }}
        >
          <AppTable className="app-table--sticky">
            <AppTableHead>
              <AppTableRow>
                <AppTableCell style={{ fontWeight: 600 }}>Type</AppTableCell>
                <AppTableCell style={{ fontWeight: 600 }}>Result</AppTableCell>
                <AppTableCell style={{ fontWeight: 600 }} align="right">
                  Power On Hours
                </AppTableCell>
              </AppTableRow>
            </AppTableHead>
            <AppTableBody>
              {(
                nvmeSelfTestLog.table as {
                  self_test_code?: {
                    string?: string;
                  };
                  self_test_result?: {
                    string?: string;
                    value?: number;
                  };
                  power_on_hours?: number;
                }[]
              ).map((entry, idx) => (
                <AppTableRow key={idx}>
                  <AppTableCell>
                    {entry.self_test_code?.string || "Unknown"}
                  </AppTableCell>
                  <AppTableCell
                    style={{
                      color:
                        entry.self_test_result?.value === 0
                          ? "var(--mui-palette-success-main)"
                          : "var(--mui-palette-error-main)",
                    }}
                  >
                    {entry.self_test_result?.string || "Unknown"}
                  </AppTableCell>
                  <AppTableCell align="right">
                    {entry.power_on_hours?.toLocaleString() || "N/A"}
                  </AppTableCell>
                </AppTableRow>
              ))}
            </AppTableBody>
          </AppTable>
        </AppTableContainer>
      ) : (
        <AppTypography color="text.secondary">
          No self-test history available.
        </AppTypography>
      )}
    </>
  );
};
