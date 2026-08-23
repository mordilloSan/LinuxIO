import type { HTMLAttributes, Ref } from "react";

import "./app-circular-progress.css";

export interface AppCircularProgressProps extends HTMLAttributes<HTMLSpanElement> {
  color?: "primary" | "inherit";
  ref?: Ref<HTMLSpanElement>;
  size?: number;
}

const AppCircularProgress = ({
  ref,
  size = 40,
  color = "primary",
  className,
  style,
  ...rest
}: AppCircularProgressProps) => {
  const cls = ["app-circular-progress", className].filter(Boolean).join(" ");

  return (
    <span
      className={cls}
      ref={ref}
      role="progressbar"
      style={{
        width: size,
        height: size,
        ...(color === "inherit" && { color: "inherit" }),
        ...style,
      }}
      {...rest}
    >
      <svg height={size} viewBox="22 22 44 44" width={size}>
        <circle
          className="app-circular-progress__circle"
          cx="44"
          cy="44"
          fill="none"
          r="20.2"
          strokeWidth="3.6"
        />
      </svg>
    </span>
  );
};

AppCircularProgress.displayName = "AppCircularProgress";

export default AppCircularProgress;
