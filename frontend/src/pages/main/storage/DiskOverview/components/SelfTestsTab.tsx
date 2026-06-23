import React from "react";

import { getSmartNumber, getSmartString } from "../utils";

import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
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

interface StandardSelfTestRow {
  lifetime_hours?: unknown;
  num?: unknown;
  status?: {
    passed?: boolean;
    string?: unknown;
    value?: unknown;
  };
  type?: {
    string?: unknown;
    value?: unknown;
  };
}

interface NvmeSelfTestRow {
  power_on_hours?: unknown;
  self_test_code?: {
    string?: unknown;
    value?: unknown;
  };
  self_test_result?: {
    string?: unknown;
    value?: unknown;
  };
}

const standardSelfTestColumns: AppDataTableColumnDef<StandardSelfTestRow>[] = [
  {
    id: "number",
    header: "#",
    cell: ({ row }) => getSmartNumber(row.original.num) ?? row.index + 1,
  },
  {
    id: "type",
    header: "Type",
    cell: ({ row }) => getSmartString(row.original.type) || "Unknown",
  },
  {
    id: "status",
    header: "Status",
    cell: ({ row }) => (
      <span
        style={{
          color: row.original.status?.passed
            ? "var(--app-palette-success-main)"
            : "var(--app-palette-error-main)",
        }}
      >
        {getSmartString(row.original.status) || "Unknown"}
      </span>
    ),
  },
  {
    accessorKey: "lifetime_hours",
    header: "Lifetime Hours",
    cell: ({ row }) =>
      getSmartNumber(row.original.lifetime_hours)?.toLocaleString() || "N/A",
    meta: { align: "right" },
  },
];

const nvmeSelfTestColumns: AppDataTableColumnDef<NvmeSelfTestRow>[] = [
  {
    id: "type",
    header: "Type",
    cell: ({ row }) => getSmartString(row.original.self_test_code) || "Unknown",
  },
  {
    id: "result",
    header: "Result",
    cell: ({ row }) => (
      <span
        style={{
          color:
            getSmartNumber(row.original.self_test_result) === 0
              ? "var(--app-palette-success-main)"
              : "var(--app-palette-error-main)",
        }}
      >
        {getSmartString(row.original.self_test_result) || "Unknown"}
      </span>
    ),
  },
  {
    accessorKey: "power_on_hours",
    header: "Power On Hours",
    cell: ({ row }) =>
      getSmartNumber(row.original.power_on_hours)?.toLocaleString() || "N/A",
    meta: { align: "right" },
  },
];

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
  const standardRows =
    (selfTestLog?.standard?.table as StandardSelfTestRow[] | undefined) ?? [];
  const nvmeRows =
    (nvmeSelfTestLog?.table as NvmeSelfTestRow[] | undefined) ?? [];

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
      {standardRows.length > 0 ? (
        <AppDataTable
          ariaLabel="SMART self-test history"
          columns={standardSelfTestColumns}
          data={standardRows}
          density="compact"
          emptyMessage="No self-test history available."
          getRowId={(entry, index) =>
            String(getSmartNumber(entry.num) ?? index)
          }
          maxHeight={400}
          variant="embedded"
        />
      ) : nvmeRows.length > 0 ? (
        <AppDataTable
          ariaLabel="NVMe self-test history"
          columns={nvmeSelfTestColumns}
          data={nvmeRows}
          density="compact"
          emptyMessage="No self-test history available."
          getRowId={(_, index) => String(index)}
          maxHeight={400}
          variant="embedded"
        />
      ) : (
        <AppTypography color="text.secondary">
          No self-test history available.
        </AppTypography>
      )}
    </>
  );
};
