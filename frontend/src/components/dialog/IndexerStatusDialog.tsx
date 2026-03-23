import { Icon } from "@iconify/react";
import { useAppTheme } from "@/theme";
import React from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography, {
  type AppTypographyProps,
} from "@/components/ui/AppTypography";
export interface IndexerStat {
  value: React.ReactNode;
  label: string;
  valueColor?: string;
  valueVariant?: AppTypographyProps["variant"];
}
interface IndexerStatusDialogProps {
  open: boolean;
  onClose: () => void;
  onExited?: () => void;
  title: string;
  isRunning: boolean;
  success: boolean;
  error?: string | null;
  phaseLabel: string;
  progressStats?: IndexerStat[];
  showProgressStats?: boolean;
  successMessage?: string;
  successDescription?: React.ReactNode;
  summaryTitle?: string;
  summaryStats?: IndexerStat[];
}
const IndexerStatusDialog: React.FC<IndexerStatusDialogProps> = ({
  open,
  onClose,
  onExited,
  title,
  isRunning,
  success,
  error,
  phaseLabel,
  progressStats = [],
  showProgressStats = true,
  successMessage = "Indexing completed successfully!",
  successDescription,
  summaryTitle,
  summaryStats = [],
}) => {
  const theme = useAppTheme();
  const sectionBackground = theme.codeBlock.background;
  const hasProgressStats = showProgressStats && progressStats.length > 0;
  const hasSummary = Boolean(summaryTitle) && summaryStats.length > 0;
  return (
    <GeneralDialog
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      slotProps={{
        transition: {
          onExited,
        },
      }}
    >
      <AppDialogTitle
        style={{
          backgroundColor: theme.header.background,
          borderBottom: `1px solid ${theme.palette.divider}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 4,
          }}
        >
          {isRunning && (
            <AppLinearProgress
              style={{
                width: 100,
              }}
            />
          )}
          {!isRunning && success && (
            <Icon
              icon="mdi:check-circle"
              width={24}
              height={24}
              color={theme.palette.success.main}
            />
          )}
          {!isRunning && error && (
            <Icon
              icon="mdi:alert-circle"
              width={24}
              height={24}
              color={theme.palette.error.main}
            />
          )}
          <AppTypography variant="h6">{title}</AppTypography>
        </div>
        <AppIconButton onClick={onClose} size="small">
          <Icon icon="mdi:close" width={20} height={20} />
        </AppIconButton>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          padding: 12,
        }}
      >
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
          }}
        >
          <AppTypography variant="body2" color="text.secondary">
            {phaseLabel}
          </AppTypography>

          {hasProgressStats && (
            <div
              style={{
                display: "flex",
                gap: 12,
                padding: 8,
                backgroundColor: sectionBackground,
                borderRadius: 4,
              }}
            >
              {progressStats.map((stat) => (
                <div key={stat.label}>
                  <AppTypography
                    variant={stat.valueVariant ?? "h4"}
                    style={
                      stat.valueColor
                        ? {
                            color: stat.valueColor,
                          }
                        : undefined
                    }
                  >
                    {stat.value}
                  </AppTypography>
                  <AppTypography variant="caption" color="text.secondary">
                    {stat.label}
                  </AppTypography>
                </div>
              ))}
            </div>
          )}

          {success && (
            <div
              style={{
                marginTop: 8,
              }}
            >
              <AppTypography variant="body2" color="success" gutterBottom>
                ✓ {successMessage}
              </AppTypography>
              {successDescription && (
                <AppTypography
                  variant="caption"
                  color="text.secondary"
                  style={{
                    display: "block",
                  }}
                >
                  {successDescription}
                </AppTypography>
              )}

              {hasSummary && (
                <div
                  style={{
                    marginTop: 8,
                    padding: 8,
                    backgroundColor: sectionBackground,
                    borderRadius: 4,
                  }}
                >
                  <AppTypography
                    variant="subtitle2"
                    color="text.primary"
                    gutterBottom
                  >
                    {summaryTitle}
                  </AppTypography>
                  <div
                    style={{
                      display: "flex",
                      gap: 12,
                      marginTop: 4,
                    }}
                  >
                    {summaryStats.map((stat) => (
                      <div key={stat.label}>
                        <AppTypography
                          variant={stat.valueVariant ?? "h5"}
                          style={
                            stat.valueColor
                              ? {
                                  color: stat.valueColor,
                                }
                              : undefined
                          }
                        >
                          {stat.value}
                        </AppTypography>
                        <AppTypography variant="caption" color="text.secondary">
                          {stat.label}
                        </AppTypography>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {error && (
            <AppTypography color="error" variant="body2">
              Error: {error}
            </AppTypography>
          )}
        </div>
      </AppDialogContent>
    </GeneralDialog>
  );
};
export default IndexerStatusDialog;
