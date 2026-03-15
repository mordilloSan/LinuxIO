import { Icon } from "@iconify/react";
import {
  DialogContent,
  DialogTitle,
  IconButton,
  LinearProgress,
  Typography,
  type TypographyProps,
  useTheme,
} from "@mui/material";
import React from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";

export interface IndexerStat {
  value: React.ReactNode;
  label: string;
  valueColor?: string;
  valueVariant?: TypographyProps["variant"];
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
  const theme = useTheme();
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
      <DialogTitle
        sx={{
          backgroundColor: theme.header.background,
          borderBottom: `1px solid ${theme.palette.divider}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
          {isRunning && <LinearProgress sx={{ width: 100 }} />}
          {!isRunning && success && <Icon icon="mdi:check-circle" width={24} height={24} color={theme.palette.success.main} />}
          {!isRunning && error && <Icon icon="mdi:alert-circle" width={24} height={24} color={theme.palette.error.main} />}
          <Typography variant="h6">{title}</Typography>
        </div>
        <IconButton onClick={onClose} size="small">
          <Icon icon="mdi:close" width={20} height={20} />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ p: 3 }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <Typography variant="body2" color="text.secondary">
            {phaseLabel}
          </Typography>

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
                  <Typography
                    variant={stat.valueVariant ?? "h4"}
                    sx={
                      stat.valueColor ? { color: stat.valueColor } : undefined
                    }
                  >
                    {stat.value}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {stat.label}
                  </Typography>
                </div>
              ))}
            </div>
          )}

          {success && (
            <div style={{ marginTop: 8 }}>
              <Typography variant="body2" color="success.main" gutterBottom>
                ✓ {successMessage}
              </Typography>
              {successDescription && (
                <Typography
                  variant="caption"
                  color="text.secondary"
                  display="block"
                >
                  {successDescription}
                </Typography>
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
                  <Typography
                    variant="subtitle2"
                    color="text.primary"
                    gutterBottom
                  >
                    {summaryTitle}
                  </Typography>
                  <div style={{ display: "flex", gap: 12, marginTop: 4 }}>
                    {summaryStats.map((stat) => (
                      <div key={stat.label}>
                        <Typography
                          variant={stat.valueVariant ?? "h5"}
                          sx={
                            stat.valueColor
                              ? { color: stat.valueColor }
                              : undefined
                          }
                        >
                          {stat.value}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {stat.label}
                        </Typography>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {error && (
            <Typography color="error" variant="body2">
              Error: {error}
            </Typography>
          )}
        </div>
      </DialogContent>
    </GeneralDialog>
  );
};

export default IndexerStatusDialog;
