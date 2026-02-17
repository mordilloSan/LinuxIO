import { Dialog, DialogProps } from "@mui/material";
import { SxProps, Theme, useTheme } from "@mui/material/styles";
import { SystemStyleObject } from "@mui/system";
import React from "react";

type SxElement =
  | SystemStyleObject<Theme>
  | ((theme: Theme) => SystemStyleObject<Theme>);

const normalizeSx = (sx?: SxProps<Theme>): SxElement[] => {
  if (sx === undefined) {
    return [];
  }

  if (Array.isArray(sx)) {
    return sx.filter((v): v is SxElement => v !== false && v !== undefined);
  }

  return [sx as SxElement];
};

const getSlotSx = (slot: unknown): SxProps<Theme> | undefined => {
  if (slot && typeof slot === "object" && "sx" in slot) {
    return (slot as { sx?: SxProps<Theme> }).sx;
  }
  return undefined;
};

const GeneralDialog: React.FC<DialogProps> = ({
  slotProps,
  children,
  ...dialogProps
}) => {
  const theme = useTheme();
  const paperSx = normalizeSx(getSlotSx(slotProps?.paper));
  const backdropSx = normalizeSx(getSlotSx(slotProps?.backdrop));

  return (
    <Dialog
      {...dialogProps}
      slotProps={{
        ...slotProps,
        paper: {
          ...slotProps?.paper,
          sx: [
            {
              backgroundColor: theme.header.background,
              borderRadius: 4,
              border: "1px solid rgba(255, 255, 255, 0.2)",
              boxShadow:
                "0 0 10px rgba(255, 255, 255, 0.5), 0 0 20px rgba(255, 255, 255, 0.3), inset 0 0 20px rgba(255, 255, 255, 0.1)",
              backdropFilter: "blur(10px)",
            },
            ...paperSx,
          ],
        },
        backdrop: {
          ...slotProps?.backdrop,
          sx: [
            {
              backdropFilter: "blur(4px)",
              backgroundColor: "rgba(0, 0, 0, 0.7)",
            },
            ...backdropSx,
          ],
        },
      }}
    >
      {children}
    </Dialog>
  );
};

export default GeneralDialog;
