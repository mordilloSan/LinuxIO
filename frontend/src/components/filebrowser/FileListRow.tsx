import { useTheme } from "@mui/material/styles";
import React, { useCallback, useMemo } from "react";

import FileIcon from "@/components/filebrowser/FileIcon";
import { useFileDirectorySize } from "@/hooks/useFileDirectorySize";
import { formatFileSize } from "@/utils/formaters";

// Styles are injected by FileCard.tsx (shared animation)

export interface FileListRowProps {
  name: string;
  type: string;
  path?: string;
  size?: number;
  modTime?: string;
  isDirectory: boolean;
  isSymlink?: boolean;
  selected?: boolean;
  hidden?: boolean;
  showFullPath?: boolean; // Show full directory path (for search results)
  directorySizeLoading?: boolean;
  directorySizeError?: Error | null;
  directorySizeUnavailable?: boolean;
  onClick: (event: React.MouseEvent) => void;
  onDoubleClick?: () => void;
  onContextMenu?: (event: React.MouseEvent) => void;
  borderRadius?: number | string;
  disableHover?: boolean;
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
    showFullPath = false,
    directorySizeLoading = false,
    directorySizeError = null,
    directorySizeUnavailable = false,
    onClick,
    onDoubleClick,
    onContextMenu,
    borderRadius,
  }) => {
    const theme = useTheme();

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
        return `color-mix(in srgb, var(--mui-palette-primary-main), transparent 60%)`;
      }
      if (hidden) {
        return theme.palette.mode === "dark"
          ? `color-mix(in srgb, #20292f, transparent 50%)`
          : `color-mix(in srgb, #ffffff, transparent 50%)`;
      }
      return theme.palette.mode === "dark" ? "#20292f" : "#ffffff";
    }, [selected, theme.palette.mode, hidden]);

    const resolvedBorderRadius = borderRadius ?? theme.shape.borderRadius;

    return (
      <div
        data-file-card="true"
        data-file-path={path}
        onClick={handleClick}
        onDoubleClick={handleDoubleClick}
        onContextMenu={onContextMenu}
        style={{
          display: "grid",
          gridTemplateColumns: COLUMN_TEMPLATE,
          alignItems: "center",
          backgroundColor: baseBg,
          cursor: "pointer",
          borderRadius: resolvedBorderRadius,
          userSelect: "none",
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
              isDirectory={isDirectory}
              filename={name}
              hidden={hidden}
              size={24}
              isSymlink={isSymlink}
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
              <div
                style={{
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                  flex: 1,
                  minWidth: 0,
                }}
              >
                {name}
              </div>
              {showFullPath && (
                <span
                  style={{
                    fontSize: "0.65rem",
                    fontWeight: 600,
                    color: isDirectory
                      ? theme.palette.primary.main
                      : theme.palette.text.secondary,
                    backgroundColor: isDirectory
                      ? `color-mix(in srgb, var(--mui-palette-primary-main), transparent 85%)`
                      : `color-mix(in srgb, var(--mui-palette-text-secondary), transparent 90%)`,
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
              <div
                style={{
                  fontSize: "0.75rem",
                  color: theme.palette.text.secondary,
                  opacity: 0.7,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                  marginTop: "2px",
                }}
                title={path}
              >
                {path.replace(/\/[^/]*$/, "") || "/"}
              </div>
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
