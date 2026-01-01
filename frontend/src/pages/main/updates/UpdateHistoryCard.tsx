import { Typography, Box } from "@mui/material";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import React from "react";

import { linuxio } from "@/api/linuxio";
import CollapsibleTable from "@/components/tables/CollapsibleTable";
import { CollapsibleColumn } from "@/types/tables";

interface UpgradeItem {
  package: string;
}

interface UpdateHistoryRow {
  date: string;
  upgrades: UpgradeItem[];
}

const columns: CollapsibleColumn[] = [{ field: "date", headerName: "Date" }];

const chunkArray = <T,>(array: T[], chunkSize: number): T[][] => {
  const result: T[][] = [];
  for (let i = 0; i < array.length; i += chunkSize) {
    result.push(array.slice(i, i + chunkSize));
  }
  return result;
};

const UpdateHistoryCard: React.FC = () => {
  const { data: rows = [] } = linuxio.call<UpdateHistoryRow[]>(
    "dbus",
    "GetUpdateHistory",
    [],
    {},
  );

  const renderCollapseContent = (row: UpdateHistoryRow) => {
    const chunked = chunkArray(row.upgrades, 5);

    return (
      <>
        <Typography variant="h6" gutterBottom>
          Packages Installed
        </Typography>
        <Table size="small" sx={{ borderCollapse: "collapse" }}>
          <TableBody>
            {chunked.map((group, i) => (
              <TableRow key={i}>
                {group.map((pkg, j) => (
                  <TableCell
                    key={j}
                    sx={{
                      width: "20%",
                      border: "none", // remove cell borders
                      padding: "8px 12px", // optional: add cleaner spacing
                      color: "text.secondary",
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
    );
  };

  return (
    <Box>
      <CollapsibleTable
        rows={rows}
        columns={columns}
        renderCollapseContent={renderCollapseContent}
      />
    </Box>
  );
};

export default UpdateHistoryCard;
