import { useMediaQuery } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import LoginComponent from "@/components/auth/Login";
import AppPaper from "@/components/ui/AppPaper";
import AppTypography from "@/components/ui/AppTypography";
import { alpha } from "@/utils/color";

import "./login-page.css";

const Login: React.FC = () => {
  const theme = useTheme();
  const isSmallUp = useMediaQuery(theme.breakpoints.up("sm"));
  const paperRadius = Number(theme.shape.borderRadius) * 4;
  const paperOverlay = `linear-gradient(${alpha(theme.palette.common.white, 0.051)}, ${alpha(theme.palette.common.white, 0.051)})`;

  return (
    <div style={{ width: "100%", maxWidth: 520, position: "relative" }}>
      <div
        className="login-badge"
        style={{
          borderRadius: "9999px",
          border: `1px solid ${alpha(theme.palette.text.secondary, 0.25)}`,
          background: `linear-gradient(160deg, ${alpha(theme.palette.background.paper, 0.95)} 0%, ${alpha(theme.palette.background.default, 0.92)} 100%)`,
          boxShadow: `0 24px 54px -36px ${alpha(theme.palette.common.black, 0.85)}`,
          display: "inline-flex",
          alignItems: "center",
          gap: 4,
          backdropFilter: "blur(10px)",
          WebkitBackdropFilter: "blur(10px)",
        }}
      >
        <AppTypography
          fontWeight={600}
          fontSize={isSmallUp ? "0.82rem" : "0.78rem"}
          color="text.primary"
          style={{
            fontFamily: theme.typography.fontFamily,
            letterSpacing: "0.06em",
          }}
        >
          Linux
        </AppTypography>
        <div
          className="login-badge-icon"
          style={{
            borderRadius: "50%",
            border: `2px solid ${theme.palette.primary.main}`,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontWeight: 700,
            color: theme.palette.primary.main,
            fontSize: "0.8rem",
            letterSpacing: "-0.02em",
          }}
        >
          i/O
        </div>
      </div>

      <AppPaper
        className="login-paper"
        style={{
          borderRadius: paperRadius,
          backgroundColor: alpha(theme.palette.background.default, 0.9),
          backgroundImage: paperOverlay,
          border: `1px solid ${alpha(theme.palette.text.secondary, 0.2)}`,
          boxShadow: `0 26px 60px -40px ${alpha(theme.palette.common.black, 0.75)}`,
          backdropFilter: "blur(14px)",
          WebkitBackdropFilter: "blur(14px)",
        }}
      >
        <div
          style={{
            display: "grid",
            gap: theme.spacing(1),
            textAlign: "center",
            marginBottom: theme.spacing(2),
          }}
        >
          <AppTypography
            variant="h4"
            style={{
              fontFamily: theme.typography.fontFamily,
              fontSize: "2.125rem",
              fontWeight: 600,
              letterSpacing: "-0.02em",
              lineHeight: 1.235,
            }}
          >
            Welcome back
          </AppTypography>
          <AppTypography
            variant="body2"
            style={{
              fontFamily: theme.typography.fontFamily,
              fontSize: "0.875rem",
              lineHeight: 1.43,
              color: theme.palette.text.secondary,
            }}
          >
            Sign in to manage your Linux i/O instance.
          </AppTypography>
        </div>
        <LoginComponent />
      </AppPaper>
    </div>
  );
};

export default Login;
