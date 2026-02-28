import { Card, CardProps } from "@mui/material";
import { alpha } from "@mui/material/styles";
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
          alpha(theme.card.background, theme.palette.mode === "dark" ? 0.6 : 0.82),
        backgroundImage: (theme) =>
          theme.palette.mode === "dark"
            ? `linear-gradient(180deg, ${alpha(theme.palette.common.white, 0.18)} 0%, ${alpha(theme.palette.common.white, 0.14)} 18%, ${alpha(theme.palette.common.white, 0.11)} 38%, ${alpha(theme.palette.common.white, 0.09)} 62%, ${alpha(theme.palette.common.white, 0.08)} 100%)`
            : `linear-gradient(180deg, ${alpha(theme.palette.background.paper, 0.72)} 0%, ${alpha(theme.palette.common.white, 0.82)} 18%, ${alpha(theme.palette.common.white, 0.9)} 40%, ${alpha(theme.palette.common.white, 0.95)} 70%, ${alpha(theme.palette.common.white, 0.98)} 100%)`,
        border: "1px solid transparent",
        backdropFilter: (theme) =>
          theme.palette.mode === "dark" ? "blur(20px)" : "blur(16px)",
        boxShadow: (theme) =>
          `0 16px 40px -28px ${alpha(theme.palette.common.black, 0.6)}`,
        ...sx, // allow overriding styles if needed
      }}
      {...props}
    >
      {children}
    </Card>
  );
};

export default FrostedCard;
