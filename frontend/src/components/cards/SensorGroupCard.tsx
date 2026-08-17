import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { linuxio, type SensorGroup, type SensorReading } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { CARD_PADDING_MD } from "@/theme/constants";
import { alpha } from "@/utils/color";

import {
  formatFanSensorValue,
  formatNumericSensorValue,
  isPrimarySensorReading,
  isTemperatureReading,
  observeFanChannel,
} from "./sensorGroupHelpers";

// ─── helpers ─────────────────────────────────────────────────────────────────

const getTempColor = (
  value: number,
  palette: { success: string; warning: string; error: string },
): string => {
  if (value < 50) return palette.success;
  if (value < 75) return palette.warning;
  return palette.error;
};

const isBooleanSensorReading = (reading: SensorReading): boolean =>
  reading.kind === "boolean";

const isNumericSensorReading = (reading: SensorReading): boolean =>
  reading.kind === "number";

const isFanReading = (reading: SensorReading): boolean =>
  isNumericSensorReading(reading) && reading.unit.toLowerCase() === "rpm";

const isVoltageReading = (reading: SensorReading): boolean =>
  isNumericSensorReading(reading) && reading.unit.toLowerCase() === "v";

const formatSensorValue = (reading: SensorReading): string => {
  if (isBooleanSensorReading(reading))
    return reading.value !== 0 ? "True" : "False";
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
      return reading.value !== 0 ? "error" : "success";
    return reading.value !== 0 ? "warning" : "default";
  }
  return unitChipColor(reading.unit);
};

// ─── component ───────────────────────────────────────────────────────────────

interface SensorGroupIdentity {
  adapter: string;
  sourceIndex: number;
}

interface SensorGroupCardProps extends SensorGroupIdentity {
  visibleReadingCount: number;
}

const selectSensorGroup =
  ({ adapter, sourceIndex }: SensorGroupIdentity) =>
  (groups: SensorGroup[]): SensorGroup | null => {
    const group = groups[sourceIndex];
    // The adapter check protects against a reordered source array between the
    // parent identity update and this cache observer's notification.
    return group?.adapter === adapter ? group : null;
  };

const SensorGroupCardLive = ({ adapter, sourceIndex }: SensorGroupIdentity) => {
  const theme = useAppTheme();
  const selectGroup = useMemo(
    () => selectSensorGroup({ adapter, sourceIndex }),
    [adapter, sourceIndex],
  );
  const { data: group } = useQuery({
    ...linuxio.system.get_sensor_info,
    refetchOnMount: false,
    select: selectGroup,
  });

  if (!group) return null;

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
    <>
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
              color={getTempColor(r.value, {
                success: theme.palette.success.main,
                warning: theme.palette.warning.main,
                error: theme.palette.error.main,
              })}
              key={`temp-${i}`}
              label={getSensorDisplayLabel(r)}
              percent={Math.min((r.value / 105) * 100, 100)}
              rightLabel={formatNumericSensorValue(r.value, r.unit)}
              tooltip={`${getSensorDisplayLabel(r)}: ${formatNumericSensorValue(r.value, r.unit)}`}
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
                  color={
                    r.value > 0
                      ? theme.palette.info.main
                      : alpha(theme.palette.text.secondary, 0.4)
                  }
                  height={14}
                  icon="mdi:fan"
                  width={14}
                />
                <AppTypography variant="caption">
                  {getSensorDisplayLabel(r)}
                </AppTypography>
              </div>
              <AppTypography
                style={{ fontVariantNumeric: "tabular-nums" }}
                variant="caption"
              >
                {formatFanSensorValue(r.value, observeFanChannel(adapter, r))}
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
                  color={theme.palette.success.main}
                  height={14}
                  icon="mdi:flash"
                  width={14}
                />
                <AppTypography variant="caption">
                  {getSensorDisplayLabel(r)}
                </AppTypography>
              </div>
              <AppTypography
                style={{ fontVariantNumeric: "tabular-nums" }}
                variant="caption"
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
              color={sensorChipColor(r)}
              label={formatSensorValue(r)}
              size="small"
              style={{ height: 20, fontSize: "0.65rem" }}
              variant="soft"
            />
          </div>
        ))}
    </>
  );
};

export const SensorGroupCardShell = ({
  adapter,
  sourceIndex,
  visibleReadingCount,
}: SensorGroupCardProps) => {
  const theme = useAppTheme();

  return (
    <FrostedCard
      accent
      hoverLift
      style={{ padding: CARD_PADDING_MD, height: "100%" }}
    >
      <CardIconHeader
        icon={
          <Icon
            color={theme.palette.primary.main}
            height={24}
            icon="mdi:chip"
            width={24}
          />
        }
        style={{ marginBottom: 8 }}
        subtitle={`${visibleReadingCount} reading${visibleReadingCount !== 1 ? "s" : ""}`}
        title={adapter}
      />
      <SensorGroupCardLive adapter={adapter} sourceIndex={sourceIndex} />
    </FrostedCard>
  );
};

export default SensorGroupCardShell;
