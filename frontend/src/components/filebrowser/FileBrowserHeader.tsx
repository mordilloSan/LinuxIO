import CloseIcon from "@mui/icons-material/Close";
import SaveIcon from "@mui/icons-material/Save";
import SyncIcon from "@mui/icons-material/Sync";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Box,
  CircularProgress,
  IconButton,
  Stack,
  Typography,
  useMediaQuery,
  useTheme,
  Tooltip,
} from "@mui/material";
import React, { ReactNode, useCallback } from "react";

import ReindexDialog from "./ReindexDialog";
import SearchBar from "./SearchBar";
import { ViewMode } from "../../types/filebrowser";

import useAuth from "@/hooks/useAuth";
import { useFileTransfers } from "@/hooks/useFileTransfers";

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
  searchQuery?: string;
  onSearchChange?: (value: string) => void;
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
  searchQuery = "",
  onSearchChange = () => {},
}) => {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));
  const { indexerAvailable } = useAuth();
  const { startReindex, isReindexing, openReindexDialog } = useFileTransfers();

  const handleReindex = useCallback(() => {
    openReindexDialog();
    void startReindex({});
  }, [openReindexDialog, startReindex]);

  return (
    <>
      <Box
        sx={(theme) => ({
          display: "flex",
          alignItems: "center",
          px: 3,
          minHeight: 64,
          backgroundColor:
            theme.palette.mode === "light"
              ? theme.darken(theme.sidebar.background, 0.13)
              : theme.lighten(theme.sidebar.background, 0.06),
          boxShadow: theme.shadows[2],
        })}
      >
        {/* Left section - Status indicator when editing */}
        {showQuickSave && (
          <Box
            sx={{
              minWidth: 150,
              display: "flex",
              alignItems: "center",
              gap: 1,
            }}
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
        {/* Center section - File info when editing OR search bar when browsing */}
        {showQuickSave && editingFileName ? (
          <Box sx={{ flex: 1, textAlign: "center", mx: 2 }}>
            <Typography variant="h6" fontWeight={600}>
              {editingFileName}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {editingFilePath}
            </Typography>
          </Box>
        ) : (
          <Box
            sx={{
              flex: 1,
              display: "flex",
              justifyContent: "center",
              mx: 2,
            }}
          >
            <SearchBar
              value={searchQuery}
              onChange={onSearchChange}
              placeholder="Search files and folders..."
            />
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
                      showHiddenFiles
                        ? "Hide hidden files"
                        : "Show hidden files"
                    }
                  >
                    {showHiddenFiles ? (
                      <VisibilityIcon />
                    ) : (
                      <VisibilityOffIcon />
                    )}
                  </IconButton>
                </Tooltip>

                <Tooltip
                  title={
                    isReindexing
                      ? "Reindexing..."
                      : indexerAvailable === false
                        ? "Indexer unavailable"
                        : "Reindex filesystem"
                  }
                >
                  <span>
                    <IconButton
                      onClick={handleReindex}
                      disabled={isReindexing || indexerAvailable === false}
                      aria-label="Reindex filesystem"
                      sx={{
                        position: "relative",
                      }}
                    >
                      {isReindexing ? (
                        <CircularProgress size={24} />
                      ) : (
                        <SyncIcon
                          sx={{
                            color:
                              indexerAvailable === false
                                ? "text.disabled"
                                : "inherit",
                          }}
                        />
                      )}
                    </IconButton>
                  </span>
                </Tooltip>
              </>
            )}
          </Stack>
        </Box>
      </Box>
      <ReindexDialog />
    </>
  );
};

export default FileBrowserHeader;
