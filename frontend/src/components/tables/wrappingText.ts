import type { CSSProperties } from "react";

import "./wrapping-text.css";

/* className for text that may wrap anywhere and shrinks on phones. */
export const RESPONSIVE_TEXT_CLASS = "wrap-text-responsive";

/* className for unbroken machine values (ids, digests, addresses). */
export const LONG_TEXT_CLASS = "wrap-text-long";

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
