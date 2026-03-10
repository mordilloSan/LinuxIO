import type { GpuDevice } from "@/api/linuxio-types";

export const hasGpuValue = <T>(
  value: T | null | undefined | "",
): value is Exclude<T, null | undefined | ""> =>
  value !== null && value !== undefined && value !== "";

export const formatGpuPercent = (value?: number | null): string =>
  typeof value === "number" && Number.isFinite(value)
    ? `${Math.round(value)}%`
    : "—";

export const formatGpuBytes = (value?: number | null): string => {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    return "—";
  }
  if (value < 1024) return `${value} B`;
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`;
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MiB`;
  if (value < 1024 ** 4) return `${(value / 1024 ** 3).toFixed(2)} GiB`;
  return `${(value / 1024 ** 4).toFixed(2)} TiB`;
};

export const formatGpuWatts = (value?: number | null): string =>
  typeof value === "number" && Number.isFinite(value) ? `${value} W` : "—";

export const formatGpuTemperature = (value?: number | null): string =>
  typeof value === "number" && Number.isFinite(value) ? `${value}°C` : "—";

export const formatGpuClock = (value?: number | null): string =>
  typeof value === "number" && Number.isFinite(value) ? `${value} MHz` : "—";

export const formatGpuDisplays = (gpu: GpuDevice): string => {
  if (gpu.display_names && gpu.display_names.length > 0) {
    return gpu.display_names.join(", ");
  }
  if (typeof gpu.connected_displays === "number") {
    return `${gpu.connected_displays} connected`;
  }
  return "—";
};

export const getGpuType = (gpu: GpuDevice): string =>
  gpu.subclass_name ?? gpu.class_name ?? "Graphics controller";
