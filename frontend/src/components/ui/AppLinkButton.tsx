import { forwardRef, type AnchorHTMLAttributes, type MouseEvent } from "react";

import { getButtonColorVars, type ButtonColor } from "./app-button-colors";
import "./app-button.css";
import "./app-link-button.css";

type ButtonVariant = "contained" | "outlined" | "text";
type ButtonSize = "small" | "medium";

export interface AppLinkButtonProps extends Omit<
  AnchorHTMLAttributes<HTMLAnchorElement>,
  "color"
> {
  disabled?: boolean;
  color?: ButtonColor;
  fullWidth?: boolean;
  size?: ButtonSize;
  variant?: ButtonVariant;
}

const AppLinkButton = forwardRef<HTMLAnchorElement, AppLinkButtonProps>(
  (
    {
      variant = "text",
      color = "primary",
      size = "medium",
      fullWidth,
      className,
      style,
      disabled,
      onClick,
      href,
      role,
      children,
      ...rest
    },
    ref,
  ) => {
    const cls = [
      "app-btn",
      "app-link-btn",
      variant !== "text" && `app-btn--${variant}`,
      color === "inherit" && "app-btn--inherit",
      size === "small" && "app-btn--small",
      fullWidth && "app-btn--fullwidth",
      disabled && "app-link-btn--disabled",
      className,
    ]
      .filter(Boolean)
      .join(" ");
    const colorVars = getButtonColorVars(color);
    const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
      if (disabled) {
        event.preventDefault();
        return;
      }
      onClick?.(event);
    };
    return (
      <a
        {...rest}
        aria-disabled={disabled || undefined}
        className={cls}
        href={disabled ? undefined : href}
        onClick={handleClick}
        ref={ref}
        role={role ?? (disabled ? "link" : undefined)}
        style={{ ...colorVars, ...style }}
        tabIndex={disabled ? -1 : rest.tabIndex}
      >
        {children}
      </a>
    );
  },
);

AppLinkButton.displayName = "AppLinkButton";
export default AppLinkButton;
