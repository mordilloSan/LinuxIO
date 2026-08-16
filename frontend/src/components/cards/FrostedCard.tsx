import { forwardRef, type CSSProperties, type HTMLAttributes } from "react";

import "./FrostedCard.css";

import { useAppTheme } from "@/theme";
import { cardBorderRadius } from "@/theme/constants";
import {
  getAccentCardStyles,
  getFrostedCardLiftShadow,
  getFrostedCardStyles,
} from "@/theme/surfaces";

type FrostedCardProps = HTMLAttributes<HTMLDivElement> & {
  /** When true, card lifts on hover (translateY + stronger shadow). */
  hoverLift?: boolean;
  /**
   * Draws the accent line along the card's bottom edge. It is the shared mark
   * of a card that can be held to reorder — holding lights the line, which is
   * the whole affordance (see `.accent-card` in FrostedCard.css) — so every
   * card in a sortable grid should carry it and cards outside one should not.
   *
   * `true` uses the theme's primary. Pass a colour instead to key the line to
   * the card's own state, as the interface cards do with their status colour;
   * the hold then lights that colour rather than the primary.
   */
  accent?: boolean | string;
};

const FrostedCard = forwardRef<HTMLDivElement, FrostedCardProps>(
  ({ accent, children, style, hoverLift, className, ...props }, ref) => {
    const theme = useAppTheme();

    const accentColor =
      accent === true ? theme.palette.primary.main : accent || undefined;

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
  },
);

FrostedCard.displayName = "FrostedCard";

export default FrostedCard;
