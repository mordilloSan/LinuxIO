import React, { useState } from "react";

import { linuxio } from "@/api";
import DashboardCard, {
  type SelectOption,
} from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";

const MotherBoardInfo: React.FC = () => {
  const theme = useAppTheme();
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const { data: motherboardInfo } =
    linuxio.system.get_motherboard_info.useQuery({
      refetchInterval: 50000,
    });

  const visibleDetails = motherboardInfo ? (
    <div
      style={{ display: "flex", flexDirection: "column", width: "fit-content" }}
    >
      {[
        {
          label: "Board",
          value: `${motherboardInfo.baseboard.manufacturer} - ${motherboardInfo.baseboard.model}`,
        },
        {
          label: "BIOS",
          value: `${motherboardInfo.bios.vendor}, V.${motherboardInfo.bios.version}`,
        },
      ].map(({ label, value }, index, rows) => (
        <div
          key={label}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "flex-start",
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === rows.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
            gap: theme.spacing(1),
          }}
        >
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </AppTypography>
          <AppTypography variant="body2" fontWeight={500} noWrap>
            {value}
          </AppTypography>
        </div>
      ))}
    </div>
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
      title="Motherboard"
      stats={visibleDetails}
      icon_text={IconText}
      icon="mdi:thermometer"
      avatarIcon="bi:motherboard"
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
