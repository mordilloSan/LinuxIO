import { Box, Typography } from "@mui/material";
import React from "react";

// same colors as your local dark theme
const PALETTE = {
  primary: "#1976d2",
  text: "rgba(255,255,255,0.87)",
};

type LogoDisplayDarkProps = {
  showText?: boolean;
};

const LogoDisplayDark: React.FC<LogoDisplayDarkProps> = ({
  showText = false,
}) => {
  return (
    <Typography
      variant="h6"
      noWrap
      sx={{
        fontWeight: 400,
        fontSize: "1.75rem",
        display: "inline-flex",
        alignItems: "center",
        color: PALETTE.text,
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
          border: `3px solid ${PALETTE.primary}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontWeight: 900,
          color: PALETTE.primary,
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
