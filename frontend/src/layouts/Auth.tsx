import { CssBaseline } from "@mui/material";
import { useMediaQuery } from "@mui/material";
import { alpha, ThemeProvider } from "@mui/material/styles";
import React from "react";
import { Outlet } from "react-router-dom";

import authTheme from "@/theme/authTheme";

const Auth: React.FC = () => {
  return (
    <ThemeProvider theme={authTheme}>
      <CssBaseline />
      <AuthContent />
    </ThemeProvider>
  );
};

const AuthContent: React.FC = () => {
  const isSmallUp = useMediaQuery(authTheme.breakpoints.up("sm"));

  return (
    <div
      style={{
        ["--accent" as string]: authTheme.palette.primary.main,
        ["--accent-strong" as string]: authTheme.palette.primary.dark,
        ["--accent-soft" as string]: authTheme.palette.primary.light,
        minHeight: "100vh",
        width: "100%",
        display: "grid",
        placeItems: "center",
        position: "relative",
        overflowX: "hidden",
        overflowY: "auto",
        paddingLeft: isSmallUp ? authTheme.spacing(4) : authTheme.spacing(2),
        paddingRight: isSmallUp ? authTheme.spacing(4) : authTheme.spacing(2),
        paddingTop: isSmallUp ? authTheme.spacing(8) : authTheme.spacing(6),
        paddingBottom: isSmallUp ? authTheme.spacing(8) : authTheme.spacing(6),
        backgroundImage: `radial-gradient(900px 420px at 12% 8%, ${alpha(authTheme.palette.primary.main, 0.25)}, ${alpha(authTheme.palette.background.default, 0)} 60%), radial-gradient(800px 360px at 90% 0%, ${alpha(authTheme.palette.primary.light, 0.2)}, ${alpha(authTheme.palette.background.default, 0)} 60%), linear-gradient(160deg, ${alpha(authTheme.palette.background.default, 0.92)} 0%, ${authTheme.palette.background.default} 45%, ${alpha(authTheme.palette.background.default, 0.72)} 100%)`,
      }}
    >
      <div
        aria-hidden
        style={{
          position: "absolute",
          top: -140,
          right: -180,
          width: 420,
          height: 420,
          borderRadius: "50%",
          background: `radial-gradient(circle at 30% 30%, ${alpha(authTheme.palette.primary.main, 0.35)}, ${alpha(authTheme.palette.primary.main, 0)} 70%)`,
          opacity: 0.9,
        }}
      />
      <div
        aria-hidden
        style={{
          position: "absolute",
          bottom: -180,
          left: -160,
          width: 460,
          height: 460,
          borderRadius: "50%",
          background: `radial-gradient(circle at 70% 40%, ${alpha(authTheme.palette.primary.dark, 0.3)}, ${alpha(authTheme.palette.primary.dark, 0)} 70%)`,
          opacity: 0.8,
        }}
      />
      <div
        style={{
          position: "relative",
          zIndex: 1,
          width: "100%",
          display: "flex",
          justifyContent: "center",
        }}
      >
        <Outlet />
      </div>
    </div>
  );
};

export default Auth;
