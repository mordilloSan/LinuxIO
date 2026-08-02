import { useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import DashboardCard, {
  type SelectOption,
} from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";

import DashboardStatRows from "./DashboardStatRows";

const MotherBoardInfo = () => {
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const { data: motherboardInfo } = useSuspenseQuery(
    linuxio.system.get_motherboard_info.queryOptions({
      refetchInterval: 50000,
    }),
  );

  const visibleDetails = motherboardInfo ? (
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
  ) : (
    <AppTypography variant="body2">
      No system information available.
    </AppTypography>
  );

  const sensors = motherboardInfo?.temperatures?.sensors ?? {};
  const sensorKeys = Object.keys(sensors);
  const [selectedSensor, setSelectedSensor] = useState<string | undefined>(
    undefined,
  );

  const formatSensorLabel = (key: string): string => {
    const match = key.match(/^([a-zA-Z]+)(\d+)$/);
    if (match)
      return `${match[1].charAt(0).toUpperCase() + match[1].slice(1)} ${match[2]}`;
    return key.charAt(0).toUpperCase() + key.slice(1);
  };

  const defaultMbSensor =
    sensorKeys.find((k) => k.startsWith("mb")) ?? sensorKeys[0];
  const effectiveSensor =
    selectedSensor && sensors[selectedSensor] !== undefined
      ? selectedSensor
      : defaultMbSensor;

  const IconText = lmSensorsAvailable
    ? effectiveSensor !== undefined && sensors[effectiveSensor] !== undefined
      ? `${sensors[effectiveSensor]}°C`
      : "--°C"
    : "N/A";

  const sensorOptions: SelectOption[] = sensorKeys.map((key) => ({
    value: key,
    label: formatSensorLabel(key),
  }));

  return (
    <DashboardCard
      avatarIcon="bi:motherboard"
      icon="mdi:thermometer"
      icon_text={IconText}
      stats={visibleDetails}
      title="Motherboard"
      {...(lmSensorsAvailable &&
        sensorOptions.length >= 1 && {
          iconTextSelectOptions: sensorOptions,
          selectedIconTextOption: effectiveSensor,
          onIconTextSelect: setSelectedSensor,
        })}
    />
  );
};

export default MotherBoardInfo;
