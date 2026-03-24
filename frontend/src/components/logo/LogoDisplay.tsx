import React from "react";

import { useAppTheme } from "@/theme";

interface LogoDisplayProps {
  showText?: boolean;
}

const LogoDisplay: React.FC<LogoDisplayProps> = ({ showText = false }) => {
  const theme = useAppTheme();
  const dur = theme.transitions.duration.standard;

  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        margin: 0,
        fontFamily: theme.typography.fontFamily,
        fontSize: "1.45rem",
        fontWeight: 400,
        lineHeight: 1.25,
      }}
    >
      <span
        style={{
          color: theme.palette.text.primary,
          display: "inline-block",
          whiteSpace: "nowrap",
          opacity: showText ? 1 : 0,
          marginRight: showText ? 8 : -50,
          transition: `opacity ${dur}ms ease-in-out, margin-right ${dur}ms ease-in-out`,
        }}
      >
        Linux
      </span>

      <span
        style={{
          width: 36,
          height: 36,
          borderRadius: "50%",
          border: `3px solid ${theme.palette.primary.main}`,
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          fontWeight: 900,
          color: theme.palette.primary.main,
          fontSize: "0.95rem",
          whiteSpace: "nowrap",
          boxSizing: "border-box",
        }}
      >
        i/O
      </span>
    </div>
  );
};

export default LogoDisplay;
