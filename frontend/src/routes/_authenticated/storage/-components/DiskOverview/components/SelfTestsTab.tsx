import type {
  NVMeSelfTestLog,
  NVMeSelfTestLogEntry,
  SmartSelfTestEntry,
  SmartSelfTestLog,
} from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";

import { getSmartNumber, getSmartString } from "../utils";

interface SelfTestsTabProps {
  nvmeSelfTestLog?: NVMeSelfTestLog;
  onRunTest: (testType: "short" | "long") => void;
  percentage?: number;
  selfTestLog?: SmartSelfTestLog;
  smartmontoolsAvailable: boolean;
  smartmontoolsReason?: string;
  startPending: "short" | "long" | null;
}

const standardSelfTestColumns: AppVirtualTableColumnDef<SmartSelfTestEntry>[] =
  [
    {
      id: "number",
      header: "#",
      cell: ({ row }) => row.original.num ?? row.index + 1,
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
            color:
              row.original.status?.passed === true ||
              getSmartNumber(row.original.status) === 0
                ? "var(--app-palette-success-main)"
                : row.original.status?.passed === false ||
                    getSmartNumber(row.original.status) !== null
                  ? "var(--app-palette-error-main)"
                  : "inherit",
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

const nvmeSelfTestColumns: AppVirtualTableColumnDef<NVMeSelfTestLogEntry>[] = [
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

export const SelfTestsTab = ({
  startPending,
  percentage,
  onRunTest,
  selfTestLog,
  nvmeSelfTestLog,
  smartmontoolsAvailable,
  smartmontoolsReason,
}: SelfTestsTabProps) => {
  const testActionsDisabled = startPending !== null || !smartmontoolsAvailable;
  const displayPercent =
    percentage !== undefined ? Math.max(0, Math.min(100, percentage)) : 0;
  const standardRows = selfTestLog?.standard?.table ?? [];
  const nvmeRows = nvmeSelfTestLog?.table ?? [];

  return (
    <>
      <div
        style={{
          marginBottom: "var(--app-space-12)",
        }}
      >
        <AppTypography gutterBottom variant="subtitle2">
          Run SMART Self-Test
        </AppTypography>
        <div
          style={{
            display: "flex",
            gap: "var(--app-space-8)",
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
              marginTop: "var(--app-space-6)",
              display: "flex",
              alignItems: "center",
              gap: "var(--app-space-6)",
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
        <AppVirtualTable
          ariaLabel="SMART self-test history"
          columns={standardSelfTestColumns}
          data={standardRows}
          density="compact"
          emptyMessage="No self-test history available."
          fillAvailable={false}
          getRowId={(_, index) => String(index)}
          maxHeight={400}
          variant="embedded"
        />
      ) : nvmeRows.length > 0 ? (
        <AppVirtualTable
          ariaLabel="NVMe self-test history"
          columns={nvmeSelfTestColumns}
          data={nvmeRows}
          density="compact"
          emptyMessage="No self-test history available."
          fillAvailable={false}
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
