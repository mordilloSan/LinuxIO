import { useQuery } from "@tanstack/react-query";

import { linuxio, type Count, type MonitoringLive } from "@/api";
import AppPaper from "@/components/ui/AppPaper";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize, formatUptimeClock } from "@/utils/formatters";

import ProcessVirtualTable from "./ProcessVirtualTable";
import { PROCESS_REFRESH_INTERVAL_MS } from "./ProgramsTab";

import "./htop.css";

interface MeterSegment {
  tone: "used" | "buffers" | "cache" | "swap";
  value: number;
}

const percentOf = (part: number, total: number) =>
  total > 0 ? Math.min(Math.max((part / total) * 100, 0), 100) : 0;

const Meter = ({
  label,
  segments,
  text,
}: {
  label: string;
  segments: MeterSegment[];
  text: string;
}) => {
  const total = segments.reduce((sum, segment) => sum + segment.value, 0);
  return (
    <div className="htop-meter">
      <span className="htop-meter__label">{label}</span>
      <div
        aria-label={label}
        aria-valuemax={100}
        aria-valuemin={0}
        aria-valuenow={Math.round(total)}
        aria-valuetext={text}
        className="htop-meter__track"
        role="meter"
      >
        {segments.map((segment) => (
          <span
            className={`htop-meter__fill htop-meter__fill--${segment.tone}`}
            key={segment.tone}
            style={{ width: `${segment.value}%` }}
          />
        ))}
        <span className="htop-meter__text">{text}</span>
      </div>
    </div>
  );
};

const formatSize = (bytes: number) => formatFileSize(bytes, 1, "0");

export const HtopHeader = ({
  count,
  live,
}: {
  count?: Count;
  live: MonitoringLive;
}) => {
  const { cpu, memory } = live;
  const used = percentOf(memory.used_bytes, memory.total_bytes);
  const buffers = Math.min(
    percentOf(memory.buffers_bytes, memory.total_bytes),
    100 - used,
  );
  const cache = Math.min(
    percentOf(memory.cached_bytes, memory.total_bytes),
    100 - used - buffers,
  );
  const swapUsed = Math.max(
    memory.swap_total_bytes - memory.swap_free_bytes,
    0,
  );

  return (
    <AppPaper className="htop-header" variant="outlined">
      <div className="htop-header__cores">
        {cpu.per_core_percent.map((percent, core) => (
          <Meter
            // Cores are positional: index is their identity.
            key={core}
            label={String(core)}
            segments={[{ tone: "used", value: percentOf(percent, 100) }]}
            text={`${percent.toFixed(1)}%`}
          />
        ))}
      </div>
      <div className="htop-header__summary">
        <div className="htop-header__memory">
          <Meter
            label="Mem"
            segments={[
              { tone: "used", value: used },
              { tone: "buffers", value: buffers },
              { tone: "cache", value: cache },
            ]}
            text={`${formatSize(memory.used_bytes)}/${formatSize(memory.total_bytes)}`}
          />
          <Meter
            label="Swp"
            segments={[
              {
                tone: "swap",
                value: percentOf(swapUsed, memory.swap_total_bytes),
              },
            ]}
            text={`${formatSize(swapUsed)}/${formatSize(memory.swap_total_bytes)}`}
          />
        </div>
        <dl className="htop-header__stats">
          <dt>Tasks</dt>
          <dd>
            {count
              ? `${count.total}, ${count.thread} thr; ${count.running} running`
              : "—"}
          </dd>
          <dt>Load average</dt>
          <dd>
            {cpu.load_average.map((load) => load.toFixed(2)).join(" ") || "—"}
          </dd>
          <dt>Uptime</dt>
          <dd>{formatUptimeClock(live.uptime_seconds)}</dd>
        </dl>
      </div>
    </AppPaper>
  );
};

const HtopTab = () => {
  const live = useQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: PROCESS_REFRESH_INTERVAL_MS,
  });
  const processes = useQuery({
    ...linuxio.monitoring.get_processes,
    refetchInterval: PROCESS_REFRESH_INTERVAL_MS,
  });
  const error = live.error ?? processes.error;
  // htop's default order: busiest first, re-ranked on every refresh.
  const rows = (processes.data?.items ?? []).toSorted(
    (a, b) => b.cpu_percent - a.cpu_percent,
  );

  return (
    <div className="htop">
      {error && (
        <AppTypography color="error" variant="body2">
          Unable to load system activity: {error.message}
        </AppTypography>
      )}
      {live.data && (
        <HtopHeader count={processes.data?.count} live={live.data} />
      )}
      <ProcessVirtualTable data={rows} mode="processes" />
    </div>
  );
};

export default HtopTab;
