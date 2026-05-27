import React, { ReactNode } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface QuickActionButtonProps {
  ariaLabel?: string;
  disabled?: boolean;
  icon: ReactNode;
  label: string;
  onClick: () => void;
}

const QuickActionButton: React.FC<QuickActionButtonProps> = ({
  icon,
  label,
  onClick,
  disabled,
  ariaLabel,
}) => {
  const theme = useAppTheme();
  const isDark = theme.palette.mode === "dark";

  return (
    <AppTooltip title={label}>
      <span>
        <AppIconButton
          aria-label={ariaLabel ?? label}
          className="quick-toggle action app-icon-btn--quick-action"
          disabled={disabled}
          onClick={onClick}
          size="small"
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
