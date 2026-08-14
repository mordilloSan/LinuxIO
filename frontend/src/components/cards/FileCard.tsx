import {
  memo,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
} from "react";

import FileIcon from "@/components/filebrowser/FileIcon";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useFileDirectorySize } from "@/hooks/filebrowser/useFileDirectorySize";
import { useAppTheme } from "@/theme";
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

const FileCard = memo<FileCardProps>(
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
    onClick,
    onDoubleClick,
    onContextMenu,
    onConfirmRename,
    onCancelRename,
    disableHover = false,
  }) => {
    const theme = useAppTheme();
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
    } = useFileDirectorySize(path || "", needsIndividualDirSize);

    // Override size props with fetched data when displaying search results
    const effectiveSize = needsIndividualDirSize ? (fetchedSize ?? size) : size;
    const effectiveSizeLoading = needsIndividualDirSize
      ? isSizeLoading
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
        return `color-mix(in srgb, var(--app-palette-primary-main), transparent 60%)`;
      }
      if (hidden) {
        return `color-mix(in srgb, ${theme.fileBrowser.surface}, transparent 50%)`;
      }
      return theme.fileBrowser.surface;
    }, [hidden, selected, theme.fileBrowser.surface]);

    const baseBorderAlpha = theme.palette.mode === "dark" ? 0.15 : 0.1;

    const baseBorderColor = useMemo(
      () => `rgba(var(--app-palette-dividerChannel) / ${baseBorderAlpha})`,
      [baseBorderAlpha],
    );

    const borderColor = useMemo(() => {
      if (selected) {
        return `rgba(var(--app-palette-primary-mainChannel) / 0.7)`;
      }
      if (!isDirectory) return "transparent";

      // IMPORTANT: match old behavior: hidden overwrites alpha to 0.05
      return hidden
        ? `rgba(var(--app-palette-dividerChannel) / 0.05)`
        : baseBorderColor;
    }, [selected, isDirectory, hidden, baseBorderColor]);

    // Keep file and folder titles consistent while still dimming supporting text
    const metadataOpacity = isDirectory ? 0.85 : 0.65;

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

    // Use CSS class for hover - no React state updates during hover
    const className = `file-card-hover${disableHover ? " file-card-disable-hover" : ""}`;

    return (
      <div
        className={className}
        data-file-card="true"
        data-file-path={path}
        onClick={handleClick}
        onContextMenu={onContextMenu}
        onDoubleClick={handleDoubleClick}
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(1.5),
          padding: theme.spacing(1.5),
          border: "3px solid",
          borderColor: borderColor,
          borderRadius: 20,
          backgroundColor: baseBg,
          cursor: "pointer",
          minHeight: "60px",
          userSelect: "none",
          opacity: isCut ? 0.5 : 1,
        }}
      >
        <FileIcon
          filename={name}
          hidden={hidden}
          isDirectory={isDirectory}
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
          {isRenaming ? (
            <div
              style={{
                alignItems: "center",
                display: "flex",
                gap: theme.spacing(1),
              }}
            >
              <input
                disabled={isRenamePending}
                onBlur={isRenamePending ? undefined : handleRenameBlur}
                onChange={(e) => setRenameValue(e.target.value)}
                onClick={(e) => e.stopPropagation()}
                onDoubleClick={(e) => e.stopPropagation()}
                onKeyDown={handleRenameKeyDown}
                ref={inputRef}
                style={{
                  background: "transparent",
                  border: "none",
                  boxSizing: "border-box",
                  color: theme.palette.text.primary,
                  flex: 1,
                  fontSize: "0.90rem",
                  fontWeight: 400,
                  lineHeight: 1.2,
                  minWidth: 0,
                  outline: "none",
                  padding: 0,
                }}
                type="text"
                value={renameValue}
              />
              {isRenamePending && (
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
            </div>
          ) : (
            <AppTypography
              component="div"
              color="text.primary"
              fontSize="0.90rem"
              fontWeight={400}
              noWrap
              style={{
                lineHeight: 1.2,
                opacity: 1,
              }}
              title={name}
              variant="body1"
            >
              {name}
            </AppTypography>
          )}

          {/* Directory path and type badge for search results */}
          {showFullPath && (
            <>
              {path && (
                <AppTypography
                  component="div"
                  color="text.secondary"
                  fontSize="0.75rem"
                  noWrap
                  style={{
                    opacity: 0.7,
                    lineHeight: 1.2,
                    marginTop: "2px",
                  }}
                  title={path}
                  variant="body2"
                >
                  {path.replace(/\/[^/]*$/, "") || "/"}
                </AppTypography>
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
