import React from "react";

import "./app-icon-button.css";

type IconButtonColor = "default" | "inherit" | "primary" | "error";

type IconButtonSize = "small" | "medium";

export interface AppIconButtonProps extends Omit<
  React.ButtonHTMLAttributes<HTMLButtonElement>,
  "color"
> {
  color?: IconButtonColor;
  size?: IconButtonSize;
  edge?: "start" | "end" | false;
}

const AppIconButton = React.forwardRef<HTMLButtonElement, AppIconButtonProps>(
  (
    {
      color = "default",
      size = "medium",
      edge = false,
      className,
      children,
      ...rest
    },
    ref,
  ) => {
    const cls = [
      "app-icon-btn",
      color !== "default" && `app-icon-btn--${color}`,
      size === "small" && "app-icon-btn--small",
      edge && `app-icon-btn--edge-${edge}`,
      className,
    ]
      .filter(Boolean)
      .join(" ");

    return (
      <button ref={ref} type="button" className={cls} {...rest}>
        {children}
      </button>
    );
  },
);

AppIconButton.displayName = "AppIconButton";

export default AppIconButton;
