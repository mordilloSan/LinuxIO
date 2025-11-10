import ArchiveIcon from "@mui/icons-material/Archive";
import AudioFileIcon from "@mui/icons-material/AudioFile";
import CodeIcon from "@mui/icons-material/Code";
import DescriptionIcon from "@mui/icons-material/Description";
import FolderIcon from "@mui/icons-material/Folder";
import ImageIcon from "@mui/icons-material/Image";
import LinkIcon from "@mui/icons-material/Link";
import PictureAsPdfIcon from "@mui/icons-material/PictureAsPdf";
import TableChartIcon from "@mui/icons-material/TableChart";
import TerminalIcon from "@mui/icons-material/Terminal";
import TextFieldsIcon from "@mui/icons-material/TextFields";
import VideocamIcon from "@mui/icons-material/Videocam";
import { useTheme } from "@mui/material/styles";
import React from "react";

interface FileIconProps {
  isDirectory: boolean;
  filename?: string;
  hidden?: boolean;
  size?: number;
  isSymlink?: boolean;
}

const getIconForType = (filename?: string) => {
  if (!filename) return DescriptionIcon;

  // Extract extension from filename
  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return DescriptionIcon;

  const ext = filename.slice(lastDotIndex + 1).toLowerCase();

  // PDF
  if (ext === "pdf") return PictureAsPdfIcon;

  // Documents
  if (["doc", "docx", "odt", "rtf"].includes(ext)) return DescriptionIcon;

  // Spreadsheets
  if (["xls", "xlsx", "csv", "ods"].includes(ext)) return TableChartIcon;

  // Images
  if (["png", "jpg", "jpeg", "gif", "svg", "bmp", "ico", "webp"].includes(ext))
    return ImageIcon;

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
    return CodeIcon;

  // Text
  if (["txt", "md", "markdown", "log"].includes(ext)) return TextFieldsIcon;

  // Video
  if (["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm"].includes(ext))
    return VideocamIcon;

  // Audio
  if (["mp3", "wav", "flac", "aac", "m4a", "ogg", "wma"].includes(ext))
    return AudioFileIcon;

  // Archives
  if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz"].includes(ext))
    return ArchiveIcon;

  // Executables
  if (["exe", "bin", "sh", "app", "dmg"].includes(ext)) return TerminalIcon;

  return DescriptionIcon;
};

const getIconColor = (filename: string | undefined, hidden: boolean, isDark: boolean): string => {
  if (!filename) return isDark ? "#ffffff" : "rgba(0, 0, 0, 0.6)";

  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return isDark ? "#ffffff" : "rgba(0, 0, 0, 0.6)";

  const ext = filename.slice(lastDotIndex + 1).toLowerCase();

  // Code files - yellow/gold
  if (["js", "ts", "tsx", "jsx", "py", "go", "cpp", "c", "java", "rs", "php", "rb", "sh", "bash", "json", "html", "css"].includes(ext)) {
    return "#f9a825";
  }

  // PDF - red
  if (ext === "pdf") return "#d32f2f";

  // Images - purple
  if (["png", "jpg", "jpeg", "gif", "svg", "bmp", "ico", "webp"].includes(ext)) {
    return "#7b1fa2";
  }

  // Video - pink
  if (["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm"].includes(ext)) {
    return "#c2185b";
  }

  // Audio - teal
  if (["mp3", "wav", "flac", "aac", "m4a", "ogg", "wma"].includes(ext)) {
    return "#00897b";
  }

  // Archives - orange
  if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz"].includes(ext)) {
    return "#f57c00";
  }

  // Spreadsheets - green
  if (["xls", "xlsx", "csv", "ods"].includes(ext)) {
    return "#388e3c";
  }

  // Documents - blue
  if (["doc", "docx", "odt", "rtf", "txt", "md", "markdown", "log"].includes(ext)) {
    return "#1976d2";
  }

  // Executables - red/dark
  if (["exe", "bin", "app", "dmg"].includes(ext)) {
    return "#b71c1c";
  }

  // Default
  return isDark ? "#ffffff" : "rgba(0, 0, 0, 0.6)";
};

const FileIcon = React.memo(
  ({ isDirectory, filename, hidden, size = 70, isSymlink = false }: FileIconProps) => {
    const theme = useTheme();

    const IconComponent = isDirectory ? FolderIcon : getIconForType(filename);
    const iconColor = isDirectory
      ? theme.palette.primary.main
      : getIconColor(filename, hidden || false, theme.palette.mode === "dark");
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
          <IconComponent
            sx={{
              fontSize: size,
              color: iconColor,
              flexShrink: 0,
            }}
          />
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
        <IconComponent
          sx={{
            fontSize: size,
            color: iconColor,
            flexShrink: 0,
          }}
        />
        <LinkIcon
          sx={{
            position: "absolute",
            fontSize: size * 0.35,
            color: "#b0b0b0",
            bottom: isDirectory ? "20%" : "10%",
            right: isDirectory ? "10%" : "15%",
            transform: "rotate(-45deg)",
            filter: "drop-shadow(0px 1px 2px rgba(0, 0, 0, 0.5))",
          }}
        />
      </div>
    );
  },
);

FileIcon.displayName = "FileIcon";

export default FileIcon;
