import { Card, CardProps } from "@mui/material";
import { type Theme } from "@mui/material/styles";
import React from "react";

import { cardBorderRadius } from "@/constants";
import { getFrostedCardStyles } from "@/theme/surfaces";

type FrostedCardProps = CardProps;

const FrostedCard: React.FC<FrostedCardProps> = ({
  children,
  sx,
  ...props
}) => {
  const composedSx = [
    (theme: Theme) => ({
      borderRadius: cardBorderRadius,
      ...getFrostedCardStyles(theme),
    }),
    ...(Array.isArray(sx) ? sx : sx ? [sx] : []),
  ];

  return (
    <Card sx={composedSx} {...props}>
      {children}
    </Card>
  );
};

export default FrostedCard;
