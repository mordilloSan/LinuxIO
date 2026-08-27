import {
  memo,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent,
} from "react";

import "./file-listing.css";

import FileIcon from "@/components/filebrowser/FileIcon";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useFileDirectorySize } from "@/hooks/filebrowser/useFileDirectorySize";
import {
  getFileEntryBackground,
  getFileEntryHoverBackground,
  mixWithTransparency,
} from "@/theme/surfaces";
import { formatFileSize } from "@/utils/formaters";

export interface FileListRowProps {
  borderRadius?: number | string;
  directorySizeError?: Error | null;
  directorySizeLoading?: boolean;
  directorySizeUnavailable?: boolean;
  disableHover?: boolean;
  hidden?: boolean;
  isCut?: boolean;
  isDirectory: boolean;
  isRenaming?: boolean;
  isRenamePending?: boolean;
  isSymlink?: boolean;
  modTime?: string;
  name: string;
  onCancelRename?: () => void;
  onClick: (event: MouseEvent) => void;
  onConfirmRename?: (newName: string) => void;
  onContextMenu?: (event: MouseEvent) => void;
  onDoubleClick?: () => void;
  path?: string;
  renameProgressPct?: number;
  selected?: boolean;
  showFullPath?: boolean; // Show full directory path (for search results)
  size?: number;
  type: string;
}

const COLUMN_TEMPLATE =
  "minmax(0, 1fr) clamp(80px, 16vw, 140px) clamp(120px, 22vw, 200px)";
// The name cell contains a 24px icon whose SVG may mount after the row first
// renders. Reserve the resting row height so centered text does not move when
// that icon becomes available.
const FILE_LIST_ROW_MIN_HEIGHT = 40;

const FileListRow = memo<FileListRowProps>(
  ({
    name,
    path,
    size,
    modTime,
    isDirectory,
    isSymlink = false,
    selected = false,
    hidden = false,
    isCut = false,
    isRenaming = false,
    isRenamePending = false,
    renameProgressPct,
    showFullPath = false,
    directorySizeLoading = false,
    directorySizeError = null,
    directorySizeUnavailable = false,
    onClick,
    onDoubleClick,
    onContextMenu,
    onConfirmRename,
    onCancelRename,
    borderRadius,
    disableHover = false,
  }) => {
    const [renameValue, setRenameValue] = useState(name);
    const inputRef = useRef<HTMLInputElement>(null);

    // Reset the draft to the current name when entering rename mode — the
    // render-time adjustment pattern (react.dev "adjusting state when a prop
    // changes"), not a setState-in-effect.
    const [wasRenaming, setWasRenaming] = useState(isRenaming);
    if (isRenaming !== wasRenaming) {
      setWasRenaming(isRenaming);
      if (isRenaming) {
        setRenameValue(name);
      }
    }

    // Auto-focus and select text when entering rename mode
    useEffect(() => {
      if (isRenaming && inputRef.current) {
        inputRef.current.focus();
        // Select filename without extension for files, or full name for directories
        const dotIndex = isDirectory ? -1 : name.lastIndexOf(".");
        if (dotIndex > 0) {
          inputRef.current.setSelectionRange(0, dotIndex);
        } else {
          inputRef.current.select();
        }
      }
    }, [isRenaming, name, isDirectory]);

    const handleRenameKeyDown = useCallback(
      (e: KeyboardEvent<HTMLInputElement>) => {
        if (isRenamePending) return;
        if (e.key === "Enter") {
          e.preventDefault();
          const trimmed = renameValue.trim();
          if (trimmed && trimmed !== name && onConfirmRename) {
            onConfirmRename(trimmed);
          } else {
            onCancelRename?.();
          }
        } else if (e.key === "Escape") {
          e.preventDefault();
          onCancelRename?.();
        }
      },
      [isRenamePending, name, onCancelRename, onConfirmRename, renameValue],
    );

    const handleRenameBlur = useCallback(() => {
      const trimmed = renameValue.trim();
      if (trimmed && trimmed !== name && onConfirmRename) {
        onConfirmRename(trimmed);
      } else {
        onCancelRename?.();
      }
    }, [renameValue, name, onConfirmRename, onCancelRename]);

    // For search results (showFullPath=true), fetch individual directory sizes
    const needsIndividualDirSize = showFullPath && isDirectory && !isSymlink;
    const {
      size: fetchedSize,
      isLoading: isSizeLoading,
      error: fetchError,
      isUnavailable: isSizeUnavailable,
    } = useFileDirectorySize(path || "", needsIndividualDirSize);

    // Override size props with fetched data when displaying search results
    const effectiveSize = needsIndividualDirSize ? (fetchedSize ?? size) : size;
    const effectiveSizeLoading = needsIndividualDirSize
      ? isSizeLoading
      : directorySizeLoading;
    const effectiveSizeError = needsIndividualDirSize
      ? fetchError
      : directorySizeError;
    const effectiveSizeUnavailable = needsIndividualDirSize
      ? isSizeUnavailable
      : directorySizeUnavailable;

    const formattedDate = modTime
      ? new Date(modTime).toLocaleDateString("en-GB")
      : "";

    const formattedSize = useMemo(() => {
      if (
        effectiveSizeLoading &&
        effectiveSize !== undefined &&
        effectiveSize !== 0
      ) {
        return formatFileSize(effectiveSize, 1, "");
      }
      if (effectiveSizeLoading) {
        return ""; // Will render glow effect instead
      }
      if (effectiveSizeUnavailable) {
        return ""; // Will render warning icon instead
      }
      if (effectiveSize !== undefined && effectiveSize !== 0) {
        return formatFileSize(effectiveSize, 1, "");
      }
      return "—";
    }, [effectiveSize, effectiveSizeLoading, effectiveSizeUnavailable]);

    const handleClick = useCallback(
      (e: MouseEvent) => {
        e.stopPropagation();
        onClick(e);
      },
      [onClick],
    );

    const handleDoubleClick = useCallback(
      (e: MouseEvent) => {
        e.stopPropagation();
        onDoubleClick?.();
      },
      [onDoubleClick],
    );

    const baseBg = useMemo(
      () => getFileEntryBackground({ hidden, selected }),
      [hidden, selected],
    );

    const hoverBg = useMemo(
      () => getFileEntryHoverBackground(baseBg),
      [baseBg],
    );

    const resolvedBorderRadius = borderRadius ?? "var(--app-radius-base)";

    return (
      <div
        className={`file-row hover-lift${disableHover ? " hover-lift--disabled" : ""}`}
        data-file-card="true"
        data-file-path={path}
        onClick={handleClick}
        onContextMenu={onContextMenu}
        onDoubleClick={handleDoubleClick}
        style={
          {
            display: "grid",
            gridTemplateColumns: COLUMN_TEMPLATE,
            alignItems: "center",
            minHeight: FILE_LIST_ROW_MIN_HEIGHT,
            "--file-row-bg": baseBg,
            "--file-row-bg-hover": hoverBg,
            cursor: "pointer",
            borderRadius: resolvedBorderRadius,
            userSelect: "none",
            opacity: isCut ? 0.5 : 1,
          } as CSSProperties
        }
      >
        {/* Name and Icon */}
        <AppTypography
          color="text.primary"
          component="div"
          fontWeight={500}
          style={{
            display: "flex",
            alignItems: "center",
            gap: "var(--app-space-6)",
            padding: "var(--app-space-6) var(--app-space-8)",
            overflow: "hidden",
            opacity: hidden ? 0.5 : undefined,
            minWidth: 0,
            minHeight: FILE_LIST_ROW_MIN_HEIGHT,
          }}
          variant="body1"
        >
          <div style={{ flexShrink: 0 }}>
            <FileIcon
              className="hover-lift__icon"
              filename={name}
              hidden={hidden}
              isDirectory={isDirectory}
              isSymlink={isSymlink}
              size={24}
            />
          </div>
          <div
            style={{
              overflow: "hidden",
              minWidth: 0,
              flex: 1,
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: "var(--app-space-4)",
                overflow: "hidden",
              }}
            >
              {isRenaming ? (
                <input
                  className="file-row-rename-input"
                  disabled={isRenamePending}
                  onBlur={isRenamePending ? undefined : handleRenameBlur}
                  onChange={(e) => setRenameValue(e.target.value)}
                  onClick={(e) => e.stopPropagation()}
                  onDoubleClick={(e) => e.stopPropagation()}
                  onKeyDown={handleRenameKeyDown}
                  ref={inputRef}
                  style={{
                    color: "var(--app-palette-text-primary)",
                    background: "transparent",
                    border: "none",
                    padding: 0,
                    outline: "none",
                    flex: 1,
                    minWidth: 0,
                    boxSizing: "border-box",
                  }}
                  type="text"
                  value={renameValue}
                />
              ) : (
                <AppTypography
                  component="div"
                  noWrap
                  style={{
                    flex: 1,
                    minWidth: 0,
                  }}
                  title={name}
                  variant="body1"
                >
                  {name}
                </AppTypography>
              )}
              {isRenaming && isRenamePending && (
                <>
                  <AppCircularProgress aria-label="Renaming" size={16} />
                  {/* Only slow copy-fallback renames report a percentage. */}
                  {renameProgressPct !== undefined && (
                    <AppTypography color="text.secondary" variant="caption">
                      {renameProgressPct}%
                    </AppTypography>
                  )}
                </>
              )}
              {showFullPath && (
                <AppTypography
                  component="span"
                  style={{
                    fontWeight: 600,
                    color: isDirectory
                      ? "var(--app-palette-primary-main)"
                      : "var(--app-palette-text-secondary)",
                    backgroundColor: isDirectory
                      ? mixWithTransparency(
                          "var(--app-palette-primary-main)",
                          0.15,
                        )
                      : mixWithTransparency(
                          "var(--app-palette-text-secondary)",
                          0.1,
                        ),
                    padding: "2px 6px",
                    borderRadius: "4px",
                    letterSpacing: "0.5px",
                    flexShrink: 0,
                    textTransform: "uppercase",
                  }}
                  variant="caption"
                >
                  {isDirectory ? "Folder" : "File"}
                </AppTypography>
              )}
            </div>
            {showFullPath && path && (
              <AppTypography
                component="div"
                color="text.secondary"
                noWrap
                style={{
                  opacity: 0.7,
                  marginTop: "2px",
                }}
                title={path}
                variant="caption"
              >
                {path.replace(/\/[^/]*$/, "") || "/"}
              </AppTypography>
            )}
          </div>
        </AppTypography>
        {/* Size */}
        <AppTypography
          color="text.secondary"
          component="div"
          style={{
            padding: "var(--app-space-6) var(--app-space-8)",
            opacity: hidden ? 0.5 : undefined,
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-start",
          }}
          title={effectiveSizeError?.message}
          variant="body2"
        >
          {effectiveSizeLoading ? (
            <span className="file-size-pending">—</span>
          ) : (
            formattedSize
          )}
        </AppTypography>
        {/* Modified Date */}
        <AppTypography
          color="text.secondary"
          component="div"
          style={{
            padding: "var(--app-space-6) var(--app-space-8)",
            opacity: hidden ? 0.5 : undefined,
            display: "flex",
            justifyContent: "center",
            textAlign: "center",
          }}
          variant="body2"
        >
          {formattedDate}
        </AppTypography>
      </div>
    );
  },
);

FileListRow.displayName = "FileListRow";

export default FileListRow;
