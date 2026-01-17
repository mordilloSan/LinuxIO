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
            ? "rgba(17,25,40,0.6)"
            : "rgba(255,255,255,0.82)",
        backgroundImage: (theme) =>
          theme.palette.mode === "dark"
            ? "linear-gradient(180deg, rgba(255,255,255,0.18) 0%, rgba(255,255,255,0.14) 18%, rgba(255,255,255,0.11) 38%, rgba(255,255,255,0.09) 62%, rgba(255,255,255,0.08) 100%)"
            : "linear-gradient(180deg, rgba(226,232,240,0.72) 0%, rgba(255,255,255,0.82) 18%, rgba(255,255,255,0.9) 40%, rgba(255,255,255,0.95) 70%, rgba(255,255,255,0.98) 100%)",
        border: (theme) =>
          theme.palette.mode === "dark"
            ? "1px solid transparent"
            : "1px solid transparent",
        backdropFilter: (theme) =>
          theme.palette.mode === "dark" ? "blur(20px)" : "blur(16px)",
        boxShadow: (theme) =>
          theme.palette.mode === "dark"
            ? "0 16px 40px -28px rgba(0,0,0,0.6)"
            : "0 18px 36px -28px rgba(15,23,42,0.18)",
        ...sx, // allow overriding styles if needed
      }}
      {...props}
    >
      {children}
    </Card>
  );
};

export default FrostedCard;
