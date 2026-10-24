import { FILE_TYPE_COLORS } from "./fileTypeColors";

interface FileTypeStyle {
  // Undefined falls back to the CSS-driven default (.file-icon--default-color).
  color?: string;
  extensions: string[];
  icon: string;
}

// The single source of truth for how an extension looks. The first entry that
// lists an extension wins.
const FILE_TYPE_STYLES: FileTypeStyle[] = [
  {
    extensions: ["pdf"],
    icon: "mdi:file-pdf-box",
    color: FILE_TYPE_COLORS.pdf,
  },
  {
    extensions: ["doc", "docx", "odt", "rtf"],
    icon: "mdi:file-document",
    color: FILE_TYPE_COLORS.document,
  },
  {
    extensions: ["xls", "xlsx", "csv", "ods"],
    icon: "mdi:file-table",
    color: FILE_TYPE_COLORS.spreadsheet,
  },
  {
    extensions: ["yaml", "yml"],
    icon: "mdi:file-cog-outline",
    color: FILE_TYPE_COLORS.code,
  },
  {
    extensions: ["png", "jpg", "jpeg", "gif", "svg", "bmp", "ico", "webp"],
    icon: "mdi:file-image",
    color: FILE_TYPE_COLORS.image,
  },
  {
    extensions: [
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
    ],
    icon: "mdi:file-code",
    color: FILE_TYPE_COLORS.code,
  },
  {
    extensions: ["txt", "md", "markdown", "log"],
    icon: "mdi:file-document-outline",
    color: FILE_TYPE_COLORS.document,
  },
  {
    extensions: ["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm"],
    icon: "mdi:file-video",
    color: FILE_TYPE_COLORS.video,
  },
  {
    extensions: ["mp3", "wav", "flac", "aac", "m4a", "ogg", "wma"],
    icon: "mdi:file-music",
    color: FILE_TYPE_COLORS.audio,
  },
  {
    extensions: ["zip", "rar", "7z", "tar", "gz", "bz2", "xz"],
    icon: "mdi:archive",
    color: FILE_TYPE_COLORS.archive,
  },
  {
    extensions: ["exe", "bin", "app", "dmg"],
    icon: "mdi:console",
    color: FILE_TYPE_COLORS.executable,
  },
];

const getFileTypeStyle = (filename?: string): FileTypeStyle | undefined => {
  const dot = filename?.lastIndexOf(".") ?? -1;
  if (!filename || dot === -1) return undefined;
  const ext = filename.slice(dot + 1).toLowerCase();
  return FILE_TYPE_STYLES.find((style) => style.extensions.includes(ext));
};

export const getIconForType = (filename?: string): string =>
  getFileTypeStyle(filename)?.icon ?? "mdi:file";

export const getIconColor = (filename?: string): string | undefined =>
  getFileTypeStyle(filename)?.color;
