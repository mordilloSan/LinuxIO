import SaveIcon from "@mui/icons-material/Save";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Box,
  IconButton,
  Stack,
  useMediaQuery,
  useTheme,
  darken,
  lighten,
  Tooltip,
} from "@mui/material";
import React, { ReactNode } from "react";

import QuickActionButton from "./QuickActionButton";
import { ViewMode } from "../../types/filebrowser";

interface FileBrowserHeaderProps {
  viewMode: ViewMode;
  showHiddenFiles: boolean;
  showQuickSave?: boolean;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  viewIcon: ReactNode;
}

const FileBrowserHeader: React.FC<FileBrowserHeaderProps> = ({
  viewMode,
  showHiddenFiles,
  showQuickSave = false,
  onSwitchView,
  onToggleHiddenFiles,
  viewIcon,
}) => {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));

  return (
    <Box
      sx={(theme) => ({
        display: "flex",
        px: 3,
        minHeight: 64,
        backgroundColor:
          theme.palette.mode === "light"
            ? darken(theme.sidebar.background, 0.13)
            : lighten(theme.sidebar.background, 0.06),
        boxShadow: theme.shadows[2],
      })}
    >
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
            <QuickActionButton
              icon={<SaveIcon fontSize="small" />}
              label="Save"
              onClick={() => {
                // TODO: integrate editor save handler when available.
              }}
              ariaLabel="Save changes"
            />
          )}
          <Tooltip title="Switch view">
            <IconButton onClick={onSwitchView} aria-label="Switch view">
              {viewIcon}
            </IconButton>
          </Tooltip>

          <Tooltip
            title={showHiddenFiles ? "Hide hidden files" : "Show hidden files"}
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
        </Stack>
      </Box>
    </Box>
  );
};

export default FileBrowserHeader;
