import ErrorOutlineIcon from "@mui/icons-material/ErrorOutline";
import { Typography } from "@mui/material";
import React from "react";

const ErrorMessage: React.FC = () => {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        minHeight: "100%",
        textAlign: "center",
        gap: 8,
      }}
    >
      <ErrorOutlineIcon color="error" fontSize="large" />
      <Typography color="error" variant="body1">
        Failed to load!
      </Typography>
    </div>
  );
};

export default ErrorMessage;
