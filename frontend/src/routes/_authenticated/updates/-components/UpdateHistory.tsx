import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { memo, useMemo } from "react";

import { linuxio, type UpdateHistoryRow } from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

interface PackageChunkRow {
  id: string;
  upgrades: Array<{ package: string }>;
}

interface PackageHistoryTableProps {
  date: string;
  upgrades: UpdateHistoryRow["upgrades"];
}

const chunkArray = <T,>(array: T[], chunkSize: number): T[][] => {
  const result: T[][] = [];
  for (let i = 0; i < array.length; i += chunkSize) {
    result.push(array.slice(i, i + chunkSize));
  }
  return result;
};

const getPackageChunkRowId = (row: PackageChunkRow) => row.id;

const PackageHistoryTable = memo(function PackageHistoryTable({
  date,
  upgrades,
}: PackageHistoryTableProps) {
  const theme = useAppTheme();
  const data = useMemo(
    () =>
      chunkArray(upgrades, 5).map((chunk, index) => ({
        id: String(index),
        upgrades: chunk,
      })),
    [upgrades],
  );
  const columns = useMemo<AppVirtualTableColumnDef<PackageChunkRow>[]>(
    () =>
      Array.from({ length: 5 }, (_, index) => ({
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
      })),
    [theme.typography.fontFamily],
  );

  return (
    <>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Packages Installed:</b>
      </AppTypography>
      <AppVirtualTable
        ariaLabel={`Packages installed on ${date}`}
        columns={columns}
        data={data}
        density="compact"
        emptyMessage="No packages recorded."
        fillAvailable={false}
        getRowId={getPackageChunkRowId}
        maxHeight={260}
        showHeader={false}
        variant="embedded"
      />
    </>
  );
});

const UpdateHistory = () => {
  const theme = useAppTheme();
  const { data: rows } = useSuspenseQuery(linuxio.updates.get_update_history);

  const columns: AppVirtualTableColumnDef<(typeof rows)[number]>[] = [
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
  return (
    <AppVirtualTable
      ariaLabel="Update history"
      columns={columns}
      data={rows}
      emptyMessage="No update history available."
      fillAvailable
      // `date` is the backend's history-map key, so it is unique per row and
      // stable across reloads — unlike the array index, which shifts as new
      // entries arrive and would re-expand the wrong row.
      getRowId={(row) => row.date}
      getRowCanExpand={(row) => row.original.upgrades.length > 0}
      persistExpandedKey="update-history"
      renderExpandedContent={({ original: row }) => (
        <PackageHistoryTable date={row.date} upgrades={row.upgrades} />
      )}
    />
  );
};
export default UpdateHistory;
