import { useQuery } from "@tanstack/react-query";
import {
  type CSSProperties,
  type UIEvent,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { linuxio, openChannel, useStreamMux } from "@/api";
import PageLoader from "@/components/loaders/PageLoader";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import Chip from "@/components/ui/AppChip";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useLiveStream } from "@/hooks/useLiveStream";
import { copyToClipboard } from "@/utils/clipboard";

import {
  type DockerLogEntry,
  dockerLogHue,
  formatDockerLogTime,
  mergeDockerLogEntries,
  parseDockerLogFrame,
  stageDockerLogEntries,
} from "./dockerLogs";

import "./docker-logs.css";

const TAIL_OPTIONS = ["100", "500", "1000", "5000"];
const INITIAL_SILENCE_TIMEOUT_MS = 1500;
const BOTTOM_THRESHOLD_PX = 24;

interface DockerLogsPageProps {
  /** Container name filter, owned by the route search params. */
  container?: string;
  onContainerChange: (container: string | undefined) => void;
}

const getRowId = (entry: DockerLogEntry) => String(entry.seq);

const renderExpanded = (row: { original: DockerLogEntry }) => (
  <pre className="docker-logs__expanded">{row.original.line}</pre>
);

const DockerLogsPage = ({
  container,
  onContainerChange,
}: DockerLogsPageProps) => {
  const [entries, setEntries] = useState<DockerLogEntry[]>([]);
  const [search, setSearch] = useState("");
  const [tail, setTail] = useState("100");
  const [liveMode, setLiveMode] = useState(true);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [streamEpoch, setStreamEpoch] = useState(0);

  const pendingRef = useRef<DockerLogEntry[]>([]);
  const seqRef = useRef(0);
  const flushFrameRef = useRef<number | null>(null);
  const silenceTimerRef = useRef<number | null>(null);
  const hasReceivedDataRef = useRef(false);
  const pinnedToBottomRef = useRef(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  const { streamRef, openStream, closeStream } = useLiveStream();
  const { isOpen: muxIsOpen } = useStreamMux();
  const { data: containers } = useQuery(linuxio.docker.list_containers);

  const clearSilenceTimer = () => {
    if (silenceTimerRef.current !== null) {
      window.clearTimeout(silenceTimerRef.current);
      silenceTimerRef.current = null;
    }
  };

  const flushPending = () => {
    if (flushFrameRef.current !== null) {
      window.cancelAnimationFrame(flushFrameRef.current);
      flushFrameRef.current = null;
    }
    const batch = pendingRef.current;
    if (batch.length === 0) return;
    pendingRef.current = [];
    setEntries((current) => mergeDockerLogEntries(current, batch));
  };

  const handleStreamText = useEffectEvent((text: string) => {
    const batch = parseDockerLogFrame(text, seqRef.current);
    seqRef.current += batch.length;
    if (batch.length === 0) return;
    stageDockerLogEntries(pendingRef.current, batch);
    if (!hasReceivedDataRef.current) {
      hasReceivedDataRef.current = true;
      clearSilenceTimer();
      setIsLoading(false);
      // First frame flushes now so the loader never yields to an empty table.
      flushPending();
      return;
    }
    if (flushFrameRef.current === null) {
      flushFrameRef.current = window.requestAnimationFrame(flushPending);
    }
  });

  const handleStreamResult = useEffectEvent(
    (result: { status: "ok" | "error"; error?: string }) => {
      clearSilenceTimer();
      flushPending();
      setIsLoading(false);
      if (result.status === "error") {
        setError(result.error || "Failed to load logs");
      }
    },
  );

  const handleStreamClose = useEffectEvent(() => {
    clearSilenceTimer();
    flushPending();
    setIsLoading(false);
  });

  const handleStreamOpenError = useEffectEvent(() => {
    clearSilenceTimer();
    queueMicrotask(() => {
      setError("Failed to connect to log stream");
      setIsLoading(false);
    });
  });

  // Effect event so the opening effect does not depend on `tail`; a resumed
  // stream asks for tail 0 because the buffer already holds the history.
  const startStream = useEffectEvent(() => {
    hasReceivedDataRef.current = false;
    setError(null);
    const opened = openStream({
      open: () =>
        openChannel("docker.logs.follow_all", {
          tail: seqRef.current > 0 ? "0" : tail,
        }),
      onOpenError: handleStreamOpenError,
      onText: handleStreamText,
      onResult: handleStreamResult,
      onClose: handleStreamClose,
    });
    if (opened) {
      clearSilenceTimer();
      silenceTimerRef.current = window.setTimeout(() => {
        silenceTimerRef.current = null;
        if (!hasReceivedDataRef.current) setIsLoading(false);
      }, INITIAL_SILENCE_TIMEOUT_MS);
    }
  });

  useEffect(() => {
    if (!muxIsOpen || !liveMode || streamRef.current) return;
    startStream();
    // `streamEpoch` restarts the stream after the lines select cleared the buffer.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [muxIsOpen, liveMode, streamEpoch, streamRef]);

  useEffect(
    () => () => {
      if (silenceTimerRef.current !== null) {
        window.clearTimeout(silenceTimerRef.current);
      }
      if (flushFrameRef.current !== null) {
        window.cancelAnimationFrame(flushFrameRef.current);
      }
    },
    [],
  );

  // Keep the newest line in view while the user is at the bottom.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && pinnedToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
    // `entries` retriggers the pin after streamed rows render.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [entries]);

  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const el = event.currentTarget;
    pinnedToBottomRef.current =
      el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_THRESHOLD_PX;
  };

  const handleLiveModeChange = (checked: boolean) => {
    setLiveMode(checked);
    if (!checked) {
      clearSilenceTimer();
      flushPending();
      closeStream();
      setIsLoading(false);
    }
  };

  const handleTailChange = (value: string) => {
    if (value === tail) return;
    clearSilenceTimer();
    closeStream();
    pendingRef.current = [];
    seqRef.current = 0;
    hasReceivedDataRef.current = false;
    pinnedToBottomRef.current = true;
    setEntries([]);
    setError(null);
    setIsLoading(true);
    setLiveMode(true);
    setTail(value);
    setStreamEpoch((epoch) => epoch + 1);
  };

  const runningNames = (containers ?? [])
    .filter((item) => item.State === "running")
    .map((item) => item.Names[0]?.replace(/^\//, "") ?? item.Id.slice(0, 12));
  const containerNames = Array.from(
    new Set([...runningNames, ...entries.map((entry) => entry.name)]),
  ).sort((a, b) => a.localeCompare(b));

  const needle = search.trim().toLowerCase();
  const visible = entries.filter(
    (entry) =>
      (!container || entry.name === container) &&
      (!needle || entry.line.toLowerCase().includes(needle)),
  );

  const columns = useMemo<AppVirtualTableColumnDef<DockerLogEntry>[]>(
    () => [
      {
        id: "time",
        header: "Time",
        enableSorting: false,
        cell: ({ row }) => (
          <AppTypography noWrap title={row.original.ts} variant="body2">
            {formatDockerLogTime(row.original.ts)}
          </AppTypography>
        ),
        meta: {
          getCellRenderKey: (row) => (row as DockerLogEntry).seq,
          hideBelow: "sm",
          width: "96px",
        },
      },
      {
        id: "container",
        header: "Container",
        enableSorting: false,
        cell: ({ row }) => (
          <Chip
            className="docker-logs__source"
            label={row.original.name}
            onClick={(event) => {
              event.stopPropagation();
              onContainerChange(row.original.name);
            }}
            size="xsmall"
            style={
              {
                "--docker-log-hue": dockerLogHue(row.original.name),
              } as CSSProperties
            }
            title={`Show only ${row.original.name} (${row.original.id})`}
            variant="soft"
          />
        ),
        meta: {
          deferTooltipWhileScrolling: true,
          getCellRenderKey: (row) => (row as DockerLogEntry).seq,
          width: "minmax(110px, 200px)",
        },
      },
      {
        id: "line",
        header: "Message",
        enableSorting: false,
        cell: ({ row }) => (
          <AppTypography
            className={
              row.original.stderr
                ? "docker-logs__line docker-logs__line--stderr"
                : "docker-logs__line"
            }
            noWrap
            variant="body2"
          >
            {row.original.line}
          </AppTypography>
        ),
        meta: {
          align: "left",
          deferTooltipWhileScrolling: true,
          getCellRenderKey: (row) => (row as DockerLogEntry).seq,
        },
      },
    ],
    [onContainerChange],
  );

  // Raw Docker timestamps first so exported logs stay machine-sortable.
  const exportText = () =>
    visible
      .map((entry) => `${entry.ts} ${entry.name} ${entry.line}`)
      .join("\n");

  const handleDownload = () => {
    const blob = new Blob([exportText()], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${container ?? "docker"}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const emptyMessage =
    runningNames.length === 0 && entries.length === 0
      ? "No running containers."
      : entries.length === 0
        ? "No log output yet."
        : "No matching logs.";

  return (
    <>
      <RoutedTabSearch active={search !== ""}>
        <AppHeaderSearch
          clearOnDocumentEscape
          onChange={setSearch}
          placeholder="Search logs…"
          value={search}
        />
      </RoutedTabSearch>
      <div className="docker-logs">
        <div className="docker-logs__toolbar">
          <AppSelect
            label="Lines"
            onChange={(event) => handleTailChange(event.target.value)}
            size="small"
            style={{ width: 112 }}
            value={tail}
          >
            {TAIL_OPTIONS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </AppSelect>
          <AppSelect
            label="Container"
            onChange={(event) =>
              onContainerChange(event.target.value || undefined)
            }
            size="small"
            style={{ minWidth: 180 }}
            value={container ?? ""}
          >
            <option value="">All containers</option>
            {containerNames.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </AppSelect>
          <AppActionIconButton
            disabled={visible.length === 0}
            icon="mdi:content-copy"
            iconSize={20}
            label="Copy logs"
            onClick={() => void copyToClipboard(exportText())}
          />
          <AppActionIconButton
            disabled={visible.length === 0}
            icon="mdi:download"
            iconSize={20}
            label="Download logs"
            onClick={handleDownload}
          />
          <AppTooltip
            title={liveMode ? "Live streaming ON" : "Live streaming OFF"}
          >
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={liveMode}
                  onChange={(_, checked) => handleLiveModeChange(checked)}
                  size="small"
                />
              }
              label="Live"
            />
          </AppTooltip>
          <AppTypography fontWeight={700}>{visible.length} shown</AppTypography>
        </div>

        {isLoading && <PageLoader />}
        {error && <AppAlert severity="error">{error}</AppAlert>}
        {!isLoading && !error && (
          <AppVirtualTable
            ariaLabel="Docker logs"
            columns={columns}
            data={visible}
            emptyMessage={emptyMessage}
            fillAvailable
            getRowId={getRowId}
            onScroll={handleScroll}
            renderExpandedContent={renderExpanded}
            scrollElementRef={scrollRef}
          />
        )}
      </div>
    </>
  );
};

export default DockerLogsPage;
