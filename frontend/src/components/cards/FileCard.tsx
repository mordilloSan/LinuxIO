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

import "@/components/filebrowser/file-listing.css";

import FileIcon from "@/components/filebrowser/FileIcon";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useFileDirectorySize } from "@/hooks/filebrowser/useFileDirectorySize";
import { CARD_PADDING_SM } from "@/theme/constants";
import {
  getFileEntryBackground,
  getSubtleDividerColor,
} from "@/theme/surfaces";
import { formatFileSize } from "@/utils/formaters";

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
    directorySizeUnavailable = false,
    onClick,
    onDoubleClick,
    onContextMenu,
    onConfirmRename,
    onCancelRename,
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

    const formattedDate = useMemo(() => {
      if (!modTime) return "";
      const date = new Date(modTime);
      return date.toLocaleDateString("en-GB");
    }, [modTime]);

    const baseBg = getFileEntryBackground({ hidden, selected });

    const baseBorderColor = getSubtleDividerColor();

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

    // Use CSS classes for hover - no React state updates during hover
    const className = `file-card hover-lift${disableHover ? " hover-lift--disabled" : ""}`;

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
          gap: "var(--app-space-6)",
          padding: CARD_PADDING_SM,
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
          className="hover-lift__icon"
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
                gap: "var(--app-space-4)",
              }}
            >
              <input
                className="file-card-rename-input"
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
                  color: "var(--app-palette-text-primary)",
                  flex: 1,
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
              fontWeight={400}
              noWrap
              style={{
                lineHeight: 1.2,
                opacity: 1,
              }}
              title={name}
              variant="body2"
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
                  noWrap
                  style={{
                    opacity: 0.7,
                    lineHeight: 1.2,
                    marginTop: "2px",
                  }}
                  title={path}
                  variant="caption"
                >
                  {path.replace(/\/[^/]*$/, "") || "/"}
                </AppTypography>
              )}
            </>
          )}

          {/* Size line (middle) */}
          <AppTypography
            component="div"
            color="text.secondary"
            style={{
              gap: "var(--app-space-2)",
              lineHeight: 1.2,
              opacity: metadataOpacity,
              display: "flex",
              alignItems: "center",
              height: "1.2em",
            }}
            title={effectiveSizeError?.message}
            variant="body2"
          >
            {effectiveSizeUnavailable ? (
              "Unavailable"
            ) : effectiveSizeLoading &&
              (effectiveSize === undefined || effectiveSize === 0) ? (
              <span className="file-size-pending">—</span>
            ) : effectiveSize !== undefined ? (
              formatFileSize(effectiveSize, 1, "")
            ) : (
              "—"
            )}
          </AppTypography>

          <AppTypography
            component="div"
            color="text.secondary"
            style={{
              gap: "var(--app-space-2)",
              lineHeight: 1.2,
              opacity: metadataOpacity,
            }}
            variant="body2"
          >
            {formattedDate}
          </AppTypography>
        </div>
      </div>
    );
  },
);

FileCard.displayName = "FileCard";

export default FileCard;
