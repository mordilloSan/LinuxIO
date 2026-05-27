import { Icon } from "@iconify/react";
import React from "react";

import { type Peer } from "@/api";
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

function formatAgo(unix?: number): string {
  if (!unix) return "never";
  const diff = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  if (diff < 60) return `${diff}s ago`;
  const m = Math.floor(diff / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

// ── PeerCardRow ───────────────────────────────────────────────────────────────

interface PeerCardRowProps {
  label: string;
  noDivider?: boolean;
  value: React.ReactNode;
  wrap?: boolean;
}

const PeerCardRow: React.FC<PeerCardRowProps> = ({
  label,
  value,
  wrap = false,
  noDivider = false,
}) => (
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

export interface WireguardPeerCardProps {
  isOnline: boolean;
  onDelete: () => void;
  onDownloadConfig: () => void;
  onViewQrCode: () => void;
  peer: Peer;
}

const WireguardPeerCard: React.FC<WireguardPeerCardProps> = ({
  peer,
  isOnline,
  onDelete,
  onDownloadConfig,
  onViewQrCode,
}) => (
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
            {peer.name || "Peer"}
          </AppTypography>
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
        </div>
        <div style={{ display: "flex" }}>
          <AppIconButton aria-label="Delete" color="error" onClick={onDelete}>
            <Icon height={22} icon="mdi:delete" width={22} />
          </AppIconButton>
          <AppIconButton
            aria-label="Download Config"
            onClick={onDownloadConfig}
          >
            <Icon height={22} icon="mdi:download" width={22} />
          </AppIconButton>
          <AppIconButton aria-label="View QR Code" onClick={onViewQrCode}>
            <Icon height={22} icon="mdi:qrcode" width={22} />
          </AppIconButton>
        </div>
      </div>

      {/* Stats */}
      <div style={{ marginTop: 6 }}>
        <PeerCardRow
          label="Handshake"
          value={formatAgo(peer.last_handshake_unix)}
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
    </AppCardContent>
  </FrostedCard>
);

export default WireguardPeerCard;
