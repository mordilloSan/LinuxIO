import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useSyncExternalStore, type CSSProperties } from "react";

import { linuxio, type Peer } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { getWireguardStatusColor } from "@/constants/statusColors";
import { GAP_SM } from "@/theme/constants";

const CARD_STYLE: CSSProperties = {
  padding: 8,
  display: "flex",
  flexDirection: "column",
  height: "100%",
};

const THROUGHPUT_STYLE: CSSProperties = {
  color: "var(--app-palette-text-secondary)",
  fontWeight: 400,
};

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
  useQuery({
    ...linuxio.wireguard.list_peers({ interfaceName }),
    refetchOnMount: false,
    select: selectPeer(peerName),
  });

const isPeerOnline = (peer: Peer, now: number) => {
  const lastHandshake = peer.last_handshake_unix ?? 0;
  return lastHandshake > 0 && now - lastHandshake < 180;
};

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
        color={getWireguardStatusColor(isOnline ? "Active" : "Inactive")}
        label={isOnline ? "Online" : "Offline"}
        labelStyle={{ paddingInline: 6 }}
        size="small"
        style={{ fontSize: "0.65rem" }}
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

  const allowedIps = peer.allowed_ips?.join(", ") || "-";

  return (
    <>
      {/* Allowed IPs read as the peer's address, mirroring the interface card */}
      <AppTypography
        color="text.secondary"
        noWrap
        style={{
          display: "block",
          fontFamily: "var(--app-font-mono)",
          fontSize: "0.8rem",
          marginTop: 2,
        }}
        title={allowedIps}
        variant="body2"
      >
        {allowedIps}
      </AppTypography>

      <div style={{ marginTop: GAP_SM }}>
        <InfoRow label="Handshake">
          {formatAgo(peer.last_handshake_unix, now)}
        </InfoRow>
        <InfoRow label="Rx">
          {formatFileSize(peer.rx_bytes)}{" "}
          <span style={THROUGHPUT_STYLE}>({formatBps(peer.rx_bps)})</span>
        </InfoRow>
        <InfoRow label="Tx">
          {formatFileSize(peer.tx_bytes)}{" "}
          <span style={THROUGHPUT_STYLE}>({formatBps(peer.tx_bps)})</span>
        </InfoRow>
        <InfoRow label="Endpoint" wrap>
          {peer.endpoint || "-"}
        </InfoRow>
        <InfoRow label="Preshared Key" wrap>
          {peer.preshared_key || "-"}
        </InfoRow>
        <InfoRow label="Keep Alive">{peer.persistent_keepalive ?? "-"}</InfoRow>
      </div>
    </>
  );
};

export interface WireguardPeerCardProps {
  interfaceName: string;
  onDelete: (peerName: string) => void;
  onDownloadConfig: (peerName: string) => void;
  onViewQrCode: (peerName: string) => void;
  pendingAction?: WireguardPeerAction;
  peerName: string;
}

export type WireguardPeerAction = "delete" | "download";

const WireguardPeerCard = ({
  interfaceName,
  peerName,
  onDelete,
  onDownloadConfig,
  onViewQrCode,
  pendingAction,
}: WireguardPeerCardProps) => (
  <FrostedCard accent hoverLift style={CARD_STYLE}>
    {/* Header: icon + name + live status chip */}
    <div style={{ display: "flex", alignItems: "center", gap: GAP_SM }}>
      <Icon
        color="var(--app-palette-primary-main)"
        height={32}
        icon="mdi:account-network-outline"
        width={32}
      />
      <AppTypography
        fontWeight={600}
        noWrap
        title={peerName}
        variant="subtitle1"
      >
        {peerName || "Peer"}
      </AppTypography>
      <div style={{ marginLeft: "auto" }}>
        <WireguardPeerStatus
          interfaceName={interfaceName}
          peerName={peerName}
        />
      </div>
    </div>

    <WireguardPeerStats interfaceName={interfaceName} peerName={peerName} />

    <AppDivider style={{ marginBlock: 12 }} />

    {/* Actions */}
    <div
      aria-busy={Boolean(pendingAction)}
      aria-label={`Actions for ${peerName}`}
      role="group"
      style={{ display: "flex", gap: 2, marginTop: "auto" }}
    >
      <AppActionIconButton
        ariaLabel={
          pendingAction === "download"
            ? `Downloading config for ${peerName}`
            : "Download Config"
        }
        disabled={Boolean(pendingAction)}
        icon="mdi:download"
        iconSize={20}
        label="Download Config"
        loading={pendingAction === "download"}
        onClick={() => onDownloadConfig(peerName)}
      />
      <AppActionIconButton
        ariaLabel="View QR Code"
        icon="mdi:qrcode"
        iconSize={20}
        label="View QR Code"
        onClick={() => onViewQrCode(peerName)}
      />
      <AppActionIconButton
        ariaLabel={
          pendingAction === "delete" ? `Deleting peer ${peerName}` : "Delete"
        }
        color="var(--app-palette-error-main)"
        disabled={Boolean(pendingAction)}
        icon="mdi:delete"
        iconSize={20}
        label="Delete Peer"
        loading={pendingAction === "delete"}
        onClick={() => onDelete(peerName)}
      />
    </div>
  </FrostedCard>
);

export default WireguardPeerCard;
