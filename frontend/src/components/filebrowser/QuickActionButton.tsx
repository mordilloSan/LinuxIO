import React, { ReactNode } from "react";
import { IconButton, Tooltip, useTheme } from "@mui/material";

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
    <Tooltip title={label}>
      <span>
        <IconButton
          className="quick-toggle action"
          size="small"
          onClick={onClick}
          disabled={disabled}
          aria-label={ariaLabel ?? label}
          sx={{
            width: { xs: "3em", sm: "2.5em" },
            height: { xs: "3em", sm: "2.5em" },
            borderRadius: "50%",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            backgroundColor: isDark
              ? "rgba(37, 49, 55, 0.33)"
              : "rgba(37, 49, 55, 0.12)",
            color: theme.palette.text.primary,
            boxShadow: "none !important",
            "& .MuiSvgIcon-root": {
              fontSize: "1.6em",
            },
            "&:hover": {
              backgroundColor: theme.palette.primary.main,
              color: "#fff",
              boxShadow: "none",
            },
          }}
        >
          {icon}
        </IconButton>
      </span>
    </Tooltip>
  );
};

export default QuickActionButton;
