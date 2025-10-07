import { CssBaseline, Box } from "@mui/material";
import React from "react";
import { Outlet } from "react-router-dom";

const Auth: React.FC = () => {
  return (
    <Box
      sx={{
        maxWidth: 520,
        mx: "auto",
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
        alignItems: "center",
        minHeight: "100vh",
      }}
    >
      <CssBaseline />
      <Outlet />
    </Box>
  );
};

export default Auth;
