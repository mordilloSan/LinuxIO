export const formatThroughput = (bytesPerSec: number): string => {
  if (!isFinite(bytesPerSec) || bytesPerSec <= 0) return "0 B/s";
  if (bytesPerSec >= 1024 * 1024 * 1024) {
    return `${(bytesPerSec / (1024 * 1024 * 1024)).toFixed(1)} GB/s`;
  }
  if (bytesPerSec >= 1024 * 1024) {
    return `${(bytesPerSec / (1024 * 1024)).toFixed(bytesPerSec >= 10 * 1024 * 1024 ? 0 : 1)} MB/s`;
  }
  if (bytesPerSec >= 1024) {
    return `${(bytesPerSec / 1024).toFixed(bytesPerSec >= 10 * 1024 ? 0 : 1)} kB/s`;
  }
  return `${bytesPerSec.toFixed(0)} B/s`;
};

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

export const formatDate = (dateString?: string): string => {
  if (!dateString) return "Unknown";
  try {
    const date = new Date(dateString);
    return date.toLocaleString();
  } catch {
    return dateString;
  }
};
