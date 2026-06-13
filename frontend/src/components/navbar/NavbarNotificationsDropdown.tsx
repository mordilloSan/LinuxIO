import { Icon } from "@iconify/react";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import { iconSize as iconSizes } from "@/constants";
import { type ToastHistoryItem } from "@/contexts/ToastContext";
import { useBackgroundJobActions } from "@/hooks/backgroundJobs/useBackgroundJobActions";
import { useBackgroundJobState } from "@/hooks/backgroundJobs/useBackgroundJobState";
import { useDismissibleLayer } from "@/hooks/useDismissibleLayer";
import { useClearToastHistory, useToastHistory } from "@/hooks/useToastHistory";
import { useAppTheme } from "@/theme";

const MAX_RECENT_TOASTS = 5;
const PEEK_DURATION_MS = 3000;

interface CompletedTransfer {
  completedAt: Date;
  id: string;
  label?: string;
  type:
    | "download"
    | "upload"
    | "compression"
    | "extraction"
    | "indexer"
    | "copy"
    | "move"
    | "job";
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
    case "job":
      return "Running job";
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
    case "job":
      return "Job complete";
    default:
      return "Operation complete";
  }
};

// --- Shared transfer list item ---

interface TransferLike {
  bytes?: unknown;
  id: string;
  indeterminate?: boolean;
  label?: string;
  progress: number;
  speed?: unknown;
  total?: unknown;
  type: string;
}

interface TransferItemProps {
  getTransferIcon: (type: string) => { icon: React.ReactNode; color: string };
  onCancel: (transfer: TransferLike) => void;
  onIndexerClick: () => void;
  transfer: TransferLike;
}

const TransferItem = React.memo(function TransferItem({
  transfer,
  getTransferIcon,
  onCancel,
  onIndexerClick,
}: TransferItemProps) {
  const isIndexer = transfer.type === "indexer";
  const visuals = getTransferIcon(transfer.type);
  const label = transfer.label
    ? removePercentage(transfer.label)
    : getTransferTitle(transfer.type);

  const isIndeterminate = transfer.indeterminate === true;
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

  const detailParts = isIndeterminate ? ["In progress"] : [percentText];
  if (speedText) detailParts.push(speedText);
  if (timeRemainingText) detailParts.push(timeRemainingText);
  const detailText = detailParts.join(" \u2022 ");

  return (
    <li
      className={`app-navbar-notifications__item ${isIndexer ? "app-navbar-notifications__item--interactive" : ""}`.trim()}
      onClick={isIndexer ? onIndexerClick : undefined}
      onKeyDown={
        isIndexer
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onIndexerClick();
              }
            }
          : undefined
      }
      role={isIndexer ? "button" : undefined}
      tabIndex={isIndexer ? 0 : undefined}
    >
      <div
        className="app-navbar-notifications__icon"
        style={{ color: visuals.color }}
      >
        {visuals.icon}
      </div>
      <div className="app-navbar-notifications__content">
        <p className="app-navbar-notifications__title">{label}</p>
        <div className="app-navbar-notifications__meta">
          <AppTooltip arrow placement="top" title={detailText}>
            <AppLinearProgress
              style={{ height: 5, borderRadius: 1, marginBottom: 2 }}
              value={transfer.progress}
              variant={isIndeterminate ? "indeterminate" : "determinate"}
            />
          </AppTooltip>
          <p className="app-navbar-notifications__caption">{detailText}</p>
        </div>
      </div>
      {!isIndexer ? (
        <AppIconButton
          aria-label="Cancel task"
          onClick={() => onCancel(transfer)}
          size="small"
        >
          <Icon height={22} icon="mdi:close" width={22} />
        </AppIconButton>
      ) : null}
    </li>
  );
});

// --- Main component ---

function NavbarNotificationsDropdown() {
  const theme = useAppTheme();
  const ref = useRef<HTMLButtonElement>(null);
  const iconSize = iconSizes.md;

  // Full dropdown state (user-clicked)
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);
  const [now, setNow] = useState(0);
  const isFullOpen = Boolean(anchorEl);
  const layerRef = useDismissibleLayer<HTMLDivElement>(isFullOpen, () =>
    setAnchorEl(null),
  );

  // Peek state (auto-triggered)
  const [peekOpen, setPeekOpen] = useState(false);
  const peekTimerRef = useRef<number>(0);

  const recentToasts = useToastHistory(MAX_RECENT_TOASTS);
  const clearToastHistory = useClearToastHistory();

  // File transfers
  const { transfers } = useBackgroundJobState();
  const {
    cancelDownload,
    cancelUpload,
    cancelCompression,
    cancelExtraction,
    cancelCopy,
    cancelMove,
    cancelJob,
    openIndexerDialog,
  } = useBackgroundJobActions();

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
    setAnchorEl((current) => (current ? null : ref.current));
  };

  const handleClose = () => setAnchorEl(null);

  const handlePeekClick = () => {
    // Clicking the peek opens the full dropdown
    window.clearTimeout(peekTimerRef.current);
    setPeekOpen(false);
    setNow(Date.now());
    setAnchorEl(ref.current);
  };

  const handleCancel = useCallback(
    (transfer: TransferLike) => {
      if (transfer.type === "indexer") return;
      if (transfer.type === "download") cancelDownload(transfer.id);
      else if (transfer.type === "upload") cancelUpload(transfer.id);
      else if (transfer.type === "compression") cancelCompression(transfer.id);
      else if (transfer.type === "extraction") cancelExtraction(transfer.id);
      else if (transfer.type === "copy") cancelCopy(transfer.id);
      else if (transfer.type === "move") cancelMove(transfer.id);
      else if (transfer.type === "job") cancelJob(transfer.id);
    },
    [
      cancelDownload,
      cancelUpload,
      cancelCompression,
      cancelExtraction,
      cancelCopy,
      cancelMove,
      cancelJob,
    ],
  );

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
            <Icon height={iconSize} icon="mdi:check-circle" width={iconSize} />
          ),
          color: theme.palette.success.main,
        };
      case "error":
        return {
          icon: (
            <Icon height={iconSize} icon="mdi:close-circle" width={iconSize} />
          ),
          color: theme.palette.error.main,
        };
      case "warning":
        return {
          icon: <Icon height={iconSize} icon="mdi:alert" width={iconSize} />,
          color: theme.palette.warning.main,
        };
      case "info":
        return {
          icon: (
            <Icon height={iconSize} icon="mdi:information" width={iconSize} />
          ),
          color: theme.palette.info.main,
        };
      case "loading":
        return {
          icon: <Icon height={iconSize} icon="mdi:loading" width={iconSize} />,
          color: theme.palette.text.secondary,
        };
      default:
        return {
          icon: <Icon height={iconSize} icon="mdi:bell" width={iconSize} />,
          color: theme.palette.text.secondary,
        };
    }
  };

  const getTransferIcon = useCallback(
    (type: string) => {
      switch (type) {
        case "download":
        case "compression":
          return {
            icon: (
              <Icon height={iconSize} icon="mdi:download" width={iconSize} />
            ),
            color: theme.palette.info.main,
          };
        case "upload":
        case "extraction":
          return {
            icon: <Icon height={iconSize} icon="mdi:upload" width={iconSize} />,
            color: theme.palette.info.main,
          };
        case "indexer":
        case "copy":
        case "move":
        case "job":
          return {
            icon: (
              <Icon height={iconSize} icon="mdi:folder-sync" width={iconSize} />
            ),
            color: theme.palette.info.main,
          };
        default:
          return {
            icon: (
              <Icon height={iconSize} icon="mdi:loading" width={iconSize} />
            ),
            color: theme.palette.text.secondary,
          };
      }
    },
    [iconSize, theme.palette.info.main, theme.palette.text.secondary],
  );

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
        className="app-navbar-notifications__peek"
        onClick={handlePeekClick}
        style={{
          cursor: peekVisible ? "pointer" : undefined,
          overflow: "hidden",
          maxWidth: peekVisible ? 200 : 0,
          opacity: peekVisible ? 1 : 0,
        }}
      >
        {peekTransfer && (
          <>
            <AppLinearProgress
              style={{ width: 60, height: 5, borderRadius: 1, flexShrink: 0 }}
              value={peekTransfer.progress}
              variant="determinate"
            />
            <span className="app-navbar-notifications__peek-copy">
              {peekTransfer.label
                ? removePercentage(peekTransfer.label)
                : getTransferTitle(peekTransfer.type)}{" "}
              {Math.round(peekTransfer.progress)}%
            </span>
          </>
        )}
      </div>

      <div className="app-navbar-dropdown" ref={layerRef}>
        <AppTooltip title="Notifications">
          <AppIconButton
            aria-controls={
              isFullOpen ? "navbar-notifications-panel" : undefined
            }
            aria-expanded={isFullOpen}
            aria-haspopup="dialog"
            color="inherit"
            onClick={handleOpen}
            ref={ref}
          >
            <Icon height={22} icon="mdi:bell" width={22} />
          </AppIconButton>
        </AppTooltip>

        {isFullOpen ? (
          <div
            aria-label="Notifications"
            className="app-navbar-panel app-navbar-panel--notifications"
            id="navbar-notifications-panel"
            role="dialog"
          >
            <div className="app-navbar-panel__header app-navbar-panel__header--centered">
              <p className="app-navbar-panel__title">
                {totalItems === 0
                  ? "No notifications yet"
                  : `${totalItems} notification${totalItems === 1 ? "" : "s"}`}
              </p>
            </div>

            <ul className="app-navbar-notifications__list custom-scrollbar">
              {totalItems === 0 ? (
                <li className="app-navbar-notifications__item">
                  <div className="app-navbar-notifications__content">
                    <p className="app-navbar-notifications__title">
                      You&apos;re all caught up.
                    </p>
                  </div>
                </li>
              ) : (
                <>
                  {transfers.map((transfer) => (
                    <TransferItem
                      getTransferIcon={getTransferIcon}
                      key={`transfer-${transfer.id}`}
                      onCancel={handleCancel}
                      onIndexerClick={openIndexerDialog}
                      transfer={transfer}
                    />
                  ))}

                  {completedTransfers.map((transfer) => {
                    const isIndexer = transfer.type === "indexer";
                    return (
                      <li
                        className={`app-navbar-notifications__item ${isIndexer ? "app-navbar-notifications__item--interactive" : ""}`.trim()}
                        key={`completed-${transfer.id}`}
                        onClick={isIndexer ? openIndexerDialog : undefined}
                        onKeyDown={
                          isIndexer
                            ? (event) => {
                                if (
                                  event.key === "Enter" ||
                                  event.key === " "
                                ) {
                                  event.preventDefault();
                                  openIndexerDialog();
                                }
                              }
                            : undefined
                        }
                        role={isIndexer ? "button" : undefined}
                        tabIndex={isIndexer ? 0 : undefined}
                      >
                        <div
                          className="app-navbar-notifications__icon"
                          style={{ color: "var(--app-palette-success-main)" }}
                        >
                          <Icon
                            height={iconSize}
                            icon="mdi:check-circle"
                            width={iconSize}
                          />
                        </div>
                        <div className="app-navbar-notifications__content">
                          <p className="app-navbar-notifications__title">
                            {transfer.label || getCompletedTitle(transfer.type)}
                          </p>
                          <p className="app-navbar-notifications__caption">
                            just now
                          </p>
                        </div>
                      </li>
                    );
                  })}

                  {recentToasts.map((toastItem) => {
                    const visuals = getToastVisuals(toastItem.type);
                    return (
                      <li
                        className="app-navbar-notifications__item"
                        key={toastItem.id}
                      >
                        <div
                          className="app-navbar-notifications__icon"
                          style={{ color: visuals.color }}
                        >
                          {visuals.icon}
                        </div>
                        <div className="app-navbar-notifications__content">
                          <p className="app-navbar-notifications__title">
                            {toastItem.description
                              ? `${toastItem.title} - ${toastItem.description}`
                              : toastItem.title}
                          </p>
                          <div className="app-navbar-notifications__meta-row">
                            <p className="app-navbar-notifications__caption">
                              {formatTimeAgo(toastItem.createdAt)}
                            </p>
                            {toastItem.meta?.href ? (
                              <Link
                                className="app-navbar-notifications__link"
                                onClick={handleClose}
                                to={toastItem.meta.href}
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
                            ) : null}
                          </div>
                        </div>
                      </li>
                    );
                  })}
                </>
              )}
            </ul>

            <div className="app-navbar-panel__footer">
              <AppButton
                disabled={
                  recentToastCount === 0 && completedTransfers.length === 0
                }
                onClick={() => {
                  clearToastHistory();
                  clearCompletedTransfers();
                }}
                size="small"
              >
                Clear
              </AppButton>
            </div>
          </div>
        ) : null}
      </div>
    </>
  );
}

export default React.memo(NavbarNotificationsDropdown);
