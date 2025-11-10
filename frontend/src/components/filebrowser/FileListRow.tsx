import { alpha, useTheme } from "@mui/material/styles";
import React, { useState, useCallback, useMemo } from "react";

import FileIcon from "@/components/filebrowser/FileIcon";

export interface FileListRowProps {
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

const FileListRow: React.FC<FileListRowProps> = React.memo(
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

    const formattedDate = modTime ? new Date(modTime).toLocaleDateString("en-GB") : "";
    const formattedSize = !isDirectory ? formatBytes(size) : "";

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
          gridTemplateColumns: "auto 1fr 150px 200px",
          alignItems: "center",
          gap: theme.spacing(2),
          padding: theme.spacing(1.5, 2),
          backgroundColor: bgColor,
          cursor: "pointer",
          transition: "background-color 0.15s ease",
          borderRadius: theme.shape.borderRadius,
        }}
      >
        {/* Icon */}
        <div style={{ display: "flex", alignItems: "center", width: 24 }}>
          <FileIcon
            isDirectory={isDirectory}
            filename={name}
            hidden={hidden}
            size={24}
            isSymlink={isSymlink}
          />
        </div>

        {/* Name */}
        <div
          style={{
            fontWeight: 500,
            fontSize: "0.9375rem",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            color: theme.palette.text.primary,
            opacity: hidden ? 0.5 : undefined,
          }}
        >
          {name}
        </div>

        {/* Size */}
        <div
          style={{
            fontSize: "0.875rem",
            color: theme.palette.text.secondary,
            opacity: hidden ? 0.5 : undefined,
            textAlign: "right",
          }}
        >
          {formattedSize}
        </div>

        {/* Modified Date */}
        <div
          style={{
            fontSize: "0.875rem",
            color: theme.palette.text.secondary,
            opacity: hidden ? 0.5 : undefined,
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
