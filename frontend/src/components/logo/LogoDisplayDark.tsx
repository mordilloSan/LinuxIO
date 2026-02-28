import { Box, Typography } from "@mui/material";
import { alpha, useTheme } from "@mui/material/styles";
import React from "react";

interface LogoDisplayDarkProps {
  showText?: boolean;
}

const LogoDisplayDark: React.FC<LogoDisplayDarkProps> = ({
  showText = false,
}) => {
  const theme = useTheme();

  return (
    <Typography
      variant="h6"
      noWrap
      sx={{
        fontWeight: 400,
        fontSize: "1.75rem",
        display: "inline-flex",
        alignItems: "center",
        color: alpha(theme.palette.common.white, 0.87),
        fontFamily:
          '"Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol"',
      }}
    >
      {showText && (
        <Box
          component="span"
          sx={{
            display: "inline-block",
            whiteSpace: "nowrap",
            mr: 1, // space before the badge
          }}
        >
          Linux
        </Box>
      )}

      <Box
        sx={{
          width: 36,
          height: 36,
          borderRadius: "50%",
          border: `3px solid ${theme.palette.primary.main}`,
          display: "flex",
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
      </Box>
    </Typography>
  );
};

export default LogoDisplayDark;
