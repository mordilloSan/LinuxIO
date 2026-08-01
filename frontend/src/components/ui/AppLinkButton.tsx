import {
  forwardRef,
  type AnchorHTMLAttributes,
  type CSSProperties,
  type MouseEvent,
} from "react";

import "./app-button.css";
import "./app-link-button.css";

type ButtonVariant = "contained" | "outlined" | "text";
type ButtonSize = "small" | "medium";

export interface AppLinkButtonProps extends Omit<
  AnchorHTMLAttributes<HTMLAnchorElement>,
  "color"
> {
  disabled?: boolean;
  fullWidth?: boolean;
  size?: ButtonSize;
  variant?: ButtonVariant;
}

const AppLinkButton = forwardRef<HTMLAnchorElement, AppLinkButtonProps>(
  (
    {
      variant = "text",
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
      size === "small" && "app-btn--small",
      fullWidth && "app-btn--fullwidth",
      disabled && "app-link-btn--disabled",
      className,
    ]
      .filter(Boolean)
      .join(" ");
    const colorVars = {
      "--_btn-main": "var(--app-palette-primary-main)",
      "--_btn-dark": "var(--app-palette-primary-dark)",
      "--_btn-contrast": "var(--app-palette-primary-contrast-text)",
    } as CSSProperties;
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
