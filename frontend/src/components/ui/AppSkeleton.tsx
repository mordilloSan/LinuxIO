import React from "react";

import "./app-skeleton.css";

type SkeletonVariant = "text" | "rectangular" | "circular";

export interface AppSkeletonProps {
  className?: string;
  height?: number | string;
  style?: React.CSSProperties;
  variant?: SkeletonVariant;
  width?: number | string;
}

const AppSkeleton: React.FC<AppSkeletonProps> = ({
  variant = "text",
  width,
  height,
  className,
  style,
}) => (
  <span
    aria-hidden="true"
    className={["app-skeleton", `app-skeleton--${variant}`, className]
      .filter(Boolean)
      .join(" ")}
    style={{
      width,
      height,
      ...style,
    }}
  />
);

AppSkeleton.displayName = "AppSkeleton";

export default AppSkeleton;
