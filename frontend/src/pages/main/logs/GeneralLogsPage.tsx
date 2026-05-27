import { Icon } from "@iconify/react";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useNavigate } from "react-router-dom";

import type { Service } from "@/api";
import type { UnifiedTableColumn } from "@/components/tables/UnifiedCollapsibleTable";

import {
  CACHE_TTL_MS,
  decodeString,
  linuxio,
  openGeneralLogsStream,
  useStreamMux,
} from "@/api";
import PageLoader from "@/components/loaders/PageLoader";
import UnifiedCollapsibleTable from "@/components/tables/UnifiedCollapsibleTable";
import AppAlert from "@/components/ui/AppAlert";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import Chip from "@/components/ui/AppChip";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPaper from "@/components/ui/AppPaper";
import AppSearchField from "@/components/ui/AppSearchField";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { getLogPriorityAccent } from "@/constants/statusColors";
import { useLiveStream } from "@/hooks/useLiveStream";
import { useAppTheme } from "@/theme";

const DEFAULT_TAIL = "200";
// Hard cap on in-memory buffer. Protects against the "All in window" tail blowing
// up memory on busy systems; oldest entries are dropped beyond this.
const MAX_BUFFER = 5000;
// Hard cap on how many rows we actually feed to the table. Each row carries
// an always-mounted AppCollapse + motion.div, so render cost is proportional
// to the array, not to visible rows. Copy / Download still operate on the
// full matched set.
const DISPLAY_LIMIT = 1000;

// Log priority levels (syslog standard)
enum LogPriority {
  EMERGENCY = 0,
  ALERT = 1,
  CRITICAL = 2,
  ERROR = 3,
  WARNING = 4,
  NOTICE = 5,
  INFO = 6,
  DEBUG = 7,
}

interface LogEntry {
  identifier: string;
  message: string;
  priority: LogPriority;
  rawJson?: Record<string, unknown>;
  timestamp: string;
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
): { section: string; param: string; unit: string } | null => {
  const raw = log.rawJson;
  const systemdUnit =
    typeof raw?._SYSTEMD_UNIT === "string" && raw._SYSTEMD_UNIT
      ? (raw._SYSTEMD_UNIT as string)
      : typeof raw?.UNIT === "string" && raw.UNIT
        ? (raw.UNIT as string)
        : null;

  let unit = systemdUnit;
  if (!unit) {
    const ident = log.identifier?.trim();
    if (!ident || ident === "system") return null;
    unit = ident.includes(".") ? ident : `${ident}.service`;
  }

  if (unit.endsWith(".timer")) {
    return { section: "timers", param: "timer", unit };
  }
  if (unit.endsWith(".socket")) {
    return { section: "sockets", param: "socket", unit };
  }
  if (unit.endsWith(".service")) {
    return { section: "services", param: "service", unit };
  }
  return null;
};

const GeneralLogsPage: React.FC = () => {
  const theme = useAppTheme();
  const navigate = useNavigate();
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [search, setSearch] = useState("");
  const [timePeriod, setTimePeriod] = useState("24h");
  const [priorityFilter, setPriorityFilter] = useState("all");
  const [tailSize, setTailSize] = useState<string>(DEFAULT_TAIL);
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
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const hasReceivedData = useRef(false);
  const hasOpenedOnce = useRef(false);
  // Pending log entries waiting for the next animation-frame flush. Buffering
  // here turns a per-line setState (potentially thousands per second on a
  // chatty journal) into ~60 batched updates per second.
  const pendingLogsRef = useRef<LogEntry[]>([]);
  const flushScheduledRef = useRef(false);
  const { streamRef, openStream, closeStream } = useLiveStream();

  const { isOpen: muxIsOpen } = useStreamMux();

  // Table columns configuration - icon goes in the first empty cell, not in columns array
  const columns: UnifiedTableColumn[] = [
    {
      field: "priority",
      headerName: "Priority",
      className: "app-table-hide-below-sm",
    },
    { field: "identifier", headerName: "Identifier" },
    { field: "timestamp", headerName: "Timestamp" },
    { field: "message", headerName: "Message" },
  ];

  // Extract priority from message content
  const extractPriorityFromMessage = useCallback(
    (message: string): LogPriority | null => {
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
    },
    [],
  );

  // Parse JSON log entry
  const parseLogEntry = useCallback(
    (jsonStr: string): LogEntry | null => {
      try {
        const data = JSON.parse(jsonStr);

        const timestamp = data.__REALTIME_TIMESTAMP
          ? new Date(
              parseInt(data.__REALTIME_TIMESTAMP) / 1000,
            ).toLocaleTimeString([], { hour12: false })
          : new Date().toLocaleTimeString([], { hour12: false });

        const message = data.MESSAGE || "";

        const messagePriority = extractPriorityFromMessage(message);
        const journalPriority =
          typeof data.PRIORITY === "string"
            ? parseInt(data.PRIORITY, 10)
            : typeof data.PRIORITY === "number"
              ? data.PRIORITY
              : LogPriority.INFO;
        const priority = messagePriority ?? journalPriority;

        const identifier = data.SYSLOG_IDENTIFIER || data._COMM || "system";

        return {
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
    },
    [extractPriorityFromMessage],
  );

  // Get unique identifiers from logs
  const uniqueIdentifiers = useMemo(() => {
    const identifiers = new Set<string>();
    logs.forEach((log) => identifiers.add(log.identifier));
    return Array.from(identifiers).sort();
  }, [logs]);

  // Current systemd unit states, used by the unit-status filter below.
  const { data: services = [] } = linuxio.systemd.list_services.useQuery({
    staleTime: CACHE_TTL_MS.THIRTY_SECONDS,
  });

  // Set of unit names matching the selected status. `null` means the filter is
  // either "all" (no filter) or "no_unit" (which is handled by checking for an
  // empty _SYSTEMD_UNIT field, not by Set membership).
  const matchingUnitNames = useMemo<Set<string> | null>(() => {
    if (unitStatusFilter === "all" || unitStatusFilter === "no_unit") {
      return null;
    }
    const wanted = new Set<string>();
    for (const svc of services as Service[]) {
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
  }, [services, unitStatusFilter]);

  // Scroll to top when new logs arrive
  useEffect(() => {
    if (liveMode && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = 0;
    }
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
      setLogs((prev) => {
        const combined =
          reversed.length + prev.length > MAX_BUFFER
            ? [...reversed, ...prev].slice(0, MAX_BUFFER)
            : [...reversed, ...prev];
        return combined;
      });
    });
  }, []);

  const openLogsStream = useCallback(
    (
      lines: string,
      timePeriod: string,
      priority: string,
      identifier: string,
      fields: string[],
    ) => {
      if (!muxIsOpen) return false;

      hasReceivedData.current = false;

      return openStream({
        open: () =>
          openGeneralLogsStream(
            lines,
            timePeriod,
            priority,
            identifier,
            fields,
          ),
        onOpenError: () => {
          queueMicrotask(() => {
            setError("Failed to connect to log stream");
            setIsLoading(false);
          });
        },
        onData: (data: Uint8Array) => {
          const text = decodeString(data);
          if (!hasReceivedData.current) {
            hasReceivedData.current = true;
            setIsLoading(false);
            setError(null);
          }
          const logEntry = parseLogEntry(text.trimEnd());
          if (logEntry) {
            pendingLogsRef.current.push(logEntry);
            scheduleFlush();
          }
        },
        onClose: () => {
          if (!hasReceivedData.current) {
            setIsLoading(false);
          }
        },
      });
    },
    [muxIsOpen, parseLogEntry, openStream, scheduleFlush],
  );

  // Open stream on mount and when filters change
  useEffect(() => {
    if (!muxIsOpen || !liveMode || streamRef.current) {
      return;
    }

    const tail = hasOpenedOnce.current ? "0" : tailSize;
    const backendIdentifier =
      identifierFilter === "all"
        ? ""
        : identifierIsExact
          ? identifierFilter
          : "";
    if (
      openLogsStream(
        tail,
        timePeriod,
        priorityFilter === "all" ? "" : priorityFilter,
        backendIdentifier,
        fieldFilters,
      )
    ) {
      hasOpenedOnce.current = true;
    }
  }, [
    muxIsOpen,
    liveMode,
    streamRef,
    timePeriod,
    priorityFilter,
    tailSize,
    identifierFilter,
    identifierIsExact,
    fieldFilters,
    openLogsStream,
  ]);

  const handleLiveModeChange = (
    _: React.ChangeEvent<HTMLInputElement>,
    checked: boolean,
  ) => {
    setLiveMode(checked);
    if (!checked) {
      closeStream();
      if (!hasReceivedData.current) {
        setIsLoading(false);
      }
      return;
    }
    setError(null);
  };

  // Close the stream and drop both rendered logs and queued (pre-flush) logs.
  // Used by every filter change that needs to re-issue the backend stream.
  const resetBuffer = useCallback(() => {
    closeStream();
    setLogs([]);
    pendingLogsRef.current = [];
    hasOpenedOnce.current = false;
  }, [closeStream]);

  // Filter change handlers
  const handleTimePeriodChange = (value: string) => {
    resetBuffer();
    setTimePeriod(value);
  };

  const handlePriorityFilterChange = (value: string) => {
    resetBuffer();
    setPriorityFilter(value);
  };

  const handleTailSizeChange = (value: string) => {
    resetBuffer();
    setTailSize(value);
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

  // Debounce: when the autocomplete input settles, apply it.
  useEffect(() => {
    const trimmed = identifierInput.trim();
    const intended = trimmed === "" ? "all" : trimmed;
    if (intended === identifierFilter) {
      return;
    }
    const handle = setTimeout(() => {
      applyIdentifierFilter(identifierInput);
    }, 150);
    return () => clearTimeout(handle);
  }, [identifierInput, identifierFilter, applyIdentifierFilter]);

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

  // Cleanup stream
  useEffect(() => {
    return () => {
      closeStream();
    };
  }, [closeStream]);

  const filteredLogs = useMemo(() => {
    let filtered = logs;

    // Use the live input for substring matching so typing reflects immediately.
    // Run unconditionally: when applied filter is exact and matches the live
    // input, this is idempotent. When the user is mid-edit over an exact
    // value, this narrows the visible set right away instead of waiting for
    // the debounce + re-stream.
    const liveTrimmed = identifierInput.trim();
    if (liveTrimmed) {
      const pattern = liveTrimmed.toLowerCase();
      filtered = filtered.filter((log) =>
        log.identifier.toLowerCase().includes(pattern),
      );
    }

    if (unitStatusFilter !== "all") {
      filtered = filtered.filter((log) => {
        // _SYSTEMD_UNIT is the trusted source-process unit; UNIT is set by
        // systemd[1] when it logs *about* a unit (e.g. "Started foo.service").
        // Check both so manager-emitted entries about a failed/running unit
        // still match the corresponding status filter — mirrors the
        // `resolveUnitTarget` logic for the row-click → services navigation.
        const raw = log.rawJson;
        const systemdUnit =
          typeof raw?._SYSTEMD_UNIT === "string"
            ? (raw._SYSTEMD_UNIT as string)
            : "";
        const aboutUnit =
          typeof raw?.UNIT === "string" ? (raw.UNIT as string) : "";
        if (unitStatusFilter === "no_unit") {
          return systemdUnit === "" && aboutUnit === "";
        }
        return (
          matchingUnitNames !== null &&
          (matchingUnitNames.has(systemdUnit) ||
            matchingUnitNames.has(aboutUnit))
        );
      });
    }

    const trimmed = search.trim();
    if (trimmed) {
      const needle = trimmed.toLowerCase();
      filtered = filtered.filter(
        (log) =>
          log.message.toLowerCase().includes(needle) ||
          log.identifier.toLowerCase().includes(needle),
      );
    }

    return filtered;
  }, [logs, search, identifierInput, unitStatusFilter, matchingUnitNames]);

  // Cap what we actually feed the table. Copy/Download still use the full
  // matched set above — the cap is purely a render-cost guard.
  const displayedLogs = useMemo(
    () => filteredLogs.slice(0, DISPLAY_LIMIT),
    [filteredLogs],
  );
  const isTruncated = filteredLogs.length > DISPLAY_LIMIT;

  const handleCopy = () => {
    if (filteredLogs.length === 0) return;
    const text = filteredLogs
      .map(
        (log) =>
          `${log.timestamp} [${getPriorityLabel(log.priority)}] ${log.identifier}: ${log.message}`,
      )
      .join("\n");
    navigator.clipboard.writeText(text);
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
    (log: LogEntry, event: React.MouseEvent) => {
      event.stopPropagation();
      const target = resolveUnitTarget(log);
      if (!target) return;
      const params = new URLSearchParams({
        section: target.section,
        [target.param]: target.unit,
      });
      navigate(`/services?${params.toString()}`);
    },
    [navigate],
  );

  // Render main row content (without icon - icon goes in first cell)
  const renderMainRow = useCallback(
    (log: LogEntry) => {
      const target = resolveUnitTarget(log);
      const isLinkable = target !== null;
      return (
        <>
          <AppTableCell
            className="app-table-hide-below-sm"
            style={{ width: "1%" }}
          >
            <Chip
              color={getPriorityColor(log.priority) as any}
              label={getPriorityLabel(log.priority)}
              size="small"
              style={{ fontSize: "0.7rem" }}
              variant="soft"
            />
          </AppTableCell>
          <AppTableCell style={{ width: "1%" }}>
            {isLinkable ? (
              <AppTooltip title={`Open ${target!.unit} in services`}>
                <AppTypography
                  className="log-identifier-link"
                  onClick={(e) => handleIdentifierClick(log, e)}
                  role="link"
                  style={{
                    fontSize: "0.85rem",
                    whiteSpace: "nowrap",
                    display: "inline-block",
                  }}
                  tabIndex={0}
                  variant="body2"
                >
                  {log.identifier}
                </AppTypography>
              </AppTooltip>
            ) : (
              <AppTypography
                style={{ fontSize: "0.85rem", whiteSpace: "nowrap" }}
                variant="body2"
              >
                {log.identifier}
              </AppTypography>
            )}
          </AppTableCell>
          <AppTableCell style={{ width: "1%" }}>
            <AppTypography
              style={{ fontSize: "0.83rem", whiteSpace: "nowrap" }}
              variant="body2"
            >
              {log.timestamp}
            </AppTypography>
          </AppTableCell>
          <AppTableCell style={{ maxWidth: 0 }}>
            <AppTypography
              color="text.secondary"
              noWrap
              style={{ fontSize: "0.75rem" }}
              variant="body2"
            >
              {log.message}
            </AppTypography>
          </AppTableCell>
        </>
      );
    },
    [handleIdentifierClick],
  );

  // Render expanded content
  const renderExpandedContent = useCallback(
    (log: LogEntry) => {
      const filterableEntries = collectFilterableFields(
        log.rawJson,
        fieldFilters,
      );
      return (
        <>
          {filterableEntries.length > 0 && (
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                alignItems: "center",
                gap: theme.spacing(0.75),
                marginTop: theme.spacing(-1),
                marginBottom: theme.spacing(3.5),
              }}
            >
              {filterableEntries.map(({ key, value }) => {
                const filter = `${key}=${value}`;
                return (
                  <Chip
                    color="primary"
                    key={filter}
                    label={`${key}=${value}`}
                    onClick={() => addFieldFilter(filter)}
                    size="small"
                    style={{ fontSize: "0.7rem", maxWidth: 360 }}
                    title={`Filter to entries where ${key}=${value}`}
                    variant="soft"
                  />
                );
              })}
            </div>
          )}
          <AppTypography gutterBottom variant="subtitle2">
            <b>Full Message:</b>
          </AppTypography>
          <AppPaper
            style={{
              padding: 8,
              marginBottom: 8,
              backgroundColor: theme.codeBlock.background,
              fontFamily: "monospace",
              fontSize: "0.85rem",
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
              maxWidth: "100%",
              overflowX: "auto",
            }}
          >
            {log.message}
          </AppPaper>

          {log.rawJson && (
            <>
              <AppTypography gutterBottom variant="subtitle2">
                <b>Raw Journal Entry:</b>
              </AppTypography>
              <AppPaper
                className="custom-scrollbar"
                style={{
                  padding: 8,
                  backgroundColor: theme.codeBlock.background,
                  fontFamily: "monospace",
                  fontSize: "0.75rem",
                  maxHeight: 300,
                  overflowY: "auto",
                  maxWidth: "100%",
                  overflowX: "auto",
                }}
              >
                <pre
                  style={{
                    margin: 0,
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-word",
                    overflowWrap: "anywhere",
                  }}
                >
                  {JSON.stringify(log.rawJson, null, 2)}
                </pre>
              </AppPaper>
            </>
          )}
        </>
      );
    },
    [theme, fieldFilters, addFieldFilter],
  );

  return (
    <div>
      {/* Filters */}
      <div
        style={{
          display: "flex",
          gap: theme.spacing(2),
          flexWrap: "wrap",
          alignItems: "center",
          marginBottom: theme.spacing(2),
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
          label="Lines"
          onChange={(e) => handleTailSizeChange(e.target.value)}
          size="small"
          style={{ minWidth: 130 }}
          value={tailSize}
        >
          <option value="200">200</option>
          <option value="500">500</option>
          <option value="2000">2000</option>
          <option value="all">All in window</option>
        </AppSelect>

        <AppSelect
          label="Service status"
          onChange={(e) => setUnitStatusFilter(e.target.value)}
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
            applyIdentifierFilter(next);
          }}
          onInputChange={(value) => setIdentifierInput(value)}
          options={uniqueIdentifiers}
          placeholder="All"
          shrinkLabel={true}
          size="small"
          style={{ minWidth: 180 }}
          value={identifierInput}
        />

        <AppSearchField
          label="Search logs"
          onChange={(event) => setSearch(event.target.value)}
          startAdornment={<Icon height={20} icon="mdi:magnify" width={20} />}
          style={{ minWidth: 220, flex: "1 1 260px" }}
          value={search}
        />
        <AppTooltip title="Copy logs">
          <span>
            <AppIconButton
              disabled={filteredLogs.length === 0}
              onClick={handleCopy}
              size="small"
            >
              <Icon height={20} icon="mdi:content-copy" width={20} />
            </AppIconButton>
          </span>
        </AppTooltip>
        <AppTooltip title="Download logs">
          <span>
            <AppIconButton
              disabled={filteredLogs.length === 0}
              onClick={handleDownload}
              size="small"
            >
              <Icon height={20} icon="mdi:download" width={20} />
            </AppIconButton>
          </span>
        </AppTooltip>
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
            ? `${DISPLAY_LIMIT} of ${filteredLogs.length} shown`
            : `${filteredLogs.length} shown`}
        </AppTypography>
      </div>

      {fieldFilters.length > 0 && (
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            gap: theme.spacing(1),
            marginBottom: theme.spacing(2),
          }}
        >
          <AppTypography
            color="text.secondary"
            style={{ fontSize: "0.8rem" }}
            variant="body2"
          >
            Field filters:
          </AppTypography>
          {fieldFilters.map((filter) => (
            <Chip
              color="primary"
              key={filter}
              label={filter}
              onDelete={() => removeFieldFilter(filter)}
              size="small"
              style={{ fontSize: "0.7rem", maxWidth: 360 }}
              variant="soft"
            />
          ))}
          <AppTooltip title="Clear all field filters">
            <AppIconButton onClick={clearFieldFilters} size="small">
              <Icon height={18} icon="mdi:filter-remove" width={18} />
            </AppIconButton>
          </AppTooltip>
        </div>
      )}

      {isLoading && <PageLoader />}

      {error && <AppAlert severity="error">{error}</AppAlert>}

      {!isLoading && !error && (
        <div ref={logsBoxRef}>
          <UnifiedCollapsibleTable
            columns={columns}
            data={displayedLogs}
            emptyMessage={
              logs.length === 0 ? "No logs available." : "No matching logs."
            }
            getRowKey={(_, index) => index}
            renderExpandedContent={renderExpandedContent}
            renderFirstCell={renderIcon}
            renderMainRow={renderMainRow}
          />
        </div>
      )}
    </div>
  );
};

export default GeneralLogsPage;
