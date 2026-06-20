import { Icon } from "@iconify/react";
import React from "react";

import { linuxio } from "@/api";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

interface PackageChunkRow {
  id: string;
  upgrades: Array<{ package: string }>;
}

const chunkArray = <T,>(array: T[], chunkSize: number): T[][] => {
  const result: T[][] = [];
  for (let i = 0; i < array.length; i += chunkSize) {
    result.push(array.slice(i, i + chunkSize));
  }
  return result;
};
const UpdateHistory: React.FC = () => {
  const theme = useAppTheme();
  const { data: rows = [] } = linuxio.updates.get_update_history.useQuery();
  const columns: AppDataTableColumnDef<(typeof rows)[number]>[] = [
    {
      id: "history",
      header: "",
      enableSorting: false,
      cell: () => (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            color: theme.palette.primary.main,
          }}
        >
          <Icon height={20} icon="mdi:history" width={20} />
        </div>
      ),
      meta: { width: "40px" },
    },
    {
      accessorKey: "date",
      header: "Date",
      cell: ({ row }) => (
        <AppTypography
          fontWeight={500}
          style={{
            wordBreak: "break-word",
            overflowWrap: "break-word",
          }}
          variant="body2"
        >
          {row.original.date}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      accessorFn: (row) => row.upgrades.length,
      id: "packages",
      header: "Packages Updated",
      cell: ({ row }) => (
        <AppChip
          color="success"
          label={row.original.upgrades.length}
          size="small"
          style={{
            minWidth: 40,
          }}
          variant="soft"
        />
      ),
      meta: {
        align: "center",
        style: {
          minWidth: 112,
          whiteSpace: "nowrap",
        },
        width: 148,
      },
    },
  ];
  const packageColumns: AppDataTableColumnDef<PackageChunkRow>[] = Array.from(
    { length: 5 },
    (_, index) => ({
      id: `package-${index}`,
      header: "",
      cell: ({ row }) => {
        const pkg = row.original.upgrades[index];
        if (!pkg) return null;

        return (
          <span
            style={{
              color: "var(--app-palette-text-secondary)",
              fontFamily: theme.typography.fontFamily,
              fontSize: "0.85rem",
              overflowWrap: "break-word",
              wordBreak: "break-word",
            }}
          >
            {pkg.package}
          </span>
        );
      },
      meta: { width: "20%" },
    }),
  );

  return (
    <AppDataTable
      ariaLabel="Update history"
      columns={columns}
      data={rows}
      emptyMessage="No update history available."
      fillAvailable
      getRowId={(_, index) => String(index)}
      renderExpandedContent={({ original: row }) => (
        <>
          <AppTypography gutterBottom variant="subtitle2">
            <b>Packages Installed:</b>
          </AppTypography>
          <AppDataTable
            ariaLabel={`Packages installed on ${row.date}`}
            columns={packageColumns}
            data={chunkArray(row.upgrades, 5).map((upgrades, index) => ({
              id: String(index),
              upgrades,
            }))}
            density="compact"
            emptyMessage="No packages recorded."
            getRowId={(packageRow) => packageRow.id}
            maxHeight={260}
            showHeader={false}
            variant="embedded"
          />
        </>
      )}
    />
  );
};
export default UpdateHistory;
