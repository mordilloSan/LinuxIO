import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { CACHE_TTL_MS, linuxio, type Peer, useCallMutation } from "@/api";
import WireguardPeerCard from "@/components/cards/WireguardPeerCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { AppDialogContent } from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useScopedToast } from "@/hooks/useScopedToast";

const WIREGUARD_TOAST_META = {
  label: "Open WireGuard",
  to: "/wireguard",
} as const;

interface InterfaceDetailsProps {
  params: {
    id: string;
  };
}

interface PeerIdentity {
  name: string;
}

export const selectPeerIdentities = (peers: Peer[]): PeerIdentity[] =>
  peers.map((peer) => ({ name: peer.name }));

const EMPTY_PEER_IDENTITIES: { name: string }[] = [];
const getPeerId = (peer: { name: string }) => peer.name;

const InterfaceClients = ({ params }: InterfaceDetailsProps) => {
  const toast = useScopedToast(WIREGUARD_TOAST_META);
  // Peer whose QR code dialog is open; opening the dialog drives the fetch.
  const [qrPeer, setQrPeer] = useState<string | null>(null);
  const interfaceName = params.id;

  const {
    data: peerIdentities,
    isLoading,
    isError,
  } = useQuery({
    ...linuxio.wireguard.list_peers({ interfaceName }),
    enabled: !!interfaceName,
    // poll so bps updates
    refetchInterval: 3000,
    select: selectPeerIdentities,
  });

  // Mutations
  const { mutate: deletePeer } = useCallMutation(
    linuxio.wireguard.remove_peer,
    {
      success: (_result, variables) =>
        toast.success(`WireGuard Peer '${variables.peerName}' deleted`),
      error: "Failed to delete peer",
      toast: WIREGUARD_TOAST_META,
    },
  );
  const handleDeletePeer = (peerName: string) => {
    deletePeer({ interfaceName, peerName });
  };

  const { mutate: downloadConfig } = useCallMutation(
    linuxio.wireguard.peer_config_download,
    {
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
    },
  );

  const qrQuery = useQuery({
    ...linuxio.wireguard.peer_qrcode({ interfaceName, peerName: qrPeer ?? "" }),
    enabled: qrPeer !== null,
    staleTime: CACHE_TTL_MS.NONE,
    gcTime: CACHE_TTL_MS.NONE,
  });
  // Peers are per interface, so the saved order is too.
  const surface = useReorderableSurface({
    getId: getPeerId,
    items: peerIdentities ?? EMPTY_PEER_IDENTITIES,
    surface: `wireguard.peers.${interfaceName}`,
  });

  if (isLoading) return <PageLoader />;
  if (isError)
    return (
      <AppTypography color="error">Failed to load peer details</AppTypography>
    );
  return (
    <>
      {!peerIdentities || peerIdentities.length === 0 ? (
        <AppGrid container spacing={3}>
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
        </AppGrid>
      ) : (
        <ReorderableCardGrid
          getId={getPeerId}
          renderItem={(peer) => (
            <WireguardPeerCard
              interfaceName={interfaceName}
              onDelete={handleDeletePeer}
              onDownloadConfig={(peerName) =>
                downloadConfig({ interfaceName, peerName })
              }
              onViewQrCode={setQrPeer}
              peerName={peer.name}
            />
          )}
          size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
          spacing={3}
          surface={surface}
        />
      )}

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
