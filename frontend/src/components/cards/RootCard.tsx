import { Card, CardProps } from "@mui/material";
import React from "react";

import { cardBorderRadius } from "@/constants";

type FrostedCardProps = CardProps;

const FrostedCard: React.FC<FrostedCardProps> = ({
  children,
  sx,
  ...props
}) => {
  return (
    <Card
      sx={{
        borderRadius: cardBorderRadius,
        backgroundColor: (theme) =>
          theme.palette.mode === "dark"
            ? "rgba(255,255,255,0.06)"
            : "rgba(0,0,0,0.03)",
        backgroundImage: (theme) =>
          theme.palette.mode === "dark"
            ? "linear-gradient(180deg, rgba(255,255,255,0.18) 0%, rgba(255,255,255,0.02) 100%)"
            : "linear-gradient(180deg, rgba(0,0,0,0.14) 0%, rgba(0,0,0,0.01) 100%)",
        backdropFilter: (theme) =>
          theme.palette.mode === "dark" ? "blur(12px)" : "blur(6px)",
        boxShadow: "0 2px 8px rgba(0,0,0,0.2)",
        ...sx, // allow overriding styles if needed
      }}
      {...props}
    >
      {children}
    </Card>
  );
};

export default FrostedCard;
