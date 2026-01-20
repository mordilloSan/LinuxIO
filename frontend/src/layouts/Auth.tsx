import { Box, CssBaseline } from "@mui/material";
import { ThemeProvider, createTheme } from "@mui/material/styles";
import React, { useMemo } from "react";
import { Outlet } from "react-router-dom";

const Auth: React.FC = () => {
  const theme = useMemo(
    () =>
      createTheme({
        palette: {
          mode: "dark",
          primary: {
            main: "#407ad6",
            light: "#6395e0",
            dark: "#2f65cb",
            contrastText: "#FFF",
          },
          secondary: { main: "#6395e0", contrastText: "#FFF" },
          background: {
            default: "#1B2635",
            paper: "#233044",
          },
          text: {
            primary: "rgba(255, 255, 255, 0.95)",
            secondary: "rgba(255, 255, 255, 0.6)",
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
          backgroundImage:
            "radial-gradient(900px 420px at 12% 8%, rgba(64,122,214,0.25), rgba(27,38,53,0) 60%), radial-gradient(800px 360px at 90% 0%, rgba(99,149,224,0.2), rgba(27,38,53,0) 60%), linear-gradient(160deg, #0f172a 0%, #1B2635 45%, #0b1324 100%)",
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
            background:
              "radial-gradient(circle at 30% 30%, rgba(64,122,214,0.35), rgba(64,122,214,0) 70%)",
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
              "radial-gradient(circle at 70% 40%, rgba(47,101,203,0.3), rgba(47,101,203,0) 70%)",
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
