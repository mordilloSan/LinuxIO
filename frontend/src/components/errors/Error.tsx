import ErrorOutlineIcon from "@mui/icons-material/ErrorOutline";
import { Box, Typography } from "@mui/material";
import React from "react";

const ErrorMessage: React.FC = () => {
  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        minHeight: "100%",
        textAlign: "center",
        gap: 2,
      }}
    >
      <ErrorOutlineIcon color="error" fontSize="large" />
      <Typography color="error" variant="body1">
        Failed to load!
      </Typography>
    </Box>
  );
};

export default ErrorMessage;
