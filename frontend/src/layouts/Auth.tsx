import { Box, CssBaseline } from "@mui/material";
import { ThemeProvider, createTheme } from "@mui/material/styles";
import React, { useMemo } from "react";
import { Outlet } from "react-router-dom";

const Auth: React.FC = () => {
  const theme = useMemo(
    () =>
      createTheme({
        palette: {
          mode: "light",
          primary: { main: "#0ea5a4" },
          secondary: { main: "#f97316" },
          background: {
            default: "transparent",
            paper: "rgba(255,255,255,0.92)",
          },
          text: {
            primary: "#0f172a",
            secondary: "#64748b",
          },
        },
        shape: { borderRadius: 16 },
        typography: {
          fontFamily: '"Space Grotesk", "Sora", sans-serif',
          button: { textTransform: "none", fontWeight: 600 },
          h4: { fontWeight: 600, letterSpacing: "-0.02em" },
        },
      }),
    [],
  );

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <Box
        sx={{
          "--accent": "#0ea5a4",
          "--accent-strong": "#0f766e",
          "--accent-warm": "#f97316",
          minHeight: "100vh",
          width: "100%",
          display: "grid",
          placeItems: "center",
          position: "relative",
          overflowX: "hidden",
          overflowY: "auto",
          px: { xs: 2, sm: 4 },
          py: { xs: 6, sm: 8 },
          backgroundImage:
            "radial-gradient(900px 420px at 12% 8%, rgba(14,165,164,0.18), rgba(14,165,164,0) 60%), radial-gradient(800px 360px at 90% 0%, rgba(249,115,22,0.2), rgba(249,115,22,0) 60%), linear-gradient(145deg, #fff7ed 0%, #e0f2fe 40%, #f0fdf4 100%)",
        }}
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
            background:
              "radial-gradient(circle at 30% 30%, rgba(14,165,164,0.35), rgba(14,165,164,0) 70%)",
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
            background:
              "radial-gradient(circle at 70% 40%, rgba(249,115,22,0.3), rgba(249,115,22,0) 70%)",
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
