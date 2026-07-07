import React, { useEffect, useMemo, useState } from "react";

import { CACHE_TTL_MS, linuxio, type Peer } from "@/api";
import WireguardPeerCard from "@/components/cards/WireguardPeerCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import { AppDialogContent } from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";

const WIREGUARD_TOAST_META = { href: "/wireguard", label: "Open WireGuard" };

interface InterfaceDetailsProps {
  params: {
    id: string;
  };
}

const InterfaceClients: React.FC<InterfaceDetailsProps> = ({ params }) => {
  const toast = useScopedToast(WIREGUARD_TOAST_META);
  // Peer whose QR code dialog is open; opening the dialog drives the fetch.
  const [qrPeer, setQrPeer] = useState<string | null>(null);
  const [currentTime, setCurrentTime] = useState(() => Date.now() / 1000);
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
  const { mutate: deletePeer } = linuxio.wireguard.remove_peer.useJobAction({
    success: (_result, variables) =>
      toast.success(`WireGuard Peer '${variables.peerName}' deleted`),
    error: "Failed to delete peer",
    toast: WIREGUARD_TOAST_META,
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
    deletePeer({ interfaceName, peerName });
  };

  const { mutate: downloadConfig } =
    linuxio.wireguard.peer_config_download.useAction({
      success: (result, { peerName }) => {
        const blob = new Blob([result.content], {
          type: "text/plain",
        });
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.href = url;
        link.setAttribute("download", `${peerName}.conf`);
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(url);
        toast.success(`Config for '${peerName}' downloaded successfully`);
      },
      error: "Failed to download config",
      toast: WIREGUARD_TOAST_META,
    });

  const qrQuery = linuxio.wireguard.peer_qrcode.useQuery(
    { interfaceName, peerName: qrPeer ?? "" },
    {
      enabled: qrPeer !== null,
      staleTime: CACHE_TTL_MS.NONE,
      gcTime: CACHE_TTL_MS.NONE,
    },
  );
  if (isLoading) return <PageLoader />;
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
                isOnline={peer.isOnline}
                onDelete={() => handleDeletePeer(peer.name)}
                onDownloadConfig={() =>
                  downloadConfig({ interfaceName, peerName: peer.name })
                }
                onViewQrCode={() => setQrPeer(peer.name)}
                peer={peer}
              />
            </AppGrid>
          ))
        )}
      </AppGrid>

      <GeneralDialog onClose={() => setQrPeer(null)} open={qrPeer !== null}>
        <AppDialogContent>
          {qrQuery.isLoading ? (
            <AppTypography>Loading QR code...</AppTypography>
          ) : qrQuery.data?.qrcode ? (
            <img
              alt="QR Code"
              src={qrQuery.data.qrcode}
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
