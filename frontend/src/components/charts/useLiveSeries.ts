import { useEffect, useEffectEvent, useMemo } from "react";
import type { TimeSeries } from "smoothie";

import type { MonitoringHistoryRequest } from "@/api";
import {
  backfillLiveSeries,
  getLiveSeries,
  LIVE_BACKFILL_WINDOW_MS,
  LIVE_STALE_AFTER_MS,
  resetStaleLiveSeries,
} from "@/components/charts/liveSeriesStore";
import { useCapability } from "@/hooks/useCapabilities";

export interface LiveSeriesPoint {
  /** Epoch milliseconds. */
  t: number;
  v: number;
}

/**
 * The acquire → stale-check → backfill plumbing shared by the dashboard live
 * charts. Returns the persistent TimeSeries for `ids` (same order); buffers
 * with no usable data are seeded from go-monitoring history via fetchHistory,
 * which receives the ready-made request and resolves backfill points per
 * series id. One fetch seeds all of a chart's series.
 */
export function useLiveSeries(
  ids: readonly string[],
  fetchHistory: (
    request: MonitoringHistoryRequest,
  ) => Promise<Record<string, LiveSeriesPoint[]>>,
): TimeSeries[] {
  const { isEnabled: monitoringEnabled } = useCapability("monitoringAvailable");

  // Keyed on content so callers can pass a fresh array literal every render.
  const idsKey = ids.join("\n");
  const seriesList = useMemo(
    () => idsKey.split("\n").map((id) => getLiveSeries(id)),
    [idsKey],
  );

  const fetchHistoryEvent = useEffectEvent(fetchHistory);

  useEffect(() => {
    if (!monitoringEnabled) return;
    const staleIds = idsKey
      .split("\n")
      .filter((id) => resetStaleLiveSeries(id, LIVE_STALE_AFTER_MS));
    if (staleIds.length === 0) return;
    let cancelled = false;
    fetchHistoryEvent({
      resolution: "1m",
      from_ms: Date.now() - LIVE_BACKFILL_WINDOW_MS,
      limit: 40,
    })
      .then((pointsById) => {
        if (cancelled) return;
        for (const id of staleIds) {
          backfillLiveSeries(id, pointsById[id] ?? []);
        }
      })
      .catch(() => {
        // Best-effort seed; live samples still stream in.
      });
    return () => {
      cancelled = true;
    };
  }, [idsKey, monitoringEnabled]);

  return seriesList;
}
