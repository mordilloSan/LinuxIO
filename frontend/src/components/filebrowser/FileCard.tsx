import { Folder, InsertDriveFile } from "@mui/icons-material";
import { Box } from "@mui/material";
import React, { useMemo } from "react";

export interface FileCardProps {
  name: string;
  type: string;
  size?: number;
  modTime?: string;
  isDirectory: boolean;
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

const FileCard: React.FC<FileCardProps> = React.memo(({
  name,
  type,
  size,
  modTime,
  isDirectory,
  selected = false,
  hidden = false,
  onClick,
  onDoubleClick,
  onContextMenu,
}) => {
  // Memoize expensive date formatting to prevent recalculation on every render
  const formattedDate = useMemo(() => {
    if (!modTime) return "";
    const date = new Date(modTime);
    return date.toLocaleDateString("en-GB");
  }, [modTime]);

  const handleClick = (event: React.MouseEvent) => {
    event.stopPropagation();
    onClick(event);
  };

  const handleDoubleClick = (event: React.MouseEvent) => {
    event.stopPropagation();
    if (onDoubleClick) {
      onDoubleClick();
    }
  };

  const handleContextMenu = (event: React.MouseEvent) => {
    if (onContextMenu) {
      onContextMenu(event);
    }
  };

  return (
    <Box
      data-file-card="true"
      onClick={handleClick}
      onDoubleClick={handleDoubleClick}
      onContextMenu={handleContextMenu}
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1.5,
        p: 1.5,
        border: selected ? 2 : 1,
        borderColor: selected ? "primary.main" : "divider",
        borderRadius: 2,
        bgcolor: selected
          ? "rgba(25, 118, 210, 0.40)"
          : isDirectory
            ? "action.hover"
            : "background.paper",
        cursor: "pointer",
        minHeight: "60px",
        "&:hover": {
          borderColor: "primary.main",
          bgcolor: selected
            ? "rgba(25, 118, 210, 0.12)"
            : isDirectory
              ? "action.selected"
              : "action.hover",
          transform: "translateY(-2px) scale(1.01)",
        },
      }}
    >
      {/* Icon with background */}

      {isDirectory ? (
        <Folder
          sx={{
            fontSize: 40,
            color: "primary.main",
            flexShrink: 0,
          }}
        />
      ) : (
        <InsertDriveFile
          sx={{
            fontSize: 40,
            color: hidden
              ? (theme) => theme.palette.mode === "dark" ? "text.secondary" : "rgba(0, 0, 0, 0.26)"
              : (theme) => theme.palette.mode === "dark" ? "#ffffff" : "rgba(0, 0, 0, 0.6)",
            flexShrink: 0,
          }}
        />
      )}

      {/* Text Content */}
      <Box sx={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
        {/* Name */}
        <Box
          sx={{
            fontWeight: isDirectory ? 600 : 500,
            fontSize: "0.875rem",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            color: "text.primary",
          }}
        >
          {name}
        </Box>

        {/* Size and Date */}
        <Box sx={{ display: "flex", gap: 1.5, fontSize: "0.75rem", color: "text.secondary" }}>
          {!isDirectory && size !== undefined && <span>{formatBytes(size)}</span>}
          {formattedDate && <span>{formattedDate}</span>}
        </Box>
      </Box>
    </Box>
  );
});

FileCard.displayName = "FileCard";

export default FileCard;
