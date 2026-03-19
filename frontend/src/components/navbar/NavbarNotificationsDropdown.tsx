import { Icon } from "@iconify/react";
import {
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Popover,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { shadowSm } from "@/constants";
import { iconSize as iconSizes } from "@/constants";
import { type ToastHistoryItem } from "@/contexts/ToastContext";
import { useFileTransfers } from "@/hooks/useFileTransfers";
import { useClearToastHistory, useToastHistory } from "@/hooks/useToastHistory";

const MAX_RECENT_TOASTS = 5;
const PEEK_DURATION_MS = 3000;

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

// --- File transfer helpers ---

const removePercentage = (label: string) =>
  label.replace(/\s*\(\d+%\)\s*$/, "");

const formatSpeed = (speed?: number) => {
  if (!speed || speed <= 0) return null;
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
  if (seconds < 0 || !isFinite(seconds)) return null;
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = Math.round(seconds % 60);
  if (hours > 0) return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  return secs > 0 ? `${minutes}m ${secs}s` : `${minutes}m`;
};

const getTransferTitle = (type: string) => {
  switch (type) {
    case "download":
      return "Downloading";
    case "upload":
      return "Uploading";
    case "compression":
      return "Compressing";
    case "extraction":
      return "Extracting";
    case "indexer":
      return "Indexing";
    case "copy":
      return "Copying";
    case "move":
      return "Moving";
    default:
      return "Processing";
  }
};

const getCompletedTitle = (type: string) => {
  switch (type) {
    case "download":
      return "Download complete";
    case "upload":
      return "Upload complete";
    case "compression":
      return "Compression complete";
    case "extraction":
      return "Extraction complete";
    case "indexer":
      return "Indexing complete";
    case "copy":
      return "Copy complete";
    case "move":
      return "Move complete";
    default:
      return "Operation complete";
  }
};

// --- Shared transfer list item ---

function TransferItem({
  transfer,
  getTransferIcon,
  onCancel,
  onIndexerClick,
}: {
  transfer: {
    id: string;
    type: string;
    label?: string;
    progress: number;
    speed?: unknown;
    bytes?: unknown;
    total?: unknown;
  };
  iconSize: number;
  getTransferIcon: (type: string) => { icon: React.ReactNode; color: string };
  onCancel: () => void;
  onIndexerClick: () => void;
}) {
  const isIndexer = transfer.type === "indexer";
  const visuals = getTransferIcon(transfer.type);
  const label = transfer.label
    ? removePercentage(transfer.label)
    : getTransferTitle(transfer.type);

  const percentText = `${Math.round(transfer.progress)}%`;
  const speedText =
    typeof transfer.speed === "number" ? formatSpeed(transfer.speed) : null;

  let timeRemainingText: string | null = null;
  if (
    typeof transfer.speed === "number" &&
    transfer.speed > 0 &&
    typeof transfer.bytes === "number" &&
    typeof transfer.total === "number" &&
    transfer.total > 0
  ) {
    const remainingBytes = transfer.total - transfer.bytes;
    const secondsRemaining = remainingBytes / transfer.speed;
    timeRemainingText = formatTimeRemaining(secondsRemaining);
  }

  const detailParts = [percentText];
  if (speedText) detailParts.push(speedText);
  if (timeRemainingText) detailParts.push(timeRemainingText);
  const detailText = detailParts.join(" \u2022 ");

  return (
    <ListItem
      divider
      sx={{
        alignItems: "flex-start",
        cursor: isIndexer ? "pointer" : undefined,
      }}
      onClick={isIndexer ? onIndexerClick : undefined}
      secondaryAction={
        !isIndexer ? (
          <AppIconButton edge="end" size="small" onClick={onCancel}>
            <Icon icon="mdi:close" width={22} height={22} />
          </AppIconButton>
        ) : undefined
      }
    >
      <ListItemIcon sx={{ minWidth: 36, color: visuals.color, mt: 0.5 }}>
        {visuals.icon}
      </ListItemIcon>
      <ListItemText
        disableTypography
        primary={
          <AppTypography variant="subtitle2" color="text.primary">
            {label}
          </AppTypography>
        }
        secondary={
          <div style={{ marginTop: 4 }}>
            <AppTooltip title={detailText} arrow placement="top">
              <AppLinearProgress
                variant="determinate"
                value={transfer.progress}
                style={{ height: 5, borderRadius: 1, marginBottom: 2 }}
              />
            </AppTooltip>
            <AppTypography variant="caption" color="text.secondary">
              {detailText}
            </AppTypography>
          </div>
        }
      />
    </ListItem>
  );
}

// --- Main component ---

function NavbarNotificationsDropdown() {
  const theme = useTheme();
  const ref = useRef<HTMLButtonElement>(null);
  const iconSize = iconSizes.md;

  // Full dropdown state (user-clicked)
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);
  const [now, setNow] = useState(0);
  const isFullOpen = Boolean(anchorEl);

  // Peek state (auto-triggered)
  const [peekOpen, setPeekOpen] = useState(false);
  const peekTimerRef = useRef<number>(0);

  const recentToasts = useToastHistory(MAX_RECENT_TOASTS);
  const clearToastHistory = useClearToastHistory();

  // File transfers
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

  const [completedTransfers, setCompletedTransfers] = useState<
    CompletedTransfer[]
  >([]);

  // Track completed transfers
  const prevTransfersRef = useRef(transfers);
  useEffect(() => {
    const prevTransfers = prevTransfersRef.current;
    const currentTransferIds = new Set(transfers.map((t) => t.id));

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
      );
    }

    prevTransfersRef.current = transfers;
  }, [transfers]);

  // Auto-peek when a new transfer starts (only react to id changes, not progress)
  const transferIds = transfers.map((t) => t.id).join(",");
  const prevTransferIdsRef = useRef(transferIds);

  useEffect(() => {
    const prevIds = prevTransferIdsRef.current;
    prevTransferIdsRef.current = transferIds;

    if (transferIds === prevIds) return;

    const prevSet = new Set(prevIds ? prevIds.split(",") : []);
    const currentList = transferIds ? transferIds.split(",") : [];
    const hasNewTransfer = currentList.some((id) => id && !prevSet.has(id));

    if (hasNewTransfer && !isFullOpen) {
      window.clearTimeout(peekTimerRef.current);
      // Open peek after a microtask to avoid synchronous setState in effect
      const openTimer = window.setTimeout(() => setPeekOpen(true), 0);
      peekTimerRef.current = window.setTimeout(() => {
        setPeekOpen(false);
      }, PEEK_DURATION_MS);
      return () => window.clearTimeout(openTimer);
    }
  }, [transferIds, isFullOpen]);

  const handleOpen = () => {
    // User clicked — close peek, open full
    window.clearTimeout(peekTimerRef.current);
    setPeekOpen(false);
    setNow(Date.now());
    setAnchorEl(ref.current);
  };

  const handleClose = () => setAnchorEl(null);

  const handlePeekClick = () => {
    // Clicking the peek opens the full dropdown
    window.clearTimeout(peekTimerRef.current);
    setPeekOpen(false);
    setNow(Date.now());
    setAnchorEl(ref.current);
  };

  const handleCancel = (transfer: (typeof transfers)[number]) => {
    if (transfer.type === "indexer") return;
    if (transfer.type === "download") cancelDownload(transfer.id);
    else if (transfer.type === "upload") cancelUpload(transfer.id);
    else if (transfer.type === "compression") cancelCompression(transfer.id);
    else if (transfer.type === "extraction") cancelExtraction(transfer.id);
    else if (transfer.type === "copy") cancelCopy(transfer.id);
    else if (transfer.type === "move") cancelMove(transfer.id);
  };

  const clearCompletedTransfers = () => setCompletedTransfers([]);

  const recentToastCount = recentToasts.length;

  useEffect(() => {
    if (!isFullOpen) return;
    const intervalId = window.setInterval(() => {
      setNow(Date.now());
    }, 60_000);
    return () => {
      window.clearInterval(intervalId);
    };
  }, [isFullOpen]);

  const formatTimeAgo = (timestamp: number) => {
    if (!now) return "";
    const diff = Math.max(0, now - timestamp);
    if (diff < 60_000) return "just now";
    const minutes = Math.floor(diff / 60_000);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 7) return `${days}d ago`;
    const weeks = Math.floor(days / 7);
    if (weeks < 4) return `${weeks}w ago`;
    const months = Math.floor(days / 30);
    if (months < 12) return `${months}mo ago`;
    const years = Math.floor(days / 365);
    return `${years}y ago`;
  };

  const getToastVisuals = (type?: ToastHistoryItem["type"]) => {
    switch (type) {
      case "success":
        return {
          icon: (
            <Icon icon="mdi:check-circle" width={iconSize} height={iconSize} />
          ),
          color: theme.palette.success.main,
        };
      case "error":
        return {
          icon: (
            <Icon icon="mdi:close-circle" width={iconSize} height={iconSize} />
          ),
          color: theme.palette.error.main,
        };
      case "warning":
        return {
          icon: <Icon icon="mdi:alert" width={iconSize} height={iconSize} />,
          color: theme.palette.warning.main,
        };
      case "info":
        return {
          icon: (
            <Icon icon="mdi:information" width={iconSize} height={iconSize} />
          ),
          color: theme.palette.info.main,
        };
      case "loading":
        return {
          icon: <Icon icon="mdi:loading" width={iconSize} height={iconSize} />,
          color: theme.palette.text.secondary,
        };
      default:
        return {
          icon: <Icon icon="mdi:bell" width={iconSize} height={iconSize} />,
          color: theme.palette.text.secondary,
        };
    }
  };

  const getTransferIcon = (type: string) => {
    switch (type) {
      case "download":
      case "compression":
        return {
          icon: <Icon icon="mdi:download" width={iconSize} height={iconSize} />,
          color: theme.palette.info.main,
        };
      case "upload":
      case "extraction":
        return {
          icon: <Icon icon="mdi:upload" width={iconSize} height={iconSize} />,
          color: theme.palette.info.main,
        };
      case "indexer":
      case "copy":
      case "move":
        return {
          icon: (
            <Icon icon="mdi:folder-sync" width={iconSize} height={iconSize} />
          ),
          color: theme.palette.info.main,
        };
      default:
        return {
          icon: <Icon icon="mdi:loading" width={iconSize} height={iconSize} />,
          color: theme.palette.text.secondary,
        };
    }
  };

  const totalItems =
    transfers.length + completedTransfers.length + recentToastCount;

  // Pick the transfer with least progress for the peek
  const peekTransfer =
    transfers.length > 0
      ? transfers.reduce(
          (lowest, t) => (t.progress < lowest.progress ? t : lowest),
          transfers[0],
        )
      : null;

  const peekVisible = peekOpen && peekTransfer && !isFullOpen;

  return (
    <>
      {/* Inline peek — compact progress in the navbar */}
      <div
        onClick={handlePeekClick}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 8,
          cursor: peekVisible ? "pointer" : undefined,
          overflow: "hidden",
          maxWidth: peekVisible ? 200 : 0,
          opacity: peekVisible ? 1 : 0,
          transition: "max-width 300ms ease, opacity 300ms ease",
          whiteSpace: "nowrap",
        }}
      >
        {peekTransfer && (
          <>
            <AppLinearProgress
              variant="determinate"
              value={peekTransfer.progress}
              style={{ width: 60, height: 5, borderRadius: 1, flexShrink: 0 }}
            />
            <AppTypography
              variant="caption"
              color="inherit"
              style={{ opacity: 0.8, fontSize: "0.7rem" }}
            >
              {peekTransfer.label
                ? removePercentage(peekTransfer.label)
                : getTransferTitle(peekTransfer.type)}{" "}
              {Math.round(peekTransfer.progress)}%
            </AppTypography>
          </>
        )}
      </div>

      <AppTooltip title="Notifications">
        <AppIconButton color="inherit" ref={ref} onClick={handleOpen}>
          <Icon icon="mdi:bell" width={22} height={22} />
        </AppIconButton>
      </AppTooltip>

      {/* Full dropdown — everything */}
      <Popover
        anchorOrigin={{ vertical: "bottom", horizontal: "center" }}
        anchorEl={anchorEl}
        onClose={handleClose}
        open={isFullOpen}
        slotProps={{
          paper: {
            sx: {
              width: 360,
              border: `1px solid ${theme.palette.divider}`,
              boxShadow: shadowSm,
            },
          },
        }}
      >
        {/* Header */}
        <div
          style={{
            textAlign: "center",
            borderBottom: `1px solid ${theme.palette.divider}`,
            padding: theme.spacing(2),
          }}
        >
          <AppTypography variant="subtitle2" color="text.primary">
            {totalItems === 0
              ? "No notifications yet"
              : `${totalItems} notification${totalItems === 1 ? "" : "s"}`}
          </AppTypography>
        </div>

        {/* Unified list */}
        <List disablePadding sx={{ maxHeight: 400, overflow: "auto" }}>
          {totalItems === 0 ? (
            <ListItem>
              <ListItemText primary="You're all caught up." />
            </ListItem>
          ) : (
            <>
              {/* Active transfers - always at the top */}
              {transfers.map((transfer) => (
                <TransferItem
                  key={`transfer-${transfer.id}`}
                  transfer={transfer}
                  iconSize={iconSize}
                  getTransferIcon={getTransferIcon}
                  onCancel={() => handleCancel(transfer)}
                  onIndexerClick={openIndexerDialog}
                />
              ))}

              {/* Completed transfers */}
              {completedTransfers.map((transfer) => {
                const isIndexer = transfer.type === "indexer";
                return (
                  <ListItem
                    key={`completed-${transfer.id}`}
                    divider
                    sx={{
                      alignItems: "center",
                      cursor: isIndexer ? "pointer" : undefined,
                    }}
                    onClick={isIndexer ? openIndexerDialog : undefined}
                  >
                    <ListItemIcon
                      sx={{ minWidth: 36, color: theme.palette.success.main }}
                    >
                      <Icon
                        icon="mdi:check-circle"
                        width={iconSize}
                        height={iconSize}
                      />
                    </ListItemIcon>
                    <ListItemText
                      disableTypography
                      primary={
                        <AppTypography variant="subtitle2" color="text.primary">
                          {transfer.label || getCompletedTitle(transfer.type)}
                        </AppTypography>
                      }
                      secondary={
                        <AppTypography variant="caption" color="text.secondary">
                          just now
                        </AppTypography>
                      }
                    />
                  </ListItem>
                );
              })}

              {/* Toast notifications */}
              {recentToasts.map((toastItem) => {
                const visuals = getToastVisuals(toastItem.type);
                return (
                  <ListItem
                    key={toastItem.id}
                    divider
                    sx={{ alignItems: "center" }}
                  >
                    <ListItemIcon sx={{ minWidth: 36, color: visuals.color }}>
                      {visuals.icon}
                    </ListItemIcon>
                    <ListItemText
                      disableTypography
                      primary={
                        <AppTypography variant="subtitle2" color="text.primary">
                          {toastItem.description
                            ? `${toastItem.title} — ${toastItem.description}`
                            : toastItem.title}
                        </AppTypography>
                      }
                      secondary={
                        <div
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: theme.spacing(1),
                          }}
                        >
                          <AppTypography
                            variant="caption"
                            color="text.secondary"
                          >
                            {formatTimeAgo(toastItem.createdAt)}
                          </AppTypography>
                          {toastItem.meta?.href && (
                            <Link
                              to={toastItem.meta.href}
                              onClick={handleClose}
                              style={{ marginLeft: "auto" }}
                            >
                              <AppButton
                                size="small"
                                style={{
                                  minWidth: "auto",
                                  padding: 0,
                                  lineHeight: 1.2,
                                }}
                              >
                                {toastItem.meta.label || "Open"}
                              </AppButton>
                            </Link>
                          )}
                        </div>
                      }
                    />
                  </ListItem>
                );
              })}
            </>
          )}
        </List>

        {/* Footer */}
        <div
          style={{
            display: "flex",
            justifyContent: "center",
            gap: theme.spacing(1),
            padding: theme.spacing(1),
            borderTop: `1px solid ${theme.palette.divider}`,
          }}
        >
          <AppButton
            size="small"
            onClick={() => {
              clearToastHistory();
              clearCompletedTransfers();
            }}
            disabled={recentToastCount === 0 && completedTransfers.length === 0}
          >
            Clear
          </AppButton>
        </div>
      </Popover>
    </>
  );
}

export default React.memo(NavbarNotificationsDropdown);
