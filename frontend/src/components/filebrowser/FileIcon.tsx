import React from "react";
import {
  Folder,
  InsertDriveFile,
  PictureAsPdf,
  Description,
  Image,
  Code,
  TextFields,
  Videocam,
  AudioFile,
  Archive,
  TableChart,
  Terminal,
} from "@mui/icons-material";
import { useTheme } from "@mui/material/styles";

interface FileIconProps {
  isDirectory: boolean;
  filename?: string;
  hidden?: boolean;
}

const getIconForType = (filename?: string) => {
  if (!filename) return InsertDriveFile;

  // Extract extension from filename
  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return InsertDriveFile;

  const ext = filename.slice(lastDotIndex + 1).toLowerCase();

  // PDF
  if (ext === "pdf") return PictureAsPdf;

  // Documents
  if (["doc", "docx", "odt", "rtf"].includes(ext)) return Description;

  // Spreadsheets
  if (["xls", "xlsx", "csv", "ods"].includes(ext)) return TableChart;

  // Images
  if (["png", "jpg", "jpeg", "gif", "svg", "bmp", "ico", "webp"].includes(ext))
    return Image;

  // Code
  if (
    ["js", "ts", "tsx", "jsx", "py", "go", "cpp", "c", "java", "rs", "php", "rb", "sh", "bash", "json", "html", "css"].includes(ext)
  )
    return Code;

  // Text
  if (["txt", "md", "markdown", "log"].includes(ext)) return TextFields;

  // Video
  if (["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm"].includes(ext))
    return Videocam;

  // Audio
  if (["mp3", "wav", "flac", "aac", "m4a", "ogg", "wma"].includes(ext))
    return AudioFile;

  // Archives
  if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz"].includes(ext))
    return Archive;

  // Executables
  if (["exe", "bin", "sh", "app", "dmg"].includes(ext)) return Terminal;

  return InsertDriveFile;
};

const FileIcon = React.memo(({ isDirectory, filename, hidden }: FileIconProps) => {
  const theme = useTheme();

  if (isDirectory) {
    return (
      <Folder
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
});

FileIcon.displayName = "FileIcon";

export default FileIcon;
