import { Icon } from "@iconify/react";
import React from "react";

import AppTypography from "@/components/ui/AppTypography";
import { iconSize } from "@/constants";
import { useAppTheme } from "@/theme";

const ErrorMessage: React.FC = () => {
  const theme = useAppTheme();

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        minHeight: "100%",
        textAlign: "center",
        gap: 8,
      }}
    >
      <Icon
        icon="mdi:alert-circle-outline"
        width={iconSize.lg}
        height={iconSize.lg}
        color={theme.palette.error.main}
      />
      <AppTypography color="error" variant="body1">
        Failed to load!
      </AppTypography>
    </div>
  );
};

export default ErrorMessage;
