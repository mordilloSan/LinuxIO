import { Icon } from "@iconify/react";
import {
  type CSSProperties,
  type HTMLAttributes,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
  type Ref,
} from "react";

import AppTooltip, { useIsInsideAppTooltip } from "@/components/ui/AppTooltip";

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

type AppChipSize = "xsmall" | "small" | "medium";
type AppChipVariant = "filled" | "outlined" | "soft";

type NativeChipProps = Omit<
  HTMLAttributes<HTMLSpanElement>,
  "color" | "style" | "className"
>;

export interface AppChipProps extends NativeChipProps {
  className?: string;
  color?: AppChipColor;
  disabled?: boolean;
  label: ReactNode;
  labelStyle?: CSSProperties;
  onDelete?: (
    event: MouseEvent<HTMLButtonElement> | KeyboardEvent<HTMLSpanElement>,
  ) => void;
  ref?: Ref<HTMLSpanElement>;
  size?: AppChipSize;
  style?: CSSProperties;
  title?: string;
  variant?: AppChipVariant;
}

const getPlainText = (node: ReactNode): string => {
  if (typeof node === "string" || typeof node === "number") {
    return String(node).trim();
  }

  if (Array.isArray(node)) {
    return node.map(getPlainText).filter(Boolean).join(" ").trim();
  }

  return "";
};

// Palette names become an app-chip--<name> class that app-chip.css maps to
// the matching --app-palette-* variable; any other string is a literal colour
// and is passed through as --app-chip-color.
const PALETTE_COLORS = new Set<string>([
  "default",
  "primary",
  "secondary",
  "success",
  "error",
  "warning",
  "info",
]);

const AppChip = ({
  ref,
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
}: AppChipProps) => {
  const isInsideTooltip = useIsInsideAppTooltip();
  const isPaletteColor = PALETTE_COLORS.has(color);
  const isInteractive = Boolean(onClick || onDelete);
  const chipClassName = [
    "app-chip",
    `app-chip--${size}`,
    `app-chip--${variant}`,
    isPaletteColor && `app-chip--${color}`,
    disabled && "app-chip--disabled",
    isInteractive && "app-chip--interactive",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  const handleDeleteClick = (event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    onDelete?.(event);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLSpanElement>) => {
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
        isPaletteColor
          ? style
          : ({ "--app-chip-color": color, ...style } as CSSProperties)
      }
      tabIndex={isInteractive ? (tabIndex ?? 0) : tabIndex}
      title={showTruncatedTooltip ? undefined : title}
    >
      <span className="app-chip__label" style={labelStyle}>
        {label}
      </span>
      {onDelete && (
        <button
          aria-label={typeof label === "string" ? `Remove ${label}` : "Remove"}
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
};

AppChip.displayName = "AppChip";

export default AppChip;
