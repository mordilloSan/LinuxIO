import { CircularProgress } from "@mui/material";
import { alpha, useTheme } from "@mui/material/styles";
import React, { useMemo, useState, useCallback } from "react";

import FileIcon from "@/components/filebrowser/FileIcon";
import { formatFileSize } from "@/utils/formaters";

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
  onClick: (event: React.MouseEvent) => void;
  onDoubleClick?: () => void;
  onContextMenu?: (event: React.MouseEvent) => void;
}

const FileCard: React.FC<FileCardProps> = React.memo(
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
  }) => {
    const theme = useTheme();
    const [hovered, setHovered] = useState(false);

    const formattedDate = useMemo(() => {
      if (!modTime) return "";
      const date = new Date(modTime);
      return date.toLocaleDateString("en-GB");
    }, [modTime]);

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
      const bg = theme.palette.mode === "dark" ? "#2a3540" : "#f5f5f5";
      return hidden ? alpha(bg, 0.5) : bg;
    }, [selected, theme, hidden]);

    const baseBorderColor = alpha(
      theme.palette.divider,
      theme.palette.mode === "dark" ? 0.15 : 0.1,
    );

    const borderColor = useMemo(() => {
      if (selected) {
        return alpha(theme.palette.primary.main, 0.7);
      }
      if (!isDirectory) {
        return "transparent";
      }
      return hidden ? alpha(baseBorderColor, 0.05) : baseBorderColor;
    }, [selected, isDirectory, baseBorderColor, hidden]);

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

    return (
      <div
        data-file-card="true"
        onClick={handleClick}
        onDoubleClick={handleDoubleClick}
        onContextMenu={onContextMenu}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(1.5),
          padding: theme.spacing(1.5),
          border: selected ? "2px solid" : "3px solid",
          borderColor: borderColor,
          borderRadius: 20,
          backgroundColor: hovered ? hoverBg : baseBg,
          cursor: "pointer",
          minHeight: "60px",
          transition: "all 120ms ease",
          transform: hovered ? "translateY(-2px) scale(1.01)" : "none",
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

          {/* Size line (middle) */}
          <div
            style={{
              fontSize: "0.90rem",
              color: directorySizeUnavailable
                ? theme.palette.error.main
                : theme.palette.text.secondary,
              gap: theme.spacing(0.5),
              lineHeight: 1.2,
              opacity: metadataOpacity,
              display: "flex",
              alignItems: "center",
              height: "1.2em",
            }}
            title={directorySizeError?.message}
          >
            {directorySizeLoading ? (
              <CircularProgress size={14} thickness={3} />
            ) : directorySizeUnavailable ? (
              <span style={{ fontSize: "0.85rem" }}>⚠</span>
            ) : size !== undefined ? (
              formatFileSize(size, 1, "")
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
