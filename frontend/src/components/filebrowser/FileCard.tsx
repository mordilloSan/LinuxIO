import React, { useMemo, useState, useCallback } from "react";
import { alpha, useTheme } from "@mui/material/styles";
import LinkIcon from "@mui/icons-material/Link";
import FileIcon from "@/components/filebrowser/FileIcon";

export interface FileCardProps {
  name: string;
  type: string;
  size?: number;
  modTime?: string;
  isDirectory: boolean;
  isSymlink?: boolean;
  selected?: boolean;
  hidden?: boolean;
  onClick: (event: React.MouseEvent) => void;
  onDoubleClick?: () => void;
  onContextMenu?: (event: React.MouseEvent) => void;
}

const formatBytes = (bytes?: number) => {
  if (bytes === undefined || bytes === null) return "";
  const thresh = 1024;
  if (Math.abs(bytes) < thresh) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let u = -1;
  let value = bytes;
  do {
    value /= thresh;
    ++u;
  } while (Math.abs(value) >= thresh && u < units.length - 1);
  return `${value.toFixed(1)} ${units[u]}`;
};

const FileCard: React.FC<FileCardProps> = React.memo(
  ({
    name,
    type,
    size,
    modTime,
    isDirectory,
    isSymlink = false,
    selected = false,
    hidden = false,
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

    const baseBg = useMemo(
      () => {
        if (selected) {
          return alpha(theme.palette.primary.main, 0.4);
        }
        return theme.palette.mode === "dark" ? "#20292f" : "#ffffff";
      },
      [selected, theme],
    );

    const hoverBg = useMemo(
      () => {
        if (selected) {
          return alpha(theme.palette.primary.main, 0.4);
        }
        return theme.palette.mode === "dark" ? "#2a3540" : "#f5f5f5";
      },
      [selected, theme],
    );

    const baseBorderColor = alpha(theme.palette.divider, theme.palette.mode === "dark" ? 0.15 : 0.1);

    const borderColor = useMemo(
      () => {
        if (selected) {
          return alpha(theme.palette.primary.main, 0.7);
        }
        return isDirectory ? baseBorderColor : "transparent";
      },
      [selected, isDirectory, theme, baseBorderColor],
    );

    const textOpacity = isDirectory ? 1 : 0.5;

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
          borderRadius: `${(theme.shape.borderRadius as number) * 2}px`,
          backgroundColor: hovered ? hoverBg : baseBg,
          cursor: "pointer",
          minHeight: "60px",
          transition: "all 120ms ease",
          transform: hovered ? "translateY(-2px) scale(1.01)" : "none",
        }}
      >
        <FileIcon isDirectory={isDirectory} filename={name} hidden={hidden} />

        <div
          style={{
            flex: 1,
            minWidth: 0,
            display: "flex",
            flexDirection: "column",
            gap: 0,
          }}
        >
          <div
            style={{
              fontWeight:  450,
              fontSize: "0.95rem",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              color: theme.palette.text.primary,
              opacity: textOpacity,
            }}
          >
            {name}
          </div>

          <div
            style={{
              display: "flex",
              flexDirection: "column",
              fontSize: "0.95rem",
              color: theme.palette.text.secondary,
              gap: 0,
              lineHeight: 1.2,
              opacity: textOpacity,
            }}
          >
            {!isDirectory && size !== undefined && (
              <span>{formatBytes(size)}</span>
            )}
            {formattedDate && <span>{formattedDate}</span>}
          </div>
        </div>
        {isSymlink && (
          <LinkIcon
            sx={{
              fontSize: 24,
              color: theme.palette.primary.main,
              flexShrink: 0,
            }}
          />
        )}
      </div>
    );
  },
);

FileCard.displayName = "FileCard";

export default FileCard;
