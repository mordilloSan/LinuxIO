import BugReportIcon from "@mui/icons-material/BugReport";
import DownloadIcon from "@mui/icons-material/Download";
import ErrorIcon from "@mui/icons-material/Error";
import FileCopyIcon from "@mui/icons-material/FileCopy";
import InfoIcon from "@mui/icons-material/Info";
import SearchIcon from "@mui/icons-material/Search";
import WarningIcon from "@mui/icons-material/Warning";
import {
  Alert,
  Autocomplete,
  Box,
  Chip,
  FormControl,
  FormControlLabel,
  IconButton,
  InputAdornment,
  InputLabel,
  MenuItem,
  Select,
  Switch,
  TextField,
  Tooltip,
  Typography,
  useTheme,
} from "@mui/material";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { useStreamMux, generalLogsPayload, decodeString } from "@/api/linuxio";
import type { Stream } from "@/api/linuxio";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const DEFAULT_TAIL = "200";

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
  timestamp: string;
  priority: LogPriority;
  identifier: string;
  message: string;
  rawJson?: Record<string, unknown>;
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

const getPriorityColor = (
  priority: LogPriority,
  isDark: boolean,
): { bg: string; text: string } => {
  switch (priority) {
    case LogPriority.EMERGENCY:
    case LogPriority.ALERT:
    case LogPriority.CRITICAL:
      return {
        bg: isDark ? "rgba(255, 23, 68, 0.2)" : "rgba(255, 23, 68, 0.1)",
        text: "#ff1744",
      };
    case LogPriority.ERROR:
      return {
        bg: isDark ? "rgba(244, 67, 54, 0.2)" : "rgba(244, 67, 54, 0.1)",
        text: "#ff5252",
      };
    case LogPriority.WARNING:
      return {
        bg: isDark ? "rgba(255, 152, 0, 0.2)" : "rgba(255, 152, 0, 0.1)",
        text: "#ff9800",
      };
    case LogPriority.NOTICE:
      return {
        bg: isDark ? "rgba(33, 150, 243, 0.2)" : "rgba(33, 150, 243, 0.1)",
        text: "#2196f3",
      };
    case LogPriority.INFO:
      return {
        bg: isDark ? "rgba(0, 230, 118, 0.15)" : "rgba(0, 230, 118, 0.1)",
        text: "#00e676",
      };
    case LogPriority.DEBUG:
      return {
        bg: isDark ? "rgba(158, 158, 158, 0.15)" : "rgba(158, 158, 158, 0.1)",
        text: isDark ? "#9e9e9e" : "#757575",
      };
    default:
      return {
        bg: isDark ? "rgba(158, 158, 158, 0.15)" : "rgba(158, 158, 158, 0.1)",
        text: "#757575",
      };
  }
};

const getPriorityIcon = (priority: LogPriority) => {
  switch (priority) {
    case LogPriority.EMERGENCY:
    case LogPriority.ALERT:
    case LogPriority.CRITICAL:
    case LogPriority.ERROR:
      return <ErrorIcon fontSize="small" />;
    case LogPriority.WARNING:
      return <WarningIcon fontSize="small" />;
    case LogPriority.INFO:
    case LogPriority.NOTICE:
      return <InfoIcon fontSize="small" />;
    case LogPriority.DEBUG:
      return <BugReportIcon fontSize="small" />;
    default:
      return <InfoIcon fontSize="small" />;
  }
};

const GeneralLogsPage: React.FC = () => {
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [search, setSearch] = useState("");
  const [timePeriod, setTimePeriod] = useState("24h");
  const [priorityFilter, setPriorityFilter] = useState("all");
  const [identifierFilter, setIdentifierFilter] = useState("all");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const streamRef = useRef<Stream | null>(null);
  const hasReceivedData = useRef(false);
  const hasOpenedOnce = useRef(false);

  const { isOpen: muxIsOpen, openStream } = useStreamMux();

  // Extract priority from message content (e.g., "[DEBUG]", "[INFO]", "[WARN]", etc.)
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

        // Extract timestamp
        const timestamp = data.__REALTIME_TIMESTAMP
          ? new Date(
            parseInt(data.__REALTIME_TIMESTAMP) / 1000,
          ).toLocaleString()
          : new Date().toLocaleString();

        // Extract message
        const message = data.MESSAGE || "";

        // Extract priority - first try from message content, then fall back to journal PRIORITY
        const messagePriority = extractPriorityFromMessage(message);
        const journalPriority =
          typeof data.PRIORITY === "string"
            ? parseInt(data.PRIORITY, 10)
            : typeof data.PRIORITY === "number"
              ? data.PRIORITY
              : LogPriority.INFO;
        const priority = messagePriority ?? journalPriority;

        // Extract identifier (SYSLOG_IDENTIFIER or _COMM)
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

  // Scroll to top when new logs arrive (newest on top)
  useEffect(() => {
    if (liveMode && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = 0;
    }
  }, [logs, liveMode]);

  // Close stream helper
  const closeStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  const openLogsStream = useCallback(
    (
      lines: string,
      timePeriod: string,
      priority: string,
      identifier: string,
    ) => {
      if (!muxIsOpen) return false;

      hasReceivedData.current = false;

      const payload = generalLogsPayload(
        lines,
        timePeriod,
        priority,
        identifier,
      );
      const stream = openStream("general-logs", payload);

      if (!stream) {
        queueMicrotask(() => {
          setError("Failed to connect to log stream");
          setIsLoading(false);
        });
        return false;
      }

      streamRef.current = stream;

      stream.onData = (data: Uint8Array) => {
        const text = decodeString(data);
        if (!hasReceivedData.current) {
          hasReceivedData.current = true;
          setIsLoading(false);
          setError(null);
        }
        const logEntry = parseLogEntry(text.trimEnd());
        if (logEntry) {
          setLogs((prev) => [logEntry, ...prev]); // Prepend: newest on top
        }
      };

      stream.onClose = () => {
        streamRef.current = null;
        if (!hasReceivedData.current) {
          setIsLoading(false);
        }
      };
      return true;
    },
    [muxIsOpen, openStream, parseLogEntry],
  );

  // Check if identifier is an exact match (should be sent to backend)
  // Partial matches (like "linuxio" for "linuxio-auth") are filtered client-side
  const isExactIdentifier = useMemo(() => {
    return (
      identifierFilter === "all" || uniqueIdentifiers.includes(identifierFilter)
    );
  }, [identifierFilter, uniqueIdentifiers]);

  // Open stream on mount and when filters change
  // Note: Filter change handlers (handleTimePeriodChange, etc.) close the stream and clear logs
  useEffect(() => {
    if (!muxIsOpen || !liveMode || streamRef.current) {
      return;
    }

    const tail = hasOpenedOnce.current ? "0" : DEFAULT_TAIL;
    // Only send exact identifier matches to backend; partial matches are filtered client-side
    const backendIdentifier =
      identifierFilter === "all"
        ? ""
        : isExactIdentifier
          ? identifierFilter
          : "";
    if (
      openLogsStream(
        tail,
        timePeriod,
        priorityFilter === "all" ? "" : priorityFilter,
        backendIdentifier,
      )
    ) {
      hasOpenedOnce.current = true;
    }
  }, [
    muxIsOpen,
    liveMode,
    timePeriod,
    priorityFilter,
    identifierFilter,
    isExactIdentifier,
    openLogsStream,
  ]);

  const handleLiveModeChange = (
    _: React.ChangeEvent<HTMLInputElement>,
    checked: boolean,
  ) => {
    setLiveMode(checked);
    if (!checked) {
      closeStream();
      return;
    }
    setError(null);
  };

  // Filter change handlers - clear logs and reset to load historical logs
  const handleTimePeriodChange = (value: string) => {
    closeStream();
    setLogs([]);
    hasOpenedOnce.current = false; // Reset to load historical logs with new filter
    setTimePeriod(value);
  };

  const handlePriorityFilterChange = (value: string) => {
    closeStream();
    setLogs([]);
    hasOpenedOnce.current = false; // Reset to load historical logs with new filter
    setPriorityFilter(value);
  };

  const handleIdentifierFilterChange = (value: string) => {
    closeStream();
    setLogs([]);
    hasOpenedOnce.current = false; // Reset to load historical logs with new filter
    setIdentifierFilter(value);
  };

  // Cleanup stream when page unmounts
  useEffect(() => {
    return () => {
      closeStream();
    };
  }, [closeStream]);

  const filteredLogs = useMemo(() => {
    let filtered = logs;

    // Apply client-side identifier filtering for partial matches (e.g., "linuxio" matches "linuxio-auth")
    if (identifierFilter !== "all" && !isExactIdentifier) {
      const pattern = identifierFilter.toLowerCase();
      filtered = filtered.filter((log) =>
        log.identifier.toLowerCase().includes(pattern),
      );
    }

    // Apply client-side search text filtering
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
  }, [logs, search, identifierFilter, isExactIdentifier]);

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

  return (
    <Box sx={{ display: "flex", flexDirection: "column", height: "100%" }}>
      {/* Fixed Header */}
      <Box
        sx={{
          position: "sticky",
          top: 0,
          zIndex: 10,
          borderRadius: "6px",
          p: 2,
          mb: 2,
        }}
      >
        {/* Title and Actions */}
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 2,
            flexWrap: "wrap",
            mb: 1.5,
          }}
        >
          <Box sx={{ flex: 1 }} />

        </Box>

        {/* Filters */}
        <Box
          sx={{
            display: "flex",
            gap: 2,
            flexWrap: "wrap",
            alignItems: "center",
          }}
        >
          <FormControl size="small" sx={{ minWidth: 150 }}>
            <InputLabel>Time Period</InputLabel>
            <Select
              value={timePeriod}
              label="Time Period"
              onChange={(e) => handleTimePeriodChange(e.target.value)}
            >
              <MenuItem value="1h">Last 1 hour</MenuItem>
              <MenuItem value="6h">Last 6 hours</MenuItem>
              <MenuItem value="24h">Last 24 hours</MenuItem>
              <MenuItem value="7d">Last 7 days</MenuItem>
              <MenuItem value="30d">Last 30 days</MenuItem>
            </Select>
          </FormControl>

          <FormControl size="small" sx={{ minWidth: 180 }}>
            <InputLabel>Priority</InputLabel>
            <Select
              value={priorityFilter}
              label="Priority"
              onChange={(e) => handlePriorityFilterChange(e.target.value)}
            >
              <MenuItem value="all">All</MenuItem>
              <MenuItem value="0">Emergency and above</MenuItem>
              <MenuItem value="1">Alert and above</MenuItem>
              <MenuItem value="2">Critical and above</MenuItem>
              <MenuItem value="3">Error and above</MenuItem>
              <MenuItem value="4">Warning and above</MenuItem>
              <MenuItem value="5">Notice and above</MenuItem>
              <MenuItem value="6">Info and above</MenuItem>
              <MenuItem value="7">Debug and above</MenuItem>
            </Select>
          </FormControl>

          <Autocomplete
            size="small"
            freeSolo
            options={uniqueIdentifiers}
            value={identifierFilter === "all" ? "" : identifierFilter}
            onChange={(_, newValue) => {
              // Called when user selects from dropdown or clears
              handleIdentifierFilterChange(newValue || "all");
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                // When user presses Enter, use current input value
                const input = e.target as HTMLInputElement;
                handleIdentifierFilterChange(input.value || "all");
              }
            }}
            filterOptions={(options, { inputValue }) => {
              if (!inputValue) return options;
              const lower = inputValue.toLowerCase();
              return options.filter((opt) =>
                opt.toLowerCase().includes(lower),
              );
            }}
            renderInput={(params) => (
              <TextField
                {...params}
                label="Identifier"
                placeholder="All"
                sx={{ minWidth: 180 }}
              />
            )}
            sx={{ minWidth: 180 }}
          />

          <TextField
            size="small"
            placeholder="Search logs..."
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            sx={{ minWidth: 220, flex: "1 1 260px" }}
            slotProps={{
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchIcon fontSize="small" />
                  </InputAdornment>
                ),
              },
            }}
          />
          <Tooltip title="Copy logs">
            <span>
              <IconButton
                onClick={handleCopy}
                size="small"
                disabled={filteredLogs.length === 0}
              >
                <FileCopyIcon fontSize="small" />
              </IconButton>
            </span>
          </Tooltip>
          <Tooltip title="Download logs">
            <span>
              <IconButton
                onClick={handleDownload}
                size="small"
                disabled={filteredLogs.length === 0}
              >
                <DownloadIcon fontSize="small" />
              </IconButton>
            </span>
          </Tooltip>
          <Tooltip title={liveMode ? "Live streaming ON" : "Live streaming OFF"}>
            <FormControlLabel
              control={
                <Switch
                  checked={liveMode}
                  onChange={handleLiveModeChange}
                  color="primary"
                  size="small"
                />
              }
              label="Live"
              sx={{ ml: 1 }}
            />
          </Tooltip>
        </Box>
      </Box>

      {isLoading && <ComponentLoader />}

      {error && <Alert severity="error">{error}</Alert>}

      {!isLoading && !error && (
        <Box
          ref={logsBoxRef}
          className="custom-scrollbar"
          sx={{
            flex: 1,
            minHeight: 0,
            bgcolor: "background.paper",
            borderRadius: 1,
            overflow: "auto",
            border: 1,
            borderColor: "divider",
          }}
        >
          {filteredLogs.length > 0 ? (
            filteredLogs.map((log, idx) => {
              const colors = getPriorityColor(log.priority, isDark);
              return (
                <Box
                  key={idx}
                  sx={{
                    display: "flex",
                    gap: 1.5,
                    px: 2,
                    py: 1.5,
                    borderBottom: 1,
                    borderColor: "divider",
                    bgcolor:
                      idx % 2 === 0
                        ? "transparent"
                        : isDark
                          ? "rgba(255,255,255,0.02)"
                          : "rgba(0,0,0,0.02)",
                    "&:hover": {
                      bgcolor: isDark
                        ? "rgba(255,255,255,0.05)"
                        : "rgba(0,0,0,0.04)",
                    },
                    "&:last-child": {
                      borderBottom: 0,
                    },
                  }}
                >
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "flex-start",
                      color: colors.text,
                      pt: 0.25,
                    }}
                  >
                    {getPriorityIcon(log.priority)}
                  </Box>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Box
                      sx={{
                        display: "flex",
                        gap: 1,
                        alignItems: "center",
                        mb: 0.5,
                      }}
                    >
                      <Chip
                        label={getPriorityLabel(log.priority)}
                        size="small"
                        sx={{
                          bgcolor: colors.bg,
                          color: colors.text,
                          fontWeight: 600,
                          fontSize: "0.7rem",
                          height: 20,
                          border: `1px solid ${colors.text}`,
                        }}
                      />
                      <Chip
                        label={log.identifier}
                        size="small"
                        variant="outlined"
                        sx={{
                          fontSize: "0.7rem",
                          height: 20,
                          borderColor: isDark
                            ? "rgba(255,255,255,0.23)"
                            : "rgba(0,0,0,0.23)",
                        }}
                      />
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{ ml: "auto", whiteSpace: "nowrap" }}
                      >
                        {log.timestamp}
                      </Typography>
                    </Box>
                    <Typography
                      variant="body2"
                      sx={{
                        wordBreak: "break-word",
                        whiteSpace: "pre-wrap",
                        fontFamily: "monospace",
                        fontSize: "0.8rem",
                        color: "text.primary",
                      }}
                    >
                      {log.message}
                    </Typography>
                  </Box>
                </Box>
              );
            })
          ) : (
            <Box sx={{ p: 4, textAlign: "center" }}>
              <Typography color="text.secondary">
                {logs.length === 0 ? "No logs available." : "No matching logs."}
              </Typography>
            </Box>
          )}
        </Box>
      )}
    </Box>
  );
};

export default GeneralLogsPage;
