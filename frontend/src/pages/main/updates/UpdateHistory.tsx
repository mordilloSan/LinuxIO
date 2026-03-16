import { Icon } from "@iconify/react";
import { Table, TableBody, TableCell, TableRow } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import { linuxio } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppChip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { responsiveTextStyles } from "@/theme/tableStyles";
const chunkArray = <T,>(array: T[], chunkSize: number): T[][] => {
  const result: T[][] = [];
  for (let i = 0; i < array.length; i += chunkSize) {
    result.push(array.slice(i, i + chunkSize));
  }
  return result;
};
const UpdateHistory: React.FC = () => {
  const theme = useTheme();
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
      sx: {
        width: {
          xs: 112,
          sm: 148,
        },
        minWidth: {
          xs: 112,
          sm: 148,
        },
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
          <TableCell>
            <AppTypography
              variant="body2"
              fontWeight={500}
              style={responsiveTextStyles}
            >
              {row.date}
            </AppTypography>
          </TableCell>
          <TableCell
            align="center"
            sx={{
              width: {
                xs: 112,
                sm: 148,
              },
              minWidth: {
                xs: 112,
                sm: 148,
              },
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
          </TableCell>
        </>
      )}
      renderExpandedContent={(row) => (
        <>
          <AppTypography variant="subtitle2" gutterBottom>
            <b>Packages Installed:</b>
          </AppTypography>
          <Table
            size="small"
            sx={{
              borderCollapse: "collapse",
              "& .MuiTableCell-root": {
                border: "none",
              },
              overflowX: "auto",
              display: "block",
            }}
          >
            <TableBody>
              {chunkArray(row.upgrades, 5).map((group, i) => (
                <TableRow key={i}>
                  {group.map((pkg, j) => (
                    <TableCell
                      key={j}
                      sx={{
                        width: "20%",
                        padding: "8px 12px",
                        color: "text.secondary",
                        fontFamily: theme.typography.fontFamily,
                        fontSize: "0.85rem",
                        ...responsiveTextStyles,
                      }}
                    >
                      {pkg.package}
                    </TableCell>
                  ))}
                  {group.length < 5 &&
                    [...Array(5 - group.length)].map((_, j) => (
                      <TableCell
                        key={`empty-${j}`}
                        sx={{
                          width: "20%",
                          border: "none",
                        }}
                      />
                    ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </>
      )}
      emptyMessage="No update history available."
    />
  );
};
export default UpdateHistory;
