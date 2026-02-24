import { Box, useTheme } from "@mui/material";
import React from "react";

function PageLoader() {
  const theme = useTheme();
  const color = theme.palette.primary.main;

  return (
    <Box
      sx={{
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
      <Box
        sx={{
          width: 300,
          height: 6,
          backgroundColor: theme.palette.background.paper,
          borderRadius: 3,
          overflow: "hidden",
          position: "relative",
        }}
      >
        <Box
          sx={{
            height: "100%",
            width: 150,
            position: "absolute",
            left: 0,
            top: 0,
            background: `linear-gradient(90deg, ${color}, color-mix(in srgb, ${color}, transparent 50%))`,
            filter: "blur(1px)",
            borderRadius: 3,
            transform: "translateX(-150px)",
            animation:
              "page-loader-slide 1s cubic-bezier(0.42, 0, 0.58, 1) infinite",
            "@keyframes page-loader-slide": {
              "0%": {
                transform: "translateX(-150px)",
              },
              "100%": {
                transform: "translateX(300px)",
              },
            },
          }}
        />
      </Box>
    </Box>
  );
}

export default PageLoader;
