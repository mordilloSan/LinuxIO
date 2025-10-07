import KeyboardArrowDownIcon from "@mui/icons-material/KeyboardArrowDown";
import KeyboardArrowUpIcon from "@mui/icons-material/KeyboardArrowUp";
import { Box, TableRow, TableCell, IconButton, Collapse } from "@mui/material";
import { useState } from "react";

import { CollapsibleTableProps } from "@/types/collapsible";

const CollapsibleRow = <T extends Record<string, any>>({
  row,
  isLast,
  columns,
  renderCollapseContent,
}: {
  row: T;
  isLast: boolean;
  columns: CollapsibleTableProps<T>["columns"];
  renderCollapseContent: CollapsibleTableProps<T>["renderCollapseContent"];
}) => {
  const [open, setOpen] = useState(false);

  return (
    <>
      <TableRow>
        <TableCell
          sx={{
            width: "50px",
            borderBottom:
              open || isLast ? "none" : "1px solid rgba(255,255,255,0.1)",
          }}
        >
          <IconButton
            aria-label="expand row"
            size="small"
            onClick={() => setOpen(!open)}
            disableRipple
            sx={{
              backgroundColor: "transparent !important",
              "&:hover, &:focus": {
                backgroundColor: "transparent !important",
                boxShadow: "none",
              },
              "&:focus-visible": {
                outline: "none",
              },
            }}
          >
            {open ? <KeyboardArrowUpIcon /> : <KeyboardArrowDownIcon />}
          </IconButton>
        </TableCell>
        {columns.map((column, index) => (
          <TableCell
            key={index}
            align={column.align || "left"}
            sx={{
              borderBottom:
                open || isLast ? "none" : "1px solid rgba(255,255,255,0.1)",
            }}
          >
            {row[column.field]}
          </TableCell>
        ))}
      </TableRow>

      {open && (
        <TableRow>
          <TableCell
            colSpan={columns.length + 1}
            sx={{
              paddingTop: 0,
              paddingBottom: 0,
              borderBottom: isLast ? "none" : "1px solid rgba(255,255,255,0.1)",
            }}
          >
            <Collapse in={open} timeout="auto" unmountOnExit>
              <Box sx={{ margin: 1, marginTop: 5 }}>
                {renderCollapseContent(row)}
              </Box>
            </Collapse>
          </TableCell>
        </TableRow>
      )}
    </>
  );
};

export default CollapsibleRow;
