import { useSuspenseQueries, useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { CACHE_TTL_MS, linuxio, type MonitoringLive } from "@/api";
import DashboardCard, { CardBadge } from "@/components/cards/DashboardCard";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import { useCapability } from "@/hooks/useCapabilities";

import DashboardStatRows from "./DashboardStatRows";
import ProcessorGraph from "./ProcessorGraph";
import { formatSensorLabel } from "./sensors";

const formatLoadAverage = (loadAverage?: readonly number[]): string =>
  loadAverage && loadAverage.length >= 3
    ? `${loadAverage[0].toFixed(2)} / ${loadAverage[1].toFixed(2)} / ${loadAverage[2].toFixed(2)}`
    : "N/A";

const CpuTempBadge = () => {
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const [selectedSensor, setSelectedSensor] = useState<string | undefined>(
    undefined,
  );

  const selectBadge = useCallback(
    (live: MonitoringLive) => {
      const values = live.cpu.temperatures ?? {};
      const keys = Object.keys(values);
      const defaultSensor =
        values["package"] !== undefined ? "package" : keys[0];
      const effectiveSensor =
        selectedSensor && values[selectedSensor] !== undefined
          ? selectedSensor
          : defaultSensor;

      return {
        sensorKeys: keys,
        selected: effectiveSensor,
        text:
          effectiveSensor !== undefined && values[effectiveSensor] !== undefined
            ? `${values[effectiveSensor].toFixed(1)}°C`
            : "--°C",
      };
    },
    [selectedSensor],
  );

  const { data: badge } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
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

const CpuStats = () => {
  const [{ data: CPUInfo }, { data: live }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_cpu_info, staleTime: CACHE_TTL_MS.ONE_DAY },
      {
        ...linuxio.monitoring.get_live,
        refetchInterval: DASHBOARD_REFETCH_FAST_MS,
      },
    ],
  });

  const averageCpuUsage = live.cpu.percent;
  const perCoreUsage = live.cpu.per_core_percent ?? [];
  const peakCpuUsage = perCoreUsage.length ? Math.max(...perCoreUsage) : 0;

  return (
    <DashboardStatRows
      rows={[
        { label: "CPU", value: CPUInfo?.modelName },
        {
          label: "Usage",
          value: `${averageCpuUsage.toFixed(0)}% (${peakCpuUsage.toFixed(0)}% peak)`,
        },
        {
          label: "Load",
          value: formatLoadAverage(live.cpu.load_average),
        },
        {
          label: "Cores",
          value: CPUInfo ? `${CPUInfo.cores} Threads` : undefined,
        },
      ]}
    />
  );
};

const CpuUsageGraph = () => {
  const { data: usage } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: (live) => live.cpu.percent,
  });

  return <ProcessorGraph usage={usage} />;
};

const Processor = () => (
  <DashboardCard
    avatarIcon="ph:cpu"
    headerExtras={<CpuTempBadge />}
    stats={<CpuStats />}
    stats2={
      <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
        <CpuUsageGraph />
      </div>
    }
    title="Processor"
  />
);

export default Processor;
