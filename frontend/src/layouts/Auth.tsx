import { Box, CssBaseline } from "@mui/material";
import { alpha, ThemeProvider } from "@mui/material/styles";
import React from "react";
import { Outlet } from "react-router-dom";

import authTheme from "@/theme/authTheme";

const Auth: React.FC = () => {
  return (
    <ThemeProvider theme={authTheme}>
      <CssBaseline />
      <Box
        sx={(theme) => ({
          "--accent": theme.palette.primary.main,
          "--accent-strong": theme.palette.primary.dark,
          "--accent-soft": theme.palette.primary.light,
          minHeight: "100vh",
          width: "100%",
          display: "grid",
          placeItems: "center",
          position: "relative",
          overflowX: "hidden",
          overflowY: "auto",
          px: { xs: 2, sm: 4 },
          py: { xs: 6, sm: 8 },
          backgroundImage: `radial-gradient(900px 420px at 12% 8%, ${alpha(theme.palette.primary.main, 0.25)}, ${alpha(theme.palette.background.default, 0)} 60%), radial-gradient(800px 360px at 90% 0%, ${alpha(theme.palette.primary.light, 0.2)}, ${alpha(theme.palette.background.default, 0)} 60%), linear-gradient(160deg, ${alpha(theme.palette.background.default, 0.92)} 0%, ${theme.palette.background.default} 45%, ${alpha(theme.palette.background.default, 0.72)} 100%)`,
        })}
      >
        <Box
          aria-hidden
          sx={{
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
        <Box
          aria-hidden
          sx={{
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
        <Box
          sx={{
            position: "relative",
            zIndex: 1,
            width: "100%",
            display: "flex",
            justifyContent: "center",
          }}
        >
          <Outlet />
        </Box>
      </Box>
    </ThemeProvider>
  );
};

export default Auth;
