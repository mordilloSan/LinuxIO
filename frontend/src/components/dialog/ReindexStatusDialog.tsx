import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import CloseIcon from "@mui/icons-material/Close";
import ErrorIcon from "@mui/icons-material/Error";
import {
  Box,
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

export interface ReindexStat {
  value: React.ReactNode;
  label: string;
  valueColor?: string;
  valueVariant?: TypographyProps["variant"];
}

interface ReindexStatusDialogProps {
  open: boolean;
  onClose: () => void;
  onExited?: () => void;
  title: string;
  isRunning: boolean;
  success: boolean;
  error?: string | null;
  phaseLabel: string;
  progressStats?: ReindexStat[];
  showProgressStats?: boolean;
  successMessage?: string;
  successDescription?: React.ReactNode;
  summaryTitle?: string;
  summaryStats?: ReindexStat[];
}

const ReindexStatusDialog: React.FC<ReindexStatusDialogProps> = ({
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
  successMessage = "Reindex completed successfully!",
  successDescription,
  summaryTitle,
  summaryStats = [],
}) => {
  const theme = useTheme();
  const sectionBackground =
    theme.palette.mode === "dark" ? "#1e1e1e" : "#f5f5f5";
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
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          {isRunning && <LinearProgress sx={{ width: 100 }} />}
          {!isRunning && success && <CheckCircleIcon color="success" />}
          {!isRunning && error && <ErrorIcon color="error" />}
          <Typography variant="h6">{title}</Typography>
        </Box>
        <IconButton onClick={onClose} size="small">
          <CloseIcon />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ p: 3 }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          <Typography variant="body2" color="text.secondary">
            {phaseLabel}
          </Typography>

          {hasProgressStats && (
            <Box
              sx={{
                display: "flex",
                gap: 3,
                p: 2,
                backgroundColor: sectionBackground,
                borderRadius: 1,
              }}
            >
              {progressStats.map((stat) => (
                <Box key={stat.label}>
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
                </Box>
              ))}
            </Box>
          )}

          {success && (
            <Box sx={{ mt: 2 }}>
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
                <Box
                  sx={{
                    mt: 2,
                    p: 2,
                    backgroundColor: sectionBackground,
                    borderRadius: 1,
                  }}
                >
                  <Typography
                    variant="subtitle2"
                    color="text.primary"
                    gutterBottom
                  >
                    {summaryTitle}
                  </Typography>
                  <Box sx={{ display: "flex", gap: 3, mt: 1 }}>
                    {summaryStats.map((stat) => (
                      <Box key={stat.label}>
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
                      </Box>
                    ))}
                  </Box>
                </Box>
              )}
            </Box>
          )}

          {error && (
            <Typography color="error" variant="body2">
              Error: {error}
            </Typography>
          )}
        </Box>
      </DialogContent>
    </GeneralDialog>
  );
};

export default ReindexStatusDialog;
