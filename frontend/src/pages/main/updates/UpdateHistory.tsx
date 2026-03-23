import { Icon } from "@iconify/react";
import { useAppTheme } from "@/theme";
import React from "react";

import { linuxio } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppChip from "@/components/ui/AppChip";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTypography from "@/components/ui/AppTypography";
const chunkArray = <T,>(array: T[], chunkSize: number): T[][] => {
  const result: T[][] = [];
  for (let i = 0; i < array.length; i += chunkSize) {
    result.push(array.slice(i, i + chunkSize));
  }
  return result;
};
const UpdateHistory: React.FC = () => {
  const theme = useAppTheme();
  const { data: rows = [] } = linuxio.dbus.get_update_history.useQuery();
  const columns: UnifiedTableColumn[] = [
    {
      field: "date",
      headerName: "Date",
      align: "left",
    },
    {
      field: "packages",
      headerName: "Packages Updated",
      align: "center",
      style: {
        width: 148,
        minWidth: 112,
        whiteSpace: "nowrap",
      },
    },
  ];
  return (
    <UnifiedCollapsibleTable
      data={rows}
      columns={columns}
      getRowKey={(row, index) => index}
      renderFirstCell={() => (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            color: theme.palette.primary.main,
          }}
        >
          <Icon icon="mdi:history" width={20} height={20} />
        </div>
      )}
      renderMainRow={(row) => (
        <>
          <AppTableCell>
            <AppTypography
              variant="body2"
              fontWeight={500}
              style={{
                wordBreak: "break-word",
                overflowWrap: "break-word",
              }}
            >
              {row.date}
            </AppTypography>
          </AppTableCell>
          <AppTableCell
            align="center"
            style={{
              width: 148,
              minWidth: 112,
            }}
          >
            <AppChip
              label={row.upgrades.length}
              size="small"
              color="success"
              variant="soft"
              style={{
                minWidth: 40,
              }}
            />
          </AppTableCell>
        </>
      )}
      renderExpandedContent={(row) => (
        <>
          <AppTypography variant="subtitle2" gutterBottom>
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
                        color: "var(--mui-palette-text-secondary)",
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
      emptyMessage="No update history available."
    />
  );
};
export default UpdateHistory;
