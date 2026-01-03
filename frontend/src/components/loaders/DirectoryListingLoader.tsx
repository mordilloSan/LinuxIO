import { Box, useTheme } from "@mui/material";
import { motion } from "framer-motion";
import React from "react";

const MotionBox = motion.create(Box);

function DirectoryListingLoader() {
  const theme = useTheme();
  const color = theme.palette.primary.main;

  return (
    <Box
      sx={{
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        py: 4,
        width: "100%",
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
        <MotionBox
          animate={{
            x: ["-150px", "300px"],
          }}
          transition={{
            duration: 1.0,
            repeat: Infinity,
            ease: [0.42, 0, 0.58, 1],
          }}
          sx={{
            height: "100%",
            width: 150,
            position: "absolute",
            left: 0,
            top: 0,
            background: `linear-gradient(90deg, ${color}, color-mix(in srgb, ${color}, transparent 50%))`,
            filter: "blur(1px)",
            borderRadius: 3,
          }}
        />
      </Box>
    </Box>
  );
}

export default DirectoryListingLoader;
