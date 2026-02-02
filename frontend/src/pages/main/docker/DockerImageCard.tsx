import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import {
  Box,
  CardContent,
  IconButton,
  Collapse,
  Typography,
} from "@mui/material";
import { useMemo, useState } from "react";

import { cardBorderRadius } from "@/constants";
import { CollapsibleTableProps } from "@/types/collapsible";

interface Props<T extends Record<string, any>> {
  row: T;
  columns: CollapsibleTableProps<T>["columns"];
  renderCollapseContent: CollapsibleTableProps<T>["renderCollapseContent"];
  selected: boolean;
  onToggleSelected: () => void;
};

export default function CollapsibleCard<T extends Record<string, any>>({
  row,
  columns,
  renderCollapseContent,
  selected,
  onToggleSelected,
}: Props<T>) {
  const [open, setOpen] = useState(false);

  const leftColor = useMemo(
    () => (selected || open ? "primary.main" : "transparent"),
    [selected, open],
  );

  return (
    <Box
      onClick={onToggleSelected}
      sx={{
        mb: 2,
        position: "relative",
        borderLeft: 4,
        borderLeftColor: leftColor,
        borderRadius: cardBorderRadius,
        cursor: "pointer",
        overflow: "visible",
        transition:
          "border-left-color 160ms ease, transform 140ms ease-out, box-shadow 140ms ease-out",
        transformOrigin: "left center", // scale out from the accent
        "&:hover": {
          transform: "scale(1.005)",
          boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
        },
      }}
    >
      {/* frosted bg (not the text) */}
      <Box
        sx={{
          position: "absolute",
          inset: 0,
          borderRadius: cardBorderRadius,
          backgroundColor: (t) =>
            t.palette.mode === "dark"
              ? "rgba(255,255,255,0.08)"
              : "rgba(0,0,0,0.05)",
          backdropFilter: (t) =>
            t.palette.mode === "dark" ? "blur(12px)" : "blur(6px)",
          zIndex: 0,
        }}
      />

      {/* content */}
      <Box sx={{ position: "relative", zIndex: 1 }}>
        <CardContent
          sx={{
            display: "flex",
            flexDirection: { xs: "column", sm: "row" },
            alignItems: { xs: "flex-start", sm: "center" },
            minHeight: 64,
            gap: { xs: 0.5, sm: 1 },
          }}
        >
          {columns.map((col) => (
            <Box key={col.field} sx={{ flex: 1, minWidth: 0 }}>
              <Typography
                variant={col.field === "repo" ? "subtitle1" : "body2"}
                color="text.primary"
              >
                {(row as any)[col.field]}
              </Typography>
            </Box>
          ))}

          {/* ONLY chevron expands */}
          <IconButton
            size="small"
            onClick={(e) => {
              e.stopPropagation();
              setOpen((o) => !o);
            }}
          >
            {open ? <ExpandLessIcon /> : <ExpandMoreIcon />}
          </IconButton>
        </CardContent>

        <Collapse in={open} unmountOnExit>
          <Box sx={{ px: { xs: 1, sm: 2 }, pb: 2 }}>
            {renderCollapseContent(row)}
          </Box>
        </Collapse>
      </Box>
    </Box>
  );
}
