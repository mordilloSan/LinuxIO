import { Icon } from "@iconify/react";
import React from "react";

import AppTooltip, { useIsInsideAppTooltip } from "@/components/ui/AppTooltip";
import { AppTheme, useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

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
  className?: string;
  color?: AppChipColor;
  disabled?: boolean;
  label: React.ReactNode;
  onDelete?: (
    event:
      | React.MouseEvent<HTMLButtonElement>
      | React.KeyboardEvent<HTMLSpanElement>,
  ) => void;
  size?: AppChipSize;
  style?: React.CSSProperties;
  sx?: AppChipSx;
  title?: string;
  variant?: AppChipVariant;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const getPlainText = (node: React.ReactNode): string => {
  if (typeof node === "string" || typeof node === "number") {
    return String(node).trim();
  }

  if (Array.isArray(node)) {
    return node.map(getPlainText).filter(Boolean).join(" ").trim();
  }

  return "";
};

const getPaletteColor = (theme: AppTheme, color: AppChipColor) => {
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

const toSpacing = (theme: AppTheme, value: unknown) =>
  typeof value === "number" ? theme.spacing(value) : value;

const applySpacing = (
  style: React.CSSProperties,
  theme: AppTheme,
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
  theme: AppTheme,
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

const getChipSxStyles = (sx: AppChipSx | undefined, theme: AppTheme) => {
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
    const theme = useAppTheme();
    const isInsideTooltip = useIsInsideAppTooltip();
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

    const tooltipText =
      typeof title === "string" && title.trim()
        ? title.trim()
        : getPlainText(label);
    const showTruncatedTooltip = Boolean(tooltipText && !isInsideTooltip);

    const chip = (
      <span
        {...nativeProps}
        aria-disabled={disabled || undefined}
        className={chipClassName}
        onClick={disabled ? undefined : onClick}
        onKeyDown={handleKeyDown}
        ref={ref}
        role={isInteractive ? "button" : nativeProps.role}
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
            "--app-chip-font-size": "0.8125rem",
            "--app-chip-font-weight": isSoft
              ? 600
              : theme.typography.fontWeightRegular,
            "--app-chip-line-height": 1.5,
            ...cssVars,
            ...rootStyle,
            ...style,
          } as React.CSSProperties
        }
        tabIndex={isInteractive ? (tabIndex ?? 0) : tabIndex}
        title={showTruncatedTooltip ? undefined : title}
      >
        <span className="app-chip__label" style={labelStyle}>
          {label}
        </span>
        {onDelete && (
          <button
            aria-label={
              typeof label === "string" ? `Remove ${label}` : "Remove"
            }
            className="app-chip__delete"
            disabled={disabled}
            onClick={handleDeleteClick}
            tabIndex={-1}
            type="button"
          >
            <Icon height={16} icon="mdi:close-circle" width={16} />
          </button>
        )}
      </span>
    );

    if (!showTruncatedTooltip) {
      return chip;
    }

    return (
      <AppTooltip contentWidth onlyWhenTruncated title={tooltipText}>
        {chip}
      </AppTooltip>
    );
  },
);

AppChip.displayName = "AppChip";

export default AppChip;
