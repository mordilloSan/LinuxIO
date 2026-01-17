// src/pages/auth/Login.tsx
import { Box, Paper, Stack, Typography } from "@mui/material";
import { keyframes } from "@mui/system";
import React from "react";

import LoginComponent from "@/components/auth/Login";

const float = keyframes`
  0%, 100% { transform: translate(-50%, 0); }
  50% { transform: translate(-50%, -8px); }
`;

const liftIn = keyframes`
  from { opacity: 0; transform: translateY(18px) scale(0.98); }
  to { opacity: 1; transform: translateY(0) scale(1); }
`;

const Login: React.FC = () => {
  return (
    <Box
      sx={{
        width: "100%",
        maxWidth: 520,
        position: "relative",
      }}
    >
      <Box
        sx={{
          position: "absolute",
          top: { xs: -80, sm: -96 },
          left: "50%",
          px: { xs: 2, sm: 2.5 },
          py: { xs: 1.05, sm: 1.25 },
          borderRadius: 999,
          border: "1px solid rgba(148,163,184,0.25)",
          background:
            "linear-gradient(160deg, rgba(35,48,68,0.95) 0%, rgba(15,23,42,0.92) 100%)",
          boxShadow: "0 24px 54px -36px rgba(0,0,0,0.85)",
          display: "inline-flex",
          alignItems: "center",
          gap: 1,
          backdropFilter: "blur(10px)",
          animation: `${float} 6s ease-in-out infinite`,
          "@media (prefers-reduced-motion: reduce)": {
            animation: "none",
          },
        }}
      >
        <Typography
          sx={{
            fontWeight: 600,
            fontSize: { xs: "0.78rem", sm: "0.82rem" },
            letterSpacing: "0.06em",
            color: "text.primary",
          }}
        >
          Linux
        </Typography>
        <Box
          sx={(theme) => ({
            width: { xs: 30, sm: 34 },
            height: { xs: 30, sm: 34 },
            borderRadius: "50%",
            border: `2px solid ${theme.palette.primary.main}`,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontWeight: 700,
            color: theme.palette.primary.main,
            fontSize: "0.8rem",
            letterSpacing: "-0.02em",
          })}
        >
          i/O
        </Box>
      </Box>

      <Paper
        sx={{
          p: { xs: 3, sm: 4.5 },
          pt: { xs: 6, sm: 7 },
          borderRadius: 4,
          backgroundColor: "rgba(17,25,40,0.9)",
          border: "1px solid rgba(148,163,184,0.2)",
          boxShadow: "0 26px 60px -40px rgba(0,0,0,0.75)",
          backdropFilter: "blur(14px)",
          opacity: 0,
          transform: "translateY(18px) scale(0.98)",
          animation: `${liftIn} 0.7s ease forwards`,
          "@media (prefers-reduced-motion: reduce)": {
            animation: "none",
            opacity: 1,
            transform: "none",
          },
        }}
      >
        <Stack spacing={1} sx={{ textAlign: "center", mb: 2 }}>
          <Typography variant="h4">Welcome back</Typography>
          <Typography variant="body2" color="text.secondary">
            Sign in to manage your Linux i/O instance.
          </Typography>
        </Stack>
        <LoginComponent />
      </Paper>
    </Box>
  );
};

export default Login;
