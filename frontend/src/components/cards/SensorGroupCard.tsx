import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

// ─── types ───────────────────────────────────────────────────────────────────

export interface SensorReading {
  label: string;
  value: number | boolean;
  kind: "number" | "boolean";
  unit: string;
}

export interface SensorGroup {
  adapter: string;
  readings: SensorReading[];
}

type NumericSensorReading = SensorReading & {
  kind: "number";
  value: number;
};

type BooleanSensorReading = SensorReading & {
  kind: "boolean";
  value: boolean;
};

// ─── helpers ─────────────────────────────────────────────────────────────────

const getTempColor = (
  value: number,
  palette: { success: string; warning: string; error: string },
): string => {
  if (value < 50) return palette.success;
  if (value < 75) return palette.warning;
  return palette.error;
};

const isNumericSensorReading = (
  reading: SensorReading,
): reading is NumericSensorReading =>
  reading.kind === "number" && typeof reading.value === "number";

const isBooleanSensorReading = (
  reading: SensorReading,
): reading is BooleanSensorReading =>
  reading.kind === "boolean" && typeof reading.value === "boolean";

export const isTemperatureReading = (
  reading: SensorReading,
): reading is NumericSensorReading => {
  if (!isNumericSensorReading(reading)) return false;
  const unit = reading.unit.toLowerCase();
  return unit === "c" || unit === "°c";
};

const isFanReading = (
  reading: SensorReading,
): reading is NumericSensorReading =>
  isNumericSensorReading(reading) && reading.unit.toLowerCase() === "rpm";

const isVoltageReading = (
  reading: SensorReading,
): reading is NumericSensorReading =>
  isNumericSensorReading(reading) && reading.unit.toLowerCase() === "v";

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

const formatSensorValue = (reading: SensorReading): string => {
  if (isBooleanSensorReading(reading)) return reading.value ? "True" : "False";
  if (isNumericSensorReading(reading))
    return formatNumericSensorValue(reading.value, reading.unit);
  return String(reading.value);
};

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

export const isPrimarySensorReading = (reading: SensorReading): boolean => {
  const { suffix } = getSensorLabelMeta(reading.label);
  return suffix === null || suffix === "input";
};

const getSensorDisplayLabel = (reading: SensorReading): string => {
  const { baseLabel, suffix, context } = getSensorLabelMeta(reading.label);
  if (suffix !== "input") return reading.label;
  if (!context) return baseLabel;
  return `${baseLabel} (${context})`;
};

const unitChipColor = (
  unit: string,
): "success" | "warning" | "info" | "default" => {
  const u = unit.toLowerCase();
  if (u === "c" || u === "°c") return "warning";
  if (u === "rpm") return "info";
  if (u === "v") return "success";
  return "default";
};

const sensorChipColor = (
  reading: SensorReading,
): "success" | "warning" | "info" | "default" | "error" => {
  if (isBooleanSensorReading(reading)) {
    if (reading.label.toLowerCase().includes("alarm"))
      return reading.value ? "error" : "success";
    return reading.value ? "warning" : "default";
  }
  return unitChipColor(reading.unit);
};

// ─── component ───────────────────────────────────────────────────────────────

const SensorGroupCard: React.FC<{ group: SensorGroup }> = ({ group }) => {
  const theme = useAppTheme();
  const visibleReadings = group.readings.filter(isPrimarySensorReading);
  const temps = visibleReadings.filter(isTemperatureReading);
  const fans = visibleReadings.filter(isFanReading);
  const voltages = visibleReadings.filter(isVoltageReading);
  const other = visibleReadings.filter((r) => {
    if (!isNumericSensorReading(r)) return true;
    const unit = r.unit.toLowerCase();
    return unit !== "c" && unit !== "°c" && unit !== "rpm" && unit !== "v";
  });

  return (
    <FrostedCard style={{ padding: 10, height: "100%" }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          marginBottom: 8,
        }}
      >
        <div
          style={{
            width: 36,
            height: 36,
            borderRadius: 8,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flexShrink: 0,
          }}
        >
          <Icon
            icon="mdi:chip"
            width={24}
            height={24}
            color={theme.palette.primary.main}
          />
        </div>
        <div style={{ minWidth: 0 }}>
          <AppTypography
            variant="subtitle2"
            fontWeight={700}
            style={{ lineHeight: 1.2 }}
            noWrap
          >
            {group.adapter}
          </AppTypography>
          <AppTypography variant="caption" color="text.secondary">
            {visibleReadings.length} reading
            {visibleReadings.length !== 1 ? "s" : ""}
          </AppTypography>
        </div>
      </div>

      {temps.length > 0 && (
        <div
          style={{
            marginBottom:
              temps.length > 0 && (fans.length > 0 || voltages.length > 0)
                ? 8
                : 0,
          }}
        >
          {temps.map((r, i) => (
            <MetricBar
              key={`temp-${i}`}
              label={getSensorDisplayLabel(r)}
              percent={Math.min((r.value / 105) * 100, 100)}
              color={getTempColor(r.value, {
                success: theme.palette.success.main,
                warning: theme.palette.warning.main,
                error: theme.palette.error.main,
              })}
              tooltip={`${getSensorDisplayLabel(r)}: ${formatNumericSensorValue(r.value, r.unit)}`}
              rightLabel={formatNumericSensorValue(r.value, r.unit)}
            />
          ))}
        </div>
      )}

      {fans.length > 0 && (
        <div
          style={{
            marginBottom: voltages.length > 0 || other.length > 0 ? 8 : 0,
          }}
        >
          {fans.map((r, i) => (
            <div
              key={`fan-${i}`}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                paddingBlock: 2,
                paddingInline: 2,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                <Icon
                  icon="mdi:fan"
                  width={14}
                  height={14}
                  color={
                    r.value > 0
                      ? theme.palette.info.main
                      : alpha(theme.palette.text.secondary, 0.4)
                  }
                />
                <AppTypography variant="caption">
                  {getSensorDisplayLabel(r)}
                </AppTypography>
              </div>
              <AppTypography
                variant="caption"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {formatNumericSensorValue(r.value, r.unit)}
              </AppTypography>
            </div>
          ))}
        </div>
      )}

      {voltages.length > 0 && (
        <div style={{ marginBottom: other.length > 0 ? 8 : 0 }}>
          {voltages.map((r, i) => (
            <div
              key={`volt-${i}`}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                paddingBlock: 2,
                paddingInline: 2,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                <Icon
                  icon="mdi:flash"
                  width={14}
                  height={14}
                  color={theme.palette.success.main}
                />
                <AppTypography variant="caption">
                  {getSensorDisplayLabel(r)}
                </AppTypography>
              </div>
              <AppTypography
                variant="caption"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {formatNumericSensorValue(r.value, r.unit)}
              </AppTypography>
            </div>
          ))}
        </div>
      )}

      {other.length > 0 &&
        other.map((r, i) => (
          <div
            key={`other-${i}`}
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              paddingBlock: 2,
              paddingInline: 2,
            }}
          >
            <AppTypography variant="caption">
              {getSensorDisplayLabel(r)}
            </AppTypography>
            <Chip
              size="small"
              label={formatSensorValue(r)}
              color={sensorChipColor(r)}
              variant="soft"
              style={{ height: 20, fontSize: "0.65rem" }}
            />
          </div>
        ))}
    </FrostedCard>
  );
};

export default SensorGroupCard;
