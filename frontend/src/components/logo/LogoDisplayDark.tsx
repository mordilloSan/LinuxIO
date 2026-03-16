import { useTheme } from "@mui/material/styles";
import React from "react";

import { alpha } from "@/utils/color";
import AppTypography from "@/components/ui/AppTypography";

interface LogoDisplayDarkProps {
  showText?: boolean;
}

const LogoDisplayDark: React.FC<LogoDisplayDarkProps> = ({
  showText = false,
}) => {
  const theme = useTheme();

  return (
    <AppTypography
      variant="h6"
      noWrap
      fontWeight={400}
      fontSize="1.75rem"
      style={{
        display: "inline-flex",
        alignItems: "center",
        color: alpha(theme.palette.common.white, 0.87),
        fontFamily:
          '"Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol"',
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
    </AppTypography>
  );
};

export default LogoDisplayDark;
