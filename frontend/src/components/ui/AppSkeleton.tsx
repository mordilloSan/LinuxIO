import React from "react";

import "./app-skeleton.css";

type SkeletonVariant = "text" | "rectangular" | "circular";

export interface AppSkeletonProps {
  variant?: SkeletonVariant;
  width?: number | string;
  height?: number | string;
  className?: string;
  style?: React.CSSProperties;
}

const AppSkeleton: React.FC<AppSkeletonProps> = ({
  variant = "text",
  width,
  height,
  className,
  style,
}) => (
  <span
    className={[
      "app-skeleton",
      `app-skeleton--${variant}`,
      className,
    ]
      .filter(Boolean)
      .join(" ")}
    style={{
      width,
      height,
      ...style,
    }}
    aria-hidden="true"
  />
);

AppSkeleton.displayName = "AppSkeleton";

export default AppSkeleton;
