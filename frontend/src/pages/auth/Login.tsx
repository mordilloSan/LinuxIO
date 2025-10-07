// src/pages/auth/Login.tsx
import { Box, Typography, Paper, Container, CssBaseline } from "@mui/material";
import { ThemeProvider, createTheme } from "@mui/material/styles";
import React, { useMemo } from "react";

import LoginComponent from "@/components/auth/Login";
import LogoDisplayDark from "@/components/logo/LogoDisplayDark";

const Login: React.FC = () => {
  const dark = useMemo(
    () =>
      createTheme({
        palette: {
          mode: "dark",
          primary: { main: "#1976d2" },
          background: {
            default: "#1B2635",
            paper: "#1E2A38",
          },
        },
        typography: {
          fontFamily: `"Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol"`,
        },
      }),
    [],
  );

  return (
    <ThemeProvider theme={dark}>
      <CssBaseline />
      <Container maxWidth="sm" sx={{ py: 8 }}>
        {/* Paper form */}
        <Paper
          sx={(theme) => ({
            p: 3,
            width: "100%",
            boxSizing: "border-box",
            backgroundColor: theme.palette.background.paper,
            [theme.breakpoints.up("md")]: { p: 6 },
          })}
        >
          {/* Logo */}
          <Box mb={1} display="flex" justifyContent="center">
            <LogoDisplayDark showText />
          </Box>
          {/* Title and subtitle */}
          <Box mb={1} textAlign="center">
            <Typography
              component="h1"
              variant="caption"
              gutterBottom
              color="#9aa4af"
            >
              Log in with your server user account
            </Typography>
          </Box>
          <LoginComponent />
        </Paper>
      </Container>
    </ThemeProvider>
  );
};

export default Login;
