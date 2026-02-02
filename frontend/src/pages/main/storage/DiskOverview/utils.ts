import { formatFileSize } from "@/utils/formaters";

import type { DriveInfo, SmartData } from "./types";

export function parseSizeToBytes(input: string | undefined | null): number {
  if (!input) return 0;
  const s = String(input).trim().toUpperCase();
  const m = s.match(/^([\d.]+)\s*([KMGTPE]?)(B)?$/);
  if (!m) return 0;
  const value = parseFloat(m[1]);
  if (!isFinite(value) || value < 0) return 0;
  const unit = m[2] || "B";
  const pow =
    unit === "B"
      ? 0
      : unit === "K"
        ? 1
        : unit === "M"
          ? 2
          : unit === "G"
            ? 3
            : unit === "T"
              ? 4
              : unit === "P"
                ? 5
                : 0;
  return Math.floor(value * Math.pow(1024, pow));
}

export const getHealthColor = (
  smart: DriveInfo["smart"] | undefined,
): "success" | "error" | "warning" | "default" => {
  if (!smart?.smart_status) return "default";
  const passed = smart.smart_status.passed;
  if (passed === true) return "success";
  if (passed === false) return "error";
  return "warning";
};

export const formatPowerOnTime = (hours?: number): string => {
  if (hours === undefined) return "N/A";
  const days = Math.floor(hours / 24);
  const remainingHours = hours % 24;
  if (days > 0) {
    return `${days}d ${remainingHours}h`;
  }
  return `${hours}h`;
};

export const formatDataUnits = (units?: number): string => {
  if (units === undefined) return "N/A";
  // NVMe data units are in 512KB blocks
  const bytes = units * 512 * 1000;
  return `${units.toLocaleString()} [${formatFileSize(bytes)}]`;
};

export const getTemperature = (smart?: SmartData): number | null => {
  if (!smart) return null;
  return (
    smart.nvme_smart_health_information_log?.temperature ??
    smart.temperature?.current ??
    null
  );
};

export const getTemperatureColor = (temp: number | null): string => {
  if (temp === null) return "text.secondary";
  if (temp > 70) return "error.main";
  if (temp > 50) return "warning.main";
  return "success.main";
};

export const getSmartValue = (
  val: unknown,
  preferString = true,
): string | number | null => {
  if (val === undefined || val === null) return null;
  if (typeof val === "string" || typeof val === "number") return val;
  if (typeof val === "object") {
    const obj = val as { string?: string; value?: number };
    if (preferString && obj.string !== undefined) return obj.string;
    if (obj.value !== undefined) return obj.value;
    if (obj.string !== undefined) return obj.string;
  }
  return null;
};

export const getSmartNumber = (val: unknown): number | null => {
  const result = getSmartValue(val, false);
  if (typeof result === "number") return result;
  if (typeof result === "string") {
    const parsed = parseFloat(result);
    return isNaN(parsed) ? null : parsed;
  }
  return null;
};

export const getSmartString = (val: unknown): string | null => {
  const result = getSmartValue(val, true);
  return result !== null ? String(result) : null;
};
