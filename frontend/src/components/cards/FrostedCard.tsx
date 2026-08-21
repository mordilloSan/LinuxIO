import { type CSSProperties, type HTMLAttributes, type Ref } from "react";

import "./FrostedCard.css";

import { useAppTheme } from "@/theme";
import { cardBorderRadius } from "@/theme/constants";
import {
  getAccentCardStyles,
  getFrostedCardLiftShadow,
  getFrostedCardStyles,
} from "@/theme/surfaces";

type FrostedCardProps = HTMLAttributes<HTMLDivElement> & {
  hoverLift?: boolean;
  accent?: boolean;
  ref?: Ref<HTMLDivElement>;
};

const FrostedCard = ({
  ref,
  accent,
  children,
  style,
  hoverLift,
  className,
  ...props
}: FrostedCardProps) => {
  const theme = useAppTheme();

  const accentColor = accent ? theme.palette.primary.main : undefined;

  const frostedStyles = {
    overflow: "hidden",
    borderRadius: cardBorderRadius,
    ...getFrostedCardStyles(theme),
    ...(hoverLift && {
      "--fc-lift-shadow": getFrostedCardLiftShadow(theme),
    }),
    ...(accentColor && {
      ...getAccentCardStyles(accentColor),
      // Read by the hold animation, so a card that keys its line to its own
      // state lights up in that colour rather than the theme's.
      "--fc-accent": accentColor,
    }),
    ...style,
  } as CSSProperties;

  const cls = [
    hoverLift && "hover-lift fc-hover-lift",
    accentColor && "accent-card",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div
      className={cls || undefined}
      ref={ref}
      style={frostedStyles}
      {...props}
    >
      {children}
    </div>
  );
};

FrostedCard.displayName = "FrostedCard";

export default FrostedCard;
