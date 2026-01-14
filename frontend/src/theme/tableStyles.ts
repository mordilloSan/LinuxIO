import { Theme } from "@mui/material/styles";

export const getTableHeaderStyles = (theme: Theme) => ({
  "& .MuiTableCell-root": { borderBottom: "none" },
  backgroundColor:
    theme.palette.mode === "dark"
      ? "rgba(255,255,255,0.08)"
      : "rgba(0,0,0,0.08)",
  borderRadius: "6px",
  boxShadow: "none",
});

export const getTableRowStyles = (theme: Theme, index: number) => ({
  "& .MuiTableCell-root": { borderBottom: "none" },
  backgroundColor:
    index % 2 === 0
      ? "transparent"
      : theme.palette.mode === "dark"
        ? "rgba(255,255,255,0.04)"
        : "rgba(0,0,0,0.05)",
});

export const getExpandedRowStyles = (theme: Theme, index: number) => ({
  "& .MuiTableCell-root": { borderBottom: "none" },
  backgroundColor:
    index % 2 === 0
      ? "transparent"
      : theme.palette.mode === "dark"
        ? "rgba(255,255,255,0.08)"
        : "rgba(0,0,0,0.05)",
});

export const getExpandedContentStyles = (theme: Theme) => ({
  margin: 2,
  borderRadius: 2,
  p: 2,
  bgcolor:
    theme.palette.mode === "dark"
      ? "rgba(255,255,255,0.05)"
      : "rgba(0,0,0,0.03)",
  maxWidth: "100%",
  overflowX: "auto",
  overflowWrap: "break-word" as const,
  wordBreak: "break-word" as const,
  "@media (max-width: 600px)": {
    margin: 1,
    padding: 1,
  },
});

export const tableContainerStyles = {
  overflowX: "auto",
  "@media (max-width: 600px)": {
    "& .MuiTable-root": {
      minWidth: "100%",
    },
    "& .MuiTableCell-root": {
      fontSize: "0.75rem",
      padding: "8px 4px",
    },
  },
};

export const responsiveTextStyles = {
  wordBreak: "break-word" as const,
  overflowWrap: "break-word" as const,
  "@media (max-width: 600px)": {
    fontSize: "0.75rem",
  },
};

export const longTextStyles = {
  wordBreak: "break-all" as const,
  overflowWrap: "anywhere" as const,
  maxWidth: "100%",
  whiteSpace: "pre-wrap" as const,
  "@media (max-width: 600px)": {
    fontSize: "0.75rem",
  },
};

export const wrappableChipStyles = {
  maxWidth: "100%",
  height: "auto",
  "& .MuiChip-label": {
    display: "block",
    whiteSpace: "normal",
    wordBreak: "break-word" as const,
    overflowWrap: "anywhere" as const,
  },
};
