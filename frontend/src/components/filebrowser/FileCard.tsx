import { useTheme } from "@mui/material/styles";
import React, { useMemo, useCallback } from "react";

import FileIcon from "@/components/filebrowser/FileIcon";
import { useFileDirectorySize } from "@/hooks/useFileDirectorySize";
import { formatFileSize } from "@/utils/formaters";

const fileCardStyles = `
  @keyframes sizeGlow {
    0% { opacity: 0.5; }
    25% { opacity: 0.7; }
    50% { opacity: 1; }
    75% { opacity: 0.7; }
    100% { opacity: 0.5; }
  }

  .file-card-hover:not(.file-card-disable-hover):hover {
    transform: translateY(-2px) scale(1.01);
  }
`;

// Inject styles
if (
  typeof document !== "undefined" &&
  !document.getElementById("fileCardStyles")
) {
  const style = document.createElement("style");
  style.id = "fileCardStyles";
  style.textContent = fileCardStyles;
  document.head.appendChild(style);
}

export interface FileCardProps {
  name: string;
  type: string;
  path?: string;
  size?: number;
  modTime?: string;
  isDirectory: boolean;
  isSymlink?: boolean;
  selected?: boolean;
  hidden?: boolean;
  directorySizeLoading?: boolean;
  directorySizeError?: Error | null;
  directorySizeUnavailable?: boolean;
  showFullPath?: boolean; // Show full directory path (for search results)
  onClick: (event: React.MouseEvent) => void;
  onDoubleClick?: () => void;
  onContextMenu?: (event: React.MouseEvent) => void;
  disableHover?: boolean;
}

const FileCard: React.FC<FileCardProps> = React.memo(
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
    onClick,
    onDoubleClick,
    onContextMenu,
    disableHover = false,
  }) => {
    const theme = useTheme();

    // For search results (showFullPath=true), fetch individual directory sizes
    const needsIndividualDirSize = showFullPath && isDirectory && !isSymlink;
    const {
      size: fetchedSize,
      isLoading: isFetchingSize,
      error: fetchError,
    } = useFileDirectorySize(path || "", needsIndividualDirSize);

    // Override size props with fetched data when displaying search results
    const effectiveSize = needsIndividualDirSize ? (fetchedSize ?? size) : size;
    const effectiveSizeLoading = needsIndividualDirSize
      ? isFetchingSize
      : directorySizeLoading;
    const effectiveSizeError = needsIndividualDirSize
      ? fetchError
      : directorySizeError;

    const formattedDate = useMemo(() => {
      if (!modTime) return "";
      const date = new Date(modTime);
      return date.toLocaleDateString("en-GB");
    }, [modTime]);

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

    const baseBorderAlpha = theme.palette.mode === "dark" ? 0.15 : 0.1;

    const baseBorderColor = useMemo(
      () => `rgba(var(--mui-palette-dividerChannel) / ${baseBorderAlpha})`,
      [baseBorderAlpha],
    );

    const borderColor = useMemo(() => {
      if (selected) {
        return `rgba(var(--mui-palette-primary-mainChannel) / 0.7)`;
      }
      if (!isDirectory) return "transparent";

      // IMPORTANT: match old behavior: hidden overwrites alpha to 0.05
      return hidden
        ? `rgba(var(--mui-palette-dividerChannel) / 0.05)`
        : baseBorderColor;
    }, [selected, isDirectory, hidden, baseBorderColor]);

    // Keep file and folder titles consistent while still dimming supporting text
    const metadataOpacity = isDirectory ? 0.85 : 0.65;

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

    // Use CSS class for hover - no React state updates during hover
    const className = `file-card-hover${disableHover ? " file-card-disable-hover" : ""}`;

    return (
      <div
        data-file-card="true"
        data-file-path={path}
        className={className}
        onClick={handleClick}
        onDoubleClick={handleDoubleClick}
        onContextMenu={onContextMenu}
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(1.5),
          padding: theme.spacing(1.5),
          border: selected ? "2px solid" : "3px solid",
          borderColor: borderColor,
          borderRadius: 20,
          backgroundColor: baseBg,
          cursor: "pointer",
          minHeight: "60px",
          userSelect: "none",
        }}
      >
        <FileIcon
          isDirectory={isDirectory}
          filename={name}
          hidden={hidden}
          isSymlink={isSymlink}
        />
        <div
          style={{
            flex: 1,
            minWidth: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          <div
            style={{
              fontWeight: 400,
              fontSize: "0.90rem",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              color: theme.palette.text.primary,
              lineHeight: 1.2,
              opacity: 1,
            }}
          >
            {name}
          </div>

          {/* Directory path and type badge for search results */}
          {showFullPath && (
            <>
              {path && (
                <div
                  style={{
                    fontSize: "0.75rem",
                    color: theme.palette.text.secondary,
                    opacity: 0.7,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                    lineHeight: 1.2,
                    marginTop: "2px",
                  }}
                  title={path}
                >
                  {path.replace(/\/[^/]*$/, "") || "/"}
                </div>
              )}
            </>
          )}

          {/* Size line (middle) */}
          <div
            style={{
              fontSize: "0.90rem",
              color: theme.palette.text.secondary,
              gap: theme.spacing(0.5),
              lineHeight: 1.2,
              opacity: metadataOpacity,
              display: "flex",
              alignItems: "center",
              height: "1.2em",
            }}
            title={effectiveSizeError?.message}
          >
            {effectiveSizeLoading &&
            (effectiveSize === undefined || effectiveSize === 0) ? (
              <span
                style={{
                  animation: "sizeGlow 2.5s infinite",
                }}
              >
                —
              </span>
            ) : effectiveSize !== undefined && effectiveSize !== 0 ? (
              formatFileSize(effectiveSize, 1, "")
            ) : (
              "—"
            )}
          </div>

          <div
            style={{
              fontSize: "0.90rem",
              color: theme.palette.text.secondary,
              gap: theme.spacing(0.5),
              lineHeight: 1.2,
              opacity: metadataOpacity,
            }}
          >
            {formattedDate}
          </div>
        </div>
      </div>
    );
  },
);

FileCard.displayName = "FileCard";

export default FileCard;
