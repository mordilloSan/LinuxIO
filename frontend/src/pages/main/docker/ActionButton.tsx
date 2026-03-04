import { Icon } from "@iconify/react";
import { useTheme } from "@mui/material/styles";
import React from "react";

interface ActionButtonProps {
  icon: string;
  onClick: () => void;
}

const ActionButton: React.FC<ActionButtonProps> = ({ icon, onClick }) => {
  const theme = useTheme();

  return (
    <div
      onClick={onClick}
      className="action-btn"
      style={
        {
          width: 18,
          height: 22,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          cursor: "pointer",
          marginInline: 0.4,
          transition: "color 0.2s",
          "--ab-color": theme.palette.text.secondary,
          "--ab-hover-color": theme.palette.text.primary,
        } as React.CSSProperties
      }
    >
      <Icon icon={icon} width={16} height={16} />
    </div>
  );
};

export default ActionButton;
