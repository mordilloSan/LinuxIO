import type { CSSProperties } from "react";

import type { AppDialogProps } from "@/components/ui/AppDialog";
import { AppDialog } from "@/components/ui/AppDialog";
import { getDialogSurfaceStyles } from "@/theme/surfaces";

interface GeneralDialogProps extends AppDialogProps {
  /** Extra styles merged onto the paper */
  paperStyle?: CSSProperties;
}

const GeneralDialog = ({
  children,
  paperStyle,
  ...dialogProps
}: GeneralDialogProps) => {
  return (
    <AppDialog
      {...dialogProps}
      backdropStyle={{
        backgroundColor:
          "color-mix(in srgb, var(--app-dialog-backdrop), transparent 30%)",
      }}
      paperStyle={{
        ...getDialogSurfaceStyles(),
        overflow: "hidden",
        ...paperStyle,
      }}
    >
      {children}
    </AppDialog>
  );
};

export default GeneralDialog;
