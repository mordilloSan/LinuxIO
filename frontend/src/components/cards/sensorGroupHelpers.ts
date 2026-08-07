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
  if (normalizedUnit === "rpm")
    return value > 0 ? `${Math.round(value)} RPM` : "Off";

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
