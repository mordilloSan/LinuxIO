import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type MouseEvent,
  type UIEvent,
} from "react";

import { CACHE_TTL_MS, linuxio, openChannel, useStreamMux } from "@/api";
import PageLoader from "@/components/loaders/PageLoader";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import Chip from "@/components/ui/AppChip";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppPaper from "@/components/ui/AppPaper";
import AppSearchField from "@/components/ui/AppSearchField";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { getLogPriorityAccent } from "@/constants/statusColors";
import { useLiveStream } from "@/hooks/useLiveStream";
import { copyToClipboard } from "@/utils/clipboard";
import { withPromiseCleanup } from "@/utils/withPromiseCleanup";

// A fixed first page replaces the old "Lines" selector. Older entries are
// fetched by cursor as the user scrolls, so choosing an up-front count no
// longer changes which history is reachable.
const INITIAL_PAGE_LINES = "500";
// Live updates retain at least this many rows. Once history pages are loaded,
// the retention window grows to preserve everything the user has reached.
const INITIAL_BUFFER_LIMIT = 5000;
// Rows fed to the table start capped at DISPLAY_LIMIT (bounds the TanStack
// row-model rebuild cost per live flush, not DOM size — rows are virtualized)
// and grow by DISPLAY_CHUNK as the user scrolls. Copy / Download always
// operate on the full matched set.
const DISPLAY_LIMIT = 1000;
const DISPLAY_CHUNK = 1000;
// "Load earlier" pagination: page size requested per backend fetch and the
// scroll distance from the bottom that triggers loading.
const PAGE_SIZE = "1000";
// Start the cached React Query fetch roughly 25 compact rows before the end,
// so the virtual table normally has the next page before the user arrives.
const SCROLL_LOAD_THRESHOLD_PX = 1200;
// A follow stream that dies without us closing it is reopened after this
// delay, giving up after MAX_RECONNECT_ATTEMPTS consecutive failures.
const RECONNECT_DELAY_MS = 1500;
const MAX_RECONNECT_ATTEMPTS = 3;
// Fallback: never leave the spinner up longer than this if the stream stays
// silent and the backlog-complete signal is lost.
const LOADING_FALLBACK_TIMEOUT_MS = 10_000;
const UNIT_STATUS_FILTERS_REQUIRING_SERVICES = new Set([
  "running",
  "failed",
  "inactive",
]);

// Log priority levels (syslog standard). Const object rather than a TS enum
// so the file stays within erasableSyntaxOnly.
const LogPriority = {
  EMERGENCY: 0,
  ALERT: 1,
  CRITICAL: 2,
  ERROR: 3,
  WARNING: 4,
  NOTICE: 5,
  INFO: 6,
  DEBUG: 7,
} as const;
type LogPriority = (typeof LogPriority)[keyof typeof LogPriority];

interface LogEntry {
  /** Stable row identity: the journal cursor, or a synthetic fallback. */
  id: string;
  cursor: string | null;
  identifier: string;
  message: string;
  priority: LogPriority;
  rawJson?: Record<string, unknown>;
  timestamp: string;
}

// Hoisted formatter: toLocaleTimeString with an options object constructs a
// new Intl.DateTimeFormat per call (~30x slower than reusing one). The time
// components must be spelled out — a DateTimeFormat built from { hour12 }
// alone formats a date, not a time.
const TIME_FORMATTER = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

// Progress frames emitted by the general-logs Channel alongside data frames. The
// backend sends backlog_complete once the one-shot history query finishes, so
// "no matches" is distinguishable from "still loading" even though the follow
// process never exits.
interface GeneralLogsProgress {
  count?: number;
  resumed?: boolean;
  truncated?: boolean;
  type?: string;
}

const getPriorityLabel = (priority: LogPriority): string => {
  switch (priority) {
    case LogPriority.EMERGENCY:
      return "EMERGENCY";
    case LogPriority.ALERT:
      return "ALERT";
    case LogPriority.CRITICAL:
      return "CRITICAL";
    case LogPriority.ERROR:
      return "ERROR";
    case LogPriority.WARNING:
      return "WARNING";
    case LogPriority.NOTICE:
      return "NOTICE";
    case LogPriority.INFO:
      return "INFO";
    case LogPriority.DEBUG:
      return "DEBUG";
    default:
      return "UNKNOWN";
  }
};

const getPriorityColor = (priority: LogPriority): string => {
  switch (priority) {
    case LogPriority.EMERGENCY:
    case LogPriority.ALERT:
    case LogPriority.CRITICAL:
      return "error";
    case LogPriority.ERROR:
      return "error";
    case LogPriority.WARNING:
      return "warning";
    case LogPriority.NOTICE:
      return "info";
    case LogPriority.INFO:
      return "success";
    case LogPriority.DEBUG:
      return "default";
    default:
      return "default";
  }
};

// Standard journald keys worth surfacing as clickable filters in addition to
// any LINUXIO_* user fields.
const FILTERABLE_STANDARD_KEYS = new Set([
  "SYSLOG_IDENTIFIER",
  "_SYSTEMD_UNIT",
  "PRIORITY",
  "CODE_FUNC",
]);

const FIELD_VALUE_MAX_LEN = 200;

const collectFilterableFields = (
  rawJson: Record<string, unknown> | undefined,
  active: string[],
): Array<{ key: string; value: string }> => {
  if (!rawJson) return [];
  const seen = new Set(active);
  const result: Array<{ key: string; value: string }> = [];
  for (const [key, raw] of Object.entries(rawJson)) {
    if (typeof raw !== "string" || raw === "") continue;
    if (raw.length > FIELD_VALUE_MAX_LEN) continue;
    if (!key.startsWith("LINUXIO_") && !FILTERABLE_STANDARD_KEYS.has(key)) {
      continue;
    }
    const filter = `${key}=${raw}`;
    if (seen.has(filter)) continue;
    result.push({ key, value: raw });
  }
  return result;
};

const getPriorityIcon = (priority: LogPriority) => {
  switch (priority) {
    case LogPriority.EMERGENCY:
    case LogPriority.ALERT:
    case LogPriority.CRITICAL:
    case LogPriority.ERROR:
      return <Icon height={20} icon="mdi:alert-circle" width={20} />;
    case LogPriority.WARNING:
      return <Icon height={20} icon="mdi:alert" width={20} />;
    case LogPriority.INFO:
    case LogPriority.NOTICE:
      return <Icon height={20} icon="mdi:information" width={20} />;
    case LogPriority.DEBUG:
      return <Icon height={20} icon="mdi:bug" width={20} />;
    default:
      return <Icon height={20} icon="mdi:information" width={20} />;
  }
};

const resolveUnitTarget = (
  log: LogEntry,
): { section: "services" | "sockets" | "timers"; unit: string } | null => {
  const raw = log.rawJson;
  const systemdUnit =
    typeof raw?._SYSTEMD_UNIT === "string" && raw._SYSTEMD_UNIT
      ? raw._SYSTEMD_UNIT
      : typeof raw?.UNIT === "string" && raw.UNIT
        ? raw.UNIT
        : null;

  let unit = systemdUnit;
  if (!unit) {
    const ident = log.identifier?.trim();
    if (!ident || ident === "system") return null;
    unit = ident.includes(".") ? ident : `${ident}.service`;
  }

  if (unit.endsWith(".timer")) {
    return { section: "timers", unit };
  }
  if (unit.endsWith(".socket")) {
    return { section: "sockets", unit };
  }
  if (unit.endsWith(".service")) {
    return { section: "services", unit };
  }
  return null;
};

// Extract priority from message content
const extractPriorityFromMessage = (message: string): LogPriority | null => {
  const match = message.match(
    /^\[(EMERGENCY|ALERT|CRITICAL|ERROR|WARN(?:ING)?|NOTICE|INFO|DEBUG)\]/i,
  );
  if (!match) return null;

  const level = match[1].toUpperCase();
  switch (level) {
    case "EMERGENCY":
      return LogPriority.EMERGENCY;
    case "ALERT":
      return LogPriority.ALERT;
    case "CRITICAL":
      return LogPriority.CRITICAL;
    case "ERROR":
      return LogPriority.ERROR;
    case "WARN":
    case "WARNING":
      return LogPriority.WARNING;
    case "NOTICE":
      return LogPriority.NOTICE;
    case "INFO":
      return LogPriority.INFO;
    case "DEBUG":
      return LogPriority.DEBUG;
    default:
      return null;
  }
};

let syntheticLogId = 0;

// Parse one journalctl -o json line (fields pre-trimmed by the backend).
const parseLogEntry = (jsonStr: string): LogEntry | null => {
  try {
    const data = JSON.parse(jsonStr);

    const timestamp = TIME_FORMATTER.format(
      data.__REALTIME_TIMESTAMP
        ? new Date(parseInt(data.__REALTIME_TIMESTAMP) / 1000)
        : new Date(),
    );

    const rawMessage = data.MESSAGE;
    const message =
      typeof rawMessage === "string"
        ? rawMessage
        : rawMessage == null
          ? ""
          : "[binary data]";

    const messagePriority = extractPriorityFromMessage(message);
    const journalPriority =
      typeof data.PRIORITY === "string"
        ? parseInt(data.PRIORITY, 10)
        : typeof data.PRIORITY === "number"
          ? data.PRIORITY
          : LogPriority.INFO;
    const priority = messagePriority ?? journalPriority;

    const identifier = data.SYSLOG_IDENTIFIER || data._COMM || "system";

    const cursor =
      typeof data.__CURSOR === "string" && data.__CURSOR !== ""
        ? data.__CURSOR
        : null;

    return {
      id: cursor ?? `synthetic-${syntheticLogId++}`,
      cursor,
      timestamp,
      priority,
      identifier,
      message,
      rawJson: data,
    };
  } catch (e) {
    console.error("Failed to parse log entry:", e);
    return null;
  }
};

const getLogRowId = (row: LogEntry) => row.id;

// Returns `current` unchanged when `entries` add no identifier, so the
// dependent sort only reruns when the set actually grows.
const addIdentifiers = (
  current: ReadonlySet<string>,
  entries: LogEntry[],
): ReadonlySet<string> => {
  let next: Set<string> | null = null;
  for (const { identifier } of entries) {
    if (current.has(identifier) || next?.has(identifier)) continue;
    (next ??= new Set(current)).add(identifier);
  }
  return next ?? current;
};

const prependUniqueLogs = (
  current: LogEntry[],
  incomingNewestFirst: LogEntry[],
  limit: number,
): LogEntry[] => {
  if (incomingNewestFirst.length === 0) return current;
  const seen = new Set(current.map((log) => log.id));
  const unique: LogEntry[] = [];
  for (const log of incomingNewestFirst) {
    if (seen.has(log.id)) continue;
    seen.add(log.id);
    unique.push(log);
  }
  if (unique.length === 0) return current;
  return [...unique, ...current].slice(0, limit);
};

const appendUniqueLogs = (
  current: LogEntry[],
  incomingOlderFirst: LogEntry[],
  limit: number,
): LogEntry[] => {
  if (incomingOlderFirst.length === 0 || current.length >= limit)
    return current;
  const seen = new Set(current.map((log) => log.id));
  const unique: LogEntry[] = [];
  for (const log of incomingOlderFirst) {
    if (seen.has(log.id)) continue;
    seen.add(log.id);
    unique.push(log);
    if (current.length + unique.length >= limit) break;
  }
  if (unique.length === 0) return current;
  return [...current, ...unique];
};

// Expanded-row detail: the stream only carries the fields the list needs, so
// the full raw entry is fetched by cursor on demand (journal entries are
// immutable — cache forever).
const LogEntryDetails = ({
  fieldFilters,
  log,
  onAddFieldFilter,
}: {
  fieldFilters: string[];
  log: LogEntry;
  onAddFieldFilter: (filter: string) => void;
}) => {
  const { data: fullEntry, isError } = useQuery({
    ...linuxio.logs.general_entry({ cursor: log.cursor ?? "" }),
    enabled: log.cursor !== null,
    staleTime: Infinity,
  });

  const entry = fullEntry ?? log.rawJson;
  const filterableEntries = collectFilterableFields(entry, fieldFilters);

  return (
    <div className="expand-panel">
      {filterableEntries.length > 0 && (
        <div className="expand-panel__chips">
          {filterableEntries.map(({ key, value }) => {
            const filter = `${key}=${value}`;
            return (
              <Chip
                color="primary"
                key={filter}
                label={`${key}=${value}`}
                onClick={() => onAddFieldFilter(filter)}
                size="xsmall"
                style={{ maxWidth: 360 }}
                title={`Filter to entries where ${key}=${value}`}
                variant="soft"
              />
            );
          })}
        </div>
      )}
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Full Message:</b>
        </AppTypography>
        <AppPaper
          style={{
            padding: 8,
            backgroundColor: "var(--app-code-block-background)",
            fontFamily: "var(--app-font-mono)",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            maxWidth: "100%",
            overflowX: "auto",
          }}
        >
          <AppTypography variant="body2">{log.message}</AppTypography>
        </AppPaper>
      </div>

      {entry && (
        <div>
          <AppTypography gutterBottom variant="subtitle2">
            <b>Raw Journal Entry:</b>
          </AppTypography>
          {isError && (
            <AppTypography color="text.secondary" variant="caption">
              Full entry unavailable — showing streamed fields only.
            </AppTypography>
          )}
          <AppPaper
            style={{
              padding: 8,
              backgroundColor: "var(--app-code-block-background)",
              fontFamily: "var(--app-font-mono)",
              maxHeight: 300,
              overflowY: "auto",
              maxWidth: "100%",
              overflowX: "auto",
            }}
          >
            <AppTypography
              component="pre"
              style={{
                margin: 0,
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
                overflowWrap: "anywhere",
              }}
              variant="caption"
            >
              {JSON.stringify(entry, null, 2)}
            </AppTypography>
          </AppPaper>
        </div>
      )}
    </div>
  );
};

const GeneralLogsPage = () => {
  const navigate = useNavigate();
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  // Identifiers seen since the last resetBuffer. Grow-only on purpose: an
  // identifier whose rows the ring has since trimmed is still a valid exact
  // backend filter, and this keeps each flush O(new rows) instead of a full
  // buffer rescan. Updated alongside every setLogs below.
  const [identifierSet, setIdentifierSet] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const [search, setSearch] = useState("");
  const [timePeriod, setTimePeriod] = useState("24h");
  const [priorityFilter, setPriorityFilter] = useState("all");
  const [unitStatusFilter, setUnitStatusFilter] = useState<string>("all");
  const [identifierFilter, setIdentifierFilter] = useState("all");
  const [identifierIsExact, setIdentifierIsExact] = useState(true);
  // identifierInput tracks the live autocomplete input value (every keystroke).
  // identifierFilter / identifierIsExact are the *applied* values, updated after
  // a debounce so we don't re-stream on every keystroke.
  const [identifierInput, setIdentifierInput] = useState("");
  const [fieldFilters, setFieldFilters] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // Bumped to force the open effect to run again after an unexpected close.
  const [streamEpoch, setStreamEpoch] = useState(0);
  // True when the backend hit its backlog cap for "All in window".
  const [backlogTruncated, setBacklogTruncated] = useState(false);
  // Infinite scroll: rows currently revealed to the table, whether older
  // journal entries remain beyond the buffer, and an in-flight guard.
  const [displayLimit, setDisplayLimit] = useState(DISPLAY_LIMIT);
  const [hasMoreOlder, setHasMoreOlder] = useState(false);
  const [isLoadingOlder, setIsLoadingOlder] = useState(false);
  const [paginationError, setPaginationError] = useState<string | null>(null);
  const isLoadingOlderRef = useRef(false);
  const paginationGenerationRef = useRef(0);
  // Live-flush retention cap. It starts at a safe baseline and grows with
  // every history page the user reaches, so live prepends preserve that
  // navigated history without imposing an artificial entry-count ceiling.
  const bufferLimitRef = useRef(INITIAL_BUFFER_LIMIT);
  const newestCursorRef = useRef<string | null>(null);
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const hasReceivedData = useRef(false);
  // Whether the buffer holds (or is about to hold) entries — decides between
  // a fresh tail request and a "new entries only" reopen. A ref (not state):
  // read inside the open effect without retriggering it per flush.
  const hasBufferedDataRef = useRef(false);
  const reconnectAttemptsRef = useRef(0);
  const reconnectTimerRef = useRef<number | null>(null);
  const loadingFallbackTimerRef = useRef<number | null>(null);
  // Pending log entries waiting for the next animation-frame flush. Buffering
  // here turns a per-line setState (potentially thousands per second on a
  // chatty journal) into ~60 batched updates per second.
  const pendingLogsRef = useRef<LogEntry[]>([]);
  const flushScheduledRef = useRef(false);
  const { streamRef, openStream, closeStream } = useLiveStream();
  const queryClient = useQueryClient();

  // Keep the retention target in sync after a logs state commit. This belongs
  // outside state updater callbacks so those callbacks remain pure and safe
  // under concurrent rendering.
  useLayoutEffect(() => {
    bufferLimitRef.current = Math.max(bufferLimitRef.current, logs.length);
  }, [logs]);

  const { isOpen: muxIsOpen } = useStreamMux();
  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current !== null) {
      window.clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const clearLoadingFallbackTimer = useCallback(() => {
    if (loadingFallbackTimerRef.current !== null) {
      window.clearTimeout(loadingFallbackTimerRef.current);
      loadingFallbackTimerRef.current = null;
    }
  }, []);

  const uniqueIdentifiers = useMemo(
    () => Array.from(identifierSet).sort(),
    [identifierSet],
  );

  // Current systemd unit states, used by the unit-status filter below.
  const unitStatusNeedsServices =
    UNIT_STATUS_FILTERS_REQUIRING_SERVICES.has(unitStatusFilter);
  const { data: services } = useQuery({
    ...linuxio.systemd.list_services,
    enabled: unitStatusNeedsServices,
    staleTime: CACHE_TTL_MS.THIRTY_SECONDS,
  });

  // Set of unit names matching the selected status. `null` means the filter is
  // either "all" (no filter) or "no_unit" (which is handled by checking for an
  // empty _SYSTEMD_UNIT field, not by Set membership).
  const matchingUnitNames = useMemo<Set<string> | null>(() => {
    if (!unitStatusNeedsServices || !services) {
      return null;
    }
    const wanted = new Set<string>();
    for (const svc of services) {
      if (unitStatusFilter === "running" && svc.sub_state === "running") {
        wanted.add(svc.name);
      } else if (
        unitStatusFilter === "failed" &&
        (svc.active_state === "failed" || svc.sub_state === "failed")
      ) {
        wanted.add(svc.name);
      } else if (
        unitStatusFilter === "inactive" &&
        svc.active_state === "inactive"
      ) {
        wanted.add(svc.name);
      }
    }
    return wanted;
  }, [services, unitStatusFilter, unitStatusNeedsServices]);

  // Keep the viewport pinned to the newest entry, but only while the user is
  // already at the top. Unconditionally resetting scrollTop on every flush
  // made scrolling impossible in live mode.
  useLayoutEffect(() => {
    if (!liveMode) return;
    const el = logsBoxRef.current;
    if (el && el.scrollTop <= 2) {
      el.scrollTop = 0;
    }
    // `logs` intentionally retriggers the DOM scroll after a buffered flush renders.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [logs, liveMode]);

  // Flush queued log entries on the next animation frame. Coalesces bursts so
  // we don't pay React reconciliation cost per arriving line.
  const scheduleFlush = useCallback(() => {
    if (flushScheduledRef.current) return;
    flushScheduledRef.current = true;
    requestAnimationFrame(() => {
      flushScheduledRef.current = false;
      const pending = pendingLogsRef.current;
      if (pending.length === 0) return;
      pendingLogsRef.current = [];
      // Pending arrived in chronological order; the table renders newest-first,
      // so reverse before prepending.
      const reversed = pending.reverse();
      setLogs((prev) =>
        prependUniqueLogs(prev, reversed, bufferLimitRef.current),
      );
      setIdentifierSet((prev) => addIdentifiers(prev, reversed));
    });
  }, []);

  const handleStreamOpenError = useEffectEvent(() => {
    queueMicrotask(() => {
      setError("Failed to connect to log stream");
      setIsLoading(false);
    });
  });

  const handleStreamText = useEffectEvent((text: string) => {
    if (!hasReceivedData.current) {
      hasReceivedData.current = true;
      reconnectAttemptsRef.current = 0;
      clearLoadingFallbackTimer();
      setIsLoading(false);
      setError(null);
    }
    const pending = pendingLogsRef.current;
    // Frames are batched server-side and may carry many lines.
    for (const line of text.split("\n")) {
      if (!line) continue;
      const logEntry = parseLogEntry(line);
      if (logEntry) {
        pending.push(logEntry);
        if (logEntry.cursor !== null) {
          newestCursorRef.current = logEntry.cursor;
        }
      }
    }
    // rAF is paused in background tabs; bound this single-frame staging area.
    // Navigated history is kept separately in logs and is not capped here.
    if (pending.length > INITIAL_BUFFER_LIMIT) {
      pending.splice(0, pending.length - INITIAL_BUFFER_LIMIT);
    }
    if (pending.length > 0) {
      hasBufferedDataRef.current = true;
      scheduleFlush();
    }
  });

  const handleStreamProgress = useEffectEvent(
    (progress: GeneralLogsProgress) => {
      if (progress?.type === "backlog_complete") {
        clearLoadingFallbackTimer();
        setIsLoading(false);
        if (!progress.resumed) {
          setBacklogTruncated(Boolean(progress.truncated));
          // Older history remains when the backlog hit the server cap, or when
          // a numeric tail came back full (the window likely holds more).
          const requested = Number.parseInt(INITIAL_PAGE_LINES, 10);
          setHasMoreOlder(
            Boolean(progress.truncated) ||
              (Number.isFinite(requested) &&
                (progress.count ?? 0) >= requested),
          );
        }
      }
    },
  );

  const handleStreamResult = useEffectEvent(
    (result: { status: "ok" | "error"; error?: string }) => {
      clearLoadingFallbackTimer();
      if (result.status === "error") {
        setError(result.error || "Log stream failed");
        setIsLoading(false);
      }
    },
  );

  const handleStreamClosed = useEffectEvent(() => {
    clearLoadingFallbackTimer();
    if (!hasReceivedData.current) {
      setIsLoading(false);
    }
    // One-shot (paused) channels close normally after the backlog; only a live
    // follow stream dying warrants a reconnect.
    if (!liveMode || !muxIsOpen) return;
    if (reconnectAttemptsRef.current >= MAX_RECONNECT_ATTEMPTS) return;
    reconnectAttemptsRef.current += 1;
    if (reconnectTimerRef.current !== null) return;
    reconnectTimerRef.current = window.setTimeout(() => {
      reconnectTimerRef.current = null;
      setStreamEpoch((epoch) => epoch + 1);
    }, RECONNECT_DELAY_MS);
  });

  // Effect event so the open effect reads the latest filter state without the
  // handlers ending up in its dependency array (same pattern as useLogStream).
  const startStream = useEffectEvent((tail: string, afterCursor: string) => {
    hasReceivedData.current = false;
    setError(null);
    const backendIdentifier =
      identifierFilter === "all"
        ? ""
        : identifierIsExact
          ? identifierFilter
          : "";
    const opened = openStream<GeneralLogsProgress>({
      open: () =>
        openChannel("logs.general.follow", {
          lines: tail,
          timePeriod,
          priority: priorityFilter === "all" ? "" : priorityFilter,
          identifier: backendIdentifier,
          fieldFilters,
          follow: liveMode,
          afterCursor,
        }),
      onOpenError: handleStreamOpenError,
      onText: handleStreamText,
      onProgress: handleStreamProgress,
      onResult: handleStreamResult,
      onClose: handleStreamClosed,
    });

    if (opened) {
      clearLoadingFallbackTimer();
      loadingFallbackTimerRef.current = window.setTimeout(() => {
        loadingFallbackTimerRef.current = null;
        if (!hasReceivedData.current) {
          setIsLoading(false);
        }
      }, LOADING_FALLBACK_TIMEOUT_MS);
    }
  });

  // Open stream on mount, on filter changes, and on reconnect epochs. The
  // filter values are read by the Effect Event; here they intentionally own
  // the stream restart lifecycle rather than the event callback's identity.
  // oxlint-disable react/exhaustive-effect-dependencies
  useEffect(() => {
    if (!muxIsOpen || streamRef.current) {
      return;
    }
    // Paused with data on screen: keep it, don't re-query.
    if (!liveMode && hasBufferedDataRef.current) {
      return;
    }

    // With entries already buffered (live toggle, reconnect) only new entries
    // are needed; a fresh buffer gets the full tail.
    const afterCursor = hasBufferedDataRef.current
      ? (newestCursorRef.current ?? "")
      : "";
    startStream(afterCursor !== "" ? "0" : INITIAL_PAGE_LINES, afterCursor);
  }, [
    muxIsOpen,
    liveMode,
    streamEpoch,
    streamRef,
    timePeriod,
    priorityFilter,
    identifierFilter,
    identifierIsExact,
    fieldFilters,
  ]);
  // oxlint-enable react/exhaustive-effect-dependencies

  const handleLiveModeChange = (
    _: ChangeEvent<HTMLInputElement>,
    checked: boolean,
  ) => {
    setLiveMode(checked);
    if (!checked) {
      clearReconnectTimer();
      clearLoadingFallbackTimer();
      closeStream();
      if (!hasReceivedData.current) {
        setIsLoading(false);
      }
      return;
    }
    reconnectAttemptsRef.current = 0;
    setError(null);
  };

  // Close the stream and drop both rendered logs and queued (pre-flush) logs.
  // Used by every filter change that needs to re-issue the backend stream.
  const resetBuffer = useCallback(() => {
    paginationGenerationRef.current += 1;
    clearReconnectTimer();
    clearLoadingFallbackTimer();
    closeStream();
    setLogs([]);
    setIdentifierSet(new Set());
    pendingLogsRef.current = [];
    hasBufferedDataRef.current = false;
    newestCursorRef.current = null;
    reconnectAttemptsRef.current = 0;
    isLoadingOlderRef.current = false;
    bufferLimitRef.current = INITIAL_BUFFER_LIMIT;
    setBacklogTruncated(false);
    setDisplayLimit(DISPLAY_LIMIT);
    setHasMoreOlder(false);
    setIsLoadingOlder(false);
    setPaginationError(null);
    setError(null);
    setIsLoading(true);
  }, [closeStream, clearReconnectTimer, clearLoadingFallbackTimer]);

  // Filter change handlers
  const handleTimePeriodChange = (value: string) => {
    resetBuffer();
    setTimePeriod(value);
  };

  const handlePriorityFilterChange = (value: string) => {
    resetBuffer();
    setPriorityFilter(value);
  };

  // Apply an identifier value as the active filter. Re-streams the backend
  // only when the journalctl `-t` argument actually changes (substring->exact,
  // exact->different exact, or to/from "all"); plain substring tweaks just
  // re-filter the existing buffer.
  const applyIdentifierFilter = useCallback(
    (rawValue: string) => {
      const trimmed = rawValue.trim();
      const newValue = trimmed === "" ? "all" : trimmed;
      const isExact =
        newValue === "all" || uniqueIdentifiers.includes(newValue);

      const oldBackend =
        identifierFilter === "all"
          ? ""
          : identifierIsExact
            ? identifierFilter
            : "";
      const newBackend = newValue === "all" ? "" : isExact ? newValue : "";

      if (oldBackend !== newBackend) {
        resetBuffer();
      }

      setIdentifierIsExact(isExact);
      setIdentifierFilter(newValue);
    },
    [identifierFilter, identifierIsExact, uniqueIdentifiers, resetBuffer],
  );

  const runIdentifierFilter = useEffectEvent((value: string) => {
    applyIdentifierFilter(value);
  });

  // Debounce: when the autocomplete input settles, apply it.
  useEffect(() => {
    const trimmed = identifierInput.trim();
    const intended = trimmed === "" ? "all" : trimmed;
    if (intended === identifierFilter) {
      return;
    }
    const handle = setTimeout(() => {
      runIdentifierFilter(identifierInput);
    }, 150);
    return () => clearTimeout(handle);
  }, [identifierInput, identifierFilter]);

  const addFieldFilter = useCallback(
    (filter: string) => {
      resetBuffer();
      setFieldFilters((prev) =>
        prev.includes(filter) ? prev : [...prev, filter],
      );
    },
    [resetBuffer],
  );

  const removeFieldFilter = useCallback(
    (filter: string) => {
      resetBuffer();
      setFieldFilters((prev) => prev.filter((f) => f !== filter));
    },
    [resetBuffer],
  );

  const clearFieldFilters = useCallback(() => {
    resetBuffer();
    setFieldFilters([]);
  }, [resetBuffer]);

  const loadOlderLogs = useCallback(async () => {
    if (isLoadingOlderRef.current || !hasMoreOlder) return;
    // backlog_complete can arrive before the animation-frame buffer flushes
    // its data rows. Keep hasMoreOlder intact so the next explicit scroll or
    // "Load older logs" action can continue after that flush.
    if (logs.length === 0) return;
    let boundaryCursor: string | null = null;
    for (let index = logs.length - 1; index >= 0; index -= 1) {
      if (logs[index].cursor !== null) {
        boundaryCursor = logs[index].cursor;
        break;
      }
    }
    if (boundaryCursor === null) {
      setHasMoreOlder(false);
      return;
    }

    const generation = paginationGenerationRef.current;
    const backendIdentifier =
      identifierFilter === "all"
        ? ""
        : identifierIsExact
          ? identifierFilter
          : "";

    // Reserve room before awaiting the query so live prepends do not evict the
    // cursor boundary while this page is in flight.
    bufferLimitRef.current = Math.max(
      bufferLimitRef.current,
      logs.length + Number.parseInt(PAGE_SIZE, 10),
    );
    isLoadingOlderRef.current = true;
    setIsLoadingOlder(true);
    setPaginationError(null);
    return withPromiseCleanup(
      (async () => {
        try {
          const page = await queryClient.query({
            ...linuxio.logs.general_page({
              cursor: boundaryCursor,
              lines: PAGE_SIZE,
              timePeriod,
              priority: priorityFilter === "all" ? "" : priorityFilter,
              identifier: backendIdentifier,
              fieldFilters,
            }),
            // The parsed rows below are the source of truth. Do not retain a
            // second raw copy of every history page in React Query's cache.
            gcTime: CACHE_TTL_MS.NONE,
            staleTime: CACHE_TTL_MS.NONE,
          });
          if (generation !== paginationGenerationRef.current) return;

          const olderLogs = (Array.isArray(page.entries) ? page.entries : [])
            .map(parseLogEntry)
            .filter((entry): entry is LogEntry => entry !== null);
          setLogs((current) => {
            // A very chatty follow stream may still have displaced part of the
            // snapshot while the query ran. Restore that tail before appending
            // its strictly-older page so the cursor boundary stays gap-free.
            const withBoundary = appendUniqueLogs(
              current,
              logs,
              Number.POSITIVE_INFINITY,
            );
            const merged = appendUniqueLogs(
              withBoundary,
              olderLogs,
              Number.POSITIVE_INFINITY,
            );
            return merged;
          });
          setIdentifierSet((prev) => addIdentifiers(prev, olderLogs));
          setDisplayLimit((current) => current + DISPLAY_CHUNK);
          setHasMoreOlder(page.hasMore && olderLogs.length > 0);
        } catch (loadError) {
          if (generation !== paginationGenerationRef.current) return;
          setPaginationError(
            loadError instanceof Error
              ? loadError.message
              : "Failed to load older logs",
          );
        }
      })(),
      () => {
        if (generation === paginationGenerationRef.current) {
          isLoadingOlderRef.current = false;
          setIsLoadingOlder(false);
        }
      },
    );
  }, [
    queryClient,
    fieldFilters,
    hasMoreOlder,
    identifierFilter,
    identifierIsExact,
    logs,
    priorityFilter,
    timePeriod,
  ]);

  // Cleanup stream and timers
  useEffect(() => {
    return () => {
      paginationGenerationRef.current += 1;
      clearReconnectTimer();
      clearLoadingFallbackTimer();
      closeStream();
    };
  }, [closeStream, clearReconnectTimer, clearLoadingFallbackTimer]);

  const filteredLogs = useMemo(() => {
    // Use the live input for substring matching so typing reflects immediately.
    // Applied unconditionally: when the applied filter is exact and matches
    // the live input, this is idempotent. When the user is mid-edit over an
    // exact value, this narrows the visible set right away instead of waiting
    // for the debounce + re-stream.
    const identifierPattern = identifierInput.trim().toLowerCase() || null;
    const statusActive = unitStatusFilter !== "all";
    const searchNeedle = search.trim().toLowerCase() || null;
    if (!identifierPattern && !statusActive && !searchNeedle) return logs;

    // One pass over the buffer with an early exit per active filter, instead
    // of three chained .filter() passes on every live flush.
    return logs.filter((log) => {
      if (
        identifierPattern &&
        !log.identifier.toLowerCase().includes(identifierPattern)
      ) {
        return false;
      }

      if (statusActive) {
        // _SYSTEMD_UNIT is the trusted source-process unit; UNIT is set by
        // systemd[1] when it logs *about* a unit (e.g. "Started foo.service").
        // Check both so manager-emitted entries about a failed/running unit
        // still match the corresponding status filter — mirrors the
        // `resolveUnitTarget` logic for the row-click → services navigation.
        const raw = log.rawJson;
        const systemdUnit =
          typeof raw?._SYSTEMD_UNIT === "string" ? raw._SYSTEMD_UNIT : "";
        const aboutUnit = typeof raw?.UNIT === "string" ? raw.UNIT : "";
        if (unitStatusFilter === "no_unit") {
          if (systemdUnit !== "" || aboutUnit !== "") return false;
        } else if (
          matchingUnitNames !== null &&
          !matchingUnitNames.has(systemdUnit) &&
          !matchingUnitNames.has(aboutUnit)
        ) {
          return false;
        }
      }

      if (
        searchNeedle &&
        !log.message.toLowerCase().includes(searchNeedle) &&
        !log.identifier.toLowerCase().includes(searchNeedle)
      ) {
        return false;
      }

      return true;
    });
  }, [logs, search, identifierInput, unitStatusFilter, matchingUnitNames]);

  // Cap what we actually feed the table. Copy/Download still use the full
  // matched set above — the cap is purely a render-cost guard.
  const displayedLogs = useMemo(
    () => filteredLogs.slice(0, displayLimit),
    [displayLimit, filteredLogs],
  );
  const isTruncated = filteredLogs.length > displayLimit;

  // The virtual table owns its scroll viewport and forwards its React scroll
  // event here. Reveal already-buffered rows first, then prefetch the next
  // cursor page shortly before the user reaches the rendered end.
  const handleLogsScroll = useCallback(
    (event: UIEvent<HTMLDivElement>) => {
      if (isLoading || error) return;
      const scrollElement = event.currentTarget;
      const distanceFromBottom =
        scrollElement.scrollHeight -
        scrollElement.scrollTop -
        scrollElement.clientHeight;
      if (distanceFromBottom > SCROLL_LOAD_THRESHOLD_PX) return;

      if (displayLimit < filteredLogs.length) {
        setDisplayLimit((current) =>
          Math.min(filteredLogs.length, current + DISPLAY_CHUNK),
        );
        return;
      }
      if (hasMoreOlder) {
        void loadOlderLogs();
      }
    },
    [
      displayLimit,
      error,
      filteredLogs.length,
      hasMoreOlder,
      isLoading,
      loadOlderLogs,
    ],
  );

  const handleCopy = () => {
    if (filteredLogs.length === 0) return;
    const text = filteredLogs
      .map(
        (log) =>
          `${log.timestamp} [${getPriorityLabel(log.priority)}] ${log.identifier}: ${log.message}`,
      )
      .join("\n");
    void copyToClipboard(text);
  };

  const handleDownload = () => {
    if (filteredLogs.length === 0) return;
    const text = filteredLogs
      .map(
        (log) =>
          `${log.timestamp} [${getPriorityLabel(log.priority)}] ${log.identifier}: ${log.message}`,
      )
      .join("\n");
    const blob = new Blob([text], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "general-logs.txt";
    link.click();
    URL.revokeObjectURL(url);
  };

  // Render icon for first cell
  const renderIcon = useCallback((log: LogEntry) => {
    return (
      <div
        style={{
          display: "flex",
          alignItems: "center",
          color: getLogPriorityAccent(getPriorityColor(log.priority)),
        }}
      >
        {getPriorityIcon(log.priority)}
      </div>
    );
  }, []);

  const handleIdentifierClick = useCallback(
    (log: LogEntry, event: MouseEvent) => {
      event.stopPropagation();
      const target = resolveUnitTarget(log);
      if (!target) return;
      if (target.section === "timers") {
        void navigate({
          to: "/services/timers",
          search: { timer: target.unit },
        });
      } else if (target.section === "sockets") {
        void navigate({
          to: "/services/sockets",
          search: { socket: target.unit },
        });
      } else {
        void navigate({
          to: "/services",
          search: { service: target.unit },
        });
      }
    },
    [navigate],
  );

  // Memoized so cell component identities stay stable across flushes — a
  // fresh cell function per render remounts every visible cell in dev, where
  // the React Compiler doesn't run.
  const columns = useMemo<AppVirtualTableColumnDef<LogEntry>[]>(
    () => [
      {
        id: "severityIcon",
        header: "",
        enableSorting: false,
        cell: ({ row }) => renderIcon(row.original),
        meta: {
          getCellRenderKey: (row) => {
            const log = row as LogEntry;
            return [log.id, log.priority];
          },
          width: "40px",
        },
      },
      {
        accessorKey: "priority",
        header: "Priority",
        cell: ({ row }) => (
          <Chip
            color={getPriorityColor(row.original.priority)}
            label={getPriorityLabel(row.original.priority)}
            size="xsmall"
            variant="soft"
          />
        ),
        meta: {
          getCellRenderKey: (row) => {
            const log = row as LogEntry;
            return [log.id, log.priority];
          },
          hideBelow: "sm",
          width: "120px",
        },
      },
      {
        accessorKey: "identifier",
        header: "Identifier",
        cell: ({ row }) => {
          const log = row.original;
          const target = resolveUnitTarget(log);
          const isLinkable = target !== null;
          return isLinkable ? (
            <AppTooltip title={`Open ${target.unit} in services`}>
              <AppTypography
                className="log-identifier-link"
                noWrap
                onClick={(event) => handleIdentifierClick(log, event)}
                role="link"
                style={{ display: "inline-block" }}
                tabIndex={0}
                title={log.identifier}
                variant="body2"
              >
                {log.identifier}
              </AppTypography>
            </AppTooltip>
          ) : (
            <AppTypography noWrap title={log.identifier} variant="body2">
              {log.identifier}
            </AppTypography>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const log = row as LogEntry;
            return [log.id, log.identifier, log.rawJson];
          },
          width: "minmax(120px, 180px)",
        },
      },
      {
        accessorKey: "timestamp",
        header: "Timestamp",
        cell: ({ row }) => (
          <AppTypography noWrap title={row.original.timestamp} variant="body2">
            {row.original.timestamp}
          </AppTypography>
        ),
        meta: {
          getCellRenderKey: (row) => {
            const log = row as LogEntry;
            return [log.id, log.timestamp];
          },
          width: "120px",
        },
      },
      {
        accessorKey: "message",
        header: "Message",
        cell: ({ row }) => (
          <AppTypography color="text.secondary" noWrap variant="caption">
            {row.original.message}
          </AppTypography>
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const log = row as LogEntry;
            return [log.id, log.message];
          },
        },
      },
    ],
    [renderIcon, handleIdentifierClick],
  );

  const renderExpandedRow = useCallback(
    (row: { original: LogEntry }) => (
      <LogEntryDetails
        fieldFilters={fieldFilters}
        log={row.original}
        onAddFieldFilter={addFieldFilter}
      />
    ),
    [fieldFilters, addFieldFilter],
  );

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {/* Filters */}
      <div
        style={{
          display: "flex",
          gap: "var(--app-space-8)",
          flexWrap: "wrap",
          alignItems: "center",
          marginBottom: "var(--app-space-8)",
        }}
      >
        <AppSelect
          label="Time Period"
          onChange={(e) => handleTimePeriodChange(e.target.value)}
          size="small"
          style={{ minWidth: 150 }}
          value={timePeriod}
        >
          <option value="1h">Last 1 hour</option>
          <option value="6h">Last 6 hours</option>
          <option value="24h">Last 24 hours</option>
          <option value="7d">Last 7 days</option>
          <option value="30d">Last 30 days</option>
        </AppSelect>

        <AppSelect
          label="Priority"
          onChange={(e) => handlePriorityFilterChange(e.target.value)}
          size="small"
          style={{ minWidth: 180 }}
          value={priorityFilter}
        >
          <option value="all">All</option>
          <option value="0">Emergency and above</option>
          <option value="1">Alert and above</option>
          <option value="2">Critical and above</option>
          <option value="3">Error and above</option>
          <option value="4">Warning and above</option>
          <option value="5">Notice and above</option>
          <option value="6">Info and above</option>
          <option value="7">Debug and above</option>
        </AppSelect>

        <AppSelect
          label="Service status"
          onChange={(e) => {
            setUnitStatusFilter(e.target.value);
            setDisplayLimit(DISPLAY_LIMIT);
          }}
          size="small"
          style={{ minWidth: 160 }}
          value={unitStatusFilter}
        >
          <option value="all">All</option>
          <option value="running">Running</option>
          <option value="failed">Failed</option>
          <option value="inactive">Inactive</option>
          <option value="no_unit">Not in systemd</option>
        </AppSelect>

        <AppAutocomplete
          filterOptions={(options, { inputValue }) => {
            if (!inputValue) return options;
            const lower = inputValue.toLowerCase();
            return options.filter((opt) => opt.toLowerCase().includes(lower));
          }}
          freeSolo
          label="Identifier"
          onChange={(value) => {
            const next = value || "";
            setIdentifierInput(next);
            setDisplayLimit(DISPLAY_LIMIT);
            applyIdentifierFilter(next);
          }}
          onInputChange={(value) => {
            setIdentifierInput(value);
            setDisplayLimit(DISPLAY_LIMIT);
          }}
          options={uniqueIdentifiers}
          placeholder="All"
          shrinkLabel={true}
          size="small"
          style={{ minWidth: 180 }}
          value={identifierInput}
        />

        <AppSearchField
          label="Search logs"
          onChange={(event) => {
            setSearch(event.target.value);
            setDisplayLimit(DISPLAY_LIMIT);
          }}
          startAdornment={<Icon height={20} icon="mdi:magnify" width={20} />}
          style={{ minWidth: 220, flex: "1 1 260px" }}
          value={search}
        />
        {hasMoreOlder && (
          <AppActionIconButton
            disabled={logs.length === 0}
            icon="mdi:history"
            iconSize={20}
            label="Load older logs"
            loading={isLoadingOlder}
            onClick={() => void loadOlderLogs()}
          />
        )}
        <AppActionIconButton
          disabled={filteredLogs.length === 0}
          icon="mdi:content-copy"
          iconSize={20}
          label="Copy logs"
          onClick={handleCopy}
        />
        <AppActionIconButton
          disabled={filteredLogs.length === 0}
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
                onChange={handleLiveModeChange}
                size="small"
              />
            }
            label="Live"
          />
        </AppTooltip>
        <AppTypography fontWeight={700}>
          {isTruncated
            ? `${displayedLogs.length} of ${filteredLogs.length} shown`
            : `${filteredLogs.length} shown`}
          {isLoadingOlder ? " · loading older…" : ""}
          {!isLoadingOlder && hasMoreOlder ? " · more available" : ""}
          {backlogTruncated ? " · initial batch truncated" : ""}
        </AppTypography>
      </div>

      {fieldFilters.length > 0 && (
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            gap: "var(--app-space-4)",
            marginBottom: "var(--app-space-8)",
          }}
        >
          <AppTypography color="text.secondary" variant="body2">
            Field filters:
          </AppTypography>
          {fieldFilters.map((filter) => (
            <Chip
              color="primary"
              key={filter}
              label={filter}
              onDelete={() => removeFieldFilter(filter)}
              size="xsmall"
              style={{ maxWidth: 360 }}
              variant="soft"
            />
          ))}
          <AppActionIconButton
            icon="mdi:filter-remove"
            iconSize={18}
            label="Clear all field filters"
            onClick={clearFieldFilters}
          />
        </div>
      )}

      {isLoading && <PageLoader />}

      {error && <AppAlert severity="error">{error}</AppAlert>}

      {paginationError && (
        <AppAlert severity="warning">
          Could not load older logs: {paginationError}. Scroll to the bottom or
          use “Load older logs” to retry.
        </AppAlert>
      )}

      {!isLoading && !error && (
        <AppVirtualTable
          ariaLabel="General logs"
          columns={columns}
          data={displayedLogs}
          emptyMessage={
            logs.length === 0
              ? "No logs match the current filters."
              : "No matching logs."
          }
          fillAvailable
          getRowId={getLogRowId}
          onScroll={handleLogsScroll}
          renderExpandedContent={renderExpandedRow}
          scrollElementRef={logsBoxRef}
        />
      )}
    </div>
  );
};

export default GeneralLogsPage;
