import { useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { type CPUInfoResponse, linuxio } from "@/api";
import DashboardCard, { CardBadge } from "@/components/cards/DashboardCard";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import { useCapability } from "@/hooks/useCapabilities";

import DashboardStatRows from "./DashboardStatRows";
import ProcessorGraph from "./ProcessorGraph";
import { formatSensorLabel } from "./sensors";

const formatLoadAverage = (loadAverage?: {
  load1: number;
  load5: number;
  load15: number;
}): string =>
  loadAverage
    ? `${loadAverage.load1.toFixed(2)} / ${loadAverage.load5.toFixed(2)} / ${loadAverage.load15.toFixed(2)}`
    : "N/A";

const selectAverageUsage = (CPUInfo: CPUInfoResponse): number =>
  CPUInfo?.perCoreUsage?.length
    ? CPUInfo.perCoreUsage.reduce((sum, cpu) => sum + cpu, 0) /
      CPUInfo.perCoreUsage.length
    : 0;

const CpuTempBadge = () => {
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const [selectedSensor, setSelectedSensor] = useState<string | undefined>(
    undefined,
  );

  const selectBadge = useCallback(
    (CPUInfo: CPUInfoResponse) => {
      const temperatures = CPUInfo?.temperature ?? {};
      const keys = Object.keys(temperatures);
      const defaultSensor =
        temperatures["package"] !== undefined ? "package" : keys[0];
      const effectiveSensor =
        selectedSensor && temperatures[selectedSensor] !== undefined
          ? selectedSensor
          : defaultSensor;

      return {
        sensorKeys: keys,
        selected: effectiveSensor,
        text:
          effectiveSensor !== undefined &&
          temperatures[effectiveSensor] !== undefined
            ? `${temperatures[effectiveSensor].toFixed(1)}°C`
            : "--°C",
      };
    },
    [selectedSensor],
  );

  const { data: badge } = useSuspenseQuery({
    ...linuxio.system.get_cpu_info,
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
  const { data: CPUInfo } = useSuspenseQuery({
    ...linuxio.system.get_cpu_info,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
  });

  const averageCpuUsage = selectAverageUsage(CPUInfo);
  const peakCpuUsage = Math.max(...(CPUInfo?.perCoreUsage || [0]));

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
          value: formatLoadAverage(CPUInfo?.loadAverage),
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
    ...linuxio.system.get_cpu_info,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: selectAverageUsage,
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
