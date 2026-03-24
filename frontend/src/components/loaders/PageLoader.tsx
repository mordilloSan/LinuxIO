import React from "react";

import "./page-loader.css";

function PageLoader() {
  return (
    <div
      style={{
        position: "fixed",
        top: 0,
        left: 0,
        width: "100vw",
        height: "100vh",
        backgroundColor: "var(--app-palette-background-default, #0f172a)",
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
          backgroundColor: "var(--app-palette-background-paper, #1e293b)",
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
            background:
              "linear-gradient(90deg, var(--app-palette-primary-main, #3b82f6), color-mix(in srgb, var(--app-palette-primary-main, #3b82f6), transparent 50%))",
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
