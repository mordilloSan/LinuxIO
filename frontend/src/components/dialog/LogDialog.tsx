import { Icon } from "@iconify/react";
import type { ReactNode, RefObject } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert from "@/components/ui/AppAlert";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";

import "./log-dialog.css";

interface LogDialogProps {
  error: string | null;
  /** Extra action buttons rendered before the live switch (e.g. copy, download). */
  extraActions?: ReactNode;
  isLoading: boolean;
  liveMode: boolean;
  logs: string;
  logsBoxRef: RefObject<HTMLDivElement | null>;
  maxWidth?: "sm" | "md" | "lg" | "xl";
  onClose: () => void;
  onExited?: () => void;
  onLiveModeChange: (value: boolean) => void;
  open: boolean;
  /** Text title shown in the header. Ignored when `titleContent` is provided. */
  title?: string;
  /** Replaces the text title (e.g. a search field). Should grow to fill available space. */
  titleContent?: ReactNode;
}
const LogDialog = ({
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
}: LogDialogProps) => {
  return (
    <GeneralDialog
      fullWidth
      maxWidth={maxWidth}
      onClose={onClose}
      open={open}
      slotProps={{
        transition: {
          onExited,
        },
      }}
    >
      <AppDialogTitle
        style={{
          display: "flex",
          alignItems: "center",
          gap: 4,
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
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={liveMode}
                onChange={(_, checked) => onLiveModeChange(checked)}
                size="small"
              />
            }
            label="Live"
            style={{ marginLeft: 4 }}
          />
        </AppTooltip>
        <AppIconButton
          aria-label="Close log dialog"
          onClick={onClose}
          size="small"
        >
          <Icon height={18} icon="mdi:close" width={18} />
        </AppIconButton>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          padding: 0,
          borderTop: "1px solid var(--app-palette-divider)",
        }}
      >
        {error ? (
          <AppAlert
            severity="error"
            style={{
              margin: 8,
            }}
          >
            {error}
          </AppAlert>
        ) : (
          <div
            className="custom-scrollbar log-dialog__log-box"
            ref={logsBoxRef}
            style={{
              position: "relative",
              backgroundColor: "var(--app-code-block-background)",
              color: "var(--app-code-block-color)",
              padding: "var(--app-space-8)",
              overflow: "auto",
              fontFamily: "Fira Mono, monospace",
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
                  background:
                    "color-mix(in srgb, var(--app-code-block-background), transparent 15%)",
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
      </AppDialogContent>
    </GeneralDialog>
  );
};
export default LogDialog;
