import { Icon } from "@iconify/react";
import { alpha, useTheme } from "@mui/material/styles";
import React from "react";

import { FILE_TYPE_COLORS } from "@/constants/fileTypeColors";

interface FileIconProps {
  isDirectory: boolean;
  filename?: string;
  hidden?: boolean;
  size?: number;
  isSymlink?: boolean;
}

const getIconForType = (filename?: string): string => {
  if (!filename) return "mdi:file";

  // Extract extension from filename
  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return "mdi:file";

  const ext = filename.slice(lastDotIndex + 1).toLowerCase();

  // PDF
  if (ext === "pdf") return "mdi:file-pdf-box";

  // Documents
  if (["doc", "docx", "odt", "rtf"].includes(ext)) return "mdi:file-document";

  // Spreadsheets
  if (["xls", "xlsx", "csv", "ods"].includes(ext)) return "mdi:file-table";

  // Images
  if (["png", "jpg", "jpeg", "gif", "svg", "bmp", "ico", "webp"].includes(ext))
    return "mdi:file-image";

  // Code
  if (
    [
      "js",
      "ts",
      "tsx",
      "jsx",
      "py",
      "go",
      "cpp",
      "c",
      "java",
      "rs",
      "php",
      "rb",
      "sh",
      "bash",
      "json",
      "html",
      "css",
    ].includes(ext)
  )
    return "mdi:file-code";

  // Text
  if (["txt", "md", "markdown", "log"].includes(ext)) return "mdi:file-document-outline";

  // Video
  if (["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm"].includes(ext))
    return "mdi:file-video";

  // Audio
  if (["mp3", "wav", "flac", "aac", "m4a", "ogg", "wma"].includes(ext))
    return "mdi:file-music";

  // Archives
  if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz"].includes(ext))
    return "mdi:archive";

  // Executables
  if (["exe", "bin", "sh", "app", "dmg"].includes(ext)) return "mdi:console";

  return "mdi:file";
};

const getIconColor = (
  filename: string | undefined,
  defaultColor: string,
): string => {
  if (!filename) return defaultColor;

  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return defaultColor;

  const ext = filename.slice(lastDotIndex + 1).toLowerCase();

  // Code files - yellow/gold
  if (
    [
      "js",
      "ts",
      "tsx",
      "jsx",
      "py",
      "go",
      "cpp",
      "c",
      "java",
      "rs",
      "php",
      "rb",
      "sh",
      "bash",
      "json",
      "html",
      "css",
    ].includes(ext)
  ) {
    return FILE_TYPE_COLORS.code;
  }

  // PDF - red
  if (ext === "pdf") return FILE_TYPE_COLORS.pdf;

  // Images - purple
  if (
    ["png", "jpg", "jpeg", "gif", "svg", "bmp", "ico", "webp"].includes(ext)
  ) {
    return FILE_TYPE_COLORS.image;
  }

  // Video - pink
  if (["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm"].includes(ext)) {
    return FILE_TYPE_COLORS.video;
  }

  // Audio - teal
  if (["mp3", "wav", "flac", "aac", "m4a", "ogg", "wma"].includes(ext)) {
    return FILE_TYPE_COLORS.audio;
  }

  // Archives - orange
  if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz"].includes(ext)) {
    return FILE_TYPE_COLORS.archive;
  }

  // Spreadsheets - green
  if (["xls", "xlsx", "csv", "ods"].includes(ext)) {
    return FILE_TYPE_COLORS.spreadsheet;
  }

  // Documents - blue
  if (
    ["doc", "docx", "odt", "rtf", "txt", "md", "markdown", "log"].includes(ext)
  ) {
    return FILE_TYPE_COLORS.document;
  }

  // Executables - red/dark
  if (["exe", "bin", "app", "dmg"].includes(ext)) {
    return FILE_TYPE_COLORS.executable;
  }

  // Default
  return defaultColor;
};

const FileIcon = React.memo(
  ({
    isDirectory,
    filename,
    hidden,
    size = 70,
    isSymlink = false,
  }: FileIconProps) => {
    const theme = useTheme();
    const iconName = isDirectory ? "mdi:folder" : getIconForType(filename);
    const defaultIconColor =
      theme.palette.mode === "dark"
        ? theme.palette.common.white
        : alpha(theme.palette.common.black, 0.6);
    const iconColor = isDirectory
      ? theme.palette.primary.main
      : getIconColor(filename, defaultIconColor);
    const wrapperOpacity = hidden ? 0.25 : 1;

    if (!isSymlink) {
      return (
        <span
          style={{
            display: "inline-flex",
            flexShrink: 0,
            opacity: wrapperOpacity,
            transition: "opacity 120ms ease",
          }}
        >
          <Icon icon={iconName} width={size} height={size} color={iconColor} style={{ flexShrink: 0 }} />
        </span>
      );
    }

    // Render with symlink overlay
    return (
      <div
        style={{
          position: "relative",
          display: "inline-flex",
          flexShrink: 0,
          opacity: wrapperOpacity,
          transition: "opacity 120ms ease",
        }}
      >
        <Icon icon={iconName} width={size} height={size} color={iconColor} style={{ flexShrink: 0 }} />
        <Icon
          icon="mdi:link"
          width={size * 0.35}
          height={size * 0.35}
          style={{
            position: "absolute",
            color: theme.palette.text.secondary,
            bottom: isDirectory ? "20%" : "10%",
            right: isDirectory ? "10%" : "15%",
            transform: "rotate(-45deg)",
            filter: `drop-shadow(0px 1px 2px ${alpha(theme.palette.common.black, 0.5)})`,
          }}
        />
      </div>
    );
  },
);

FileIcon.displayName = "FileIcon";

export default FileIcon;
