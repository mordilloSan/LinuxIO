import CloseIcon from "@mui/icons-material/Close";
import DeleteSweepIcon from "@mui/icons-material/DeleteSweep";
import {
  Box,
  Button,
  IconButton,
  LinearProgress,
  Tooltip,
  Typography,
} from "@mui/material";
import React from "react";

import { useFileTransfers } from "@/hooks/useFileTransfers";

interface CompletedTransfer {
  id: string;
  type: "download" | "upload" | "compression" | "extraction";
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
  } = useFileTransfers();
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
      default:
        return "Progress";
    }
  };

  const handleCancel = (transfer: (typeof transfers)[number]) => {
    if (transfer.type === "download") {
      cancelDownload(transfer.id);
    } else if (transfer.type === "upload") {
      cancelUpload(transfer.id);
    } else if (transfer.type === "compression") {
      cancelCompression(transfer.id);
    } else if (transfer.type === "extraction") {
      cancelExtraction(transfer.id);
    }
  };

  return (
    <Box sx={{ position: "relative", display: "inline-flex" }}>
      <Box
        role="button"
        tabIndex={0}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            setIsExpanded((prev) => !prev);
          }
        }}
        onClick={() => setIsExpanded((prev) => !prev)}
        sx={{
          cursor: "pointer",
          display: "flex",
          flexDirection: "column",
          gap: 0.5,
          border: "1px solid",
          borderColor: "transparent",
          borderRadius: 1,
          p: 1,
          boxShadow: isExpanded ? 4 : "none",
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
      </Box>
      {isExpanded && (
        <Box
          sx={{
            position: "absolute",
            bottom: "calc(100% + 12px)",
            right: 0,
            zIndex: 1400,
            minWidth: 220,
            maxWidth: 380,
            boxShadow: 6,
            borderRadius: 2,
            border: "1px solid",
            borderColor: "divider",
            overflow: "hidden",
            backgroundColor: "background.paper",
          }}
        >
          <Box
            sx={{
              p: 2,
              pb: 1.5,
              borderBottom: "1px solid",
              borderColor: "divider",
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
              >
              </Button>
            )}
          </Box>
          <Box
            sx={{
              p: 1.5,
              display: "flex",
              flexDirection: "column",
              gap: 2,
              maxHeight: 400,
              overflow: "auto",
            }}
          >
            {hasTransfers &&
              transfers.map((transfer) => (
                <Box key={transfer.id}>
                  <Box
                    sx={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      mb: 0.5,
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
                              : "Extracting archive..."}
                    </Typography>
                    <IconButton
                      size="small"
                      onClick={() => handleCancel(transfer)}
                      sx={{ ml: 1, p: 0.5 }}
                    >
                      <CloseIcon fontSize="small" />
                    </IconButton>
                  </Box>
                  {(() => {
                    const percentText = `${Math.round(transfer.progress)}%`;
                    const speedText =
                      "speed" in transfer
                        ? formatSpeed(
                          typeof (transfer as any).speed === "number"
                            ? (transfer as any).speed
                            : undefined,
                        )
                        : null;
                    const tooltipTitle = speedText
                      ? `${percentText} • ${speedText}`
                      : percentText;
                    return (
                      <Tooltip title={tooltipTitle} arrow placement="top">
                        <LinearProgress
                          variant="determinate"
                          value={transfer.progress}
                          sx={{ height: 6, borderRadius: 1, cursor: "pointer" }}
                        />
                      </Tooltip>
                    );
                  })()}
                </Box>
              ))}

            {hasCompletedTransfers &&
              completedTransfers.map((transfer) => (
                <Box key={transfer.id}>
                  <Box
                    sx={{
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
                              : "Extraction complete")}
                    </Typography>
                    <Typography
                      variant="caption"
                      color="success.main"
                      fontWeight="bold"
                    >
                      ✓
                    </Typography>
                  </Box>
                </Box>
              ))}

            {!hasTransfers && !hasCompletedTransfers && (
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ textAlign: "center", py: 1 }}
              >
                No active operations
              </Typography>
            )}
          </Box>
        </Box>
      )}
    </Box>
  );
};

export default FileNotifications;
