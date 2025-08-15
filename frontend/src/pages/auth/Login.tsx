import { Box, Typography, Paper } from "@mui/material";
import React from "react";

import LoginComponent from "@/components/auth/Login";
import LogoDisplay from "@/components/logo/LogoDisplay";

const Login: React.FC = () => {
  return (
    <>
      {/* Logo */}
      <Box mb={2} display="flex" justifyContent="center">
        <LogoDisplay showText />
      </Box>

      {/* Title and subtitle */}
      <Box textAlign="center" mb={4}>
        <Typography component="h1" variant="h5">
          Log in to your account to continue
        </Typography>
      </Box>

      {/* Paper form */}
      <Paper
        sx={(theme) => ({
          p: 6,
          width: "100%",
          maxWidth: "100%",
          boxSizing: "border-box",
          [theme.breakpoints.up("md")]: {
            p: 10,
          },
        })}
      >
        <LoginComponent />
      </Paper>
    </>
  );
};

export default Login;
