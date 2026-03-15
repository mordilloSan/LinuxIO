import { Icon } from "@iconify/react";
import { alpha, Theme, useTheme } from "@mui/material/styles";
import React from "react";

import "./app-chip.css";

type AppChipColor =
  | "default"
  | "primary"
  | "secondary"
  | "success"
  | "error"
  | "warning"
  | "info"
  | (string & {});

type AppChipSize = "small" | "medium";
type AppChipVariant = "filled" | "outlined" | "soft";

type AppChipSx = Record<string, unknown>;

type NativeChipProps = Omit<
  React.HTMLAttributes<HTMLSpanElement>,
  "color" | "style" | "className"
>;

export interface AppChipProps extends NativeChipProps {
  label: React.ReactNode;
  color?: AppChipColor;
  size?: AppChipSize;
  variant?: AppChipVariant;
  className?: string;
  style?: React.CSSProperties;
  title?: string;
  disabled?: boolean;
  sx?: AppChipSx;
  onDelete?: (
    event:
      | React.MouseEvent<HTMLButtonElement>
      | React.KeyboardEvent<HTMLSpanElement>,
  ) => void;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const getPaletteColor = (theme: Theme, color: AppChipColor) => {
  switch (color) {
    case "primary":
      return theme.palette.primary.main;
    case "secondary":
      return theme.palette.secondary.main;
    case "success":
      return theme.palette.success.main;
    case "error":
      return theme.palette.error.main;
    case "warning":
      return theme.palette.warning.main;
    case "info":
      return theme.palette.info.main;
    case "default":
      return theme.palette.text.secondary;
    default:
      return color;
  }
};

const toSpacing = (theme: Theme, value: unknown) =>
  typeof value === "number" ? theme.spacing(value) : value;

const applySpacing = (
  style: React.CSSProperties,
  theme: Theme,
  key: string,
  value: unknown,
) => {
  const resolved = toSpacing(theme, value);

  switch (key) {
    case "m":
      style.margin = resolved as React.CSSProperties["margin"];
      break;
    case "mt":
      style.marginTop = resolved as React.CSSProperties["marginTop"];
      break;
    case "mr":
      style.marginRight = resolved as React.CSSProperties["marginRight"];
      break;
    case "mb":
      style.marginBottom = resolved as React.CSSProperties["marginBottom"];
      break;
    case "ml":
      style.marginLeft = resolved as React.CSSProperties["marginLeft"];
      break;
    case "mx":
      style.marginLeft = resolved as React.CSSProperties["marginLeft"];
      style.marginRight = resolved as React.CSSProperties["marginRight"];
      break;
    case "my":
      style.marginTop = resolved as React.CSSProperties["marginTop"];
      style.marginBottom = resolved as React.CSSProperties["marginBottom"];
      break;
    case "p":
      style.padding = resolved as React.CSSProperties["padding"];
      break;
    case "pt":
      style.paddingTop = resolved as React.CSSProperties["paddingTop"];
      break;
    case "pr":
      style.paddingRight = resolved as React.CSSProperties["paddingRight"];
      break;
    case "pb":
      style.paddingBottom = resolved as React.CSSProperties["paddingBottom"];
      break;
    case "pl":
      style.paddingLeft = resolved as React.CSSProperties["paddingLeft"];
      break;
    case "px":
      style.paddingLeft = resolved as React.CSSProperties["paddingLeft"];
      style.paddingRight = resolved as React.CSSProperties["paddingRight"];
      break;
    case "py":
      style.paddingTop = resolved as React.CSSProperties["paddingTop"];
      style.paddingBottom = resolved as React.CSSProperties["paddingBottom"];
      break;
  }
};

const applyDisplay = (
  style: React.CSSProperties,
  value: unknown,
): React.CSSProperties => {
  if (typeof value === "string") {
    style.display = value;
    return style;
  }

  if (!isRecord(value)) {
    return style;
  }

  const nextStyle = { ...style };

  if (typeof value.xs === "string") {
    (nextStyle as Record<string, unknown>)["--app-chip-display-xs"] = value.xs;
  }
  if (typeof value.sm === "string") {
    (nextStyle as Record<string, unknown>)["--app-chip-display-sm"] = value.sm;
  }
  if (typeof value.md === "string") {
    (nextStyle as Record<string, unknown>)["--app-chip-display-md"] = value.md;
  }
  if (typeof value.lg === "string") {
    (nextStyle as Record<string, unknown>)["--app-chip-display-lg"] = value.lg;
  }

  return nextStyle;
};

const applyStyleObject = (
  target: React.CSSProperties,
  source: Record<string, unknown>,
  theme: Theme,
) => {
  for (const [key, value] of Object.entries(source)) {
    if (value == null) continue;

    if (
      key === "m" ||
      key === "mt" ||
      key === "mr" ||
      key === "mb" ||
      key === "ml" ||
      key === "mx" ||
      key === "my" ||
      key === "p" ||
      key === "pt" ||
      key === "pr" ||
      key === "pb" ||
      key === "pl" ||
      key === "px" ||
      key === "py"
    ) {
      applySpacing(target, theme, key, value);
      continue;
    }

    if (key === "bgcolor") {
      target.backgroundColor = value as React.CSSProperties["backgroundColor"];
      continue;
    }

    if (key === "display") {
      const resolved = applyDisplay(target, value);
      Object.assign(target, resolved);
      continue;
    }

    if (!isRecord(value)) {
      (target as Record<string, unknown>)[key] = value;
    }
  }
};

const getChipSxStyles = (sx: AppChipSx | undefined, theme: Theme) => {
  const rootStyle: React.CSSProperties = {};
  const labelStyle: React.CSSProperties = {};
  const cssVars: React.CSSProperties = {};

  if (!sx) {
    return { rootStyle, labelStyle, cssVars };
  }

  for (const [key, value] of Object.entries(sx)) {
    if (value == null) continue;

    if (key === "& .MuiChip-label" && isRecord(value)) {
      applyStyleObject(labelStyle, value, theme);
      continue;
    }

    if (key === "&:hover" && isRecord(value)) {
      if (value.opacity != null) {
        (cssVars as Record<string, unknown>)["--app-chip-hover-opacity"] =
          value.opacity;
      }
      if (value.color != null) {
        (cssVars as Record<string, unknown>)["--app-chip-hover-color"] =
          value.color;
      }
      if (value.backgroundColor != null) {
        (cssVars as Record<string, unknown>)["--app-chip-hover-background"] =
          value.backgroundColor;
      }
      if (value.bgcolor != null) {
        (cssVars as Record<string, unknown>)["--app-chip-hover-background"] =
          value.bgcolor;
      }
      if (value.borderColor != null) {
        (cssVars as Record<string, unknown>)["--app-chip-hover-border"] =
          value.borderColor;
      }
      continue;
    }

    if (
      key === "m" ||
      key === "mt" ||
      key === "mr" ||
      key === "mb" ||
      key === "ml" ||
      key === "mx" ||
      key === "my" ||
      key === "p" ||
      key === "pt" ||
      key === "pr" ||
      key === "pb" ||
      key === "pl" ||
      key === "px" ||
      key === "py"
    ) {
      applySpacing(rootStyle, theme, key, value);
      continue;
    }

    if (key === "bgcolor") {
      rootStyle.backgroundColor =
        value as React.CSSProperties["backgroundColor"];
      continue;
    }

    if (key === "display") {
      const resolved = applyDisplay(rootStyle, value);
      Object.assign(rootStyle, resolved);
      continue;
    }

    if (!isRecord(value)) {
      (rootStyle as Record<string, unknown>)[key] = value;
    }
  }

  return { rootStyle, labelStyle, cssVars };
};

const AppChip = React.forwardRef<HTMLSpanElement, AppChipProps>(
  (
    {
      label,
      color = "default",
      size = "medium",
      variant = "filled",
      className,
      style,
      title,
      disabled = false,
      sx,
      onDelete,
      onClick,
      onKeyDown,
      tabIndex,
      ...nativeProps
    },
    ref,
  ) => {
    const theme = useTheme();
    const chipColor = getPaletteColor(theme, color);
    const isDefaultColor = color === "default";
    const isOutlined = variant === "outlined";
    const isSoft = variant === "soft";
    const isInteractive = Boolean(onClick || onDelete);
    const chipClassName = [
      "app-chip",
      `app-chip--${size}`,
      `app-chip--${variant}`,
      disabled && "app-chip--disabled",
      isInteractive && "app-chip--interactive",
      className,
    ]
      .filter(Boolean)
      .join(" ");
    const { rootStyle, labelStyle, cssVars } = getChipSxStyles(sx, theme);

    const handleDeleteClick = (event: React.MouseEvent<HTMLButtonElement>) => {
      event.stopPropagation();
      onDelete?.(event);
    };

    const handleKeyDown = (event: React.KeyboardEvent<HTMLSpanElement>) => {
      onKeyDown?.(event);
      if (event.defaultPrevented || disabled) return;

      if (onDelete && (event.key === "Backspace" || event.key === "Delete")) {
        event.preventDefault();
        onDelete(event);
        return;
      }

      if (onClick && (event.key === "Enter" || event.key === " ")) {
        event.preventDefault();
        event.currentTarget.click();
      }
    };

    return (
      <span
        {...nativeProps}
        ref={ref}
        className={chipClassName}
        title={title}
        role={isInteractive ? "button" : nativeProps.role}
        aria-disabled={disabled || undefined}
        tabIndex={isInteractive ? (tabIndex ?? 0) : tabIndex}
        onClick={disabled ? undefined : onClick}
        onKeyDown={handleKeyDown}
        style={
          {
            "--app-chip-color": isOutlined
              ? chipColor
              : (rootStyle.color ?? chipColor),
            "--app-chip-background":
              rootStyle.backgroundColor ??
              (isOutlined
                ? "transparent"
                : isSoft
                  ? alpha(chipColor, isDefaultColor ? 0.06 : 0.13)
                  : alpha(
                      chipColor,
                      isDefaultColor
                        ? theme.palette.mode === "dark"
                          ? 0.06
                          : 0.03
                        : theme.palette.mode === "dark"
                          ? 0.2
                          : 0.14,
                    )),
            "--app-chip-border":
              rootStyle.borderColor ??
              (isOutlined
                ? alpha(
                    chipColor,
                    color === "default"
                      ? theme.palette.mode === "dark"
                        ? 0.5
                        : 0.32
                      : 0.7,
                  )
                : isSoft
                  ? alpha(chipColor, isDefaultColor ? 0.18 : 0.33)
                  : alpha(
                      chipColor,
                      isDefaultColor
                        ? 1
                        : theme.palette.mode === "dark"
                          ? 0.42
                          : 0.28,
                    )),
            "--app-chip-font-family": theme.typography.fontFamily,
            "--app-chip-font-size": theme.typography.pxToRem(13),
            "--app-chip-font-weight": isSoft
              ? 600
              : theme.typography.fontWeightRegular,
            "--app-chip-line-height": 1.5,
            ...cssVars,
            ...rootStyle,
            ...style,
          } as React.CSSProperties
        }
      >
        <span className="app-chip__label" style={labelStyle}>
          {label}
        </span>
        {onDelete && (
          <button
            type="button"
            className="app-chip__delete"
            onClick={handleDeleteClick}
            tabIndex={-1}
            aria-label={
              typeof label === "string" ? `Remove ${label}` : "Remove"
            }
            disabled={disabled}
          >
            <Icon icon="mdi:close-circle" width={16} height={16} />
          </button>
        )}
      </span>
    );
  },
);

AppChip.displayName = "AppChip";

export default AppChip;
