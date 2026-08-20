import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { linuxio } from "@/api";
import {
  historyCardMessage,
  historyRequest,
  HistoryCardBody,
  HistoryCardShell,
  HistoryHoverProvider,
  rangeById,
  RangeSelect,
  SynchronizedHistoryAreaChart,
  useHistoryTimestampFormatter,
} from "@/components/charts/HistoryCard";
import type { HistoryRangeId } from "@/components/charts/historyRanges";
import AppGrid from "@/components/ui/AppGrid";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";
import { DASHBOARD_CARD_SPACING } from "@/theme/constants";
import { formatFileSize, formatThroughput } from "@/utils/formaters";

import {
  containerSamples,
  containerStackSeries,
  hasBlockIO,
  type ContainerStackMetric,
} from "./containerHistory";

const formatPercent = (value: number): string => `${value.toFixed(1)}%`;
const formatPercentTick = (value: number): string => `${Math.round(value)}%`;
const formatBytes = (value: number): string => formatFileSize(value, 1);

/**
 * The one container-history query every chart on a page shares. TanStack Query
 * dedupes the identical key, so the dashboard's two stacks — or a selected
 * container's four charts — cost a single request per range.
 */
const useContainerHistory = (rangeId: HistoryRangeId) => {
  const range = rangeById(rangeId);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_container_history(historyRequest(range)),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });
  return { data, isLoading, error, isEnabled, range, reason };
};

// ─── Stacked dashboard charts ────────────────────────────────────────────────

interface ContainerStackLiveProps {
  filter: string;
  metric: ContainerStackMetric;
  rangeId: HistoryRangeId;
}

const ContainerStackLive = ({
  filter,
  metric,
  rangeId,
}: ContainerStackLiveProps) => {
  const { data, isLoading, error, isEnabled, range, reason } =
    useContainerHistory(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const series = useMemo(
    () => containerStackSeries(data, metric, filter),
    [data, filter, metric],
  );
  const message = historyCardMessage(
    series,
    isLoading,
    error,
    isEnabled,
    reason,
  );

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTick={metric === "cpu" ? formatPercentTick : formatBytes}
        formatTimestamp={formatTimestamp}
        formatValue={metric === "cpu" ? formatPercent : formatBytes}
        series={series}
        stacked
        windowMs={range.windowMs}
        // The agent reports container CPU as a share of the whole host, so the
        // stack tops out at one fully busy machine.
        yMax={metric === "cpu" ? 100 : undefined}
      />
    </HistoryCardBody>
  );
};

/**
 * Per-container CPU and memory as one stacked band each, with a filter that
 * fades the containers you are not looking at instead of removing them, so the
 * top of the stack keeps reading as the true total.
 */
export const DockerMonitoringSection = () => {
  const [rangeId, setRangeId] = useState<HistoryRangeId>("1h");
  const [filter, setFilter] = useState("");

  return (
    <HistoryHoverProvider>
      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          marginBottom: 8,
        }}
      >
        <div style={{ maxWidth: 260, width: "100%" }}>
          <AppHeaderSearch
            aria-label="Filter containers in monitoring charts"
            onChange={setFilter}
            placeholder="Filter containers…"
            value={filter}
          />
        </div>
      </div>
      <AppGrid
        alignItems="stretch"
        container
        spacing={DASHBOARD_CARD_SPACING}
        style={{ marginBottom: 8 }}
      >
        <AppGrid size={{ xs: 12, lg: 6 }}>
          <HistoryCardShell
            avatarIcon="ph:cpu"
            headerRight={<RangeSelect onChange={setRangeId} value={rangeId} />}
            title="Container CPU"
          >
            <ContainerStackLive
              filter={filter}
              metric="cpu"
              rangeId={rangeId}
            />
          </HistoryCardShell>
        </AppGrid>
        <AppGrid size={{ xs: 12, lg: 6 }}>
          <HistoryCardShell
            avatarIcon="la:memory"
            headerRight={<RangeSelect onChange={setRangeId} value={rangeId} />}
            title="Container Memory"
          >
            <ContainerStackLive
              filter={filter}
              metric="memory"
              rangeId={rangeId}
            />
          </HistoryCardShell>
        </AppGrid>
      </AppGrid>
    </HistoryHoverProvider>
  );
};

// ─── Selected container charts ───────────────────────────────────────────────

interface ContainerDetailLiveProps {
  containerId: string;
  name: string;
  rangeId: HistoryRangeId;
}

/** Shared plumbing for the four charts of one selected container. */
const useContainerDetailHistory = ({
  containerId,
  name,
  rangeId,
}: ContainerDetailLiveProps) => {
  const { data, isLoading, error, isEnabled, range, reason } =
    useContainerHistory(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const samples = useMemo(
    () => containerSamples(data, containerId, name),
    [containerId, data, name],
  );
  return {
    formatTimestamp,
    message: historyCardMessage(samples, isLoading, error, isEnabled, reason),
    range,
    samples,
  };
};

const ContainerCPULive = (props: ContainerDetailLiveProps) => {
  const theme = useAppTheme();
  const { formatTimestamp, message, range, samples } =
    useContainerDetailHistory(props);
  const cpuColor = theme.palette.primary.main;
  const series = useMemo(
    () => [
      {
        label: "CPU",
        color: cpuColor,
        points: samples.map(({ sample, t }) => ({ t, v: sample.cpu_percent })),
      },
    ],
    [cpuColor, samples],
  );

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTick={formatPercentTick}
        formatTimestamp={formatTimestamp}
        formatValue={formatPercent}
        series={series}
        windowMs={range.windowMs}
      />
    </HistoryCardBody>
  );
};

const ContainerMemoryLive = (props: ContainerDetailLiveProps) => {
  const theme = useAppTheme();
  const { formatTimestamp, message, range, samples } =
    useContainerDetailHistory(props);
  const memoryColor = theme.palette.primary.main;
  const series = useMemo(
    () => [
      {
        label: "Memory",
        color: memoryColor,
        points: samples.map(({ sample, t }) => ({
          t,
          v: sample.memory_mb * 1024 * 1024,
        })),
      },
    ],
    [memoryColor, samples],
  );

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatBytes}
        series={series}
        windowMs={range.windowMs}
      />
    </HistoryCardBody>
  );
};

const ContainerNetworkLive = (props: ContainerDetailLiveProps) => {
  const theme = useAppTheme();
  const { formatTimestamp, message, range, samples } =
    useContainerDetailHistory(props);
  const rxColor = theme.chart.rx;
  const txColor = theme.chart.tx;
  const series = useMemo(
    () => [
      {
        label: "Sent",
        color: txColor,
        points: samples.map(({ sample, t }) => ({
          t,
          v: sample.sent_bytes_per_sec,
        })),
      },
      {
        label: "Received",
        color: rxColor,
        points: samples.map(({ sample, t }) => ({
          t,
          v: sample.recv_bytes_per_sec,
        })),
      },
    ],
    [rxColor, samples, txColor],
  );

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatThroughput}
        series={series}
        windowMs={range.windowMs}
      />
    </HistoryCardBody>
  );
};

const ContainerBlockIOLive = (props: ContainerDetailLiveProps) => {
  const theme = useAppTheme();
  const { formatTimestamp, message, range, samples } =
    useContainerDetailHistory(props);
  const readColor = theme.chart.rx;
  const writeColor = theme.chart.tx;
  const series = useMemo(
    () => [
      {
        label: "Read",
        color: readColor,
        points: samples.map(({ sample, t }) => ({
          t,
          v: sample.read_bytes_per_sec ?? 0,
        })),
      },
      {
        label: "Write",
        color: writeColor,
        points: samples.map(({ sample, t }) => ({
          t,
          v: sample.write_bytes_per_sec ?? 0,
        })),
      },
    ],
    [readColor, samples, writeColor],
  );

  // Say so rather than drawing a flat zero line the container never had.
  const blockIOMessage =
    message ??
    (hasBlockIO(samples)
      ? null
      : "Block I/O needs the monitoring agent's container_telemetry plugin.");

  return (
    <HistoryCardBody message={blockIOMessage}>
      <SynchronizedHistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatThroughput}
        series={series}
        windowMs={range.windowMs}
      />
    </HistoryCardBody>
  );
};

/**
 * CPU, memory, network, and block I/O history for the selected container, all
 * reading the one shared query and sharing a crosshair so a spike in one chart
 * can be read off the others.
 */
export const ContainerHistoryCards = ({
  containerId,
  name,
}: {
  containerId: string;
  name: string;
}) => {
  const [rangeId, setRangeId] = useState<HistoryRangeId>("1h");
  const live = { containerId, name, rangeId };

  return (
    <HistoryHoverProvider>
      <AppGrid alignItems="stretch" container spacing={DASHBOARD_CARD_SPACING}>
        <AppGrid size={{ xs: 12, md: 6, xl: 3 }}>
          <HistoryCardShell
            avatarIcon="ph:cpu"
            headerRight={<RangeSelect onChange={setRangeId} value={rangeId} />}
            title="CPU"
          >
            <ContainerCPULive {...live} />
          </HistoryCardShell>
        </AppGrid>
        <AppGrid size={{ xs: 12, md: 6, xl: 3 }}>
          <HistoryCardShell
            avatarIcon="la:memory"
            headerRight={<RangeSelect onChange={setRangeId} value={rangeId} />}
            title="Memory"
          >
            <ContainerMemoryLive {...live} />
          </HistoryCardShell>
        </AppGrid>
        <AppGrid size={{ xs: 12, md: 6, xl: 3 }}>
          <HistoryCardShell
            avatarIcon="mdi:ethernet"
            headerRight={<RangeSelect onChange={setRangeId} value={rangeId} />}
            title="Network"
          >
            <ContainerNetworkLive {...live} />
          </HistoryCardShell>
        </AppGrid>
        <AppGrid size={{ xs: 12, md: 6, xl: 3 }}>
          <HistoryCardShell
            avatarIcon="mdi:harddisk"
            headerRight={<RangeSelect onChange={setRangeId} value={rangeId} />}
            title="Block I/O"
          >
            <ContainerBlockIOLive {...live} />
          </HistoryCardShell>
        </AppGrid>
      </AppGrid>
    </HistoryHoverProvider>
  );
};
