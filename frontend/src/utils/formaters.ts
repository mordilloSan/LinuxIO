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

/**
 * Age of a past epoch-millisecond timestamp, as "5m ago" / "5h ago" / "5d ago".
 * For "when did this last happen" labels, where the exact clock time matters
 * less than how stale the answer is.
 */
export const formatRelativeAge = (epochMs: number): string => {
  const seconds = Math.floor((Date.now() - epochMs) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months}mo ago`;
  return `${Math.floor(months / 12)}y ago`;
};
