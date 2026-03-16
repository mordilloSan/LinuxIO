import { Icon } from "@iconify/react";
import {
  DialogTitle,
  DialogContent,
  IconButton,
  Alert,
  Switch,
  FormControlLabel,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { alpha } from "@/utils/color";
interface LogDialogProps {
  open: boolean;
  onClose: () => void;
  /** Text title shown in the header. Ignored when `titleContent` is provided. */
  title?: string;
  /** Replaces the text title (e.g. a search field). Should grow to fill available space. */
  titleContent?: React.ReactNode;
  /** Extra action buttons rendered before the live switch (e.g. copy, download). */
  extraActions?: React.ReactNode;
  logs: string;
  isLoading: boolean;
  error: string | null;
  liveMode: boolean;
  onLiveModeChange: (value: boolean) => void;
  logsBoxRef: React.RefObject<HTMLDivElement | null>;
  onExited?: () => void;
  maxWidth?: "sm" | "md" | "lg" | "xl";
}
const LogDialog: React.FC<LogDialogProps> = ({
  open,
  onClose,
  title,
  titleContent,
  extraActions,
  logs,
  isLoading,
  error,
  liveMode,
  onLiveModeChange,
  logsBoxRef,
  onExited,
  maxWidth = "md",
}) => {
  const theme = useTheme();
  return (
    <GeneralDialog
      open={open}
      onClose={onClose}
      maxWidth={maxWidth}
      fullWidth
      slotProps={{
        transition: {
          onExited,
        },
      }}
    >
      <DialogTitle
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
        }}
      >
        <div
          style={{
            flex: 1,
            display: "flex",
            alignItems: "center",
          }}
        >
          {titleContent ?? <AppTypography variant="h6">{title}</AppTypography>}
        </div>
        {extraActions}
        <AppTooltip
          title={liveMode ? "Live streaming ON" : "Live streaming OFF"}
        >
          <FormControlLabel
            control={
              <Switch
                checked={liveMode}
                onChange={(_, checked) => onLiveModeChange(checked)}
                size="small"
              />
            }
            label="Live"
            sx={{
              ml: 1,
            }}
          />
        </AppTooltip>
        <IconButton onClick={onClose} size="small">
          <Icon icon="mdi:close" width={18} height={18} />
        </IconButton>
      </DialogTitle>

      <DialogContent
        dividers
        sx={{
          p: 0,
        }}
      >
        {error ? (
          <Alert
            severity="error"
            sx={{
              m: 2,
            }}
          >
            {error}
          </Alert>
        ) : (
          <div
            ref={logsBoxRef}
            className="custom-scrollbar"
            style={{
              position: "relative",
              backgroundColor: theme.codeBlock.background,
              color: theme.codeBlock.color,
              padding: theme.spacing(2),
              overflow: "auto",
              fontFamily: "Fira Mono, monospace",
              fontSize: "0.85rem",
              whiteSpace: "pre-wrap",
              wordBreak: "break-all",
              minHeight: 300,
              maxHeight: 500,
            }}
          >
            {isLoading && (
              <div
                style={{
                  position: "absolute",
                  inset: 0,
                  background: alpha(theme.codeBlock.background, 0.85),
                  zIndex: 10,
                }}
              >
                <ComponentLoader />
              </div>
            )}
            {!isLoading &&
              (logs || (
                <AppTypography color="text.secondary">
                  No logs available.
                </AppTypography>
              ))}
          </div>
        )}
      </DialogContent>
    </GeneralDialog>
  );
};
export default LogDialog;
