export const getIconForType = (filename?: string): string => {
  if (!filename) return "mdi:file";

  const lastDotIndex = filename.lastIndexOf(".");
  if (lastDotIndex === -1) return "mdi:file";

  const ext = filename.slice(lastDotIndex + 1).toLowerCase();

  if (ext === "pdf") return "mdi:file-pdf-box";
  if (["doc", "docx", "odt", "rtf"].includes(ext)) return "mdi:file-document";
  if (["xls", "xlsx", "csv", "ods"].includes(ext)) return "mdi:file-table";
  if (["yaml", "yml"].includes(ext)) return "mdi:file-cog-outline";
  if (["png", "jpg", "jpeg", "gif", "svg", "bmp", "ico", "webp"].includes(ext))
    return "mdi:file-image";
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
  if (["txt", "md", "markdown", "log"].includes(ext))
    return "mdi:file-document-outline";
  if (["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm"].includes(ext))
    return "mdi:file-video";
  if (["mp3", "wav", "flac", "aac", "m4a", "ogg", "wma"].includes(ext))
    return "mdi:file-music";
  if (["zip", "rar", "7z", "tar", "gz", "bz2", "xz"].includes(ext))
    return "mdi:archive";
  if (["exe", "bin", "sh", "app", "dmg"].includes(ext)) return "mdi:console";

  return "mdi:file";
};
