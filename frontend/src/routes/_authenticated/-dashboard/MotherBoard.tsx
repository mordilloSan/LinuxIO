import { useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { linuxio, type MotherboardInfo } from "@/api";
import DashboardCard, { CardBadge } from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";

import DashboardStatRows from "./DashboardStatRows";
import { formatSensorLabel } from "./sensors";

const REFETCH_INTERVAL_MS = 50000;

const MotherboardTempBadge = () => {
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const [selectedSensor, setSelectedSensor] = useState<string | undefined>(
    undefined,
  );

  const selectBadge = useCallback(
    (motherboardInfo: MotherboardInfo) => {
      const sensors = motherboardInfo?.temperatures?.sensors ?? {};
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

  const { data: badge } = useSuspenseQuery({
    ...linuxio.system.get_motherboard_info,
    refetchInterval: REFETCH_INTERVAL_MS,
    select: selectBadge,
  });

  if (!lmSensorsAvailable) {
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

const MotherboardStats = () => {
  const { data: motherboardInfo } = useSuspenseQuery({
    ...linuxio.system.get_motherboard_info,
    refetchInterval: REFETCH_INTERVAL_MS,
  });

  if (!motherboardInfo) {
    return (
      <AppTypography variant="body2">
        No system information available.
      </AppTypography>
    );
  }

  return (
    <DashboardStatRows
      containerStyle={{ alignSelf: "auto", width: "100%", minWidth: 0 }}
      rows={[
        {
          label: "Board",
          value: `${motherboardInfo.baseboard.manufacturer} - ${motherboardInfo.baseboard.model}`,
        },
        {
          label: "BIOS",
          value: `${motherboardInfo.bios.vendor}, V.${motherboardInfo.bios.version}`,
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
