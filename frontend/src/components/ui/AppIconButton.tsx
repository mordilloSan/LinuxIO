import { type ButtonHTMLAttributes, type Ref } from "react";

import "./app-icon-button.css";

type IconButtonColor =
  | "default"
  | "error"
  | "inherit"
  | "primary"
  | "secondary";

type IconButtonSize = "small" | "medium";

export interface AppIconButtonProps extends Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "color"
> {
  color?: IconButtonColor;
  edge?: "start" | "end" | false;
  ref?: Ref<HTMLButtonElement>;
  size?: IconButtonSize;
}

const AppIconButton = ({
  ref,
  color = "default",
  size = "medium",
  edge = false,
  className,
  children,
  ...rest
}: AppIconButtonProps) => {
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
};

AppIconButton.displayName = "AppIconButton";

export default AppIconButton;
