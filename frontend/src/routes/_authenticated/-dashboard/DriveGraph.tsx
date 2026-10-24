import { useQueryClient } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio } from "@/api";
import LiveChart from "@/components/charts/LiveChart";
import { formatThroughput } from "@/utils/formatters";

interface DriveGraphProps {
  readBytesPerSec: number;
  writeBytesPerSec: number;
}

const READ_ID = "disk:read";
const WRITE_ID = "disk:write";

const DriveGraph = ({ readBytesPerSec, writeBytesPerSec }: DriveGraphProps) => {
  const queryClient = useQueryClient();

  return (
    <LiveChart
      fetchHistory={async (request) => {
        // One-shot backfill: the request carries a rolling from_ms, so caching
        // the entry would only pollute the cache.
        const points = await queryClient.query({
          ...linuxio.monitoring.get_diskio_history(request),
          staleTime: CACHE_TTL_MS.NONE,
          gcTime: CACHE_TTL_MS.NONE,
        });
        return {
          [READ_ID]: points.map((point) => ({
            t: point.captured_at_ms,
            v: point.read_bytes_per_sec,
          })),
          [WRITE_ID]: points.map((point) => ({
            t: point.captured_at_ms,
            v: point.write_bytes_per_sec,
          })),
        };
      }}
      legend
      series={[
        {
          color: "rx",
          format: formatThroughput,
          id: READ_ID,
          label: "Read",
          value: readBytesPerSec,
        },
        {
          color: "tx",
          format: formatThroughput,
          id: WRITE_ID,
          label: "Write",
          value: writeBytesPerSec,
        },
      ]}
      streamDelayMs={1000}
    />
  );
};

export default DriveGraph;
