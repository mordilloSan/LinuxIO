import { Paper } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { alpha } from "@/utils/color";
import { keyframes } from "@mui/system";
import React from "react";

import LoginComponent from "@/components/auth/Login";
import AppTypography from "@/components/ui/AppTypography";

import "./login-page.css";

const liftIn = keyframes`
  from { opacity: 0; transform: translateY(18px) scale(0.98); }
  to { opacity: 1; transform: translateY(0) scale(1); }
`;

const Login: React.FC = () => {
  const theme = useTheme();

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
        }}
      >
        <AppTypography
          fontWeight={600}
          fontSize="0.82rem"
          color="text.primary"
          style={{ letterSpacing: "0.06em" }}
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

      <Paper
        sx={(theme) => ({
          p: { xs: 3, sm: 4.5 },
          pt: { xs: 6, sm: 7 },
          borderRadius: 4,
          backgroundColor: alpha(theme.palette.background.default, 0.9),
          border: `1px solid ${alpha(theme.palette.text.secondary, 0.2)}`,
          boxShadow: `0 26px 60px -40px ${alpha(theme.palette.common.black, 0.75)}`,
          backdropFilter: "blur(14px)",
          opacity: 0,
          transform: "translateY(18px) scale(0.98)",
          animation: `${liftIn} 0.5s ease forwards`,
          "@media (prefers-reduced-motion: reduce)": {
            animation: "none",
            opacity: 1,
            transform: "none",
          },
        })}
      >
        <div
          style={{
            display: "grid",
            gap: theme.spacing(1),
            textAlign: "center",
            marginBottom: theme.spacing(2),
          }}
        >
          <AppTypography variant="h4">Welcome back</AppTypography>
          <AppTypography variant="body2" color="text.secondary">
            Sign in to manage your Linux i/O instance.
          </AppTypography>
        </div>
        <LoginComponent />
      </Paper>
    </div>
  );
};

export default Login;
