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

type NativeChipProps = Omit<
  React.HTMLAttributes<HTMLSpanElement>,
  "color" | "style" | "className"
>;

export interface AppChipProps extends NativeChipProps {
  className?: string;
  color?: AppChipColor;
  disabled?: boolean;
  label: React.ReactNode;
  labelStyle?: React.CSSProperties;
  onDelete?: (
    event:
      | React.MouseEvent<HTMLButtonElement>
      | React.KeyboardEvent<HTMLSpanElement>,
  ) => void;
  size?: AppChipSize;
  style?: React.CSSProperties;
  title?: string;
  variant?: AppChipVariant;
}

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

const AppChip = React.forwardRef<HTMLSpanElement, AppChipProps>(
  (
    {
      label,
      labelStyle,
      color = "default",
      size = "medium",
      variant = "filled",
      className,
      style,
      title,
      disabled = false,
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
            "--app-chip-color": chipColor,
            "--app-chip-background": isOutlined
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
                  ),
            "--app-chip-border": isOutlined
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
                  ),
            "--app-chip-font-family": theme.typography.fontFamily,
            "--app-chip-font-size": "0.8125rem",
            "--app-chip-font-weight": isSoft
              ? 600
              : theme.typography.fontWeightRegular,
            "--app-chip-line-height": 1.5,
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
