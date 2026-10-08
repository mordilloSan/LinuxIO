import { Icon } from "@iconify/react";
import {
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
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import {
  useBackgroundTask,
  useBackgroundTaskList,
  useBackgroundTasks,
} from "@/hooks/backgroundTasks/useBackgroundTaskState";
import { iconSize as iconSizes } from "@/theme/constants";

import { useAlerts } from "./useAlerts";

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
  id: string;
}

function TransferItem({
  id,
  getTransferIcon,
  onCancel,
  onIndexerClick,
}: TransferItemProps) {
  const transfer: TransferLike | null | undefined = useBackgroundTask(id);
  if (!transfer) return null;
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
              style={{ height: 4, borderRadius: "var(--app-radius-pill)" }}
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
}

function TransferPeek({
  peekOpen,
  isFullOpen,
  onClick,
}: {
  peekOpen: boolean;
  isFullOpen: boolean;
  onClick: () => void;
}) {
  const transfers = useBackgroundTasks();
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
      onClick={onClick}
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
  );
}

// --- Main component ---

export function NavbarNotificationsDropdown() {
  const ref = useRef<HTMLButtonElement>(null);
  const iconSize = iconSizes.md;

  // Full dropdown state (user-clicked)
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);
  const [now, setNow] = useState(0);
  const isFullOpen = Boolean(anchorEl);

  // Peek state (auto-triggered)
  const [peekOpen, setPeekOpen] = useState(false);
  const peekTimerRef = useRef<number>(0);

  const { alerts, unseen, markAllSeen, dismiss } = useAlerts();

  // File transfers
  const transfers = useBackgroundTaskList();
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
    if (!anchorEl && unseen > 0) markAllSeen();
    setAnchorEl((current) => (current ? null : ref.current));
  };

  const handleClose = () => setAnchorEl(null);

  const handlePeekClick = () => {
    // Clicking the peek opens the full dropdown
    window.clearTimeout(peekTimerRef.current);
    setPeekOpen(false);
    setNow(Date.now());
    if (unseen > 0) markAllSeen();
    setAnchorEl(ref.current);
  };

  const handleCancel = (transfer: TransferLike) => {
    if (transfer.type === "indexer") return;
    if (transfer.type === "download") cancelDownload(transfer.id);
    else if (transfer.type === "upload") cancelUpload(transfer.id);
    else if (transfer.type === "compression") cancelCompression(transfer.id);
    else if (transfer.type === "extraction") cancelExtraction(transfer.id);
    else if (transfer.type === "copy") cancelCopy(transfer.id);
    else if (transfer.type === "move") cancelMove(transfer.id);
    else if (transfer.type === "task") cancelTask(transfer.id);
  };

  const clearCompletedTransfers = () => setCompletedTransfers([]);

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

  const getAlertIcon = (severity: string) => {
    switch (severity) {
      case "error":
        return {
          icon: "mdi:alert-circle-outline",
          color: "var(--app-palette-error-main)",
        };
      case "warning":
        return {
          icon: "mdi:alert-outline",
          color: "var(--app-palette-warning-main)",
        };
      default:
        return {
          icon: "mdi:information-outline",
          color: "var(--app-palette-info-main)",
        };
    }
  };

  const getTransferIcon = (type: string) => {
    switch (type) {
      case "download":
      case "compression":
        return {
          icon: <Icon height={iconSize} icon="mdi:download" width={iconSize} />,
          color: "var(--app-palette-info-main)",
        };
      case "upload":
      case "extraction":
        return {
          icon: <Icon height={iconSize} icon="mdi:upload" width={iconSize} />,
          color: "var(--app-palette-info-main)",
        };
      case "indexer":
      case "copy":
      case "move":
      case "task":
        return {
          icon: (
            <Icon height={iconSize} icon="mdi:folder-sync" width={iconSize} />
          ),
          color: "var(--app-palette-info-main)",
        };
      default:
        return {
          icon: <Icon height={iconSize} icon="mdi:loading" width={iconSize} />,
          color: "var(--app-palette-text-secondary)",
        };
    }
  };

  const totalItems =
    transfers.length + completedTransfers.length + alerts.length;

  return (
    <>
      <TransferPeek
        peekOpen={peekOpen}
        isFullOpen={isFullOpen}
        onClick={handlePeekClick}
      />

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
              icon={
                unseen > 0 || transfers.length > 0
                  ? "mdi:bell"
                  : "mdi:bell-outline"
              }
              width={16}
            />
            {unseen > 0 ? (
              <span className="navbar-notifications-badge">{unseen}</span>
            ) : null}
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
              {completedTransfers.length > 0 ? (
                <AppButton
                  className="app-navbar-panel__action"
                  onClick={clearCompletedTransfers}
                  size="small"
                >
                  Clear
                </AppButton>
              ) : null}
            </div>

            {totalItems === 0 ? (
              <div className="app-navbar-notifications__empty">
                <Icon height={30} icon="mdi:bell-outline" width={30} />
                <p className="app-navbar-notifications__empty-copy">
                  No notifications
                </p>
              </div>
            ) : (
              <ul className="app-navbar-notifications__list">
                {transfers.map((transfer) => (
                  <TransferItem
                    getTransferIcon={getTransferIcon}
                    key={`transfer-${transfer.id}`}
                    onCancel={handleCancel}
                    onIndexerClick={openIndexerDialog}
                    id={transfer.id}
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
                        style={tileStyle("var(--app-palette-success-main)")}
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

                {alerts.map((alert) => {
                  const visuals = getAlertIcon(alert.severity);
                  return (
                    <li
                      className="app-navbar-notifications__item"
                      key={alert.id}
                    >
                      <div
                        className="app-navbar-notifications__icon"
                        style={tileStyle(visuals.color)}
                      >
                        <Icon
                          height={iconSize}
                          icon={visuals.icon}
                          width={iconSize}
                        />
                      </div>
                      <div className="app-navbar-notifications__content">
                        <div className="app-navbar-notifications__row">
                          <p
                            className={`app-navbar-notifications__title${alert.seen ? "" : " app-navbar-notifications__title--unseen"}`}
                            title={alert.title}
                          >
                            {alert.title}
                          </p>
                          <p className="app-navbar-notifications__status">
                            {formatTimeAgo(Date.parse(alert.lastOccurrence))}
                            {alert.occurrenceCount > 1
                              ? ` ×${alert.occurrenceCount}`
                              : ""}
                          </p>
                          <AppIconButton
                            aria-label="Dismiss alert"
                            onClick={() => dismiss(alert.id)}
                            size="small"
                          >
                            <Icon height={14} icon="mdi:close" width={14} />
                          </AppIconButton>
                        </div>
                        {alert.message ? (
                          <p className="app-navbar-notifications__caption app-navbar-notifications__message">
                            {alert.message}
                          </p>
                        ) : null}
                        {alert.link ? (
                          <div className="app-navbar-notifications__meta-row">
                            <AppRouterLinkButton
                              className="app-navbar-notifications__link"
                              onClick={handleClose}
                              size="small"
                              to={alert.link}
                            >
                              Open
                            </AppRouterLinkButton>
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

export default NavbarNotificationsDropdown;
