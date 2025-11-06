import React, { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import {
  Add as AddIcon,
  Save as SaveIcon,
  Visibility as VisibilityIcon,
  VisibilityOff as VisibilityOffIcon,
} from "@mui/icons-material";
import { Box, IconButton, Stack, useMediaQuery, useTheme, darken, lighten, Tooltip } from "@mui/material";
import { useConfigValue } from "@/hooks/useConfig";
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
  const [themePreference] = useConfigValue("theme");
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));

  const [showMobileQuickActions, setShowMobileQuickActions] = useState(false);
  const quickActionsRef = useRef<HTMLDivElement | null>(null);

  const isDarkMode =
    typeof themePreference === "string" &&
    themePreference.toUpperCase() === "DARK";

  const toggleMobileQuickActions = useCallback(() => {
    if (!isMobile) return;
    setShowMobileQuickActions((open) => !open);
  }, [isMobile]);

  useEffect(() => {
    if (!isMobile) {
      setShowMobileQuickActions(false);
    }
  }, [isMobile]);

  useEffect(() => {
    if (!showMobileQuickActions) return;

    const handleClick = (event: MouseEvent) => {
      const target = event.target as Node;
      if (!quickActionsRef.current?.contains(target)) {
        setShowMobileQuickActions(false);
      }
    };

    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setShowMobileQuickActions(false);
      }
    };

    document.addEventListener("click", handleClick, true);
    document.addEventListener("keydown", handleKeydown);
    return () => {
      document.removeEventListener("click", handleClick, true);
      document.removeEventListener("keydown", handleKeydown);
    };
  }, [showMobileQuickActions]);

  return (
    <Box
      sx={(theme) => ({
        display: "flex",
        px: 3,
        py: 1,
        minHeight: 64,
        backgroundColor:
          theme.palette.mode === "light"
            ? darken(theme.sidebar.background, 0.13)
            : lighten(theme.sidebar.background, 0.06),
        boxShadow: theme.shadows[2],
      })}
    >
      <Box
        className={`header-right quick-actions${isMobile ? " is-mobile" : ""}${showMobileQuickActions ? " open" : ""
          }`}
        ref={quickActionsRef}
        sx={{
          display: "flex",
          ml: "auto",
          position: isMobile ? "relative" : "static",
        }}
      >
        {isMobile && (
          <IconButton
            className="quick-toggle action mobile-toggle"
            onClick={toggleMobileQuickActions}
            aria-haspopup="true"
            aria-expanded={showMobileQuickActions ? "true" : "false"}
            aria-label="More actions"
            size="small"
            sx={{
              width: { xs: "3em", sm: "2.6em" },
              height: { xs: "3em", sm: "2.6em" },
              borderRadius: "50%",
              backgroundColor: isDarkMode
                ? "rgba(37, 49, 55, 0.33)"
                : "rgba(37, 49, 55, 0.12)",
            }}
          >
            <AddIcon fontSize="small" />
          </IconButton>
        )}

        <Stack
          direction={isMobile ? "column" : "row"}
          spacing={0.4}
          className="quick-actions-group"
          sx={{
            display: isMobile
              ? showMobileQuickActions
                ? "flex"
                : "none"
              : "flex",
            position: isMobile ? "absolute" : "static",
            top: isMobile ? "calc(100% + 0.5em)" : undefined,
            right: 0,
            flexDirection: isMobile ? "column" : "row",
            alignItems: "center",
            gap: isMobile ? "0.25em" : "0.4em",
            p: isMobile ? "0.4em" : 0,
            borderRadius: isMobile ? "0.75em" : 0,
            background: isMobile
              ? isDarkMode
                ? "rgba(32, 44, 50, 0.96)"
                : "rgba(25, 35, 41, 0.96)"
              : "transparent",
            boxShadow: isMobile
              ? "0 12px 24px rgba(17, 24, 28, 0.25)"
              : "none",
            zIndex: 6,
            minWidth: isMobile ? "unset" : undefined,
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

          <Tooltip title={showHiddenFiles ? "Hide hidden files" : "Show hidden files"}>
            <IconButton
              onClick={onToggleHiddenFiles}
              aria-label={showHiddenFiles ? "Hide hidden files" : "Show hidden files"}
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
