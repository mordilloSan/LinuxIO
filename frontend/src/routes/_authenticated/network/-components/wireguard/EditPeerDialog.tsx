import { useState } from "react";

import {
  linuxio,
  type Peer,
  type WireGuardUpdatePeerRequest,
  useCallMutation,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

const WIREGUARD_TOAST_META = {
  label: "Open WireGuard",
  to: "/wireguard",
} as const;

const FULL_TUNNEL = ["0.0.0.0/0", "::/0"];
const CIDR_PATTERN = /^[0-9a-fA-F:.]+\/\d{1,3}$/;
const MIN_MTU = 1280;
const MAX_MTU = 65535;
const MAX_KEEPALIVE = 65535;

const splitList = (value: string): string[] =>
  value
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean);

const sameList = (a: string[], b: string[]) =>
  a.length === b.length && a.every((entry, index) => entry === b[index]);

const isCidrList = (value: string) =>
  splitList(value).every((entry) => CIDR_PATTERN.test(entry));

const isDnsList = (value: string) =>
  splitList(value).every((entry) => !/\s/.test(entry));

const parseNumber = (value: string): number | null => {
  if (value.trim() === "") return 0;
  return /^\d+$/.test(value.trim()) ? Number(value) : null;
};

type RegenerateAction = "keys" | "preshared-key";

interface EditPeerDialogProps {
  interfaceName: string;
  onClose: () => void;
  open: boolean;
  peer: Peer;
}

const EditPeerDialog = ({
  interfaceName,
  onClose,
  open,
  peer,
}: EditPeerDialogProps) => {
  const toast = useScopedToast(WIREGUARD_TOAST_META);
  const hasPresharedKey = Boolean(peer.preshared_key);
  const [name, setName] = useState(peer.name);
  const [clientRoutes, setClientRoutes] = useState(
    peer.client_allowed_ips.join(", "),
  );
  const [serverRoutes, setServerRoutes] = useState(
    peer.server_allowed_ips.join(", "),
  );
  const [dns, setDns] = useState(peer.dns.join(", "));
  const [mtu, setMtu] = useState(peer.mtu ? String(peer.mtu) : "");
  const [keepalive, setKeepalive] = useState(
    String(peer.persistent_keepalive ?? 0),
  );
  const [presharedKey, setPresharedKey] = useState(hasPresharedKey);
  const [confirmAction, setConfirmAction] = useState<RegenerateAction | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);

  const updatePeer = useCallMutation(linuxio.wireguard.update_peer, {
    success: () => {
      toast.success(`Peer '${peer.name}' updated`);
      onClose();
    },
    error: (mutationError) =>
      setError(getMutationErrorMessage(mutationError, "Failed to update peer")),
  });

  const mtuValue = parseNumber(mtu);
  const keepaliveValue = parseNumber(keepalive);
  const mtuError =
    mtuValue === null ||
    (mtuValue !== 0 && (mtuValue < MIN_MTU || mtuValue > MAX_MTU));
  const keepaliveError =
    keepaliveValue === null || keepaliveValue > MAX_KEEPALIVE;
  const clientRoutesError = !isCidrList(clientRoutes);
  const serverRoutesError = !isCidrList(serverRoutes);
  const dnsError = !isDnsList(dns);
  const invalid =
    mtuError ||
    keepaliveError ||
    clientRoutesError ||
    serverRoutesError ||
    dnsError;

  // Only fields that differ from the peer are sent; the backend treats
  // absent fields as unchanged.
  const request: WireGuardUpdatePeerRequest = {
    interfaceName,
    peerId: peer.id,
  };
  if (name.trim() !== peer.name) request.name = name.trim();
  const clientList = splitList(clientRoutes);
  if (!sameList(clientList, peer.client_allowed_ips)) {
    request.clientAllowedIPs = clientList;
  }
  const serverList = splitList(serverRoutes);
  if (!sameList(serverList, peer.server_allowed_ips)) {
    request.serverAllowedIPs = serverList;
  }
  const dnsList = splitList(dns);
  if (!sameList(dnsList, peer.dns)) request.dns = dnsList;
  if (mtuValue !== null && mtuValue !== peer.mtu) request.mtu = mtuValue;
  if (
    keepaliveValue !== null &&
    keepaliveValue !== (peer.persistent_keepalive ?? 0)
  ) {
    request.persistentKeepalive = keepaliveValue;
  }
  if (presharedKey !== hasPresharedKey) {
    request.presharedKey = presharedKey ? "generate" : "remove";
  }
  const hasChanges = Object.keys(request).length > 2;
  const busy = updatePeer.isPending;

  const handleSave = () => {
    if (invalid || !hasChanges || busy) return;
    setError(null);
    updatePeer.mutate(request);
  };

  const handleRegenerate = () => {
    const action = confirmAction;
    setConfirmAction(null);
    if (!action || busy) return;
    setError(null);
    updatePeer.mutate(
      action === "keys"
        ? { interfaceName, peerId: peer.id, regenerateKeys: true }
        : { interfaceName, peerId: peer.id, presharedKey: "generate" },
    );
  };

  return (
    <>
      <GeneralDialog
        aria-busy={busy}
        disableEscapeKeyDown={busy}
        fullWidth
        maxWidth="sm"
        onClose={busy ? undefined : onClose}
        open={open}
      >
        <AppDialogTitle>Edit {peer.name}</AppDialogTitle>
        <AppDialogContent>
          <div className="app-dialog-fields">
            <AppTextField
              disabled={busy}
              fullWidth
              helperText="Leave empty to use the id."
              label="Name"
              onChange={(e) => setName(e.target.value)}
              value={name}
            />
            <AppTextField
              disabled={busy}
              endAdornment={
                <AppButton
                  disabled={busy}
                  onClick={() => setClientRoutes(FULL_TUNNEL.join(", "))}
                  size="small"
                >
                  Full tunnel
                </AppButton>
              }
              error={clientRoutesError}
              fullWidth
              helperText={
                clientRoutesError
                  ? "Enter comma-separated CIDRs"
                  : "What the client sends through the tunnel. Empty means full tunnel."
              }
              label="Client routes"
              onChange={(e) => setClientRoutes(e.target.value)}
              value={clientRoutes}
            />
            <AppTextField
              disabled={busy}
              error={serverRoutesError}
              fullWidth
              helperText={
                serverRoutesError
                  ? "Enter comma-separated CIDRs"
                  : "Extra subnets the server accepts from this peer."
              }
              label="Networks behind the peer"
              onChange={(e) => setServerRoutes(e.target.value)}
              placeholder="e.g. 192.168.50.0/24"
              value={serverRoutes}
            />
            <AppTextField
              disabled={busy}
              error={dnsError}
              fullWidth
              helperText={
                dnsError
                  ? "Separate entries with commas"
                  : "Empty means the interface default."
              }
              label="DNS"
              onChange={(e) => setDns(e.target.value)}
              value={dns}
            />
            <AppTextField
              disabled={busy}
              error={mtuError}
              fullWidth
              helperText={
                mtuError
                  ? `Use 0 or ${MIN_MTU} to ${MAX_MTU}`
                  : "Empty or 0 leaves the client default."
              }
              label="MTU"
              onChange={(e) => setMtu(e.target.value)}
              value={mtu}
            />
            <AppTextField
              disabled={busy}
              error={keepaliveError}
              fullWidth
              helperText={
                keepaliveError
                  ? `Use 0 to ${MAX_KEEPALIVE}`
                  : "Seconds between keepalives. 0 disables."
              }
              label="Keepalive"
              onChange={(e) => setKeepalive(e.target.value)}
              value={keepalive}
            />
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={presharedKey}
                  disabled={busy}
                  onChange={(_, checked) => setPresharedKey(checked)}
                  role="switch"
                />
              }
              label="Preshared key"
            />
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: "var(--app-space-8)",
              }}
            >
              {hasPresharedKey && (
                <AppButton
                  disabled={busy}
                  onClick={() => setConfirmAction("preshared-key")}
                  size="small"
                  variant="outlined"
                >
                  Regenerate preshared key
                </AppButton>
              )}
              <AppButton
                disabled={busy}
                onClick={() => setConfirmAction("keys")}
                size="small"
                variant="outlined"
              >
                Regenerate keys
              </AppButton>
            </div>
            {error && <AppAlert severity="error">{error}</AppAlert>}
          </div>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={busy} onClick={onClose}>
            Cancel
          </AppButton>
          <AppButton
            disabled={busy || invalid || !hasChanges}
            onClick={handleSave}
            variant="contained"
          >
            {busy ? "Saving…" : "Save"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
      <ConfirmDialog
        confirmText="Regenerate"
        destructive
        message={
          confirmAction === "keys"
            ? "The client's current configuration stops working. Download or scan the new one afterwards."
            : "The client's current configuration stops working until it imports the new preshared key."
        }
        onClose={() => setConfirmAction(null)}
        onConfirm={handleRegenerate}
        open={confirmAction !== null}
        title={
          confirmAction === "keys"
            ? "Regenerate keys?"
            : "Regenerate preshared key?"
        }
      />
    </>
  );
};

export default EditPeerDialog;
