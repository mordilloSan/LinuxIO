import React from "react";

import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface LogoDisplayDarkProps {
  showText?: boolean;
}

const LogoDisplayDark: React.FC<LogoDisplayDarkProps> = ({
  showText = false,
}) => {
  const theme = useAppTheme();

  return (
    <div
      style={{
        display: "inline-flex",
        alignItems: "center",
        color: alpha(theme.palette.common.white, 0.87),
        fontFamily: theme.typography.fontFamily,
        fontSize: "1.75rem",
        fontWeight: 400,
        lineHeight: 1.25,
      }}
    >
      {showText && (
        <span
          style={{
            display: "inline-block",
            whiteSpace: "nowrap",
            marginRight: 4,
          }}
        >
          Linux
        </span>
      )}

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

export default LogoDisplayDark;
