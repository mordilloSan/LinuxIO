import { type ButtonHTMLAttributes, type ReactNode, type Ref } from "react";

import "./app-button.css";
import { getButtonColorVars, type ButtonColor } from "./app-button-colors";

type ButtonVariant = "contained" | "outlined" | "text";

type ButtonSize = "small" | "medium";

export interface AppButtonProps extends Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "color"
> {
  color?: ButtonColor;
  fullWidth?: boolean;
  keepTextOnMobile?: boolean;
  ref?: Ref<HTMLButtonElement>;
  size?: ButtonSize;
  startIcon?: ReactNode;
  variant?: ButtonVariant;
}

const AppButton = ({
  ref,
  variant = "text",
  color = "primary",
  size = "medium",
  startIcon,
  fullWidth,
  keepTextOnMobile = false,
  className,
  style,
  children,
  ...rest
}: AppButtonProps) => {
  const cls = [
    "app-btn",
    variant !== "text" && `app-btn--${variant}`,
    color === "inherit" && "app-btn--inherit",
    size === "small" && "app-btn--small",
    fullWidth && "app-btn--fullwidth",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  const colorVars = getButtonColorVars(color);

  return (
    <button
      className={cls}
      ref={ref}
      style={{ ...colorVars, ...style }}
      type="button"
      {...rest}
    >
      {startIcon && <span className="app-btn__icon">{startIcon}</span>}
      {startIcon && !keepTextOnMobile ? (
        <span className="app-btn__label">{children}</span>
      ) : (
        children
      )}
    </button>
  );
};

AppButton.displayName = "AppButton";

export default AppButton;
