import { Icon } from "@iconify/react";
import React from "react";

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
  label?: React.ReactNode;
  loading?: boolean;
  onClick: (event: React.MouseEvent<HTMLButtonElement>) => void;
  size?: "small" | "medium";
  tooltip?: boolean;
}

const labelToAria = (label: React.ReactNode): string | undefined =>
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
  tooltip = label !== undefined,
}: AppActionIconButtonProps) {
  const theme = useAppTheme();
  const isDisabled = disabled || loading;
  const accessibleLabel = ariaLabel ?? labelToAria(label) ?? icon;

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
        } as React.CSSProperties
      }
    >
      {loading ? (
        <AppCircularProgress color="inherit" size={14} />
      ) : (
        <Icon height={iconSize} icon={icon} width={iconSize} />
      )}
    </AppIconButton>
  );

  if (!tooltip || label === undefined) {
    return button;
  }

  return (
    <AppTooltip title={label}>
      <span>{button}</span>
    </AppTooltip>
  );
}
