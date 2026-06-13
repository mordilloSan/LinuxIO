import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
} from "react";

import FileIcon from "@/components/filebrowser/FileIcon";
import AppTypography from "@/components/ui/AppTypography";
import { useFileDirectorySize } from "@/hooks/filebrowser/useFileDirectorySize";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

// Styles are injected by FileCard.tsx (shared animation)

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
  isSymlink?: boolean;
  modTime?: string;
  name: string;
  onCancelRename?: () => void;
  onClick: (event: React.MouseEvent) => void;
  onConfirmRename?: (newName: string) => void;
  onContextMenu?: (event: React.MouseEvent) => void;
  onDoubleClick?: () => void;
  path?: string;
  selected?: boolean;
  showFullPath?: boolean; // Show full directory path (for search results)
  size?: number;
  type: string;
}

const COLUMN_TEMPLATE =
  "minmax(0, 1fr) clamp(80px, 16vw, 140px) clamp(120px, 22vw, 200px)";

const FileListRow: React.FC<FileListRowProps> = React.memo(
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
  }) => {
    const theme = useAppTheme();
    const [renameValue, setRenameValue] = useState(name);
    const inputRef = useRef<HTMLInputElement>(null);

    const syncRenameValue = useEffectEvent(() => {
      setRenameValue(name);
    });

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
      if (isRenaming) {
        syncRenameValue();
      }
    }, [isRenaming, name, isDirectory]);

    const handleRenameKeyDown = useCallback(
      (e: React.KeyboardEvent<HTMLInputElement>) => {
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
      [renameValue, name, onConfirmRename, onCancelRename],
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
      isLoading: isFetchingSize,
      error: fetchError,
      isUnavailable: isSizeUnavailable,
    } = useFileDirectorySize(path || "", needsIndividualDirSize);

    // Override size props with fetched data when displaying search results
    const effectiveSize = needsIndividualDirSize ? (fetchedSize ?? size) : size;
    const effectiveSizeLoading = needsIndividualDirSize
      ? isFetchingSize
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
      (e: React.MouseEvent) => {
        e.stopPropagation();
        onClick(e);
      },
      [onClick],
    );

    const handleDoubleClick = useCallback(
      (e: React.MouseEvent) => {
        e.stopPropagation();
        onDoubleClick?.();
      },
      [onDoubleClick],
    );

    const baseBg = useMemo(() => {
      if (selected) {
        return `color-mix(in srgb, var(--app-palette-primary-main), transparent 60%)`;
      }
      if (hidden) {
        return `color-mix(in srgb, ${theme.fileBrowser.surface}, transparent 50%)`;
      }
      return theme.fileBrowser.surface;
    }, [hidden, selected, theme.fileBrowser.surface]);

    const resolvedBorderRadius = borderRadius ?? theme.shape.borderRadius;

    return (
      <div
        data-file-card="true"
        data-file-path={path}
        onClick={handleClick}
        onContextMenu={onContextMenu}
        onDoubleClick={handleDoubleClick}
        style={{
          display: "grid",
          gridTemplateColumns: COLUMN_TEMPLATE,
          alignItems: "center",
          backgroundColor: baseBg,
          cursor: "pointer",
          borderRadius: resolvedBorderRadius,
          userSelect: "none",
          opacity: isCut ? 0.5 : 1,
        }}
      >
        {/* Name and Icon */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1.5),
            padding: theme.spacing(1.5, 2),
            fontWeight: 500,
            fontSize: "0.9375rem",
            overflow: "hidden",
            color: theme.palette.text.primary,
            opacity: hidden ? 0.5 : undefined,
            minWidth: 0,
          }}
        >
          <div style={{ flexShrink: 0 }}>
            <FileIcon
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
                gap: theme.spacing(1),
                overflow: "hidden",
              }}
            >
              {isRenaming ? (
                <input
                  onBlur={handleRenameBlur}
                  onChange={(e) => setRenameValue(e.target.value)}
                  onClick={(e) => e.stopPropagation()}
                  onDoubleClick={(e) => e.stopPropagation()}
                  onKeyDown={handleRenameKeyDown}
                  ref={inputRef}
                  style={{
                    fontSize: "0.9375rem",
                    fontWeight: 500,
                    color: theme.palette.text.primary,
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
              {showFullPath && (
                <span
                  style={{
                    fontSize: "0.65rem",
                    fontWeight: 600,
                    color: isDirectory
                      ? theme.palette.primary.main
                      : theme.palette.text.secondary,
                    backgroundColor: isDirectory
                      ? `color-mix(in srgb, var(--app-palette-primary-main), transparent 85%)`
                      : `color-mix(in srgb, var(--app-palette-text-secondary), transparent 90%)`,
                    padding: "2px 6px",
                    borderRadius: "4px",
                    textTransform: "uppercase",
                    letterSpacing: "0.5px",
                    flexShrink: 0,
                  }}
                >
                  {isDirectory ? "Folder" : "File"}
                </span>
              )}
            </div>
            {showFullPath && path && (
              <AppTypography
                component="div"
                color="text.secondary"
                fontSize="0.75rem"
                noWrap
                style={{
                  opacity: 0.7,
                  marginTop: "2px",
                }}
                title={path}
                variant="body2"
              >
                {path.replace(/\/[^/]*$/, "") || "/"}
              </AppTypography>
            )}
          </div>
        </div>
        {/* Size */}
        <div
          style={{
            padding: theme.spacing(1.5, 2),
            fontSize: "0.875rem",
            color: theme.palette.text.secondary,
            opacity: hidden ? 0.5 : undefined,
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-start",
          }}
          title={effectiveSizeError?.message}
        >
          {effectiveSizeLoading ? (
            <span
              style={{
                animation: "sizeGlow 2.5s infinite",
              }}
            >
              —
            </span>
          ) : (
            formattedSize
          )}
        </div>
        {/* Modified Date */}
        <div
          style={{
            padding: theme.spacing(1.5, 2),
            fontSize: "0.875rem",
            color: theme.palette.text.secondary,
            opacity: hidden ? 0.5 : undefined,
            display: "flex",
            justifyContent: "center",
            textAlign: "center",
          }}
        >
          {formattedDate}
        </div>
      </div>
    );
  },
);

FileListRow.displayName = "FileListRow";

export default FileListRow;
