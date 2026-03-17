import { useTheme } from "@mui/material/styles";
import React, { ReactNode } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { alpha } from "@/utils/color";

interface QuickActionButtonProps {
  icon: ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  ariaLabel?: string;
}

const QuickActionButton: React.FC<QuickActionButtonProps> = ({
  icon,
  label,
  onClick,
  disabled,
  ariaLabel,
}) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";

  return (
    <AppTooltip title={label}>
      <span>
        <AppIconButton
          className="quick-toggle action app-icon-btn--quick-action"
          size="small"
          onClick={onClick}
          disabled={disabled}
          aria-label={ariaLabel ?? label}
          style={{
            backgroundColor: isDark
              ? alpha(theme.fileBrowser.chrome, 0.33)
              : alpha(theme.fileBrowser.chrome, 0.12),
            color: theme.palette.text.primary,
          }}
        >
          {icon}
        </AppIconButton>
      </span>
    </AppTooltip>
  );
};

export default QuickActionButton;
