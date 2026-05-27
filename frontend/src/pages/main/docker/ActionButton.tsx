import { Icon } from "@iconify/react";
import React from "react";

import { useAppTheme } from "@/theme";

interface ActionButtonProps {
  icon: string;
  onClick: () => void;
}

const ActionButton: React.FC<ActionButtonProps> = ({ icon, onClick }) => {
  const theme = useAppTheme();

  return (
    <div
      className="action-btn"
      onClick={onClick}
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
      <Icon height={16} icon={icon} width={16} />
    </div>
  );
};

export default ActionButton;
