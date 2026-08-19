import type { CSSProperties } from "react";

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

export const wrappableChipStyle: CSSProperties = {
  maxWidth: "100%",
  height: "auto",
};

export const wrappableChipLabelStyle: CSSProperties = {
  display: "block",
  whiteSpace: "normal",
  wordBreak: "break-word",
  overflowWrap: "anywhere",
};
