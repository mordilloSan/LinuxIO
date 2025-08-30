import { Icon } from "@iconify/react";
import { Box } from "@mui/material";
import React from "react";

interface ActionButtonProps {
  icon: string;
  onClick: () => void;
}

const ActionButton: React.FC<ActionButtonProps> = ({ icon, onClick }) => {
  return (
    <Box
      onClick={onClick}
      sx={{
        width: 18,
        height: 22,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        cursor: "pointer",
        mx: 0.1,
        color: "text.secondary",
        transition: "color 0.2s",
        "&:hover": {
          color: "text.primary",
        },
      }}
    >
      <Icon icon={icon} width={16} height={16} />
    </Box>
  );
};

export default ActionButton;
