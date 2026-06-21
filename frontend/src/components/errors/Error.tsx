import { Icon } from "@iconify/react";
import React from "react";

import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { iconSize } from "@/theme/constants";

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
        color={theme.palette.error.main}
        height={iconSize.lg}
        icon="mdi:alert-circle-outline"
        width={iconSize.lg}
      />
      <AppTypography color="error" variant="body1">
        Failed to load!
      </AppTypography>
    </div>
  );
};

export default ErrorMessage;
