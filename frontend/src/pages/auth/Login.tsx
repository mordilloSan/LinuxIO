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
          top: { xs: -44, sm: -56 },
          left: "50%",
          width: { xs: 72, sm: 84 },
          height: { xs: 72, sm: 84 },
          borderRadius: 4,
          border: "1px solid rgba(15,23,42,0.08)",
          background:
            "linear-gradient(160deg, rgba(255,255,255,0.98) 0%, rgba(255,255,255,0.75) 100%)",
          boxShadow: "0 18px 40px -26px rgba(15,23,42,0.55)",
          display: "grid",
          placeItems: "center",
          animation: `${float} 6s ease-in-out infinite`,
          "@media (prefers-reduced-motion: reduce)": {
            animation: "none",
          },
        }}
      >
        <Box
          sx={(theme) => ({
            width: 40,
            height: 40,
            borderRadius: "50%",
            border: `3px solid ${theme.palette.primary.main}`,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontWeight: 800,
            color: theme.palette.primary.main,
            fontSize: "0.95rem",
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
          backgroundColor: "rgba(255,255,255,0.86)",
          border: "1px solid rgba(15,23,42,0.08)",
          boxShadow: "0 26px 60px -40px rgba(15,23,42,0.55)",
          backdropFilter: "blur(10px)",
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
