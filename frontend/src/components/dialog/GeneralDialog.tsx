import { useTheme } from "@mui/material/styles";
import React from "react";

import { AppDialog, AppDialogProps } from "@/components/ui/AppDialog";
import { alpha } from "@/utils/color";

interface GeneralDialogProps extends AppDialogProps {
  /** Extra styles merged onto the paper */
  paperStyle?: React.CSSProperties;
}

const GeneralDialog: React.FC<GeneralDialogProps> = ({
  children,
  paperStyle,
  ...dialogProps
}) => {
  const theme = useTheme();

  return (
    <AppDialog
      {...dialogProps}
      paperStyle={{
        backgroundColor: theme.palette.background.paper,
        borderRadius: 16,
        border: `1px solid ${alpha(theme.dialog.border, 0.2)}`,
        boxShadow: `0 0 10px ${alpha(theme.dialog.glow, 0.5)}, 0 0 20px ${alpha(theme.dialog.glow, 0.3)}, inset 0 0 20px ${alpha(theme.dialog.glow, 0.1)}`,
        backdropFilter: "blur(10px)",
        ...paperStyle,
      }}
      backdropStyle={{
        backdropFilter: "blur(4px)",
        backgroundColor: alpha(theme.dialog.backdrop, 0.7),
      }}
    >
      {children}
    </AppDialog>
  );
};

export default GeneralDialog;
