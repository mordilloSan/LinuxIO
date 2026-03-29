import type { GpuDevice } from "@/api";

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

const normalizeGpuVendorId = (vendorId?: string | null): string =>
  vendorId?.trim().toLowerCase().replace(/^0x/, "") ?? "";

const matchesAny = (value: string, patterns: string[]): boolean =>
  patterns.some((pattern) => value.includes(pattern));

export const getGpuVendorLabel = (
  gpu?: Partial<GpuDevice> | null,
): string => {
  if (!gpu) {
    return "—";
  }

  const vendorId = normalizeGpuVendorId(gpu.vendor_id);
  if (vendorId === "8086") {
    return "Intel";
  }
  if (vendorId === "10de") {
    return "NVIDIA";
  }
  if (vendorId === "1002" || vendorId === "1022") {
    return "AMD";
  }

  const driverText = `${gpu.driver_module ?? ""} ${gpu.driver ?? ""}`.toLowerCase();
  if (matchesAny(driverText, ["i915", "xe"])) {
    return "Intel";
  }
  if (matchesAny(driverText, ["nvidia", "nouveau"])) {
    return "NVIDIA";
  }
  if (matchesAny(driverText, ["amdgpu", "radeon"])) {
    return "AMD";
  }

  const modelText = `${gpu.model ?? ""}`.toLowerCase();
  if (
    matchesAny(modelText, [
      "intel",
      "uhd graphics",
      "iris",
      "alder lake",
      "raptor lake",
      "meteor lake",
      "lunar lake",
      "tiger lake",
      "dg1",
      "dg2",
      "battlemage",
      "arc",
    ])
  ) {
    return "Intel";
  }
  if (
    matchesAny(modelText, [
      "nvidia",
      "geforce",
      "quadro",
      "tesla",
      "rtx",
      "gtx",
    ])
  ) {
    return "NVIDIA";
  }
  if (
    matchesAny(modelText, [
      "amd",
      "ati",
      "radeon",
      "vega",
      "navi",
      "rembrandt",
      "phoenix",
      "strix",
      "firepro",
      "instinct",
    ])
  ) {
    return "AMD";
  }

  const normalizedVendor = gpu.vendor?.trim();
  if (!normalizedVendor) {
    return "—";
  }

  const lowerVendor = normalizedVendor.toLowerCase();
  if (lowerVendor.includes("intel")) {
    return "Intel";
  }
  if (lowerVendor.includes("nvidia")) {
    return "NVIDIA";
  }
  if (
    lowerVendor.includes("advanced micro devices") ||
    lowerVendor.includes("amd") ||
    lowerVendor.includes("ati")
  ) {
    return "AMD";
  }

  return normalizedVendor;
};

export const getGpuType = (gpu: GpuDevice): string =>
  gpu.subclass_name ?? gpu.class_name ?? "Graphics controller";
