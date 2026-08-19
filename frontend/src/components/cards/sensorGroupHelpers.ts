import type { SensorReading } from "@/api";

const isNumericSensorReading = (reading: SensorReading): boolean =>
  reading.kind === "number";

const getSensorLabelMeta = (label: string) => {
  const match = label.match(/^(.*)\(([^()]*)\)\s*$/);
  if (!match) {
    return { baseLabel: label, suffix: null as string | null, context: "" };
  }
  const baseLabel = match[1].trimEnd();
  const parts = match[2]
    .split("/")
    .map((part) => part.trim())
    .filter(Boolean);
  const suffix =
    parts.length > 0 ? parts[parts.length - 1].toLowerCase() : null;
  const context = parts.slice(0, -1).join(" / ");
  return { baseLabel, suffix, context };
};

export const isTemperatureReading = (reading: SensorReading): boolean => {
  if (!isNumericSensorReading(reading)) return false;
  const unit = reading.unit.toLowerCase();
  return unit === "c" || unit === "°c";
};

export const formatNumericSensorValue = (
  value: number,
  unit: string,
): string => {
  const normalizedUnit = unit.toLowerCase();
  let digits = 2;
  if (normalizedUnit === "c" || normalizedUnit === "°c") digits = 1;
  if (normalizedUnit === "%") digits = 1;
  if (Number.isInteger(value)) digits = 0;

  const formatted = value.toFixed(digits);
  return unit ? `${formatted} ${unit}` : formatted;
};

export const isPrimarySensorReading = (reading: SensorReading): boolean => {
  const { suffix } = getSensorLabelMeta(reading.label);
  return suffix === null || suffix === "input";
};

/* RPM channels that have read above 0 at least once this session, keyed by
   adapter + label. Some drivers expose fan inputs that never report — hp_wmi
   on the HP 15s reads a constant 0 — and a channel that has never moved is
   "no data", not a stopped fan. Only a channel that has proven it can report
   renders 0 as "Off"; the rest show "—". The latch is monotonic, so
   re-renders and refetches only ever widen it. */
const liveFanChannels = new Set<string>();

export const observeFanChannel = (
  adapter: string,
  reading: SensorReading,
): boolean => {
  if (reading.value > 0) liveFanChannels.add(`${adapter}:${reading.label}`);
  return liveFanChannels.has(`${adapter}:${reading.label}`);
};

export const formatFanSensorValue = (
  value: number,
  hasReported: boolean,
): string => {
  if (value > 0) return `${Math.round(value)} RPM`;
  return hasReported ? "Off" : "—";
};
