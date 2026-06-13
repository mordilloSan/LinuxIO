import { Icon } from "@iconify/react";
import React from "react";

import AppCircularProgress from "@/components/ui/AppCircularProgress";
import { useAppTheme } from "@/theme";

interface ActionButtonProps {
  color?: string;
  disabled?: boolean;
  icon: string;
  loading?: boolean;
  onClick: () => void;
}

const ActionButton: React.FC<ActionButtonProps> = ({
  color,
  disabled = false,
  icon,
  loading = false,
  onClick,
}) => {
  const theme = useAppTheme();
  const isDisabled = disabled || loading;

  return (
    <div
      aria-disabled={isDisabled}
      className="action-btn"
      onClick={() => {
        if (!isDisabled) onClick();
      }}
      style={
        {
          width: 18,
          height: 22,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          cursor: isDisabled ? "default" : "pointer",
          marginInline: 0.4,
          opacity: disabled && !loading ? 0.45 : 1,
          transition: "color 0.2s",
          "--ab-color": color ?? theme.palette.text.secondary,
          "--ab-hover-color": isDisabled
            ? (color ?? theme.palette.text.secondary)
            : theme.palette.text.primary,
        } as React.CSSProperties
      }
    >
      {loading ? (
        <AppCircularProgress color="inherit" size={14} />
      ) : (
        <Icon height={16} icon={icon} width={16} />
      )}
    </div>
  );
};

export default ActionButton;
