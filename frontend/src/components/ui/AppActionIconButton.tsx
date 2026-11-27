import { Icon } from "@iconify/react";
import type {
  CSSProperties,
  MouseEvent,
  ReactElement,
  ReactNode,
  Ref,
} from "react";

import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";

import "./app-action-icon-button.css";

export interface AppActionIconButtonProps {
  ariaLabel?: string;
  buttonHeight?: number;
  buttonWidth?: number;
  className?: string;
  color?: string;
  disabled?: boolean;
  /** An icon name, or an element that draws the icon itself. */
  icon: string | ReactElement;
  iconSize?: number;
  label?: ReactNode;
  loading?: boolean;
  onClick: (event: MouseEvent<HTMLButtonElement>) => void;
  ref?: Ref<HTMLButtonElement>;
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
  ref,
  size = "small",
  tooltip,
}: AppActionIconButtonProps) {
  const isDisabled = disabled || loading;
  const accessibleLabel =
    ariaLabel ??
    labelToAria(label) ??
    (typeof icon === "string" ? icon : undefined);
  const showTooltip = tooltip === undefined ? label !== undefined : tooltip;

  const button = (
    <AppIconButton
      aria-label={accessibleLabel}
      className={["app-action-icon-button", "action-btn", className]
        .filter(Boolean)
        .join(" ")}
      disabled={isDisabled}
      onClick={onClick}
      ref={ref}
      size={size}
      style={
        {
          "--ab-color": color,
          height: buttonHeight,
          opacity: disabled && !loading ? 0.45 : 1,
          width: buttonWidth,
        } as CSSProperties
      }
    >
      {loading ? (
        <AppCircularProgress color="inherit" size={14} />
      ) : typeof icon === "string" ? (
        <Icon height={iconSize} icon={icon} width={iconSize} />
      ) : (
        icon
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
