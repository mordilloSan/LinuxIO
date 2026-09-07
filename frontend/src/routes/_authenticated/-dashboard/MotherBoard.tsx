import { useQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import {
  CACHE_TTL_MS,
  linuxio,
  type MonitoringLive,
  type SensorGroup,
} from "@/api";
import DashboardCard, { CardBadge } from "@/components/cards/DashboardCard";
import { isPrimarySensorReading } from "@/components/cards/sensorGroupHelpers";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import { useCapability } from "@/hooks/useCapabilities";

import DashboardStatRows from "./DashboardStatRows";
import { formatSensorLabel } from "./sensors";

const MotherboardTempBadge = () => {
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const [selectedSensor, setSelectedSensor] = useState<string | undefined>(
    undefined,
  );

  const selectBadge = useCallback(
    (live: MonitoringLive) => {
      const sensors = selectMotherboardTemperatures(live.sensors ?? []);
      const keys = Object.keys(sensors);
      const defaultMbSensor =
        keys.find((key) => key.startsWith("mb")) ?? keys[0];
      const effectiveSensor =
        selectedSensor && sensors[selectedSensor] !== undefined
          ? selectedSensor
          : defaultMbSensor;

      return {
        sensorKeys: keys,
        selected: effectiveSensor,
        text:
          effectiveSensor !== undefined &&
          sensors[effectiveSensor] !== undefined
            ? `${sensors[effectiveSensor]}°C`
            : "--°C",
      };
    },
    [selectedSensor],
  );

  const { data: badge } = useQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: selectBadge,
  });

  if (!lmSensorsAvailable || !badge || badge.sensorKeys.length === 0) {
    return <CardBadge icon="mdi:thermometer" text="N/A" />;
  }

  return (
    <CardBadge
      icon="mdi:thermometer"
      onSelect={setSelectedSensor}
      options={badge.sensorKeys.map((key) => ({
        value: key,
        label: formatSensorLabel(key),
      }))}
      selected={badge.selected}
      text={badge.text}
    />
  );
};

const selectMotherboardTemperatures = (
  groups: SensorGroup[],
): Record<string, number> => {
  const sensors: Record<string, number> = {};
  let index = 0;

  for (const group of groups) {
    const adapter = group.adapter.toLowerCase();
    if (/(coretemp|k10temp|zenpower|nvme|hdd|ssd|drive|gpu)/.test(adapter)) {
      continue;
    }
    for (const reading of group.readings) {
      const label = reading.label.toLowerCase();
      const unit = reading.unit.toLowerCase();
      const isInput = isPrimarySensorReading(reading);
      const isBoardReading =
        adapter.includes("acpitz") ||
        label.includes("board") ||
        label.includes("system") ||
        label.includes("systin") ||
        label.includes("mb") ||
        label.startsWith("temp1");
      if (
        reading.kind === "number" &&
        (unit === "c" || unit === "°c") &&
        isInput &&
        isBoardReading
      ) {
        sensors[`mb${index}`] = reading.value;
        index += 1;
      }
    }
  }
  return sensors;
};

const MotherboardStats = () => {
  const { data: motherboardInfo } = useQuery({
    ...linuxio.system.get_motherboard_info,
    staleTime: CACHE_TTL_MS.ONE_DAY,
  });

  const board = [
    motherboardInfo?.baseboard.manufacturer,
    motherboardInfo?.baseboard.model,
  ]
    .filter(Boolean)
    .join(" - ");
  const bios = [
    motherboardInfo?.bios.vendor,
    motherboardInfo?.bios.version
      ? `V.${motherboardInfo.bios.version}`
      : undefined,
  ]
    .filter(Boolean)
    .join(", ");

  return (
    <DashboardStatRows
      containerStyle={{ alignSelf: "auto", width: "100%", minWidth: 0 }}
      rows={[
        {
          label: "Board",
          value: board || "N/A",
        },
        {
          label: "BIOS",
          value: bios || "N/A",
        },
      ].map((row) => ({
        ...row,
        rowStyle: { minWidth: 0 },
        valueStyle: { minWidth: 0, flex: 1 },
        valueTitle: row.value,
      }))}
    />
  );
};

const MotherBoardInfo = () => (
  <DashboardCard
    avatarIcon="bi:motherboard"
    headerExtras={<MotherboardTempBadge />}
    stats={<MotherboardStats />}
    title="Motherboard"
  />
);

export default MotherBoardInfo;
