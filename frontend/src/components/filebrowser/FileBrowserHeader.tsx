import { Icon } from "@iconify/react";
import { useCallback, useState, type ReactNode } from "react";

import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import { useIsIndexing } from "@/hooks/backgroundTasks/useIsIndexing";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import IndexerDialog from "./IndexerDialog";
import type { ViewMode } from "../../types/filebrowser";

interface FileBrowserHeaderProps {
  /** Leading slot for the browsing view — the breadcrumb trail. */
  breadcrumbs?: ReactNode;
  editingFileName?: string;
  editingFilePath?: string;
  isDirty?: boolean;
  isSaving?: boolean;
  onCloseEditor?: () => void;
  onSaveFile?: () => Promise<void>;
  onSearchChange?: (value: string) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery?: string;
  showHiddenFiles: boolean;
  showQuickSave?: boolean;
  viewMode: ViewMode;
}
const FileBrowserHeader = ({
  showHiddenFiles,
  showQuickSave = false,
  onSwitchView,
  onToggleHiddenFiles,
  onSaveFile,
  onCloseEditor,
  isSaving = false,
  viewMode,
  breadcrumbs,
  editingFileName,
  editingFilePath,
  isDirty = false,
  searchQuery = "",
  onSearchChange = () => {},
}: FileBrowserHeaderProps) => {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const [actionsAnchorEl, setActionsAnchorEl] = useState<HTMLElement | null>(
    null,
  );
  const { isEnabled: indexerEnabled, reason: indexerReason } =
    useCapability("indexerAvailable");
  const { startIndexer, openIndexerDialog } = useBackgroundTaskActions();
  const isIndexing = useIsIndexing();
  const handleIndexer = useCallback(() => {
    setActionsAnchorEl(null);
    openIndexerDialog();
    void startIndexer({});
  }, [openIndexerDialog, startIndexer]);
  return (
    <>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          paddingInline: theme.spacing(2),
          minHeight: 64,
        }}
      >
        {/* Left section - Status indicator when editing */}
        {showQuickSave && (
          <div
            style={{
              minWidth: isMobile ? 0 : 150,
              display: "flex",
              alignItems: "center",
              gap: 4,
              overflow: "hidden",
            }}
          >
            {isDirty && (
              <AppTypography
                style={{
                  color: theme.palette.primary.main,
                  fontWeight: 600,
                  display: "flex",
                  alignItems: "center",
                  gap: 2,
                }}
                variant="caption"
              >
                • Unsaved changes
              </AppTypography>
            )}
          </div>
        )}
        {/* Center section - File info when editing OR breadcrumbs + search when browsing */}
        {showQuickSave && editingFileName ? (
          <div
            style={{
              flex: 1,
              textAlign: "center",
              marginInline: 8,
            }}
          >
            <AppTypography fontWeight={600} variant="h6">
              {editingFileName}
            </AppTypography>
            <AppTypography color="text.secondary" variant="caption">
              {editingFilePath}
            </AppTypography>
          </div>
        ) : (
          <>
            <div
              style={{
                minWidth: 0,
                display: "flex",
                alignItems: "center",
                overflow: "hidden",
              }}
            >
              {breadcrumbs}
            </div>
            <div
              style={{
                flex: 1,
                minWidth: 0,
                display: "flex",
                justifyContent: "center",
                marginInline: 8,
              }}
            >
              <div style={{ width: "100%", maxWidth: 420 }}>
                <AppHeaderSearch
                  onChange={onSearchChange}
                  placeholder={
                    isMobile ? "Search..." : "Search files and folders..."
                  }
                  value={searchQuery}
                />
              </div>
            </div>
          </>
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
                  <AppIconButton
                    aria-label="Close editor"
                    disabled={isSaving || !onCloseEditor}
                    onClick={onCloseEditor}
                  >
                    <Icon height={22} icon="mdi:close" width={22} />
                  </AppIconButton>
                </AppTooltip>

                <AppTooltip title="Save changes">
                  <AppIconButton
                    aria-label="Save changes"
                    disabled={isSaving || !onSaveFile}
                    onClick={onSaveFile}
                  >
                    <Icon height={22} icon="mdi:content-save" width={22} />
                  </AppIconButton>
                </AppTooltip>
              </>
            )}

            {!showQuickSave && (
              <>
                {isMobile ? (
                  <>
                    <AppIconButton
                      aria-label="Actions"
                      onClick={(e) => setActionsAnchorEl(e.currentTarget)}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:tune" width={20} />
                    </AppIconButton>
                    <AppMenu
                      anchorEl={actionsAnchorEl}
                      anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
                      minWidth="unset"
                      onClose={() => setActionsAnchorEl(null)}
                      open={Boolean(actionsAnchorEl)}
                      transformOrigin={{ vertical: "top", horizontal: "right" }}
                    >
                      <div
                        style={{ display: "flex", gap: 8, padding: "4px 8px" }}
                      >
                        <ViewModeToggle
                          alternateMode="list"
                          onViewModeChange={() => {
                            setActionsAnchorEl(null);
                            onSwitchView();
                          }}
                          viewMode={viewMode}
                        />
                        <AppTooltip
                          title={
                            showHiddenFiles
                              ? "Hide hidden files"
                              : "Show hidden files"
                          }
                        >
                          <AppIconButton
                            aria-label={
                              showHiddenFiles
                                ? "Hide hidden files"
                                : "Show hidden files"
                            }
                            onClick={() => {
                              setActionsAnchorEl(null);
                              onToggleHiddenFiles();
                            }}
                          >
                            {showHiddenFiles ? (
                              <Icon height={22} icon="mdi:eye" width={22} />
                            ) : (
                              <Icon height={22} icon="mdi:eye-off" width={22} />
                            )}
                          </AppIconButton>
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
                            <AppIconButton
                              aria-label="Index filesystem"
                              disabled={isIndexing || !indexerEnabled}
                              onClick={handleIndexer}
                            >
                              {isIndexing ? (
                                <AppCircularProgress size={24} />
                              ) : (
                                <Icon
                                  height={22}
                                  icon="mdi:sync"
                                  style={{
                                    color: !indexerEnabled
                                      ? theme.palette.text.disabled
                                      : "inherit",
                                  }}
                                  width={22}
                                />
                              )}
                            </AppIconButton>
                          </span>
                        </AppTooltip>
                      </div>
                    </AppMenu>
                  </>
                ) : (
                  <>
                    <ViewModeToggle
                      alternateMode="list"
                      onViewModeChange={onSwitchView}
                      viewMode={viewMode}
                    />
                    <AppTooltip
                      title={
                        showHiddenFiles
                          ? "Hide hidden files"
                          : "Show hidden files"
                      }
                    >
                      <AppIconButton
                        aria-label={
                          showHiddenFiles
                            ? "Hide hidden files"
                            : "Show hidden files"
                        }
                        onClick={onToggleHiddenFiles}
                      >
                        {showHiddenFiles ? (
                          <Icon height={22} icon="mdi:eye" width={22} />
                        ) : (
                          <Icon height={22} icon="mdi:eye-off" width={22} />
                        )}
                      </AppIconButton>
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
                        <AppIconButton
                          aria-label="Index filesystem"
                          disabled={isIndexing || !indexerEnabled}
                          onClick={handleIndexer}
                          style={{ position: "relative" }}
                        >
                          {isIndexing ? (
                            <AppCircularProgress size={24} />
                          ) : (
                            <Icon
                              height={22}
                              icon="mdi:sync"
                              style={{
                                color: !indexerEnabled
                                  ? theme.palette.text.disabled
                                  : "inherit",
                              }}
                              width={22}
                            />
                          )}
                        </AppIconButton>
                      </span>
                    </AppTooltip>
                  </>
                )}
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
