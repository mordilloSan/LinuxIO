import { Icon } from "@iconify/react";
import type { CSSProperties, MouseEvent, ReactNode } from "react";

import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useAppTheme } from "@/theme";

import "./app-action-icon-button.css";

export interface AppActionIconButtonProps {
  ariaLabel?: string;
  buttonHeight?: number;
  buttonWidth?: number;
  className?: string;
  color?: string;
  disabled?: boolean;
  icon: string;
  iconSize?: number;
  label?: ReactNode;
  loading?: boolean;
  onClick: (event: MouseEvent<HTMLButtonElement>) => void;
  size?: "small" | "medium";
  tooltip?: boolean;
}

const labelToAria = (label: ReactNode): string | undefined =>
  typeof label === "string" ? label : undefined;

export default function AppActionIconButton({
  ariaLabel,
  buttonHeight,
  buttonWidth,
  className,
  color,
  disabled = false,
  icon,
  iconSize = 18,
  label,
  loading = false,
  onClick,
  size = "small",
  tooltip,
}: AppActionIconButtonProps) {
  const theme = useAppTheme();
  const isDisabled = disabled || loading;
  const accessibleLabel = ariaLabel ?? labelToAria(label) ?? icon;
  const showTooltip = tooltip === undefined ? label !== undefined : tooltip;

  const button = (
    <AppIconButton
      aria-label={accessibleLabel}
      className={["app-action-icon-button", "action-btn", className]
        .filter(Boolean)
        .join(" ")}
      disabled={isDisabled}
      onClick={onClick}
      size={size}
      style={
        {
          "--ab-color": color ?? theme.palette.text.secondary,
          "--ab-hover-color": isDisabled
            ? (color ?? theme.palette.text.secondary)
            : theme.palette.text.primary,
          height: buttonHeight,
          opacity: disabled && !loading ? 0.45 : 1,
          width: buttonWidth,
        } as CSSProperties
      }
    >
      {loading ? (
        <AppCircularProgress color="inherit" size={14} />
      ) : (
        <Icon height={iconSize} icon={icon} width={iconSize} />
      )}
    </AppIconButton>
  );

  if (!showTooltip || label === undefined) {
    return button;
  }

  return (
    <AppTooltip title={label}>
      <span>{button}</span>
    </AppTooltip>
  );
}
