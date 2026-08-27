import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { linuxio, type MonitoringNetworkHistoryPoint } from "@/api";
import HistoryAreaChart, {
  type HistoryChartSeries,
} from "@/components/charts/HistoryAreaChart";
import {
  historyCardMessage,
  historyRequest,
  HistoryCardBody,
  HistoryCardShell,
  rangeById,
  RangeSelect,
  useHistoryTimestampFormatter,
} from "@/components/charts/HistoryCard";
import type { HistoryRangeId } from "@/components/charts/historyRanges";
import { useCapability } from "@/hooks/useCapabilities";
import { formatThroughput } from "@/utils/formaters";

/**
 * Both directions read positive here: unlike the live chart this is an area
 * chart with a zero baseline, so there is no mirrored axis to hang a negative
 * series off.
 */
export const networkHistorySeries = (
  points: MonitoringNetworkHistoryPoint[] | undefined,
  name: string,
  colors: { rx: string; tx: string },
): HistoryChartSeries[] => {
  // A sample taken before this interface existed carries no entry for it.
  // Dropping those points leaves the left side of the window empty instead of
  // drawing an idle line the interface never had.
  const samples = (points ?? []).flatMap((point) => {
    const rates = point.interfaces?.[name];
    return rates ? [{ rates, t: point.captured_at_ms }] : [];
  });

  return [
    {
      label: "Sent",
      color: colors.tx,
      points: samples.map(({ rates, t }) => ({
        t,
        v: rates.sent_bytes_per_sec,
      })),
    },
    {
      label: "Received",
      color: colors.rx,
      points: samples.map(({ rates, t }) => ({
        t,
        v: rates.recv_bytes_per_sec,
      })),
    },
  ];
};

/**
 * Rolled-up history for one interface, from the same monitoring store the live
 * chart backfills from.
 */
const NetworkTrafficHistoryLive = ({
  name,
  rangeId,
}: {
  name: string;
  rangeId: HistoryRangeId;
}) => {
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_network_history(historyRequest(range)),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });

  const series = useMemo(
    () =>
      networkHistorySeries(data, name, {
        rx: "var(--app-chart-rx)",
        tx: "var(--app-chart-tx)",
      }),
    [data, name],
  );

  const message = historyCardMessage(
    series[0].points,
    isLoading,
    error,
    isEnabled,
    reason,
  );

  return (
    <HistoryCardBody message={message}>
      <HistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatThroughput}
        series={series}
        windowMs={range.windowMs}
      />
    </HistoryCardBody>
  );
};

const NetworkTrafficHistoryCard = ({ name }: { name: string }) => {
  // One chart, so the range is this card's own state rather than a shared
  // page-level selection the way the four hardware cards need.
  const [rangeId, setRangeId] = useState<HistoryRangeId>("1h");

  return (
    <HistoryCardShell
      avatarIcon="mdi:chart-areaspline"
      headerRight={<RangeSelect onChange={setRangeId} value={rangeId} />}
      title="Traffic History"
    >
      <NetworkTrafficHistoryLive name={name} rangeId={rangeId} />
    </HistoryCardShell>
  );
};

export default NetworkTrafficHistoryCard;
