import ArchiveIcon from "@mui/icons-material/Archive";
import AudioFileIcon from "@mui/icons-material/AudioFile";
import CodeIcon from "@mui/icons-material/Code";
import DescriptionIcon from "@mui/icons-material/Description";
import FolderIcon from "@mui/icons-material/Folder";
import ImageIcon from "@mui/icons-material/Image";
import InsertDriveFileIcon from "@mui/icons-material/InsertDriveFile";
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
}

const getIconForType = (filename?: string) => {
  if (!filename) return InsertDriveFileIcon;

  // Extract extension from filename
  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return InsertDriveFileIcon;

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

  return InsertDriveFileIcon;
};

const FileIcon = React.memo(
  ({ isDirectory, filename, hidden }: FileIconProps) => {
    const theme = useTheme();

    if (isDirectory) {
      return (
        <FolderIcon
          sx={{
            fontSize: 70,
            color: theme.palette.primary.main,
            flexShrink: 0,
          }}
        />
      );
    }

    const IconComponent = getIconForType(filename);
    const fileColor = hidden
      ? theme.palette.mode === "dark"
        ? theme.palette.text.secondary
        : "rgba(0, 0, 0, 0.26)"
      : theme.palette.mode === "dark"
        ? "#ffffff"
        : "rgba(0, 0, 0, 0.6)";

    return (
      <IconComponent
        sx={{
          fontSize: 70,
          color: fileColor,
          flexShrink: 0,
        }}
      />
    );
  },
);

FileIcon.displayName = "FileIcon";

export default FileIcon;
