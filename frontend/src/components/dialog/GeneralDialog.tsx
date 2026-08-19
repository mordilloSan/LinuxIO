import type { CSSProperties } from "react";

import type { AppDialogProps } from "@/components/ui/AppDialog";
import { AppDialog } from "@/components/ui/AppDialog";
import { useAppTheme } from "@/theme";
import { getDialogSurfaceStyles } from "@/theme/surfaces";
import { alpha } from "@/utils/color";

interface GeneralDialogProps extends AppDialogProps {
  /** Extra styles merged onto the paper */
  paperStyle?: CSSProperties;
}

const GeneralDialog = ({
  children,
  paperStyle,
  ...dialogProps
}: GeneralDialogProps) => {
  const theme = useAppTheme();

  return (
    <AppDialog
      {...dialogProps}
      backdropStyle={{
        backgroundColor: alpha(theme.dialog.backdrop, 0.7),
      }}
      paperStyle={{
        ...getDialogSurfaceStyles(theme),
        overflow: "hidden",
        ...paperStyle,
      }}
    >
      {children}
    </AppDialog>
  );
};

export default GeneralDialog;
