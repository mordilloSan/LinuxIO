import React from "react";

import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
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
  nvmeSelfTestLog?: {
    table?: unknown[];
  };
  onRunTest: (testType: "short" | "long") => void;
  percentage?: number;
  selfTestLog?: {
    standard?: {
      table?: unknown[];
    };
  };
  smartmontoolsAvailable: boolean;
  smartmontoolsReason?: string;
  startPending: "short" | "long" | null;
}
export const SelfTestsTab: React.FC<SelfTestsTabProps> = ({
  startPending,
  percentage,
  onRunTest,
  selfTestLog,
  nvmeSelfTestLog,
  smartmontoolsAvailable,
  smartmontoolsReason,
}) => {
  const theme = useAppTheme();
  const testActionsDisabled = startPending !== null || !smartmontoolsAvailable;
  const displayPercent =
    percentage !== undefined ? Math.max(0, Math.min(100, percentage)) : 0;
  return (
    <>
      <div
        style={{
          marginBottom: theme.spacing(3),
        }}
      >
        <AppTypography gutterBottom variant="subtitle2">
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
            disabled={testActionsDisabled}
            onClick={(e) => {
              e.stopPropagation();
              onRunTest("short");
            }}
            size="small"
            startIcon={
              startPending === "short" ? (
                <AppCircularProgress size={16} />
              ) : undefined
            }
            variant="outlined"
          >
            {startPending === "short" ? "Starting..." : "Short Test"}
          </AppButton>
          <AppButton
            disabled={testActionsDisabled}
            onClick={(e) => {
              e.stopPropagation();
              onRunTest("long");
            }}
            size="small"
            startIcon={
              startPending === "long" ? (
                <AppCircularProgress size={16} />
              ) : undefined
            }
            variant="outlined"
          >
            {startPending === "long" ? "Starting..." : "Extended Test"}
          </AppButton>
        </div>
        {startPending !== null && (
          <div
            style={{
              marginTop: theme.spacing(1.5),
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1.5),
            }}
          >
            <AppLinearProgress
              style={{ flex: 1 }}
              value={displayPercent}
              variant="determinate"
            />
            <AppTypography color="text.secondary" variant="caption">
              {displayPercent}%
            </AppTypography>
          </div>
        )}
        <AppTypography
          color="text.secondary"
          style={{
            marginTop: 4,
            display: "block",
          }}
          variant="caption"
        >
          {smartmontoolsAvailable
            ? "Short test takes ~2 minutes. Extended test can take hours depending on drive size."
            : smartmontoolsReason ||
              "SMART self-tests are disabled because smartmontools is unavailable."}
        </AppTypography>
      </div>

      <AppTypography gutterBottom variant="subtitle2">
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
                <AppTableCell align="right" style={{ fontWeight: 600 }}>
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
                        ? "var(--app-palette-success-main)"
                        : "var(--app-palette-error-main)",
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
                <AppTableCell align="right" style={{ fontWeight: 600 }}>
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
                          ? "var(--app-palette-success-main)"
                          : "var(--app-palette-error-main)",
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
