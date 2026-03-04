import { CircularProgress } from "@mui/material";
import React from "react";

function ComponentLoader() {
  return (
    <div
      style={{
        justifyContent: "center",
        alignItems: "center",
        display: "flex",
        minHeight: "100%",
      }}
    >
      <CircularProgress color="primary" />
    </div>
  );
}

export default ComponentLoader;
