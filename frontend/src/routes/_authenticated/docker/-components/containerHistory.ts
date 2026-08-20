import type {
  MonitoringContainerHistoryPoint,
  MonitoringContainerSample,
} from "@/api";
import type { HistoryChartSeries } from "@/components/charts/HistoryAreaChart";

/**
 * Series builders for the per-container history charts. The agent samples
 * every running container on one collector tick, so a history point carries
 * every container alive at that moment and a container that was not running
 * is simply absent from it.
 */

/** Metric a stacked container chart plots. */
export type ContainerStackMetric = "cpu" | "memory";

/**
 * Stable identity for one container across samples. The agent reports its
 * short container ID, which prefixes the full ID Docker inventory uses; a row
 * that lost its ID through an older rollup still carries the name.
 */
export const containerSeriesKey = (
  sample: MonitoringContainerSample,
): string => (sample.id ? `id:${sample.id}` : `name:${sample.name}`);

/** True when this history sample is the container Docker knows by `fullId`. */
export const sampleMatchesContainer = (
  sample: MonitoringContainerSample,
  fullId: string,
  name: string,
): boolean => (sample.id ? fullId.startsWith(sample.id) : sample.name === name);

const metricValue = (
  sample: MonitoringContainerSample,
  metric: ContainerStackMetric,
): number =>
  metric === "cpu" ? sample.cpu_percent : sample.memory_mb * 1024 * 1024;

/**
 * Evenly spaced hues around the wheel, so neighbouring bands in a stack of any
 * size stay distinguishable.
 */
export const containerBandColor = (index: number, count: number): string =>
  `hsl(${Math.round((index * 360) / Math.max(count, 1))}, 70%, 55%)`;

/**
 * Split a filter box into lowercase terms. A container matches when any term
 * is a substring of its name, so "db cache" focuses both.
 */
const filterTerms = (filter: string): string[] =>
  filter
    .toLowerCase()
    .split(" ")
    .filter((term) => term.length > 0);

/**
 * One stacked band per container, ordered and coloured by total usage over the
 * window so the heaviest container sits at the bottom of the stack and keeps
 * its colour as the window changes.
 *
 * Containers filtered out are dimmed rather than dropped: the bands still add
 * up, so the top of the stack stays the true total while the filtered ones
 * keep the tooltip and the crosshair dots.
 */
export const containerStackSeries = (
  points: MonitoringContainerHistoryPoint[] | undefined,
  metric: ContainerStackMetric,
  filter: string,
): HistoryChartSeries[] => {
  const history = points ?? [];

  // One lookup per point, then rank by summed value keeping the most recent
  // name each container used. Building the maps up front keeps the per-band
  // walk below linear rather than rescanning every point's container list.
  const byKey = history.map(
    (point) =>
      new Map(
        point.containers.map((sample) => [containerSeriesKey(sample), sample]),
      ),
  );
  const totals = new Map<string, { name: string; total: number }>();
  for (const [key, sample] of byKey.flatMap((map) => [...map])) {
    const entry = totals.get(key) ?? { name: sample.name, total: 0 };
    entry.name = sample.name;
    entry.total += metricValue(sample, metric);
    totals.set(key, entry);
  }
  const ranked = [...totals.entries()].sort(
    ([, a], [, b]) => b.total - a.total || a.name.localeCompare(b.name),
  );

  const terms = filterTerms(filter);
  return ranked.map(([key, { name }], index) => ({
    label: name,
    color: containerBandColor(index, ranked.length),
    dimmed:
      terms.length > 0 &&
      !terms.some((term) => name.toLowerCase().includes(term)),
    points: history.map((point, i) => {
      // A container missing from a sample was not running then, so it
      // contributes nothing to that moment's stack.
      const sample = byKey[i].get(key);
      return {
        t: point.captured_at_ms,
        v: sample ? metricValue(sample, metric) : 0,
      };
    }),
  }));
};

/**
 * The samples belonging to one container, dropping the points taken while it
 * was not running so its detail charts leave those stretches empty instead of
 * drawing an idle line it never had.
 */
export const containerSamples = (
  points: MonitoringContainerHistoryPoint[] | undefined,
  fullId: string,
  name: string,
): { t: number; sample: MonitoringContainerSample }[] =>
  (points ?? []).flatMap((point) => {
    const sample = point.containers.find((candidate) =>
      sampleMatchesContainer(candidate, fullId, name),
    );
    return sample ? [{ t: point.captured_at_ms, sample }] : [];
  });

/**
 * Block I/O is reported by a separate agent plugin an operator can disable and
 * older agents do not have at all, so the card says "unavailable" rather than
 * drawing a flat zero line when no sample carries it.
 */
export const hasBlockIO = (
  samples: { sample: MonitoringContainerSample }[],
): boolean =>
  samples.some(({ sample }) => sample.read_bytes_per_sec !== undefined);
