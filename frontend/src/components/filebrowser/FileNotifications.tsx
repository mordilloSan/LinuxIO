import CloseIcon from "@mui/icons-material/Close";
import DeleteSweepIcon from "@mui/icons-material/DeleteSweep";
import {
  Button,
  IconButton,
  LinearProgress,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import { useFileTransfers } from "@/hooks/useFileTransfers";

interface CompletedTransfer {
  id: string;
  type:
    | "download"
    | "upload"
    | "compression"
    | "extraction"
    | "indexer"
    | "copy"
    | "move";
  label?: string;
  completedAt: Date;
}

const FileNotifications: React.FC = () => {
  const {
    transfers,
    cancelDownload,
    cancelUpload,
    cancelCompression,
    cancelExtraction,
    cancelCopy,
    cancelMove,
    openIndexerDialog,
  } = useFileTransfers();
  const theme = useTheme();
  const [isExpanded, setIsExpanded] = React.useState(false);
  const [completedTransfers, setCompletedTransfers] = React.useState<
    CompletedTransfer[]
  >([]);
  const hasTransfers = transfers.length > 0;
  const hasCompletedTransfers = completedTransfers.length > 0;

  const leastProgressTransfer = React.useMemo(() => {
    if (!hasTransfers) return null;
    return transfers.reduce(
      (lowest, current) =>
        current.progress < lowest.progress ? current : lowest,
      transfers[0],
    );
  }, [hasTransfers, transfers]);

  // Track completed transfers
  const prevTransfersRef = React.useRef(transfers);
  React.useEffect(() => {
    const prevTransfers = prevTransfersRef.current;
    const currentTransferIds = new Set(transfers.map((t) => t.id));

    // Find transfers that were in progress but are no longer in the list
    const completedNow = prevTransfers.filter(
      (prevTransfer) =>
        prevTransfer.progress === 100 &&
        !currentTransferIds.has(prevTransfer.id),
    );

    if (completedNow.length > 0) {
      setCompletedTransfers((prev) =>
        [
          ...completedNow.map((t) => ({
            id: t.id,
            type: t.type,
            label: t.label,
            completedAt: new Date(),
          })),
          ...prev,
        ].slice(0, 10),
      ); // Keep only last 10 completed transfers
    }

    prevTransfersRef.current = transfers;
  }, [transfers]);

  const clearCompletedTransfers = () => {
    setCompletedTransfers([]);
  };

  const removePercentage = (label: string) => {
    // Remove percentage like "(44%)" from the label
    return label.replace(/\s*\(\d+%\)\s*$/, "");
  };

  const formatSpeed = (speed?: number) => {
    if (!speed || speed <= 0) {
      return null;
    }
    const units = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
    let value = speed;
    let unitIndex = 0;
    while (value >= 1024 && unitIndex < units.length - 1) {
      value /= 1024;
      unitIndex += 1;
    }
    const formatted =
      value >= 100
        ? value.toFixed(0)
        : value >= 10
          ? value.toFixed(1)
          : value.toFixed(2);
    return `${formatted} ${units[unitIndex]}`;
  };

  const formatTimeRemaining = (seconds: number) => {
    if (seconds < 0 || !isFinite(seconds)) {
      return null;
    }

    if (seconds < 60) {
      return `${Math.round(seconds)}s`;
    }

    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = Math.round(seconds % 60);

    if (hours > 0) {
      return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
    }

    return secs > 0 ? `${minutes}m ${secs}s` : `${minutes}m`;
  };

  const getTitle = (type: (typeof transfers)[number]["type"]) => {
    switch (type) {
      case "download":
        return "Download Progress";
      case "upload":
        return "Upload Progress";
      case "compression":
        return "Compression Progress";
      case "extraction":
        return "Extraction Progress";
      case "indexer":
        return "Indexer Progress";
      case "copy":
        return "Copy Progress";
      case "move":
        return "Move Progress";
      default:
        return "Progress";
    }
  };

  const handleCancel = (transfer: (typeof transfers)[number]) => {
    // Indexer sync cannot be cancelled - it must complete
    if (transfer.type === "indexer") {
      return;
    }
    if (transfer.type === "download") {
      cancelDownload(transfer.id);
    } else if (transfer.type === "upload") {
      cancelUpload(transfer.id);
    } else if (transfer.type === "compression") {
      cancelCompression(transfer.id);
    } else if (transfer.type === "extraction") {
      cancelExtraction(transfer.id);
    } else if (transfer.type === "copy") {
      cancelCopy(transfer.id);
    } else if (transfer.type === "move") {
      cancelMove(transfer.id);
    }
  };

  const handleOperationsToggle = () => {
    setIsExpanded((prev) => !prev);
  };

  const handleOperationsKeyToggle = (
    event: React.KeyboardEvent<HTMLDivElement>,
  ) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      handleOperationsToggle();
    }
  };

  const handleIndexerRowKeyDown = (event: React.KeyboardEvent<HTMLElement>) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      openIndexerDialog();
    }
  };

  return (
    <div style={{ position: "relative", display: "inline-flex" }}>
      <div
        role="button"
        tabIndex={0}
        onKeyDown={handleOperationsKeyToggle}
        onClick={handleOperationsToggle}
        style={{
          cursor: "pointer",
          display: "flex",
          flexDirection: "column",
          gap: 2,
          border: "1px solid transparent",
          borderRadius: 4,
          padding: 4,
          boxShadow: isExpanded ? theme.shadows[4] : "none",
          whiteSpace: "nowrap",
          minWidth: 90,
        }}
      >
        {hasTransfers &&
        leastProgressTransfer?.type === "compression" ? null : (
          <Typography variant="caption" color="text.secondary">
            {hasTransfers && leastProgressTransfer
              ? getTitle(leastProgressTransfer.type)
              : "File Operations"}
          </Typography>
        )}
        {hasTransfers && leastProgressTransfer ? (
          <LinearProgress
            variant="determinate"
            value={leastProgressTransfer.progress}
            sx={{ height: 6, borderRadius: 1 }}
          />
        ) : null}
      </div>
      {isExpanded && (
        <div
          style={{
            position: "absolute",
            bottom: "calc(100% + 12px)",
            right: 0,
            zIndex: 1400,
            minWidth: 250,
            maxWidth: 380,
            boxShadow: theme.shadows[6],
            borderRadius: 8,
            border: `1px solid ${theme.palette.divider}`,
            overflow: "hidden",
            backgroundColor: theme.palette.background.paper,
          }}
        >
          <div
            style={{
              padding: "8px 8px 6px",
              borderBottom: `1px solid ${theme.palette.divider}`,
              display: "flex",
              justifyContent: "space-between",
              alignItems: "center",
            }}
          >
            <Typography variant="subtitle2" fontWeight="bold">
              File Operations
            </Typography>
            {hasCompletedTransfers && (
              <Button
                size="small"
                onClick={clearCompletedTransfers}
                startIcon={<DeleteSweepIcon />}
                sx={{ minWidth: "auto" }}
              ></Button>
            )}
          </div>
          <div
            style={{
              padding: 6,
              display: "flex",
              flexDirection: "column",
              gap: 8,
              maxHeight: 400,
              overflow: "auto",
            }}
          >
            {hasTransfers &&
              transfers.map((transfer) => {
                const isIndexerTransfer = transfer.type === "indexer";

                return (
                  <div
                    key={transfer.id}
                    role={isIndexerTransfer ? "button" : undefined}
                    tabIndex={isIndexerTransfer ? 0 : undefined}
                    onClick={isIndexerTransfer ? openIndexerDialog : undefined}
                    onKeyDown={
                      isIndexerTransfer ? handleIndexerRowKeyDown : undefined
                    }
                    style={
                      isIndexerTransfer ? { cursor: "pointer" } : undefined
                    }
                  >
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        marginBottom: 2,
                      }}
                    >
                      <Typography
                        variant="body2"
                        fontWeight="medium"
                        color="text.secondary"
                        sx={{ flex: 1 }}
                      >
                        {transfer.label
                          ? removePercentage(transfer.label)
                          : transfer.type === "download"
                            ? "Preparing archive..."
                            : transfer.type === "upload"
                              ? "Preparing upload..."
                              : transfer.type === "compression"
                                ? "Compressing selection..."
                                : transfer.type === "indexer"
                                  ? "Indexing filesystem..."
                                  : transfer.type === "copy"
                                    ? "Copying..."
                                    : transfer.type === "move"
                                      ? "Moving..."
                                      : "Extracting archive..."}
                      </Typography>
                      {transfer.type !== "indexer" && (
                        <IconButton
                          size="small"
                          onClick={() => handleCancel(transfer)}
                          sx={{ ml: 1, p: 0.5 }}
                        >
                          <CloseIcon fontSize="small" />
                        </IconButton>
                      )}
                    </div>
                    {(() => {
                      const percentText = `${Math.round(transfer.progress)}%`;
                      const speedText =
                        "speed" in transfer
                          ? formatSpeed(
                              typeof transfer.speed === "number"
                                ? transfer.speed
                                : undefined,
                            )
                          : null;

                      // Calculate time remaining if we have speed and progress data
                      let timeRemainingText: string | null = null;
                      if (
                        "speed" in transfer &&
                        typeof transfer.speed === "number" &&
                        transfer.speed > 0
                      ) {
                        // Get bytes and total from transfer if available
                        const bytesProcessed =
                          "bytes" in transfer ? transfer.bytes : undefined;
                        const totalBytes =
                          "total" in transfer ? transfer.total : undefined;

                        if (
                          bytesProcessed !== undefined &&
                          totalBytes !== undefined &&
                          totalBytes > 0
                        ) {
                          const remainingBytes = totalBytes - bytesProcessed;
                          const secondsRemaining =
                            remainingBytes / transfer.speed;
                          timeRemainingText =
                            formatTimeRemaining(secondsRemaining);
                        }
                      }

                      const tooltipParts = [percentText];
                      if (speedText) tooltipParts.push(speedText);
                      if (timeRemainingText)
                        tooltipParts.push(timeRemainingText);
                      const tooltipTitle = tooltipParts.join(" • ");

                      return (
                        <Tooltip title={tooltipTitle} arrow placement="top">
                          <LinearProgress
                            variant="determinate"
                            value={transfer.progress}
                            sx={{
                              height: 6,
                              borderRadius: 1,
                              cursor: "pointer",
                            }}
                          />
                        </Tooltip>
                      );
                    })()}
                  </div>
                );
              })}

            {hasCompletedTransfers &&
              completedTransfers.map((transfer) => {
                const isIndexerTransfer = transfer.type === "indexer";

                return (
                  <div
                    key={transfer.id}
                    role={isIndexerTransfer ? "button" : undefined}
                    tabIndex={isIndexerTransfer ? 0 : undefined}
                    onClick={isIndexerTransfer ? openIndexerDialog : undefined}
                    onKeyDown={
                      isIndexerTransfer ? handleIndexerRowKeyDown : undefined
                    }
                    style={
                      isIndexerTransfer ? { cursor: "pointer" } : undefined
                    }
                  >
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                      }}
                    >
                      <Typography
                        variant="body2"
                        fontWeight="medium"
                        color="text.secondary"
                      >
                        {transfer.label ||
                          (transfer.type === "download"
                            ? "Download complete"
                            : transfer.type === "upload"
                              ? "Upload complete"
                              : transfer.type === "compression"
                                ? "Compression complete"
                                : transfer.type === "extraction"
                                  ? "Extraction complete"
                                  : transfer.type === "indexer"
                                    ? "Indexer complete"
                                    : transfer.type === "copy"
                                      ? "Copy complete"
                                      : transfer.type === "move"
                                        ? "Move complete"
                                        : "Operation complete")}
                      </Typography>
                      <Typography
                        variant="caption"
                        color="success.main"
                        fontWeight="bold"
                      >
                        ✓
                      </Typography>
                    </div>
                  </div>
                );
              })}

            {!hasTransfers && !hasCompletedTransfers && (
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ textAlign: "center", py: 1 }}
              >
                No active operations
              </Typography>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default FileNotifications;
