import { Icon } from "@iconify/react";
import {
  Chip,
  Table,
  TableBody,
  TableCell,
  TableRow,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import { linuxio } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
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
    { field: "date", headerName: "Date", align: "left" },
    {
      field: "packages",
      headerName: "Packages Updated",
      align: "center",
      sx: {
        width: { xs: 112, sm: 148 },
        minWidth: { xs: 112, sm: 148 },
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
            <Typography
              variant="body2"
              fontWeight="medium"
              sx={responsiveTextStyles}
            >
              {row.date}
            </Typography>
          </TableCell>
          <TableCell
            align="center"
            sx={{
              width: { xs: 112, sm: 148 },
              minWidth: { xs: 112, sm: 148 },
            }}
          >
            <Chip
              label={row.upgrades.length}
              size="small"
              color="success"
              sx={{ minWidth: 40 }}
            />
          </TableCell>
        </>
      )}
      renderExpandedContent={(row) => (
        <>
          <Typography variant="subtitle2" gutterBottom>
            <b>Packages Installed:</b>
          </Typography>
          <Table
            size="small"
            sx={{
              borderCollapse: "collapse",
              "& .MuiTableCell-root": { border: "none" },
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
                        sx={{ width: "20%", border: "none" }}
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
