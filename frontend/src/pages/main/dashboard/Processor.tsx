import { Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useState } from "react";

import ProcessorGraph from "./ProcessorGraph";

import { linuxio } from "@/api";
import DashboardCard, {
  type SelectOption,
} from "@/components/cards/DashboardCard";
import ErrorMessage from "@/components/errors/Error";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useCapability } from "@/hooks/useCapabilities";

const Processor: React.FC = () => {
  const theme = useTheme();
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const {
    data: CPUInfo,
    isPending,
    isError,
  } = linuxio.system.get_cpu_info.useQuery({
    refetchInterval: 2000,
  });

  const [selectedSensor, setSelectedSensor] = useState<string | undefined>(
    undefined,
  );

  const averageCpuUsage = CPUInfo?.perCoreUsage?.length
    ? CPUInfo.perCoreUsage.reduce((sum, cpu) => sum + cpu, 0) /
      CPUInfo.perCoreUsage.length
    : 0;

  const temperatures = CPUInfo?.temperature ?? {};
  const temperatureKeys = Object.keys(temperatures);

  const formatSensorLabel = (key: string): string => {
    const match = key.match(/^([a-zA-Z]+)(\d+)$/);
    if (match)
      return `${match[1].charAt(0).toUpperCase() + match[1].slice(1)} ${match[2]}`;
    return key.charAt(0).toUpperCase() + key.slice(1);
  };

  const defaultSensor =
    temperatures["package"] !== undefined ? "package" : temperatureKeys[0];
  const effectiveSensor =
    selectedSensor && temperatures[selectedSensor] !== undefined
      ? selectedSensor
      : defaultSensor;

  const displayTemp =
    effectiveSensor !== undefined && temperatures[effectiveSensor] !== undefined
      ? `${temperatures[effectiveSensor].toFixed(1)}°C`
      : "--°C";

  const sensorOptions: SelectOption[] = temperatureKeys.map((key) => ({
    value: key,
    label: formatSensorLabel(key),
  }));

  const IconText = lmSensorsAvailable ? displayTemp : "N/A";

  const data = {
    title: "Processor",
    avatarIcon: "ph:cpu",
    stats2: isError ? (
      <ErrorMessage />
    ) : isPending ? (
      <ComponentLoader />
    ) : (
      <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
        <ProcessorGraph usage={averageCpuUsage} />
      </div>
    ),
    stats: (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          alignSelf: "flex-start",
          width: "fit-content",
        }}
      >
        {[
          { label: "CPU", value: CPUInfo?.modelName },
          {
            label: "Cores",
            value: CPUInfo ? `${CPUInfo.cores} Threads` : undefined,
          },
          {
            label: "Max Usage",
            value: `${Math.max(...(CPUInfo?.perCoreUsage || [0])).toFixed(0)}%`,
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
                  : "1px solid var(--mui-palette-divider)",
              gap: theme.spacing(1),
            }}
          >
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                fontSize: "0.62rem",
                flexShrink: 0,
              }}
            >
              {label}
            </Typography>
            <Typography variant="body2" fontWeight={500} noWrap>
              {value}
            </Typography>
          </div>
        ))}
      </div>
    ),
    icon_text: IconText,
    icon: "mdi:thermometer",
    ...(lmSensorsAvailable &&
      sensorOptions.length >= 1 && {
        iconTextSelectOptions: sensorOptions,
        selectedIconTextOption: effectiveSensor,
        onIconTextSelect: setSelectedSensor,
      }),
  };

  return <DashboardCard {...data} />;
};

export default Processor;
