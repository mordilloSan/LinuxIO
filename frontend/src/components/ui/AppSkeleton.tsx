import React from "react";

import "./app-skeleton.css";
import "./app-typography.css";

type SkeletonVariant = "text" | "rectangular" | "circular";

/** Typography variants whose font-size a text skeleton can be sized to. */
type SkeletonTextVariant =
  | "h1"
  | "h2"
  | "h3"
  | "h4"
  | "h5"
  | "h6"
  | "subtitle1"
  | "subtitle2"
  | "body1"
  | "body2"
  | "caption"
  | "overline";

export interface AppSkeletonProps {
  className?: string;
  height?: number | string;
  style?: React.CSSProperties;
  /**
   * Sizes a text skeleton bar to an AppTypography variant: the bar height
   * (and `ch`-based widths) track the variant font-size straight from
   * app-typography.css — single source of truth, no duplicated values.
   * Implies variant="text".
   */
  textVariant?: SkeletonTextVariant;
  variant?: SkeletonVariant;
  width?: number | string;
}

const AppSkeleton: React.FC<AppSkeletonProps> = ({
  variant = "text",
  textVariant,
  width,
  height,
  className,
  style,
}) => (
  <span
    aria-hidden="true"
    className={[
      "app-skeleton",
      `app-skeleton--${textVariant ? "text" : variant}`,
      textVariant && `app-typo--${textVariant}`,
      className,
    ]
      .filter(Boolean)
      .join(" ")}
    style={{
      width,
      height: height ?? (textVariant ? "1em" : undefined),
      ...style,
    }}
  />
);

AppSkeleton.displayName = "AppSkeleton";

export default AppSkeleton;
