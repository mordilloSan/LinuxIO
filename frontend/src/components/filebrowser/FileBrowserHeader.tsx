import { Icon } from "@iconify/react";
import { memo, type ReactNode } from "react";

import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import FileBrowserHeaderActions from "./FileBrowserHeaderActions";
import IndexerDialog from "./IndexerDialog";
import type { ViewMode } from "../../types/filebrowser";

const noopSearchChange = () => {};

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
  onSearchChange = noopSearchChange,
}: FileBrowserHeaderProps) => {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const isBrowsing = !showQuickSave;

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
            <FileBrowserHeaderActions
              isMobile={isMobile}
              onSearchChange={onSearchChange}
              onSwitchView={onSwitchView}
              onToggleHiddenFiles={onToggleHiddenFiles}
              searchQuery={isMobile ? searchQuery : ""}
              showHiddenFiles={showHiddenFiles}
              viewMode={viewMode}
            />
          )}
        </div>
      </div>
      <IndexerDialog />
    </>
  );
};
export default memo(FileBrowserHeader);
