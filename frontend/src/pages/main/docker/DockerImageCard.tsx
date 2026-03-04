import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import {
  CardContent,
  Collapse,
  IconButton,
  Typography,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import { useMemo, useState } from "react";

import { cardBorderRadius } from "@/constants";
import { CollapsibleTableProps } from "@/types/collapsible";

interface Props<T extends Record<string, any>> {
  row: T;
  columns: CollapsibleTableProps<T>["columns"];
  renderCollapseContent: CollapsibleTableProps<T>["renderCollapseContent"];
  selected: boolean;
  onToggleSelected: () => void;
}

export default function CollapsibleCard<T extends Record<string, any>>({
  row,
  columns,
  renderCollapseContent,
  selected,
  onToggleSelected,
}: Props<T>) {
  const [open, setOpen] = useState(false);
  const theme = useTheme();
  const isSmallScreen = useMediaQuery(theme.breakpoints.down("sm"));
  const baseBorderRadius =
    typeof theme.shape.borderRadius === "number"
      ? theme.shape.borderRadius
      : Number.parseFloat(theme.shape.borderRadius);

  const leftColor = useMemo(
    () => (selected || open ? theme.palette.primary.main : "transparent"),
    [open, selected, theme.palette.primary.main],
  );

  return (
    <div
      onClick={onToggleSelected}
      style={{
        marginBottom: theme.spacing(2),
        position: "relative",
        borderLeft: `4px solid ${leftColor}`,
        borderRadius: baseBorderRadius * cardBorderRadius,
        cursor: "pointer",
        overflow: "visible",
        transition:
          "border-left-color 160ms ease, transform 140ms ease-out, box-shadow 140ms ease-out",
        transformOrigin: "left center", // scale out from the accent
      }}
      onMouseEnter={(event) => {
        event.currentTarget.style.transform = "scale(1.005)";
        event.currentTarget.style.boxShadow =
          "0 8px 24px rgba(var(--mui-palette-common-blackChannel) / 0.35)";
      }}
      onMouseLeave={(event) => {
        event.currentTarget.style.transform = "none";
        event.currentTarget.style.boxShadow = "none";
      }}
    >
      {/* frosted bg (not the text) */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          borderRadius: baseBorderRadius * cardBorderRadius,
          backgroundColor: alpha(
            theme.palette.text.primary,
            theme.palette.mode === "dark" ? 0.08 : 0.05,
          ),
          backdropFilter:
            theme.palette.mode === "dark" ? "blur(12px)" : "blur(6px)",
          zIndex: 0,
        }}
      />

      {/* content */}
      <div style={{ position: "relative", zIndex: 1 }}>
        <CardContent
          sx={{
            display: "flex",
            flexDirection: isSmallScreen ? "column" : "row",
            alignItems: isSmallScreen ? "flex-start" : "center",
            minHeight: 64,
            gap: isSmallScreen ? 0.5 : 1,
          }}
        >
          {columns.map((col) => (
            <div key={col.field} style={{ flex: 1, minWidth: 0 }}>
              <Typography
                variant={col.field === "repo" ? "subtitle1" : "body2"}
                color="text.primary"
              >
                {(row as any)[col.field]}
              </Typography>
            </div>
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
          <div
            style={{
              paddingLeft: isSmallScreen ? theme.spacing(1) : theme.spacing(2),
              paddingRight: isSmallScreen ? theme.spacing(1) : theme.spacing(2),
              paddingBottom: theme.spacing(2),
            }}
          >
            {renderCollapseContent(row)}
          </div>
        </Collapse>
      </div>
    </div>
  );
}
