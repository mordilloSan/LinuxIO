import { useTheme } from "@mui/material/styles";
import React from "react";

import "./frosted-card.css";

import { cardBorderRadius } from "@/constants";
import {
  getFrostedCardLiftShadow,
  getFrostedCardStyles,
} from "@/theme/surfaces";

// MUI sx multiplies borderRadius by theme.shape.borderRadius (default 4).
// cardBorderRadius = 4, so the effective CSS value is 4 * 4 = 16px.
const BORDER_RADIUS_PX = cardBorderRadius * 4;

type FrostedCardProps = React.HTMLAttributes<HTMLDivElement> & {
  /** When true, card lifts on hover (translateY + stronger shadow). */
  hoverLift?: boolean;
};

const FrostedCard = React.forwardRef<HTMLDivElement, FrostedCardProps>(
  ({ children, style, hoverLift, className, ...props }, ref) => {
    const theme = useTheme();

    const frostedStyles = {
      overflow: "hidden",
      borderRadius: BORDER_RADIUS_PX,
      ...getFrostedCardStyles(theme),
      ...(hoverLift && {
        transition: "transform 0.2s, box-shadow 0.2s",
        "--fc-lift-shadow": getFrostedCardLiftShadow(theme),
      }),
      ...style,
    } as React.CSSProperties;

    const cls = [hoverLift && "fc-hover-lift", className]
      .filter(Boolean)
      .join(" ");

    return (
      <div
        ref={ref}
        className={cls || undefined}
        style={frostedStyles}
        {...props}
      >
        {children}
      </div>
    );
  },
);

FrostedCard.displayName = "FrostedCard";

export default FrostedCard;
