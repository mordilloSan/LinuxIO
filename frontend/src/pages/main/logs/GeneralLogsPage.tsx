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
  Paper,
  Select,
  Switch,
  TableCell,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  useStreamMux,
  openGeneralLogsStream,
  decodeString,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import UnifiedCollapsibleTable from "@/components/tables/UnifiedCollapsibleTable";
import type { UnifiedTableColumn } from "@/components/tables/UnifiedCollapsibleTable";
import { useLiveStream } from "@/hooks/useLiveStream";

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
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [search, setSearch] = useState("");
  const [timePeriod, setTimePeriod] = useState("24h");
  const [priorityFilter, setPriorityFilter] = useState("all");
  const [identifierFilter, setIdentifierFilter] = useState("all");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const hasReceivedData = useRef(false);
  const hasOpenedOnce = useRef(false);
  const { streamRef, openStream, closeStream } = useLiveStream();

  const { isOpen: muxIsOpen } = useStreamMux();

  // Table columns configuration - icon goes in the first empty cell, not in columns array
  const columns: UnifiedTableColumn[] = [
    {
      field: "priority",
      headerName: "Priority",
      sx: { display: { xs: "none", sm: "table-cell" } },
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

  // Scroll to top when new logs arrive
  useEffect(() => {
    if (liveMode && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = 0;
    }
  }, [logs, liveMode]);

  const openLogsStream = useCallback(
    (
      lines: string,
      timePeriod: string,
      priority: string,
      identifier: string,
    ) => {
      if (!muxIsOpen) return false;

      hasReceivedData.current = false;

      return openStream({
        open: () =>
          openGeneralLogsStream(lines, timePeriod, priority, identifier),
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
            setLogs((prev) => [logEntry, ...prev]);
          }
        },
        onClose: () => {
          if (!hasReceivedData.current) {
            setIsLoading(false);
          }
        },
      });
    },
    [muxIsOpen, parseLogEntry, openStream],
  );

  const isExactIdentifier = useMemo(() => {
    return (
      identifierFilter === "all" || uniqueIdentifiers.includes(identifierFilter)
    );
  }, [identifierFilter, uniqueIdentifiers]);

  // Open stream on mount and when filters change
  useEffect(() => {
    if (!muxIsOpen || !liveMode || streamRef.current) {
      return;
    }

    const tail = hasOpenedOnce.current ? "0" : DEFAULT_TAIL;
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
      if (!hasReceivedData.current) {
        setIsLoading(false);
      }
      return;
    }
    setError(null);
  };

  // Filter change handlers
  const handleTimePeriodChange = (value: string) => {
    closeStream();
    setLogs([]);
    hasOpenedOnce.current = false;
    setTimePeriod(value);
  };

  const handlePriorityFilterChange = (value: string) => {
    closeStream();
    setLogs([]);
    hasOpenedOnce.current = false;
    setPriorityFilter(value);
  };

  const handleIdentifierFilterChange = (value: string) => {
    closeStream();
    setLogs([]);
    hasOpenedOnce.current = false;
    setIdentifierFilter(value);
  };

  // Cleanup stream
  useEffect(() => {
    return () => {
      closeStream();
    };
  }, [closeStream]);

  const filteredLogs = useMemo(() => {
    let filtered = logs;

    if (identifierFilter !== "all" && !isExactIdentifier) {
      const pattern = identifierFilter.toLowerCase();
      filtered = filtered.filter((log) =>
        log.identifier.toLowerCase().includes(pattern),
      );
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

  // Render icon for first cell
  const renderIcon = useCallback((log: LogEntry) => {
    return (
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          color:
            getPriorityColor(log.priority) === "error"
              ? "#ff5252"
              : getPriorityColor(log.priority) === "warning"
                ? "#ff9800"
                : getPriorityColor(log.priority) === "info"
                  ? "#2196f3"
                  : getPriorityColor(log.priority) === "success"
                    ? "#00e676"
                    : "#9e9e9e",
        }}
      >
        {getPriorityIcon(log.priority)}
      </Box>
    );
  }, []);

  // Render main row content (without icon - icon goes in first cell)
  const renderMainRow = useCallback((log: LogEntry) => {
    return (
      <>
        <TableCell
          sx={{
            width: "1%",
            display: { xs: "none", sm: "table-cell" },
          }}
        >
          <Chip
            label={getPriorityLabel(log.priority)}
            size="small"
            color={getPriorityColor(log.priority) as any}
            sx={{
              fontWeight: 600,
              fontSize: "0.7rem",
            }}
          />
        </TableCell>
        <TableCell sx={{ width: "1%" }}>
          <Chip
            label={log.identifier}
            size="small"
            variant="outlined"
            sx={{ fontSize: "0.75rem" }}
          />
        </TableCell>
        <TableCell sx={{ width: "1%" }}>
          <Typography
            variant="body2"
            sx={{ fontSize: "0.85rem", whiteSpace: "nowrap" }}
          >
            {log.timestamp}
          </Typography>
        </TableCell>
        <TableCell sx={{ maxWidth: 0 }}>
          <Typography
            variant="body2"
            sx={{
              fontFamily: "monospace",
              fontSize: "0.85rem",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {log.message}
          </Typography>
        </TableCell>
      </>
    );
  }, []);

  // Render expanded content
  const renderExpandedContent = useCallback((log: LogEntry) => {
    return (
      <>
        <Typography variant="subtitle2" gutterBottom>
          <b>Full Message:</b>
        </Typography>
        <Paper
          sx={(theme) => ({
            p: 2,
            mb: 2,
            bgcolor:
              theme.palette.mode === "dark"
                ? "rgba(0,0,0,0.3)"
                : "rgba(0,0,0,0.02)",
            fontFamily: "monospace",
            fontSize: "0.85rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            maxWidth: "100%",
            overflowX: "auto",
          })}
        >
          {log.message}
        </Paper>

        {log.rawJson && (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>Raw Journal Entry:</b>
            </Typography>
            <Paper
              className="custom-scrollbar"
              sx={(theme) => ({
                p: 2,
                bgcolor:
                  theme.palette.mode === "dark"
                    ? "rgba(0,0,0,0.3)"
                    : "rgba(0,0,0,0.02)",
                fontFamily: "monospace",
                fontSize: "0.75rem",
                maxHeight: 300,
                overflowY: "auto",
                maxWidth: "100%",
                overflowX: "auto",
              })}
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
            </Paper>
          </>
        )}
      </>
    );
  }, []);

  return (
    <Box>
      {/* Filters */}
      <Box
        sx={{
          display: "flex",
          gap: 2,
          flexWrap: "wrap",
          alignItems: "center",
          mb: 2,
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
            handleIdentifierFilterChange(newValue || "all");
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              const input = e.target as HTMLInputElement;
              handleIdentifierFilterChange(input.value || "all");
            }
          }}
          filterOptions={(options, { inputValue }) => {
            if (!inputValue) return options;
            const lower = inputValue.toLowerCase();
            return options.filter((opt) => opt.toLowerCase().includes(lower));
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
                color="success"
                size="small"
              />
            }
            label="Live"
            sx={{ ml: 1 }}
          />
        </Tooltip>
        <Box fontWeight="bold">{filteredLogs.length} shown</Box>
      </Box>

      {isLoading && <ComponentLoader />}

      {error && <Alert severity="error">{error}</Alert>}

      {!isLoading && !error && (
        <Box ref={logsBoxRef}>
          <UnifiedCollapsibleTable
            data={filteredLogs}
            columns={columns}
            getRowKey={(_, index) => index}
            renderFirstCell={renderIcon}
            renderMainRow={renderMainRow}
            renderExpandedContent={renderExpandedContent}
            emptyMessage={
              logs.length === 0 ? "No logs available." : "No matching logs."
            }
          />
        </Box>
      )}
    </Box>
  );
};

export default GeneralLogsPage;
