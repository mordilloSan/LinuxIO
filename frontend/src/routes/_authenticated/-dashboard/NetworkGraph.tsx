import { useQueryClient } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio } from "@/api";
import LiveChart from "@/components/charts/LiveChart";
import type { LiveSeriesPoint } from "@/components/charts/useLiveSeries";
import { formatThroughput } from "@/utils/formatters";

interface NetworkGraphProps {
  interfaceName: string;
  /** kB/s */
  rx: number;
  /** kB/s */
  tx: number;
}

// The series hold kB/s; the formatter wants bytes/s.
const formatKbPerSec = (kbPerSec: number) => formatThroughput(kbPerSec * 1024);

const NetworkGraph = ({ interfaceName, rx, tx }: NetworkGraphProps) => {
  const queryClient = useQueryClient();
  const rxId = `network:rx:${interfaceName}`;
  const txId = `network:tx:${interfaceName}`;

  return (
    <LiveChart
      fetchHistory={async (request) => {
        // One-shot backfill: the request carries a rolling from_ms, so caching
        // the entry would only pollute the cache.
        const points = await queryClient.query({
          ...linuxio.monitoring.get_network_history(request),
          staleTime: CACHE_TTL_MS.NONE,
          gcTime: CACHE_TTL_MS.NONE,
        });
        // History arrives in bytes/s; the series are kB/s.
        const rxPoints: LiveSeriesPoint[] = [];
        const txPoints: LiveSeriesPoint[] = [];
        for (const point of points) {
          const rates = point.interfaces?.[interfaceName];
          if (!rates) continue;
          rxPoints.push({
            t: point.captured_at_ms,
            v: rates.recv_bytes_per_sec / 1024,
          });
          txPoints.push({
            t: point.captured_at_ms,
            v: rates.sent_bytes_per_sec / 1024,
          });
        }
        return { [rxId]: rxPoints, [txId]: txPoints };
      }}
      legend
      series={[
        {
          color: "rx",
          format: formatKbPerSec,
          id: rxId,
          label: "Rx",
          value: rx,
        },
        {
          color: "tx",
          format: formatKbPerSec,
          id: txId,
          label: "Tx",
          value: tx,
        },
      ]}
      streamDelayMs={1000}
    />
  );
};

export default NetworkGraph;
