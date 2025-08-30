import { Box, CircularProgress } from "@mui/material";
import React from "react";

function ComponentLoader() {
  return (
    <Box
      sx={{
        justifyContent: "center",
        alignItems: "center",
        display: "flex",
        minHeight: "100%",
      }}
    >
      <CircularProgress color="secondary" />
    </Box>
  );
}

export default ComponentLoader;
