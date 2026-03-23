import React from "react";

import "./app-button.css";

type ButtonColor =
  | "primary"
  | "secondary"
  | "error"
  | "warning"
  | "success"
  | "inherit";

type ButtonVariant = "contained" | "outlined" | "text";

type ButtonSize = "small" | "medium";

const COLOR_VARS: Record<
  Exclude<ButtonColor, "inherit">,
  { main: string; dark: string; contrast: string }
> = {
  primary: {
    main: "var(--app-palette-primary-main)",
    dark: "var(--app-palette-primary-dark)",
    contrast: "var(--app-palette-primary-contrast-text)",
  },
  secondary: {
    main: "var(--app-palette-secondary-main)",
    dark: "var(--app-palette-secondary-dark)",
    contrast: "var(--app-palette-secondary-contrast-text)",
  },
  error: {
    main: "var(--app-palette-error-main)",
    dark: "var(--app-palette-error-dark)",
    contrast: "var(--app-palette-error-contrast-text)",
  },
  warning: {
    main: "var(--app-palette-warning-main)",
    dark: "var(--app-palette-warning-dark)",
    contrast: "var(--app-palette-warning-contrast-text)",
  },
  success: {
    main: "var(--app-palette-success-main)",
    dark: "var(--app-palette-success-dark)",
    contrast: "var(--app-palette-success-contrast-text)",
  },
};

export interface AppButtonProps extends Omit<
  React.ButtonHTMLAttributes<HTMLButtonElement>,
  "color"
> {
  variant?: ButtonVariant;
  color?: ButtonColor;
  size?: ButtonSize;
  startIcon?: React.ReactNode;
  fullWidth?: boolean;
}

const AppButton = React.forwardRef<HTMLButtonElement, AppButtonProps>(
  (
    {
      variant = "text",
      color = "primary",
      size = "medium",
      startIcon,
      fullWidth,
      className,
      style,
      children,
      ...rest
    },
    ref,
  ) => {
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

    const colorVars =
      color === "inherit"
        ? undefined
        : ({
            "--_btn-main": COLOR_VARS[color].main,
            "--_btn-dark": COLOR_VARS[color].dark,
            "--_btn-contrast": COLOR_VARS[color].contrast,
          } as React.CSSProperties);

    return (
      <button
        ref={ref}
        type="button"
        className={cls}
        style={{ ...colorVars, ...style }}
        {...rest}
      >
        {startIcon && <span className="app-btn__icon">{startIcon}</span>}
        {children}
      </button>
    );
  },
);

AppButton.displayName = "AppButton";

export default AppButton;
