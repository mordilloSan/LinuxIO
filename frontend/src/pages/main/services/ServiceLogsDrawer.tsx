import CloseIcon from "@mui/icons-material/Close";
import {
  Drawer,
  Box,
  IconButton,
  Typography,
  Alert,
  Switch,
  FormControlLabel,
  Tooltip,
} from "@mui/material";
import React, { useState, useEffect, useRef, useCallback } from "react";

import {
  useStreamMux,
  openServiceLogsStream,
  decodeString,
  type Stream,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";

interface ServiceLogsDrawerProps {
  open: boolean;
  onClose: () => void;
  serviceName: string;
}

const ServiceLogsDrawer: React.FC<ServiceLogsDrawerProps> = ({
  open,
  onClose,
  serviceName,
}) => {
  const [liveMode, setLiveMode] = useState(true);
  const [logs, setLogs] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const logsBoxRef = useRef<HTMLDivElement>(null);
  const streamRef = useRef<Stream | null>(null);
  const hasReceivedData = useRef(false);

  const { isOpen: muxIsOpen } = useStreamMux();

  // Scroll to bottom when logs change
  useEffect(() => {
    if (open && logsBoxRef.current) {
      logsBoxRef.current.scrollTop = logsBoxRef.current.scrollHeight;
    }
  }, [logs, open]);

  // Close stream helper
  const closeStream = useCallback(() => {
    if (streamRef.current) {
      streamRef.current.close();
      streamRef.current = null;
    }
  }, []);

  // Reset state helper - called from transition callbacks, not effects
  const resetState = useCallback(() => {
    closeStream();
    setLogs([]);
    setError(null);
    setLiveMode(true);
    setIsLoading(true);
    hasReceivedData.current = false;
  }, [closeStream]);

  // Open stream when drawer opens
  useEffect(() => {
    if (!open || !serviceName || !muxIsOpen) {
      return;
    }

    // Don't create duplicate streams
    if (streamRef.current) {
      return;
    }

    // Track state
    hasReceivedData.current = false;

    const stream = openServiceLogsStream(serviceName, "200");

    if (!stream) {
      queueMicrotask(() => {
        setError("Failed to connect to log stream");
        setIsLoading(false);
      });
      return;
    }

    streamRef.current = stream;

    // Handle incoming log data
    stream.onData = (data: Uint8Array) => {
      const text = decodeString(data);
      if (!hasReceivedData.current) {
        hasReceivedData.current = true;
        setIsLoading(false);
      }
      // Each data chunk is a line from journalctl
      setLogs((prev) => [...prev, text.trimEnd()]);
    };

    stream.onClose = () => {
      streamRef.current = null;
      if (!hasReceivedData.current) {
        setIsLoading(false);
      }
    };
  }, [open, serviceName, muxIsOpen]);

  // Handle liveMode toggle
  useEffect(() => {
    if (!liveMode && streamRef.current) {
      closeStream();
    } else if (
      liveMode &&
      !streamRef.current &&
      open &&
      serviceName &&
      muxIsOpen
    ) {
      // Re-open stream when live mode is re-enabled
      const stream = openServiceLogsStream(serviceName, "0");

      if (stream) {
        streamRef.current = stream;
        stream.onData = (data: Uint8Array) => {
          const text = decodeString(data);
          setLogs((prev) => [...prev, text.trimEnd()]);
        };
        stream.onClose = () => {
          streamRef.current = null;
        };
      }
    }
  }, [liveMode, open, serviceName, muxIsOpen, closeStream]);

  // Cleanup stream when drawer closes (only close stream, not state)
  useEffect(() => {
    if (!open) {
      closeStream();
    }
  }, [open, closeStream]);

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      slotProps={{
        transition: {
          onExited: () => {
            // Reset all state when drawer fully closes
            resetState();
          },
        },
      }}
    >
      <Box
        sx={{
          width: 700,
          p: 3,
          height: "100%",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <Box
          display="flex"
          justifyContent="space-between"
          alignItems="center"
          mb={2}
        >
          <Typography variant="h6">Logs: {serviceName}</Typography>
          <Box display="flex" alignItems="center" gap={1}>
            <Tooltip
              title={liveMode ? "Live streaming ON" : "Live streaming OFF"}
            >
              <FormControlLabel
                control={
                  <Switch
                    checked={liveMode}
                    onChange={(_, checked) => setLiveMode(checked)}
                    color="primary"
                    size="small"
                  />
                }
                label="Live"
              />
            </Tooltip>
            <IconButton onClick={onClose}>
              <CloseIcon />
            </IconButton>
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
              bgcolor: "#1e1e1e",
              color: "#d4d4d4",
              p: 2,
              borderRadius: 1,
              overflow: "auto",
              fontFamily: "monospace",
              fontSize: "0.85rem",
              whiteSpace: "pre-wrap",
              wordBreak: "break-all",
            }}
          >
            {logs.length > 0 ? (
              logs.map((line, idx) => <div key={idx}>{line}</div>)
            ) : (
              <Typography color="text.secondary">No logs available.</Typography>
            )}
          </Box>
        )}
      </Box>
    </Drawer>
  );
};

export default ServiceLogsDrawer;
