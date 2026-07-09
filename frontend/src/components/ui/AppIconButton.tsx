import { forwardRef, type ButtonHTMLAttributes } from "react";

import "./app-icon-button.css";

type IconButtonColor = "default" | "inherit" | "primary" | "error";

type IconButtonSize = "small" | "medium";

export interface AppIconButtonProps extends Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "color"
> {
  color?: IconButtonColor;
  edge?: "start" | "end" | false;
  size?: IconButtonSize;
}

const AppIconButton = forwardRef<HTMLButtonElement, AppIconButtonProps>(
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
      <button className={cls} ref={ref} type="button" {...rest}>
        {children}
      </button>
    );
  },
);

AppIconButton.displayName = "AppIconButton";

export default AppIconButton;
