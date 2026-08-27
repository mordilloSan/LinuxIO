import { Icon } from "@iconify/react";
import { memo } from "react";

import "./FileIcon.css";

import { FILE_TYPE_COLORS } from "@/theme/colors";

import { getIconForType } from "./fileIconUtils";

interface FileIconProps {
  /** Applied to the icon wrapper, so a parent can animate the icon. */
  className?: string;
  filename?: string;
  hidden?: boolean;
  isDirectory: boolean;
  isSymlink?: boolean;
  size?: number;
}

// Returns undefined when the extension has no dedicated color, so the caller
// falls back to the CSS-driven default (currentColor via .file-icon--default-color).
const getIconColor = (filename: string | undefined): string | undefined => {
  if (!filename) return undefined;

  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return undefined;

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

  // YAML - config/code
  if (["yaml", "yml"].includes(ext)) {
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

  // Default: let the CSS-driven fallback color apply.
  return undefined;
};

const FileIcon = memo(
  ({
    className,
    isDirectory,
    filename,
    hidden,
    size = 70,
    isSymlink = false,
  }: FileIconProps) => {
    const iconName = isDirectory ? "mdi:folder" : getIconForType(filename);
    const iconColor = isDirectory
      ? "var(--app-palette-primary-main)"
      : getIconColor(filename);
    const wrapperOpacity = hidden ? 0.25 : 1;

    const wrapperClass = [
      "file-icon",
      !isDirectory && iconColor === undefined && "file-icon--default-color",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    if (!isSymlink) {
      return (
        <span className={wrapperClass} style={{ opacity: wrapperOpacity }}>
          <Icon
            color={iconColor}
            height={size}
            icon={iconName}
            style={{ flexShrink: 0 }}
            width={size}
          />
        </span>
      );
    }

    // Render with symlink overlay
    return (
      <div
        className={`${wrapperClass} file-icon--symlink`}
        style={{ opacity: wrapperOpacity }}
      >
        <Icon
          color={iconColor}
          height={size}
          icon={iconName}
          style={{ flexShrink: 0 }}
          width={size}
        />
        <Icon
          height={size * 0.35}
          icon="mdi:link"
          style={{
            position: "absolute",
            color: "var(--app-palette-text-secondary)",
            bottom: isDirectory ? "20%" : "10%",
            right: isDirectory ? "10%" : "15%",
            transform: "rotate(-45deg)",
            filter:
              "drop-shadow(0px 1px 2px color-mix(in srgb, black, transparent 50%))",
          }}
          width={size * 0.35}
        />
      </div>
    );
  },
);

FileIcon.displayName = "FileIcon";

export default FileIcon;
