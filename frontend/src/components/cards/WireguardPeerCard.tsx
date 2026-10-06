import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useSyncExternalStore, type CSSProperties } from "react";

import { linuxio, type Peer } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { getWireguardStatusColor } from "@/constants/statusColors";
import { CARD_PADDING_SM, GAP_SM } from "@/theme/constants";

const CARD_STYLE: CSSProperties = {
  padding: CARD_PADDING_SM,
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

export const selectPeer = (peerId: string) => (peers: Peer[]) =>
  peers.find((peer) => peer.id === peerId);

export const usePeer = (interfaceName: string, peerId: string) =>
  useQuery({
    ...linuxio.wireguard.list_peers({ interfaceName }),
    refetchOnMount: false,
    select: selectPeer(peerId),
  });

const isPeerOnline = (peer: Peer, now: number) => {
  const lastHandshake = peer.last_handshake_unix ?? 0;
  return lastHandshake > 0 && now - lastHandshake < 180;
};

// ── WireguardPeerCard ─────────────────────────────────────────────────────────

interface WireguardPeerLiveProps {
  interfaceName: string;
  peerId: string;
}

const WireguardPeerStatus = ({
  interfaceName,
  peerId,
}: WireguardPeerLiveProps) => {
  const { data: peer } = usePeer(interfaceName, peerId);
  const now = usePeerClock();

  if (!peer) return null;

  if (!peer.enabled) {
    return (
      <AppTooltip title="Removed from the interface until enabled">
        <Chip
          color={getWireguardStatusColor("Inactive")}
          label="Disabled"
          labelStyle={{ paddingInline: 6 }}
          size="xsmall"
          variant="soft"
        />
      </AppTooltip>
    );
  }

  const isOnline = isPeerOnline(peer, now);
  return (
    <AppTooltip
      title={isOnline ? "Handshake < 3 minutes" : "No recent handshake"}
    >
      <Chip
        color={getWireguardStatusColor(isOnline ? "Active" : "Inactive")}
        label={isOnline ? "Online" : "Offline"}
        labelStyle={{ paddingInline: 6 }}
        size="xsmall"
        variant="soft"
      />
    </AppTooltip>
  );
};

const WireguardPeerStats = ({
  interfaceName,
  peerId,
}: WireguardPeerLiveProps) => {
  const { data: peer } = usePeer(interfaceName, peerId);
  const now = usePeerClock();

  if (!peer) return null;

  const networks = peer.server_allowed_ips.join(", ");

  return (
    <>
      <AppTypography
        color="text.secondary"
        noWrap
        style={{
          display: "block",
          fontFamily: "var(--app-font-mono)",
          marginTop: 2,
        }}
        title={peer.address}
        variant="body2"
      >
        {peer.address || "-"}
      </AppTypography>

      <div style={{ marginTop: GAP_SM }}>
        {networks && (
          <InfoRow label="Networks" wrap>
            {networks}
          </InfoRow>
        )}
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

interface WireguardPeerToggleProps extends WireguardPeerLiveProps {
  disabled: boolean;
  onToggleEnabled: (peerId: string, enabled: boolean) => void;
  peerName: string;
}

// The switch reads the enabled flag from the peer cache so the card shell
// stays off the polling cadence.
const WireguardPeerToggle = ({
  interfaceName,
  peerId,
  peerName,
  disabled,
  onToggleEnabled,
}: WireguardPeerToggleProps) => {
  const { data: peer } = usePeer(interfaceName, peerId);
  const enabled = peer?.enabled ?? true;

  return (
    <AppSwitch
      aria-label={
        enabled ? `Disable peer ${peerName}` : `Enable peer ${peerName}`
      }
      checked={enabled}
      disabled={disabled || !peer}
      onChange={(_, checked) => onToggleEnabled(peerId, checked)}
      role="switch"
      size="small"
    />
  );
};

export interface WireguardPeerCardProps {
  interfaceName: string;
  onDelete: (peerId: string) => void;
  onDownloadConfig: (peerId: string) => void;
  onEdit: (peerId: string) => void;
  onToggleEnabled: (peerId: string, enabled: boolean) => void;
  onViewQrCode: (peerId: string) => void;
  pendingAction?: WireguardPeerAction;
  peerId: string;
  peerName: string;
}

export type WireguardPeerAction = "delete" | "download" | "toggle";

const WireguardPeerCard = ({
  interfaceName,
  peerId,
  peerName,
  onDelete,
  onDownloadConfig,
  onEdit,
  onToggleEnabled,
  onViewQrCode,
  pendingAction,
}: WireguardPeerCardProps) => {
  const busy = Boolean(pendingAction);

  return (
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
          {peerName}
        </AppTypography>
        <div style={{ marginLeft: "auto" }}>
          <WireguardPeerStatus interfaceName={interfaceName} peerId={peerId} />
        </div>
      </div>

      <WireguardPeerStats interfaceName={interfaceName} peerId={peerId} />

      <AppDivider style={{ marginBlock: 12 }} />

      {/* Actions */}
      <div
        aria-busy={busy}
        aria-label={`Actions for ${peerName}`}
        role="group"
        style={{
          display: "flex",
          alignItems: "center",
          gap: 2,
          marginTop: "auto",
        }}
      >
        <WireguardPeerToggle
          disabled={busy}
          interfaceName={interfaceName}
          onToggleEnabled={onToggleEnabled}
          peerId={peerId}
          peerName={peerName}
        />
        <AppActionIconButton
          ariaLabel="Edit peer"
          disabled={busy}
          icon="mdi:pencil"
          iconSize={20}
          label="Edit Peer"
          onClick={() => onEdit(peerId)}
        />
        <AppActionIconButton
          ariaLabel={
            pendingAction === "download"
              ? `Downloading config for ${peerName}`
              : "Download Config"
          }
          disabled={busy}
          icon="mdi:download"
          iconSize={20}
          label="Download Config"
          loading={pendingAction === "download"}
          onClick={() => onDownloadConfig(peerId)}
        />
        <AppActionIconButton
          ariaLabel="View QR Code"
          icon="mdi:qrcode"
          iconSize={20}
          label="View QR Code"
          onClick={() => onViewQrCode(peerId)}
        />
        <AppActionIconButton
          ariaLabel={
            pendingAction === "delete" ? `Deleting peer ${peerName}` : "Delete"
          }
          color="var(--app-palette-error-main)"
          disabled={busy}
          icon="mdi:delete"
          iconSize={20}
          label="Delete Peer"
          loading={pendingAction === "delete"}
          onClick={() => onDelete(peerId)}
        />
      </div>
    </FrostedCard>
  );
};

export default WireguardPeerCard;
