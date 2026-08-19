import { Icon } from "@iconify/react";
import {
  memo,
  useCallback,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";

import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppPopover from "@/components/ui/AppPopover";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { type ToastHistoryItem } from "@/contexts/ToastContext";
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import { useBackgroundTaskState } from "@/hooks/backgroundTasks/useBackgroundTaskState";
import { useClearToastHistory, useToastHistory } from "@/hooks/useToastHistory";
import { useAppTheme } from "@/theme";
import { iconSize as iconSizes } from "@/theme/constants";

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
    | "task";
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
    case "task":
      return "Running task";
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
    case "task":
      return "Task complete";
    default:
      return "Operation complete";
  }
};

/* Every status icon in the panel is a miniature dock tile; a row only has to
   name the color its gradient and gloss are built from. */
const tileStyle = (color: string) =>
  ({ "--app-tile-color": color }) as CSSProperties;

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
  getTransferIcon: (type: string) => { icon: ReactNode; color: string };
  onCancel: (transfer: TransferLike) => void;
  onIndexerClick: () => void;
  transfer: TransferLike;
}

const TransferItem = memo(function TransferItem({
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
  const statusText = isIndeterminate
    ? "In progress"
    : `${Math.round(transfer.progress)}%`;
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

  /* The row header already carries the status, so the caption under the bar is
     only the rate and the estimate; the bar's own tooltip keeps all three. */
  const rateParts: string[] = [];
  if (speedText) rateParts.push(speedText);
  if (timeRemainingText) rateParts.push(timeRemainingText);
  const rateText = rateParts.join(" \u2022 ");
  const detailText = [statusText, ...rateParts].join(" \u2022 ");

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
      aria-label={isIndexer ? `${label} — open indexer details` : undefined}
      tabIndex={isIndexer ? 0 : undefined}
    >
      <div
        className="app-navbar-notifications__icon"
        style={tileStyle(visuals.color)}
      >
        {visuals.icon}
      </div>
      <div className="app-navbar-notifications__content">
        <div className="app-navbar-notifications__row">
          <p className="app-navbar-notifications__title">{label}</p>
          <p className="app-navbar-notifications__status">{statusText}</p>
        </div>
        <div className="app-navbar-notifications__meta">
          <AppTooltip arrow placement="top" title={detailText}>
            <AppLinearProgress
              style={{ height: 4, borderRadius: 999 }}
              value={transfer.progress}
              variant={isIndeterminate ? "indeterminate" : "determinate"}
            />
          </AppTooltip>
          {rateText ? (
            <p className="app-navbar-notifications__caption">{rateText}</p>
          ) : null}
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

export function NavbarNotificationsDropdown() {
  const theme = useAppTheme();
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
  const { transfers } = useBackgroundTaskState();
  const {
    cancelDownload,
    cancelUpload,
    cancelCompression,
    cancelExtraction,
    cancelCopy,
    cancelMove,
    cancelTask,
    openIndexerDialog,
  } = useBackgroundTaskActions();

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

  // Keep the hide timer alive when transfers disappear because that effect
  // rerun has no replacement timer. New transfers clear and replace it above;
  // the component lifecycle owns the final cleanup.
  useEffect(() => {
    return () => window.clearTimeout(peekTimerRef.current);
  }, []);

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
      else if (transfer.type === "task") cancelTask(transfer.id);
    },
    [
      cancelDownload,
      cancelUpload,
      cancelCompression,
      cancelExtraction,
      cancelCopy,
      cancelMove,
      cancelTask,
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
        case "task":
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
      <AppButton
        aria-label={
          peekTransfer
            ? `Open notifications: ${
                peekTransfer.label
                  ? removePercentage(peekTransfer.label)
                  : getTransferTitle(peekTransfer.type)
              } ${
                peekTransfer.indeterminate === true
                  ? "in progress"
                  : `${Math.round(peekTransfer.progress)}% complete`
              }`
            : "Open notifications"
        }
        className="app-navbar-notifications__peek"
        disabled={!peekVisible}
        onClick={handlePeekClick}
        style={{
          cursor: peekVisible ? "pointer" : undefined,
          overflow: "hidden",
          maxWidth: peekVisible ? 200 : 0,
          opacity: peekVisible ? 1 : 0,
          minWidth: 0,
          padding: 0,
          border: 0,
          background: "transparent",
        }}
        tabIndex={peekVisible ? 0 : -1}
      >
        {peekTransfer && (
          <>
            <AppLinearProgress
              style={{ width: 60, height: 5, borderRadius: 1, flexShrink: 0 }}
              value={peekTransfer.progress}
              variant={
                peekTransfer.indeterminate === true
                  ? "indeterminate"
                  : "determinate"
              }
            />
            <span className="app-navbar-notifications__peek-copy">
              {peekTransfer.label
                ? removePercentage(peekTransfer.label)
                : getTransferTitle(peekTransfer.type)}{" "}
              {peekTransfer.indeterminate === true
                ? ""
                : `${Math.round(peekTransfer.progress)}%`}
            </span>
          </>
        )}
      </AppButton>

      <div className="app-navbar-dropdown">
        <AppTooltip placement="top" title="Notifications">
          <AppIconButton
            aria-label="Notifications"
            aria-controls={
              isFullOpen ? "navbar-notifications-panel" : undefined
            }
            aria-expanded={isFullOpen}
            aria-haspopup="dialog"
            className="app-navbar-notifications__trigger"
            color="primary"
            onClick={handleOpen}
            ref={ref}
            size="small"
          >
            {/* Filled only when the bell has something to report, so an idle
                footer reads as an outline rather than a solid badge. */}
            <Icon
              height={16}
              icon={totalItems === 0 ? "mdi:bell-outline" : "mdi:bell"}
              width={16}
            />
          </AppIconButton>
        </AppTooltip>

        {/* Anchored in the footer, so the panel grows upward out of the
            trigger instead of off the bottom of the viewport. */}
        <AppPopover
          anchorEl={anchorEl}
          anchorOrigin={{ vertical: "top", horizontal: "right" }}
          onClose={handleClose}
          open={isFullOpen}
          paperClassName="app-navbar-panel app-navbar-panel--notifications"
          transformOrigin={{ vertical: "bottom", horizontal: "right" }}
        >
          <div
            aria-label="Notifications"
            id="navbar-notifications-panel"
            role="dialog"
          >
            <div className="app-navbar-panel__header app-navbar-panel__header--row">
              <p className="app-navbar-panel__title">
                {totalItems === 0
                  ? "Notifications"
                  : `${totalItems} notification${totalItems === 1 ? "" : "s"}`}
              </p>
              <AppButton
                className="app-navbar-panel__action"
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

            {totalItems === 0 ? (
              <div className="app-navbar-notifications__empty">
                <Icon height={30} icon="mdi:bell-outline" width={30} />
                <p className="app-navbar-notifications__empty-copy">
                  You&apos;re all caught up.
                </p>
              </div>
            ) : (
              <ul className="app-navbar-notifications__list custom-scrollbar">
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
                              if (event.key === "Enter" || event.key === " ") {
                                event.preventDefault();
                                openIndexerDialog();
                              }
                            }
                          : undefined
                      }
                      role={isIndexer ? "button" : undefined}
                      aria-label={
                        isIndexer
                          ? `${transfer.label || getCompletedTitle(transfer.type)} — open indexer details`
                          : undefined
                      }
                      tabIndex={isIndexer ? 0 : undefined}
                    >
                      <div
                        className="app-navbar-notifications__icon"
                        style={tileStyle(theme.palette.success.main)}
                      >
                        <Icon
                          height={iconSize}
                          icon="mdi:check-circle"
                          width={iconSize}
                        />
                      </div>
                      <div className="app-navbar-notifications__content">
                        <div className="app-navbar-notifications__row">
                          <p className="app-navbar-notifications__title">
                            {transfer.label || getCompletedTitle(transfer.type)}
                          </p>
                          <p className="app-navbar-notifications__status">
                            just now
                          </p>
                        </div>
                      </div>
                    </li>
                  );
                })}

                {recentToasts.map((toastItem) => {
                  const visuals = getToastVisuals(toastItem.type);
                  const fullText = toastItem.description
                    ? `${toastItem.title} - ${toastItem.description}`
                    : toastItem.title;
                  return (
                    <li
                      className="app-navbar-notifications__item"
                      key={toastItem.id}
                    >
                      <div
                        className="app-navbar-notifications__icon"
                        style={tileStyle(visuals.color)}
                      >
                        {visuals.icon}
                      </div>
                      <div className="app-navbar-notifications__content">
                        <div className="app-navbar-notifications__row">
                          <p
                            className="app-navbar-notifications__title"
                            title={fullText}
                          >
                            {fullText}
                          </p>
                          <p className="app-navbar-notifications__status">
                            {formatTimeAgo(toastItem.createdAt)}
                          </p>
                        </div>
                        {toastItem.meta?.to ? (
                          <div className="app-navbar-notifications__meta-row">
                            {toastItem.meta.to === "/filebrowser/$" ? (
                              <AppRouterLinkButton
                                className="app-navbar-notifications__link"
                                onClick={handleClose}
                                params={toastItem.meta.params}
                                size="small"
                                to={toastItem.meta.to}
                              >
                                {toastItem.meta.label || "Open"}
                              </AppRouterLinkButton>
                            ) : (
                              <AppRouterLinkButton
                                className="app-navbar-notifications__link"
                                onClick={handleClose}
                                size="small"
                                to={toastItem.meta.to}
                              >
                                {toastItem.meta.label || "Open"}
                              </AppRouterLinkButton>
                            )}
                          </div>
                        ) : null}
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </AppPopover>
      </div>
    </>
  );
}

export default memo(NavbarNotificationsDropdown);
