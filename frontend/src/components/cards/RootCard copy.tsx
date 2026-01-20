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
            ? "rgba(17,25,40,0.48)"
            : "rgba(255,255,255,0.82)",
        backgroundImage: (theme) =>
          theme.palette.mode === "dark"
            ? "radial-gradient(120% 140% at 0% 0%, rgba(64,122,214,0.2) 0%, rgba(64,122,214,0) 48%), linear-gradient(180deg, rgba(255,255,255,0.16) 0%, rgba(255,255,255,0.12) 18%, rgba(255,255,255,0.1) 38%, rgba(255,255,255,0.08) 62%, rgba(255,255,255,0.07) 100%)"
            : "radial-gradient(120% 140% at 0% 0%, rgba(64,122,214,0.18) 0%, rgba(64,122,214,0) 48%), linear-gradient(180deg, rgba(226,232,240,0.72) 0%, rgba(255,255,255,0.82) 18%, rgba(255,255,255,0.9) 40%, rgba(255,255,255,0.95) 70%, rgba(255,255,255,0.98) 100%)",
        border: (theme) =>
          theme.palette.mode === "dark"
            ? "1px solid rgba(148,163,184,0.12)"
            : "1px solid rgba(15,23,42,0.06)",
        backdropFilter: (theme) =>
          theme.palette.mode === "dark" ? "blur(20px)" : "blur(16px)",
        boxShadow: (theme) =>
          theme.palette.mode === "dark"
            ? "0 18px 40px -32px rgba(2,6,23,0.6), 0 0 0 1px rgba(148,163,184,0.06)"
            : "0 18px 34px -28px rgba(15,23,42,0.16), 0 0 0 1px rgba(15,23,42,0.04)",
        ...sx, // allow overriding styles if needed
      }}
      {...props}
    >
      {children}
    </Card>
  );
};

export default FrostedCard;
