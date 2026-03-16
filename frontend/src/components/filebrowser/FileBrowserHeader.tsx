import { Icon } from "@iconify/react";
import {
  CircularProgress,
  IconButton,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import React, { ReactNode, useCallback } from "react";

import IndexerDialog from "./IndexerDialog";
import SearchBar from "./SearchBar";
import { ViewMode } from "../../types/filebrowser";

import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
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
  const { isEnabled: indexerEnabled, reason: indexerReason } =
    useCapability("indexerAvailable");
  const { startIndexer, isIndexing, openIndexerDialog } = useFileTransfers();
  const handleIndexer = useCallback(() => {
    openIndexerDialog();
    void startIndexer({});
  }, [openIndexerDialog, startIndexer]);
  return (
    <>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          paddingInline: 12,
          minHeight: 64,
          backgroundColor:
            theme.palette.mode === "light"
              ? theme.darken(theme.sidebar.background, 0.13)
              : theme.lighten(theme.sidebar.background, 0.06),
          boxShadow: theme.shadows[2].replace(/;$/, ""),
        }}
      >
        {/* Left section - Status indicator when editing */}
        {showQuickSave && (
          <div
            style={{
              minWidth: 150,
              display: "flex",
              alignItems: "center",
              gap: 4,
            }}
          >
            {isDirty && (
              <AppTypography
                variant="caption"
                style={{
                  color: theme.palette.primary.main,
                  fontWeight: 600,
                  display: "flex",
                  alignItems: "center",
                  gap: 2,
                }}
              >
                • Unsaved changes
              </AppTypography>
            )}
          </div>
        )}
        {/* Center section - File info when editing OR search bar when browsing */}
        {showQuickSave && editingFileName ? (
          <div
            style={{
              flex: 1,
              textAlign: "center",
              marginInline: 8,
            }}
          >
            <AppTypography variant="h6" fontWeight={600}>
              {editingFileName}
            </AppTypography>
            <AppTypography variant="caption" color="text.secondary">
              {editingFilePath}
            </AppTypography>
          </div>
        ) : (
          <div
            style={{
              flex: 1,
              display: "flex",
              justifyContent: "center",
              marginInline: 8,
            }}
          >
            <SearchBar
              value={searchQuery}
              onChange={onSearchChange}
              placeholder="Search files and folders..."
            />
          </div>
        )}
        {/* Right section - Action buttons */}
        <div
          className={`header-right quick-actions${isMobile ? " is-mobile" : ""}`}
          style={{
            display: "flex",
            marginLeft: "auto",
          }}
        >
          <div
            className="quick-actions-group"
            style={{
              display: "flex",
              alignItems: "center",
              gap: "0.4em",
            }}
          >
            {showQuickSave && (
              <>
                <AppTooltip title="Close editor">
                  <IconButton
                    onClick={onCloseEditor || (() => {})}
                    disabled={isSaving}
                  >
                    <Icon icon="mdi:close" width={22} height={22} />
                  </IconButton>
                </AppTooltip>

                <AppTooltip title="Save changes">
                  <IconButton
                    onClick={onSaveFile || (() => {})}
                    disabled={isSaving}
                  >
                    <Icon icon="mdi:content-save" width={22} height={22} />
                  </IconButton>
                </AppTooltip>
              </>
            )}

            {!showQuickSave && (
              <>
                <AppTooltip title="Switch view">
                  <IconButton onClick={onSwitchView} aria-label="Switch view">
                    {viewIcon}
                  </IconButton>
                </AppTooltip>

                <AppTooltip
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
                      <Icon icon="mdi:eye" width={22} height={22} />
                    ) : (
                      <Icon icon="mdi:eye-off" width={22} height={22} />
                    )}
                  </IconButton>
                </AppTooltip>

                <AppTooltip
                  title={
                    isIndexing
                      ? "Indexing..."
                      : !indexerEnabled
                        ? indexerReason
                        : "Index filesystem"
                  }
                >
                  <span>
                    <IconButton
                      onClick={handleIndexer}
                      disabled={isIndexing || !indexerEnabled}
                      aria-label="Index filesystem"
                      sx={{
                        position: "relative",
                      }}
                    >
                      {isIndexing ? (
                        <CircularProgress size={24} />
                      ) : (
                        <Icon
                          icon="mdi:sync"
                          width={22}
                          height={22}
                          style={{
                            color: !indexerEnabled
                              ? theme.palette.text.disabled
                              : "inherit",
                          }}
                        />
                      )}
                    </IconButton>
                  </span>
                </AppTooltip>
              </>
            )}
          </div>
        </div>
      </div>
      <IndexerDialog />
    </>
  );
};
export default FileBrowserHeader;
