import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useSyncExternalStore, type ReactNode } from "react";

import { linuxio, type Peer } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCardContent from "@/components/ui/AppCardContent";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";

// ── Local format helpers ──────────────────────────────────────────────────────

function formatFileSize(n?: number): string {
  if (n == null) return "-";
  const abs = Math.abs(n);
  if (abs < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let i = -1;
  let val = n;
  do {
    val /= 1024;
    i++;
  } while (Math.abs(val) >= 1024 && i < units.length - 1);
  return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[i]}`;
}

function formatBps(n?: number): string {
  if (n == null) return "-";
  const abs = Math.abs(n);
  if (abs < 1024) return `${n.toFixed(0)} B/s`;
  const units = ["KiB/s", "MiB/s", "GiB/s", "TiB/s"];
  let i = -1;
  let val = n;
  do {
    val /= 1024;
    i++;
  } while (Math.abs(val) >= 1024 && i < units.length - 1);
  return `${val.toFixed(val >= 100 ? 0 : val >= 10 ? 1 : 2)} ${units[i]}`;
}

function formatAgo(unix: number | undefined, now: number): string {
  if (!unix) return "never";
  const diff = Math.max(0, Math.floor(now - unix));
  if (diff < 60) return `${diff}s ago`;
  const m = Math.floor(diff / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

// ── Shared peer clock ────────────────────────────────────────────────────────

const PEER_CLOCK_INTERVAL_MS = 3000;
const peerClockListeners = new Set<() => void>();
let peerClockNow = Date.now() / 1000;
let peerClockTimer: ReturnType<typeof setInterval> | undefined;

const getPeerClockSnapshot = () => peerClockNow;

const subscribePeerClock = (listener: () => void) => {
  peerClockListeners.add(listener);

  if (peerClockListeners.size === 1) {
    peerClockNow = Date.now() / 1000;
    peerClockTimer = setInterval(() => {
      peerClockNow = Date.now() / 1000;
      peerClockListeners.forEach((notify) => notify());
    }, PEER_CLOCK_INTERVAL_MS);
  }

  return () => {
    peerClockListeners.delete(listener);
    if (peerClockListeners.size === 0 && peerClockTimer !== undefined) {
      clearInterval(peerClockTimer);
      peerClockTimer = undefined;
    }
  };
};

const usePeerClock = () =>
  useSyncExternalStore(
    subscribePeerClock,
    getPeerClockSnapshot,
    getPeerClockSnapshot,
  );

// ── Peer cache observer ──────────────────────────────────────────────────────

export const selectPeer = (peerName: string) => (peers: Peer[]) =>
  peers.find((peer) => peer.name === peerName);

const usePeer = (interfaceName: string, peerName: string) =>
  useQuery(
    linuxio.wireguard.list_peers.queryOptions(interfaceName, {
      refetchOnMount: false,
      select: selectPeer(peerName),
    }),
  );

const isPeerOnline = (peer: Peer, now: number) => {
  const lastHandshake = peer.last_handshake_unix ?? 0;
  return lastHandshake > 0 && now - lastHandshake < 180;
};

// ── PeerCardRow ───────────────────────────────────────────────────────────────

interface PeerCardRowProps {
  label: string;
  noDivider?: boolean;
  value: ReactNode;
  wrap?: boolean;
}

const PeerCardRow = ({
  label,
  value,
  wrap = false,
  noDivider = false,
}: PeerCardRowProps) => (
  <div
    style={{
      display: "flex",
      alignItems: wrap ? "flex-start" : "baseline",
      justifyContent: "space-between",
      gap: 8,
      padding: "4px 0",
      borderBottom: noDivider ? "none" : "1px solid var(--app-palette-divider)",
    }}
  >
    <AppTypography
      color="text.secondary"
      style={{
        textTransform: "uppercase",
        letterSpacing: "0.06em",
        fontSize: "0.62rem",
        flexShrink: 0,
        paddingTop: wrap ? 2 : 0,
      }}
      variant="caption"
    >
      {label}
    </AppTypography>
    <AppTypography
      fontWeight={500}
      noWrap={!wrap}
      style={{
        marginLeft: "auto",
        minWidth: 0,
        textAlign: "right",
        ...(wrap ? { whiteSpace: "normal", overflowWrap: "anywhere" } : {}),
      }}
      variant="body2"
    >
      {value}
    </AppTypography>
  </div>
);

// ── WireguardPeerCard ─────────────────────────────────────────────────────────

interface WireguardPeerLiveProps {
  interfaceName: string;
  peerName: string;
}

const WireguardPeerStatus = ({
  interfaceName,
  peerName,
}: WireguardPeerLiveProps) => {
  const { data: peer } = usePeer(interfaceName, peerName);
  const now = usePeerClock();

  if (!peer) return null;

  const isOnline = isPeerOnline(peer, now);
  return (
    <AppTooltip
      title={isOnline ? "Handshake < 3 minutes" : "No recent handshake"}
    >
      <Chip
        color={isOnline ? "success" : "default"}
        label={isOnline ? "Online" : "Offline"}
        size="small"
        variant="soft"
      />
    </AppTooltip>
  );
};

const WireguardPeerStats = ({
  interfaceName,
  peerName,
}: WireguardPeerLiveProps) => {
  const { data: peer } = usePeer(interfaceName, peerName);
  const now = usePeerClock();

  if (!peer) return null;

  return (
    <div style={{ marginTop: 6 }}>
      <PeerCardRow
        label="Handshake"
        value={formatAgo(peer.last_handshake_unix, now)}
      />
      <PeerCardRow
        label="Rx"
        value={
          <>
            {formatFileSize(peer.rx_bytes)}{" "}
            <span
              style={{
                color: "var(--app-palette-text-secondary)",
                fontWeight: 400,
              }}
            >
              ({formatBps(peer.rx_bps)})
            </span>
          </>
        }
      />
      <PeerCardRow
        label="Tx"
        value={
          <>
            {formatFileSize(peer.tx_bytes)}{" "}
            <span
              style={{
                color: "var(--app-palette-text-secondary)",
                fontWeight: 400,
              }}
            >
              ({formatBps(peer.tx_bps)})
            </span>
          </>
        }
      />
      <PeerCardRow
        label="Allowed IPs"
        value={(peer.allowed_ips && peer.allowed_ips.join(", ")) || "-"}
        wrap
      />
      <PeerCardRow label="Endpoint" value={peer.endpoint || "-"} wrap />
      <PeerCardRow
        label="Preshared Key"
        value={peer.preshared_key || "-"}
        wrap
      />
      <PeerCardRow
        label="Keep Alive"
        noDivider
        value={peer.persistent_keepalive ?? "-"}
      />
    </div>
  );
};

export interface WireguardPeerCardProps {
  interfaceName: string;
  onDelete: (peerName: string) => void;
  onDownloadConfig: (peerName: string) => void;
  onViewQrCode: (peerName: string) => void;
  peerName: string;
}

const WireguardPeerCard = ({
  interfaceName,
  peerName,
  onDelete,
  onDownloadConfig,
  onViewQrCode,
}: WireguardPeerCardProps) => (
  <FrostedCard>
    <AppCardContent>
      {/* Header: name + status + actions */}
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <AppTypography style={{ fontSize: "1.1rem" }} variant="h6">
            {peerName || "Peer"}
          </AppTypography>
          <WireguardPeerStatus
            interfaceName={interfaceName}
            peerName={peerName}
          />
        </div>
        <div style={{ display: "flex" }}>
          <AppIconButton
            aria-label="Delete"
            color="error"
            onClick={() => onDelete(peerName)}
          >
            <Icon height={22} icon="mdi:delete" width={22} />
          </AppIconButton>
          <AppIconButton
            aria-label="Download Config"
            onClick={() => onDownloadConfig(peerName)}
          >
            <Icon height={22} icon="mdi:download" width={22} />
          </AppIconButton>
          <AppIconButton
            aria-label="View QR Code"
            onClick={() => onViewQrCode(peerName)}
          >
            <Icon height={22} icon="mdi:qrcode" width={22} />
          </AppIconButton>
        </div>
      </div>

      <WireguardPeerStats interfaceName={interfaceName} peerName={peerName} />
    </AppCardContent>
  </FrostedCard>
);

export default WireguardPeerCard;
