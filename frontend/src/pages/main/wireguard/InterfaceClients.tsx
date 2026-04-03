import { useQueryClient } from "@tanstack/react-query";
import React, { useState, useMemo, useEffect } from "react";
import { toast } from "sonner";

import { linuxio, type Peer } from "@/api";
import WireguardPeerCard from "@/components/cards/WireguardPeerCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { AppDialogContent } from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
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

const InterfaceClients: React.FC<InterfaceDetailsProps> = ({ params }) => {
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
          peersWithStatus.map((peer, idx) => (
            <AppGrid
              key={peer.name || idx}
              size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
            >
              <WireguardPeerCard
                peer={peer}
                isOnline={peer.isOnline}
                onDelete={() => handleDeletePeer(peer.name)}
                onDownloadConfig={() => handleDownloadConfig(peer.name)}
                onViewQrCode={() => handleViewQrCode(peer.name)}
              />
            </AppGrid>
          ))
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
