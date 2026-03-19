import { Icon } from "@iconify/react";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState, useMemo, useEffect } from "react";
import { toast } from "sonner";

import { linuxio, type Peer } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppCardContent from "@/components/ui/AppCardContent";
import Chip from "@/components/ui/AppChip";
import { AppDialogContent } from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { getMutationErrorMessage } from "@/utils/mutations";
const wireguardToastMeta = {
  meta: {
    href: "/wireguard",
    label: "Open WireGuard",
  },
};
interface InterfaceDetailsProps {
  params: {
    id: string;
  };
}

// --- small format helpers ---
const formatFileSize = (n?: number) => {
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
};
const formatBps = (n?: number) => {
  if (n == null) return "-";
  // bytes/sec formatting
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
};
const formatAgo = (unix?: number) => {
  if (!unix) return "never";
  const diff = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  if (diff < 60) return `${diff}s ago`;
  const m = Math.floor(diff / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
};
const InterfaceClients: React.FC<InterfaceDetailsProps> = ({ params }) => {
  const theme = useTheme();
  const [qrCode, setQrCode] = useState<string | null>(null);
  const [openDialog, setOpenDialog] = useState(false);
  const [currentTime, setCurrentTime] = useState(() => Date.now() / 1000);
  const queryClient = useQueryClient();
  const interfaceName = params.id;

  // Update current time every 3 seconds (matches refetchInterval)
  useEffect(() => {
    const interval = setInterval(() => {
      setCurrentTime(Date.now() / 1000);
    }, 3000);
    return () => clearInterval(interval);
  }, []);
  const {
    data: peersData,
    isPending: isLoading,
    isError,
  } = linuxio.wireguard.list_peers.useQuery(interfaceName, {
    enabled: !!interfaceName,
    // poll so bps updates
    refetchInterval: 3000,
  });

  // Mutations
  const { mutate: deletePeer } = linuxio.wireguard.remove_peer.useMutation({
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: linuxio.wireguard.list_peers.queryKey(),
      });
    },
    onError: (error: Error) => {
      toast.error(
        getMutationErrorMessage(error, "Failed to delete peer"),
        wireguardToastMeta,
      );
    },
  });
  const { mutate: downloadConfig } =
    linuxio.wireguard.peer_config_download.useMutation({
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to download config"),
          wireguardToastMeta,
        );
      },
    });
  const { mutate: getQrCode, isPending: isLoadingQrCode } =
    linuxio.wireguard.peer_qrcode.useMutation({
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to load QR code"),
          wireguardToastMeta,
        );
      },
    });

  // Type-safe API returns Peer[] directly
  const peers: Peer[] = useMemo(() => peersData || [], [peersData]);

  // Calculate online status (re-calculates when peers or time updates)
  const peersWithStatus = useMemo(() => {
    return peers.map((peer) => {
      const lastUnix = peer.last_handshake_unix ?? 0;
      const isOnline = lastUnix > 0 && currentTime - lastUnix < 180; // 3 min window
      return {
        ...peer,
        isOnline,
      };
    });
  }, [peers, currentTime]);
  const handleDeletePeer = (peerName: string) => {
    deletePeer([interfaceName, peerName], {
      onSuccess: () => {
        toast.success(
          `WireGuard Peer '${peerName}' deleted`,
          wireguardToastMeta,
        );
      },
    });
  };
  const handleDownloadConfig = (peername: string) => {
    downloadConfig([interfaceName, peername], {
      onSuccess: (result) => {
        // Create blob from config text
        const blob = new Blob([result.content], {
          type: "text/plain",
        });
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.href = url;
        link.setAttribute("download", `${peername}.conf`);
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(url);
        toast.success(
          `Config for '${peername}' downloaded successfully`,
          wireguardToastMeta,
        );
      },
    });
  };
  const handleViewQrCode = (peername: string) => {
    getQrCode([interfaceName, peername], {
      onSuccess: (result) => {
        setQrCode(result.qrcode);
        setOpenDialog(true);
        toast.success(
          `QR code for '${peername}' loaded successfully`,
          wireguardToastMeta,
        );
      },
    });
  };
  if (isLoading) return <ComponentLoader />;
  if (isError)
    return (
      <AppTypography color="error">Failed to load peer details</AppTypography>
    );
  return (
    <>
      <AppGrid container spacing={3}>
        {peersWithStatus.length === 0 ? (
          <AppGrid
            size={{
              xs: 6,
              sm: 4,
              md: 4,
              lg: 3,
              xl: 2,
            }}
          >
            <AppTypography>No peers found for this interface.</AppTypography>
          </AppGrid>
        ) : (
          peersWithStatus.map((peer, idx) => {
            const isOnline = peer.isOnline;
            return (
              <AppGrid
                size={{
                  xs: 12,
                  sm: 6,
                  md: 6,
                  lg: 4,
                  xl: 3,
                }}
                key={peer.name || idx}
              >
                <FrostedCard>
                  <AppCardContent>
                    <div
                      style={{
                        display: "flex",
                        justifyContent: "space-between",
                        alignItems: "center",
                      }}
                    >
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: theme.spacing(1),
                        }}
                      >
                        <AppTypography
                          variant="h6"
                          style={{
                            fontSize: "1.1rem",
                          }}
                        >
                          {peer.name || "Peer"}
                        </AppTypography>
                        <AppTooltip
                          title={
                            isOnline
                              ? "Handshake < 3 minutes"
                              : "No recent handshake"
                          }
                        >
                          <Chip
                            size="small"
                            label={isOnline ? "Online" : "Offline"}
                            color={isOnline ? "success" : "default"}
                            variant="soft"
                          />
                        </AppTooltip>
                      </div>
                      <div
                        style={{
                          display: "flex",
                        }}
                      >
                        <AppIconButton
                          aria-label="Delete"
                          onClick={() => handleDeletePeer(peer.name)}
                          color="error"
                        >
                          <Icon icon="mdi:delete" width={22} height={22} />
                        </AppIconButton>
                        <AppIconButton
                          aria-label="Download Config"
                          onClick={() => handleDownloadConfig(peer.name)}
                        >
                          <Icon icon="mdi:download" width={22} height={22} />
                        </AppIconButton>
                        <AppIconButton
                          aria-label="View QR Code"
                          onClick={() => handleViewQrCode(peer.name)}
                        >
                          <Icon icon="mdi:qrcode" width={22} height={22} />
                        </AppIconButton>
                      </div>
                    </div>

                    <AppTypography
                      variant="body2"
                      style={{
                        marginTop: 2,
                      }}
                    >
                      Handshake: {formatAgo(peer.last_handshake_unix)}
                    </AppTypography>

                    <AppTypography variant="body2">
                      Rx: {formatFileSize(peer.rx_bytes)}{" "}
                      <span
                        style={{
                          opacity: 0.7,
                        }}
                      >
                        ({formatBps(peer.rx_bps)})
                      </span>
                    </AppTypography>

                    <AppTypography variant="body2">
                      Tx: {formatFileSize(peer.tx_bytes)}{" "}
                      <span
                        style={{
                          opacity: 0.7,
                        }}
                      >
                        ({formatBps(peer.tx_bps)})
                      </span>
                    </AppTypography>

                    <AppTypography
                      variant="body2"
                      style={{
                        marginTop: 4,
                      }}
                    >
                      Allowed IPs:{" "}
                      {(peer.allowed_ips && peer.allowed_ips.join(", ")) || "-"}
                    </AppTypography>
                    <AppTypography
                      variant="body2"
                      style={{
                        wordBreak: "break-all",
                      }}
                    >
                      Endpoint: {peer.endpoint || "-"}
                    </AppTypography>
                    <AppTypography
                      variant="body2"
                      style={{
                        wordBreak: "break-all",
                      }}
                    >
                      Preshared Key: {peer.preshared_key || "-"}
                    </AppTypography>
                    <AppTypography variant="body2">
                      Keep Alive: {peer.persistent_keepalive ?? "-"}
                    </AppTypography>
                  </AppCardContent>
                </FrostedCard>
              </AppGrid>
            );
          })
        )}
      </AppGrid>

      <GeneralDialog
        open={openDialog}
        onClose={() => {
          setOpenDialog(false);
          setQrCode(null);
        }}
      >
        <AppDialogContent>
          {isLoadingQrCode ? (
            <AppTypography>Loading QR code...</AppTypography>
          ) : qrCode ? (
            <img
              src={qrCode}
              alt="QR Code"
              style={{
                width: 300,
                height: 300,
                maxWidth: "100%",
                display: "block",
              }}
            />
          ) : (
            <AppTypography>Failed to load QR code</AppTypography>
          )}
        </AppDialogContent>
      </GeneralDialog>
    </>
  );
};
export default InterfaceClients;
