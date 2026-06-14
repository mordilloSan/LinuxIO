import { Icon } from "@iconify/react";
import React from "react";

import { linuxio } from "@/api";
import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type { AppVirtualDataTableColumnDef } from "@/components/tables/AppVirtualDataTable";
import AppChip from "@/components/ui/AppChip";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
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
  const columns: AppVirtualDataTableColumnDef<(typeof rows)[number]>[] = [
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
    <AppVirtualDataTable
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
          <AppTable
            style={{
              borderCollapse: "collapse",
              overflowX: "auto",
              display: "block",
            }}
          >
            <AppTableBody>
              {chunkArray(row.upgrades, 5).map((group, i) => (
                <AppTableRow key={i}>
                  {group.map((pkg, j) => (
                    <AppTableCell
                      key={j}
                      style={{
                        width: "20%",
                        padding: "8px 12px",
                        color: "var(--app-palette-text-secondary)",
                        fontFamily: theme.typography.fontFamily,
                        fontSize: "0.85rem",
                        wordBreak: "break-word",
                        overflowWrap: "break-word",
                      }}
                    >
                      {pkg.package}
                    </AppTableCell>
                  ))}
                  {group.length < 5 &&
                    [...Array(5 - group.length)].map((_, j) => (
                      <AppTableCell
                        key={`empty-${j}`}
                        style={{ width: "20%" }}
                      />
                    ))}
                </AppTableRow>
              ))}
            </AppTableBody>
          </AppTable>
        </>
      )}
    />
  );
};
export default UpdateHistory;
