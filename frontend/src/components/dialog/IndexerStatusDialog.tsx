import { Icon } from "@iconify/react";
import React from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography, {
  type AppTypographyProps,
} from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
export interface IndexerStat {
  label: string;
  value: React.ReactNode;
  valueColor?: string;
  valueVariant?: AppTypographyProps["variant"];
}

export interface IndexerStatSection {
  stats: IndexerStat[];
  subtitle?: React.ReactNode;
  title: React.ReactNode;
}

interface IndexerStatusDialogProps {
  detailSections?: IndexerStatSection[];
  detailTitle?: string;
  error?: string | null;
  isRunning: boolean;
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
  phaseLabel: string;
  progressStats?: IndexerStat[];
  showProgressStats?: boolean;
  success: boolean;
  successDescription?: React.ReactNode;
  successMessage?: string;
  summaryStats?: IndexerStat[];
  summaryTitle?: string;
  title: string;
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
  detailTitle,
  detailSections = [],
  summaryTitle,
  summaryStats = [],
}) => {
  const theme = useAppTheme();
  const sectionBackground = theme.codeBlock.background;
  const hasProgressStats = showProgressStats && progressStats.length > 0;
  const hasDetails = detailSections.length > 0;
  const hasSummary = Boolean(summaryTitle) && summaryStats.length > 0;
  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
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
              color={theme.palette.success.main}
              height={24}
              icon="mdi:check-circle"
              width={24}
            />
          )}
          {!isRunning && error && (
            <Icon
              color={theme.palette.error.main}
              height={24}
              icon="mdi:alert-circle"
              width={24}
            />
          )}
          <AppTypography variant="h6">{title}</AppTypography>
        </div>
        <AppIconButton onClick={onClose} size="small">
          <Icon height={20} icon="mdi:close" width={20} />
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
          <AppTypography color="text.secondary" variant="body2">
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
                    style={
                      stat.valueColor
                        ? {
                            color: stat.valueColor,
                          }
                        : undefined
                    }
                    variant={stat.valueVariant ?? "h4"}
                  >
                    {stat.value}
                  </AppTypography>
                  <AppTypography color="text.secondary" variant="caption">
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
              <AppTypography color="success" gutterBottom variant="body2">
                ✓ {successMessage}
              </AppTypography>
              {successDescription && (
                <AppTypography
                  color="text.secondary"
                  style={{
                    display: "block",
                  }}
                  variant="caption"
                >
                  {successDescription}
                </AppTypography>
              )}

              {hasDetails && (
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: 8,
                    marginTop: 8,
                  }}
                >
                  {detailTitle && (
                    <AppTypography color="text.primary" variant="subtitle2">
                      {detailTitle}
                    </AppTypography>
                  )}
                  {detailSections.map((section, index) => (
                    <div
                      key={index}
                      style={{
                        padding: 8,
                        backgroundColor: sectionBackground,
                        borderRadius: 4,
                      }}
                    >
                      <AppTypography color="text.primary" variant="subtitle2">
                        {section.title}
                      </AppTypography>
                      {section.subtitle && (
                        <AppTypography
                          color="text.secondary"
                          noWrap
                          style={{
                            display: "block",
                          }}
                          title={
                            typeof section.subtitle === "string"
                              ? section.subtitle
                              : undefined
                          }
                          variant="caption"
                        >
                          {section.subtitle}
                        </AppTypography>
                      )}
                      <div
                        style={{
                          display: "flex",
                          gap: 12,
                          marginTop: 4,
                        }}
                      >
                        {section.stats.map((stat) => (
                          <div key={stat.label}>
                            <AppTypography
                              style={
                                stat.valueColor
                                  ? {
                                      color: stat.valueColor,
                                    }
                                  : undefined
                              }
                              variant={stat.valueVariant ?? "h5"}
                            >
                              {stat.value}
                            </AppTypography>
                            <AppTypography
                              color="text.secondary"
                              variant="caption"
                            >
                              {stat.label}
                            </AppTypography>
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
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
                    color="text.primary"
                    gutterBottom
                    variant="subtitle2"
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
                          style={
                            stat.valueColor
                              ? {
                                  color: stat.valueColor,
                                }
                              : undefined
                          }
                          variant={stat.valueVariant ?? "h5"}
                        >
                          {stat.value}
                        </AppTypography>
                        <AppTypography color="text.secondary" variant="caption">
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
