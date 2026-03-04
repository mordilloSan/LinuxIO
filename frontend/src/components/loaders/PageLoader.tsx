import { useTheme } from "@mui/material/styles";
import React from "react";

import "./page-loader.css";

function PageLoader() {
  const theme = useTheme();
  const color = theme.palette.primary.main;

  return (
    <div
      style={{
        position: "fixed",
        top: 0,
        left: 0,
        width: "100vw",
        height: "100vh",
        backgroundColor: theme.palette.background.default,
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        zIndex: 1300,
      }}
    >
      <div
        style={{
          width: 300,
          height: 6,
          backgroundColor: theme.palette.background.paper,
          borderRadius: 12,
          overflow: "hidden",
          position: "relative",
        }}
      >
        <div
          className="page-loader-bar"
          style={{
            height: "100%",
            width: 150,
            position: "absolute",
            left: 0,
            top: 0,
            background: `linear-gradient(90deg, ${color}, color-mix(in srgb, ${color}, transparent 50%))`,
            filter: "blur(1px)",
            borderRadius: 12,
            transform: "translateX(-150px)",
          }}
        />
      </div>
    </div>
  );
}

export default PageLoader;
