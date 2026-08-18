import { Icon } from "@iconify/react";
import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu from "@/components/ui/AppMenu";
import AppPopover from "@/components/ui/AppPopover";
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
  const [mobileSearchAnchorEl, setMobileSearchAnchorEl] =
    useState<HTMLElement | null>(null);
  const mobileSearchRef = useRef<HTMLDivElement | null>(null);
  const { isEnabled: indexerEnabled, reason: indexerReason } =
    useCapability("indexerAvailable");
  const { startIndexer, openIndexerDialog } = useBackgroundTaskActions();
  const isIndexing = useIsIndexing();
  const isBrowsing = !showQuickSave;
  const handleIndexer = useCallback(() => {
    setActionsAnchorEl(null);
    openIndexerDialog();
    void startIndexer({});
  }, [openIndexerDialog, startIndexer]);
  const handleActionsTriggerRef = useCallback(
    (element: HTMLButtonElement | null) => {
      if (!element) {
        setActionsAnchorEl(null);
        setMobileSearchAnchorEl(null);
      }
    },
    [],
  );
  useLayoutEffect(() => {
    if (!mobileSearchAnchorEl) return;

    mobileSearchRef.current
      ?.querySelector<HTMLInputElement | HTMLTextAreaElement>("input, textarea")
      ?.focus();
  }, [mobileSearchAnchorEl]);
  const handleMobileSearchClose = useCallback(() => {
    const focusedElement = document.activeElement;
    const trigger = mobileSearchAnchorEl;

    setMobileSearchAnchorEl(null);
    if (trigger && mobileSearchRef.current?.contains(focusedElement)) {
      trigger.focus();
    }
  }, [mobileSearchAnchorEl]);
  return (
    <>
      <div
        className="file-browser-header"
        style={{
          display: isBrowsing ? "grid" : "flex",
          // The desktop columns mirror .tab-selector (tab-selector.css), so
          // the search sits at the same screen-centered spot here as on tab
          // routes. Mobile collapses the field into the actions menu, matching
          // the routed tab headers.
          gridTemplateColumns: isBrowsing
            ? isMobile
              ? "minmax(0, 1fr) auto"
              : "minmax(0, 1fr) clamp(140px, 40vw, 400px) minmax(0, 1fr)"
            : undefined,
          gap: isBrowsing ? 6 : undefined,
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
              className="file-browser-header__breadcrumbs"
              style={{
                gridColumn: isBrowsing ? 1 : undefined,
                minWidth: 0,
                display: "flex",
                alignItems: "center",
                overflow: "hidden",
              }}
            >
              {breadcrumbs}
            </div>
            {!isMobile ? (
              <div
                className="file-browser-header__search"
                style={{
                  flex: 1,
                  gridColumn: isBrowsing ? 2 : undefined,
                  minWidth: 0,
                  display: "flex",
                  justifyContent: "center",
                  marginInline: isBrowsing ? 0 : 8,
                }}
              >
                <AppHeaderSearch
                  onChange={onSearchChange}
                  placeholder="Search files and folders..."
                  value={searchQuery}
                />
              </div>
            ) : null}
          </>
        )}
        {/* Right section - Action buttons */}
        <div
          className="file-browser-header__actions"
          style={{
            display: "flex",
            alignItems: "center",
            gridColumn: isBrowsing ? (isMobile ? 2 : 3) : undefined,
            justifySelf: isBrowsing ? "end" : undefined,
            marginLeft: isBrowsing ? 0 : "auto",
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
                    aria-expanded={Boolean(actionsAnchorEl)}
                    aria-label="Actions"
                    color={searchQuery ? "primary" : "default"}
                    onClick={(event) => {
                      setMobileSearchAnchorEl(null);
                      setActionsAnchorEl(event.currentTarget);
                    }}
                    ref={handleActionsTriggerRef}
                    size="small"
                  >
                    <Icon height={20} icon="mdi:tune" width={20} />
                  </AppIconButton>
                  <AppMenu
                    anchorEl={actionsAnchorEl}
                    anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
                    minWidth={176}
                    onClose={() => setActionsAnchorEl(null)}
                    open={Boolean(actionsAnchorEl)}
                    transformOrigin={{ vertical: "top", horizontal: "right" }}
                  >
                    <div
                      className="file-browser-header__mobile-actions"
                      style={{
                        display: "flex",
                        flexWrap: "nowrap",
                        gap: 8,
                        padding: "4px 8px",
                      }}
                    >
                      <AppIconButton
                        aria-label="Search"
                        color={searchQuery ? "primary" : "default"}
                        onClick={() => {
                          setMobileSearchAnchorEl(actionsAnchorEl);
                          setActionsAnchorEl(null);
                        }}
                        size="small"
                      >
                        <Icon height={20} icon="mdi:magnify" width={20} />
                      </AppIconButton>
                      <ViewModeToggle
                        alternateMode="list"
                        onViewModeChange={() => {
                          setActionsAnchorEl(null);
                          onSwitchView();
                        }}
                        viewMode={viewMode}
                      />
                      <AppActionIconButton
                        icon={showHiddenFiles ? "mdi:eye" : "mdi:eye-off"}
                        iconSize={20}
                        label={
                          showHiddenFiles
                            ? "Hide hidden files"
                            : "Show hidden files"
                        }
                        onClick={() => {
                          setActionsAnchorEl(null);
                          onToggleHiddenFiles();
                        }}
                      />
                      <AppActionIconButton
                        ariaLabel="Index filesystem"
                        disabled={isIndexing || !indexerEnabled}
                        icon="mdi:sync"
                        iconSize={20}
                        label={
                          isIndexing
                            ? "Indexing..."
                            : !indexerEnabled
                              ? indexerReason
                              : "Index filesystem"
                        }
                        loading={isIndexing}
                        onClick={handleIndexer}
                      />
                    </div>
                  </AppMenu>
                  <AppPopover
                    anchorEl={mobileSearchAnchorEl}
                    anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
                    keepMounted
                    onClose={handleMobileSearchClose}
                    open={Boolean(mobileSearchAnchorEl)}
                    paperStyle={{
                      width: "min(400px, calc(100vw - 16px))",
                      padding: 8,
                    }}
                    transformOrigin={{ vertical: "top", horizontal: "right" }}
                  >
                    <div
                      className="file-browser-header__mobile-search"
                      ref={mobileSearchRef}
                      role="search"
                      style={{ width: "100%" }}
                    >
                      <AppHeaderSearch
                        onChange={onSearchChange}
                        placeholder="Search files and folders..."
                        value={searchQuery}
                      />
                    </div>
                  </AppPopover>
                </>
              ) : (
                <>
                  <ViewModeToggle
                    alternateMode="list"
                    onViewModeChange={onSwitchView}
                    viewMode={viewMode}
                  />
                  <AppActionIconButton
                    icon={showHiddenFiles ? "mdi:eye" : "mdi:eye-off"}
                    iconSize={20}
                    label={
                      showHiddenFiles
                        ? "Hide hidden files"
                        : "Show hidden files"
                    }
                    onClick={onToggleHiddenFiles}
                  />
                  <AppActionIconButton
                    ariaLabel="Index filesystem"
                    disabled={isIndexing || !indexerEnabled}
                    icon="mdi:sync"
                    iconSize={20}
                    label={
                      isIndexing
                        ? "Indexing..."
                        : !indexerEnabled
                          ? indexerReason
                          : "Index filesystem"
                    }
                    loading={isIndexing}
                    onClick={handleIndexer}
                  />
                </>
              )}
            </>
          )}
        </div>
      </div>
      <IndexerDialog />
    </>
  );
};
export default FileBrowserHeader;
