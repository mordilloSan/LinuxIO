import React from "react";

import "./app-circular-progress.css";

export interface AppCircularProgressProps extends React.HTMLAttributes<HTMLSpanElement> {
  size?: number;
  color?: "primary" | "inherit";
}

const AppCircularProgress = React.forwardRef<
  HTMLSpanElement,
  AppCircularProgressProps
>(({ size = 40, color = "primary", className, style, ...rest }, ref) => {
  const cls = ["app-circular-progress", className].filter(Boolean).join(" ");

  return (
    <span
      ref={ref}
      role="progressbar"
      className={cls}
      style={{
        width: size,
        height: size,
        ...(color === "inherit" && { color: "inherit" }),
        ...style,
      }}
      {...rest}
    >
      <svg viewBox="22 22 44 44" width={size} height={size}>
        <circle
          className="app-circular-progress__circle"
          cx="44"
          cy="44"
          r="20.2"
          fill="none"
          strokeWidth="3.6"
        />
      </svg>
    </span>
  );
});

AppCircularProgress.displayName = "AppCircularProgress";

export default AppCircularProgress;
