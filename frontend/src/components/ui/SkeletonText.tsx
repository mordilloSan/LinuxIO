import React from "react";

import AppSkeleton from "./AppSkeleton";

// Font-size values matching app-typography.css — used as the skeleton bar height.
const VARIANT_HEIGHT: Record<string, string> = {
  h1: "2rem",
  h2: "1.75rem",
  h3: "1.5rem",
  h4: "1.125rem",
  h5: "1.0625rem",
  h6: "1rem",
  subtitle1: "0.9286rem",
  subtitle2: "0.8125rem",
  body1: "13px",
  body2: "0.8125rem",
  caption: "0.6964rem",
  overline: "0.6964rem",
};

export interface SkeletonTextProps {
  /**
   * Matches the height of the given AppTypography variant.
   * Defaults to "body1".
   */
  variant?: keyof typeof VARIANT_HEIGHT;
  /** Width of the skeleton bar. Use ch units (e.g. "12ch") to approximate character count. */
  width?: string | number;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * Inline shimmer placeholder sized to a typography variant.
 * Drop it wherever text would appear while data is loading.
 *
 * @example
 * <SkeletonText variant="body2" width="14ch" />
 * <SkeletonText variant="h4" width="8ch" />
 */
const SkeletonText: React.FC<SkeletonTextProps> = ({
  variant = "body1",
  width,
  className,
  style,
}) => (
  <AppSkeleton
    variant="text"
    width={width}
    height={VARIANT_HEIGHT[variant]}
    className={className}
    style={style}
  />
);

SkeletonText.displayName = "SkeletonText";

export default SkeletonText;
