import type { CSSProperties } from "react";

export type ButtonColor =
  | "primary"
  | "secondary"
  | "error"
  | "warning"
  | "success"
  | "inherit";

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

export function getButtonColorVars(
  color: ButtonColor,
): CSSProperties | undefined {
  if (color === "inherit") return undefined;
  const vars = COLOR_VARS[color];
  return {
    "--_btn-main": vars.main,
    "--_btn-dark": vars.dark,
    "--_btn-contrast": vars.contrast,
  } as CSSProperties;
}
