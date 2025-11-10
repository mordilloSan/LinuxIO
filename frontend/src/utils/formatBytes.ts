export const formatFileSize = (
  bytes?: number | null,
  decimals = 2,
  fallback = "Unknown",
): string => {
  if (bytes === null || bytes === undefined) return fallback;
  if (bytes === 0) return "0 Bytes";

  const k = 1024;
  const dm = Math.max(0, decimals);
  const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
};

// Alias for backwards compatibility with Memory.tsx which uses formatBytes
export const formatBytes = formatFileSize;

export const formatDate = (dateString?: string): string => {
  if (!dateString) return "Unknown";
  try {
    const date = new Date(dateString);
    return date.toLocaleString();
  } catch {
    return dateString;
  }
};
