import { formatFileSize } from "@/utils/formatters";

export const formatVolumeSize = (size?: number) => {
  if (size === undefined || size < 0) return "Unavailable";
  return formatFileSize(size);
};

export const formatReferenceCount = (count?: number) => {
  if (count === undefined || count < 0) return "Unavailable";
  return count.toLocaleString();
};

export const formatDockerValue = (value: unknown) => {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (value === null) return "null";
  try {
    return JSON.stringify(value) ?? "Unavailable";
  } catch {
    return "Unavailable";
  }
};
