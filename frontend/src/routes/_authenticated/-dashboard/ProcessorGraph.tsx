import { useQueryClient } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio } from "@/api";
import LiveChart from "@/components/charts/LiveChart";
import { formatPercent } from "@/utils/formatters";

interface CpuGraphProps {
  usage: number;
}

const SERIES_ID = "cpu:usage";

const CpuGraph = ({ usage }: CpuGraphProps) => {
  const queryClient = useQueryClient();

  return (
    <LiveChart
      fetchHistory={async (request) => {
        // One-shot backfill: the request carries a rolling from_ms, so caching
        // the entry would only pollute the cache.
        const points = await queryClient.query({
          ...linuxio.monitoring.get_cpu_history(request),
          staleTime: CACHE_TTL_MS.NONE,
          gcTime: CACHE_TTL_MS.NONE,
        });
        return {
          [SERIES_ID]: points.map((point) => ({
            t: point.captured_at_ms,
            v: point.usage_percent,
          })),
        };
      }}
      maxValue={100}
      series={[
        {
          color: "primary",
          format: formatPercent,
          id: SERIES_ID,
          value: usage,
        },
      ]}
      streamDelayMs={2000}
    />
  );
};

export default CpuGraph;
