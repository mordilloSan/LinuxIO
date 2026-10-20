import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { CACHE_TTL_MS, linuxio, type Peer, useCallMutation } from "@/api";
import WireguardPeerCard, {
  usePeer,
} from "@/components/cards/WireguardPeerCard";
import type { WireguardPeerAction } from "@/components/cards/WireguardPeerCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { AppDialogContent } from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useScopedToast } from "@/hooks/useScopedToast";

import EditPeerDialog from "./EditPeerDialog";

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
  id: string;
  name: string;
}

export const selectPeerIdentities = (peers: Peer[]): PeerIdentity[] =>
  peers.map((peer) => ({ id: peer.id, name: peer.name }));

const EMPTY_PEER_IDENTITIES: PeerIdentity[] = [];
const getPeerId = (peer: PeerIdentity) => peer.id;

const InterfaceClients = ({ params }: InterfaceDetailsProps) => {
  const toast = useScopedToast(WIREGUARD_TOAST_META);
  // Peer whose QR code dialog is open; opening the dialog drives the fetch.
  const [qrPeer, setQrPeer] = useState<string | null>(null);
  const [editPeer, setEditPeer] = useState<string | null>(null);
  const [pendingActions, setPendingActions] = useState<
    ReadonlyMap<string, WireguardPeerAction>
  >(() => new Map());
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

  const peerLabel = (peerId: string) =>
    peerIdentities?.find((peer) => peer.id === peerId)?.name ?? peerId;

  // Mutations
  const deletePeer = useCallMutation(linuxio.wireguard.remove_peer, {
    success: (_result, variables) =>
      toast.success(`WireGuard Peer '${peerLabel(variables.peerId)}' deleted`),
    error: "Failed to delete peer",
    toast: WIREGUARD_TOAST_META,
  });
  const updatePeer = useCallMutation(linuxio.wireguard.update_peer, {
    success: (_result, variables) =>
      toast.success(
        `Peer '${peerLabel(variables.peerId)}' ${variables.enabled ? "enabled" : "disabled"}`,
      ),
    error: "Failed to update peer",
    toast: WIREGUARD_TOAST_META,
  });
  const downloadConfig = useCallMutation(
    linuxio.wireguard.peer_config_download,
    {
      success: (result, { peerId }) => {
        const blob = new Blob([result.content], {
          type: "text/plain",
        });
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.href = url;
        link.setAttribute("download", result.filename);
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(url);
        toast.success(
          `Config for '${peerLabel(peerId)}' downloaded successfully`,
        );
      },
      error: "Failed to download config",
      toast: WIREGUARD_TOAST_META,
    },
  );

  const runPeerAction = (
    peerId: string,
    action: WireguardPeerAction,
    run: () => Promise<unknown>,
  ) => {
    if (pendingActions.has(peerId)) return;

    setPendingActions((current) => new Map(current).set(peerId, action));
    void run()
      .catch(() => undefined)
      .finally(() => {
        setPendingActions((current) => {
          if (current.get(peerId) !== action) return current;
          const next = new Map(current);
          next.delete(peerId);
          return next;
        });
      });
  };

  const handleDeletePeer = (peerId: string) => {
    runPeerAction(peerId, "delete", () =>
      deletePeer.mutateAsync({ interfaceName, peerId }),
    );
  };

  const handleDownloadConfig = (peerId: string) => {
    runPeerAction(peerId, "download", () =>
      downloadConfig.mutateAsync({ interfaceName, peerId }),
    );
  };

  const handleToggleEnabled = (peerId: string, enabled: boolean) => {
    runPeerAction(peerId, "toggle", () =>
      updatePeer.mutateAsync({ interfaceName, peerId, enabled }),
    );
  };

  const qrQuery = useQuery({
    ...linuxio.wireguard.peer_qrcode({ interfaceName, peerId: qrPeer ?? "" }),
    enabled: qrPeer !== null,
    staleTime: CACHE_TTL_MS.NONE,
    gcTime: CACHE_TTL_MS.NONE,
  });
  const { data: editingPeer } = usePeer(interfaceName, editPeer ?? "");
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
          fillAvailable={false}
          getId={getPeerId}
          renderItem={(peer) => (
            <WireguardPeerCard
              interfaceName={interfaceName}
              onDelete={handleDeletePeer}
              onDownloadConfig={handleDownloadConfig}
              onEdit={setEditPeer}
              onToggleEnabled={handleToggleEnabled}
              onViewQrCode={setQrPeer}
              pendingAction={pendingActions.get(peer.id)}
              peerId={peer.id}
              peerName={peer.name}
            />
          )}
          size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
          surface={surface}
        />
      )}

      {editingPeer && (
        <EditPeerDialog
          interfaceName={interfaceName}
          key={editingPeer.id}
          onClose={() => setEditPeer(null)}
          open
          peer={editingPeer}
        />
      )}

      <GeneralDialog
        aria-label="WireGuard peer QR code"
        onClose={() => setQrPeer(null)}
        open={qrPeer !== null}
      >
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
