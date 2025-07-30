import { ExpandMore, ExpandLess } from "@mui/icons-material";
import {
  Box,
  Card,
  CardContent,
  IconButton,
  Collapse,
  Typography,
} from "@mui/material";
import { useState } from "react";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import { CollapsibleTableProps } from "@/types/collapsible";

// List container
export function CollapsibleCardList<T extends Record<string, any>>({
  rows,
  columns,
  renderCollapseContent,
}: CollapsibleTableProps<T>) {
  if (!rows.length)
    return (
      <Box
        sx={{ width: "100%", mt: 4, display: "flex", justifyContent: "center" }}
      >
        <ComponentLoader />
      </Box>
    );

  return (
    <Box
      sx={{
        width: "100vw",
        maxWidth: "100vw",
        overflowX: "hidden",
        mx: "auto",
        mt: 2,
        px: { xs: 0, sm: 0 },
        boxSizing: "border-box",
      }}
    >
      {rows.map((row, idx) => (
        <CollapsibleCard
          key={row.id || idx}
          row={row}
          columns={columns}
          renderCollapseContent={renderCollapseContent}
        />
      ))}
    </Box>
  );
}

// Card
function CollapsibleCard<T extends Record<string, any>>({
  row,
  columns,
  renderCollapseContent,
}: {
  row: T;
  columns: CollapsibleTableProps<T>["columns"];
  renderCollapseContent: CollapsibleTableProps<T>["renderCollapseContent"];
}) {
  const [open, setOpen] = useState(false);

  return (
    <Card
      variant="outlined"
      sx={{
        mb: 2,
        width: "85%",
        borderRadius: 2,
        boxShadow: open ? 4 : 1,
        cursor: "pointer",
        transition:
          "box-shadow 0.2s, transform 0.17s cubic-bezier(.42,1.42,.62,1.03)",
        borderLeft: 4,
        borderLeftColor: open ? "primary.main" : "transparent",
        "&:hover": {
          boxShadow: 8,
          borderLeft: 4,
          borderLeftColor: "primary.main",
          zIndex: 2,
          background: (theme) =>
            theme.palette.mode === "dark"
              ? "rgba(60,130,246,0.07)"
              : "rgba(33,150,243,0.09)",
        },
      }}
      onClick={() => setOpen((v) => !v)}
    >
      <CardContent
        sx={{
          display: "flex",
          flexDirection: { xs: "column", sm: "row" },
          alignItems: { xs: "flex-start", sm: "center" },
          minHeight: 64,
        }}
      >
        {columns.map((col) => (
          <Box
            key={col.field}
            sx={{
              flex: 1,
            }}
          >
            <Typography
              variant={col.field === "repo" ? "subtitle1" : "body2"}
              color="text.primary"
              noWrap={false}
            >
              {row[col.field]}
            </Typography>
          </Box>
        ))}
        <IconButton
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            setOpen((o) => !o);
          }}
        >
          {open ? <ExpandLess /> : <ExpandMore />}
        </IconButton>
      </CardContent>
      <Collapse in={open}>
        <Box
          sx={{
            px: { xs: 1, sm: 2 },
            pb: 2,
          }}
        >
          {renderCollapseContent(row)}
        </Box>
      </Collapse>
    </Card>
  );
}

export default CollapsibleCardList;
