import React from "react";
import { Outlet } from "react-router-dom";

import { AppThemeProvider, useAppMediaQuery, useAppTheme } from "@/theme";
import authTheme from "@/theme/authTheme";
import { alpha } from "@/utils/color";

const Auth: React.FC = () => {
  return (
    <AppThemeProvider value={authTheme}>
      <AuthContent />
    </AppThemeProvider>
  );
};

const AuthContent: React.FC = () => {
  const theme = useAppTheme();
  const isSmallUp = useAppMediaQuery(theme.breakpoints.up("sm"));

  return (
    <div
      style={{
        ["--accent" as string]: theme.palette.primary.main,
        ["--accent-strong" as string]: theme.palette.primary.dark,
        ["--accent-soft" as string]: theme.palette.primary.light,
        minHeight: "100vh",
        width: "100%",
        display: "grid",
        placeItems: "center",
        position: "relative",
        overflowX: "hidden",
        overflowY: "auto",
        paddingLeft: isSmallUp ? theme.spacing(4) : theme.spacing(2),
        paddingRight: isSmallUp ? theme.spacing(4) : theme.spacing(2),
        paddingTop: isSmallUp ? theme.spacing(8) : theme.spacing(6),
        paddingBottom: isSmallUp ? theme.spacing(8) : theme.spacing(6),
        backgroundImage: `radial-gradient(900px 420px at 12% 8%, ${alpha(theme.palette.primary.main, 0.25)}, ${alpha(theme.palette.background.default, 0)} 60%), radial-gradient(800px 360px at 90% 0%, ${alpha(theme.palette.primary.light, 0.2)}, ${alpha(theme.palette.background.default, 0)} 60%), linear-gradient(160deg, ${alpha(theme.palette.background.default, 0.92)} 0%, ${theme.palette.background.default} 45%, ${alpha(theme.palette.background.default, 0.72)} 100%)`,
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
          background: `radial-gradient(circle at 30% 30%, ${alpha(theme.palette.primary.main, 0.35)}, ${alpha(theme.palette.primary.main, 0)} 70%)`,
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
          background: `radial-gradient(circle at 70% 40%, ${alpha(theme.palette.primary.dark, 0.3)}, ${alpha(theme.palette.primary.dark, 0)} 70%)`,
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
