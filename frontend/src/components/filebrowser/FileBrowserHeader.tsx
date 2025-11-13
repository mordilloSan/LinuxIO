import CloseIcon from "@mui/icons-material/Close";
import SaveIcon from "@mui/icons-material/Save";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Box,
  IconButton,
  Stack,
  Typography,
  useMediaQuery,
  useTheme,
  darken,
  lighten,
  Tooltip,
} from "@mui/material";
import React, { ReactNode } from "react";

import { ViewMode } from "../../types/filebrowser";

interface FileBrowserHeaderProps {
  viewMode: ViewMode;
  showHiddenFiles: boolean;
  showQuickSave?: boolean;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  onSaveFile?: () => Promise<void>;
  onCloseEditor?: () => void;
  isSaving?: boolean;
  viewIcon: ReactNode;
  editingFileName?: string;
  editingFilePath?: string;
  isDirty?: boolean;
}

const FileBrowserHeader: React.FC<FileBrowserHeaderProps> = ({
  showHiddenFiles,
  showQuickSave = false,
  onSwitchView,
  onToggleHiddenFiles,
  onSaveFile,
  onCloseEditor,
  isSaving = false,
  viewIcon,
  editingFileName,
  editingFilePath,
  isDirty = false,
}) => {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));

  return (
    <Box
      sx={(theme) => ({
        display: "flex",
        alignItems: "center",
        px: 3,
        minHeight: 64,
        backgroundColor:
          theme.palette.mode === "light"
            ? darken(theme.sidebar.background, 0.13)
            : lighten(theme.sidebar.background, 0.06),
        boxShadow: theme.shadows[2],
      })}
    >
      {/* Left section - Status indicator when editing */}
      {showQuickSave && (
        <Box
          sx={{ minWidth: 150, display: "flex", alignItems: "center", gap: 1 }}
        >
          {isDirty && (
            <Typography
              variant="caption"
              sx={{
                color: theme.palette.primary.main,
                fontWeight: 600,
                display: "flex",
                alignItems: "center",
                gap: 0.5,
              }}
            >
              • Unsaved changes
            </Typography>
          )}
        </Box>
      )}

      {/* Center section - File info when editing */}
      {showQuickSave && editingFileName && (
        <Box sx={{ flex: 1, textAlign: "center", mx: 2 }}>
          <Typography variant="h6" fontWeight={600}>
            {editingFileName}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {editingFilePath}
          </Typography>
        </Box>
      )}

      {/* Right section - Action buttons */}
      <Box
        className={`header-right quick-actions${isMobile ? " is-mobile" : ""}`}
        sx={{
          display: "flex",
          ml: "auto",
        }}
      >
        <Stack
          direction={"row"}
          spacing={0.4}
          className="quick-actions-group"
          sx={{
            alignItems: "center",
            gap: "0.4em",
          }}
        >
          {showQuickSave && (
            <>
              <Tooltip title="Close editor">
                <IconButton
                  onClick={onCloseEditor || (() => {})}
                  disabled={isSaving}
                >
                  <CloseIcon fontSize="medium" />
                </IconButton>
              </Tooltip>

              <Tooltip title="Save changes">
                <IconButton
                  onClick={onSaveFile || (() => {})}
                  disabled={isSaving}
                >
                  <SaveIcon fontSize="medium" />
                </IconButton>
              </Tooltip>
            </>
          )}

          {!showQuickSave && (
            <>
              <Tooltip title="Switch view">
                <IconButton onClick={onSwitchView} aria-label="Switch view">
                  {viewIcon}
                </IconButton>
              </Tooltip>

              <Tooltip
                title={
                  showHiddenFiles ? "Hide hidden files" : "Show hidden files"
                }
              >
                <IconButton
                  onClick={onToggleHiddenFiles}
                  aria-label={
                    showHiddenFiles ? "Hide hidden files" : "Show hidden files"
                  }
                >
                  {showHiddenFiles ? <VisibilityIcon /> : <VisibilityOffIcon />}
                </IconButton>
              </Tooltip>
            </>
          )}
        </Stack>
      </Box>
    </Box>
  );
};

export default FileBrowserHeader;
