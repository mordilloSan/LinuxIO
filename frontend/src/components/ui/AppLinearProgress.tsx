import React from "react";

import "./app-linear-progress.css";

type ProgressColor = "primary" | "error" | "warning" | "success";

export interface AppLinearProgressProps extends React.HTMLAttributes<HTMLDivElement> {
  color?: ProgressColor;
  value?: number;
  variant?: "determinate" | "indeterminate";
}

const AppLinearProgress = React.forwardRef<
  HTMLDivElement,
  AppLinearProgressProps
>(
  (
    {
      variant = "indeterminate",
      value = 0,
      color = "primary",
      className,
      ...rest
    },
    ref,
  ) => {
    const cls = [
      "app-linear-progress",
      `app-linear-progress--${variant}`,
      color !== "primary" && `app-linear-progress--${color}`,
      className,
    ]
      .filter(Boolean)
      .join(" ");

    const barStyle: React.CSSProperties | undefined =
      variant === "determinate"
        ? { transform: `translateX(${value - 100}%)` }
        : undefined;

    return (
      <div
        aria-valuemax={100}
        aria-valuemin={0}
        aria-valuenow={
          variant === "determinate" ? Math.round(value) : undefined
        }
        className={cls}
        ref={ref}
        role="progressbar"
        {...rest}
      >
        <span className="app-linear-progress__bar" style={barStyle} />
        {variant === "indeterminate" && (
          <span className="app-linear-progress__bar app-linear-progress__bar2" />
        )}
      </div>
    );
  },
);

AppLinearProgress.displayName = "AppLinearProgress";

export default AppLinearProgress;
