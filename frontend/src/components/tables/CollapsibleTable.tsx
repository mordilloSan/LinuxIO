import Box from "@mui/material/Box";
import Paper from "@mui/material/Paper";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";

import CollapsibleRow from "./CollapsibleRow";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import { CollapsibleTableProps } from "@/types/collapsible";

const CollapsibleTable = <T extends Record<string, any>>({
  rows,
  columns,
  renderCollapseContent,
}: CollapsibleTableProps<T>) => {
  return (
    <Box sx={{ padding: 2 }}>
      <TableContainer
        component={Paper}
        sx={{ paddingLeft: "16px", paddingRight: "16px" }}
      >
        <Table aria-label="collapsible table">
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: "50px" }} />
              {columns.map((column, index) => (
                <TableCell key={index} align={column.align || "left"}>
                  {column.headerName}
                </TableCell>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.length > 0 ? (
              rows.map((row, index) => (
                <CollapsibleRow
                  key={index}
                  row={row}
                  isLast={index === rows.length - 1}
                  columns={columns}
                  renderCollapseContent={renderCollapseContent}
                />
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={columns.length + 1} align="center">
                  <ComponentLoader />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
};

export default CollapsibleTable;
