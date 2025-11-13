import { alpha, useTheme } from "@mui/material/styles";
import React, { useState, useCallback, useMemo } from "react";

import FileIcon from "@/components/filebrowser/FileIcon";
import { formatFileSize } from "@/utils/formaters";

const glowAnimation = `
  @keyframes sizeGlow {
    0% {
      opacity: 0.5;
    }
    25% {
      opacity: 0.7;
    }
    50% {
      opacity: 1;
    }
    75% {
      opacity: 0.7;
    }
    100% {
      opacity: 0.5;
    }
  }
`;

// Inject styles
if (
  typeof document !== "undefined" &&
  !document.getElementById("sizeGlowStyles")
) {
  const style = document.createElement("style");
  style.id = "sizeGlowStyles";
  style.textContent = glowAnimation;
  document.head.appendChild(style);
}

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
  directorySizeLoading?: boolean;
  directorySizeError?: Error | null;
  directorySizeUnavailable?: boolean;
  onClick: (event: React.MouseEvent) => void;
  onDoubleClick?: () => void;
  onContextMenu?: (event: React.MouseEvent) => void;
  borderRadius?: number | string;
}

const COLUMN_TEMPLATE =
  "minmax(0, 1fr) clamp(80px, 16vw, 140px) clamp(120px, 22vw, 200px)";

const FileListRow: React.FC<FileListRowProps> = React.memo(
  ({
    name,
    size,
    modTime,
    isDirectory,
    isSymlink = false,
    selected = false,
    hidden = false,
    directorySizeLoading = false,
    directorySizeError = null,
    directorySizeUnavailable = false,
    onClick,
    onDoubleClick,
    onContextMenu,
    borderRadius,
  }) => {
    const theme = useTheme();
    const [hovered, setHovered] = useState(false);

    const formattedDate = modTime
      ? new Date(modTime).toLocaleDateString("en-GB")
      : "";

    const formattedSize = useMemo(() => {
      if (directorySizeLoading) {
        return ""; // Will render glow effect instead
      }
      if (directorySizeUnavailable) {
        return ""; // Will render warning icon instead
      }
      if (size !== undefined && size !== 0) {
        return formatFileSize(size, 1, "");
      }
      return "—";
    }, [size, directorySizeLoading, directorySizeUnavailable]);

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
        return alpha(theme.palette.primary.main, 0.4);
      }
      const bg = theme.palette.mode === "dark" ? "#20292f" : "#ffffff";
      return hidden ? alpha(bg, 0.5) : bg;
    }, [selected, theme, hidden]);

    const hoverBg = useMemo(() => {
      if (selected) {
        return alpha(theme.palette.primary.main, 0.4);
      }
      const bg = theme.palette.mode === "dark" ? "#42505e" : "#f5f5f5";
      return hidden ? alpha(bg, 0.5) : bg;
    }, [selected, theme, hidden]);

    const bgColor = hovered ? hoverBg : baseBg;

    const resolvedBorderRadius = borderRadius ?? theme.shape.borderRadius;

    return (
      <div
        data-file-card="true"
        onClick={handleClick}
        onDoubleClick={handleDoubleClick}
        onContextMenu={onContextMenu}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          display: "grid",
          gridTemplateColumns: COLUMN_TEMPLATE,
          alignItems: "center",

          backgroundColor: bgColor,
          cursor: "pointer",
          transition: "background-color 0.15s ease",
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
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
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
          <span
            style={{
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {name}
          </span>
        </div>

        {/* Size */}
        <div
          style={{
            padding: theme.spacing(1.5, 2),
            fontSize: "0.875rem",
            color: directorySizeUnavailable
              ? theme.palette.error.main
              : theme.palette.text.secondary,
            opacity: hidden ? 0.5 : undefined,
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-start",
          }}
          title={directorySizeError?.message}
        >
          {directorySizeLoading ? (
            <span
              style={{
                animation: "sizeGlow 2.5s infinite",
              }}
            >
              —
            </span>
          ) : directorySizeUnavailable ? (
            <span>⚠</span>
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
