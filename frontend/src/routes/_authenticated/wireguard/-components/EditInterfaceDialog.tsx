import { useState } from "react";

import {
  linuxio,
  type WireGuardInterface,
  type WireGuardUpdateInterfaceRequest,
  useCallMutation,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

import { isMtuValid } from "./CreateInterfaceDialog";

const WIREGUARD_TOAST_META = {
  label: "Open WireGuard",
  to: "/wireguard",
} as const;

const MIN_PORT = 1;
const MAX_PORT = 65535;

const splitList = (value: string): string[] =>
  value
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean);

const splitLines = (value: string): string[] =>
  value
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

const sameList = (a: string[], b: string[]) =>
  a.length === b.length && a.every((entry, index) => entry === b[index]);

const isHostValid = (value: string) =>
  !/[\s/]/.test(value) && value.length <= 253;

const isDnsList = (value: string) =>
  splitList(value).every((entry) => !/\s/.test(entry));

const parsePort = (value: string): number | null =>
  /^\d+$/.test(value.trim()) ? Number(value) : null;

type HookKind = "preUp" | "postUp" | "preDown" | "postDown";
const HOOK_LABELS: Record<HookKind, string> = {
  preUp: "PreUp",
  postUp: "PostUp",
  preDown: "PreDown",
  postDown: "PostDown",
};
const HOOK_KINDS = Object.keys(HOOK_LABELS) as HookKind[];

interface EditInterfaceDialogProps {
  iface: WireGuardInterface;
  onClose: () => void;
  open: boolean;
}

const EditInterfaceDialog = ({
  iface,
  onClose,
  open,
}: EditInterfaceDialogProps) => {
  const toast = useScopedToast(WIREGUARD_TOAST_META);
  const [host, setHost] = useState(iface.host);
  const [dns, setDns] = useState(iface.dns.join(", "));
  const [mtu, setMtu] = useState(iface.mtu ? String(iface.mtu) : "");
  const [port, setPort] = useState(String(iface.port));
  const [hooks, setHooks] = useState<Record<HookKind, string>>({
    preUp: iface.preUp.join("\n"),
    postUp: iface.postUp.join("\n"),
    preDown: iface.preDown.join("\n"),
    postDown: iface.postDown.join("\n"),
  });
  const [error, setError] = useState<string | null>(null);

  const updateInterface = useCallMutation(linuxio.wireguard.update_interface, {
    success: () => {
      toast.success(`Interface '${iface.name}' updated`);
      onClose();
    },
    error: (mutationError) =>
      setError(
        getMutationErrorMessage(mutationError, "Failed to update interface"),
      ),
  });

  const trimmedHost = host.trim();
  const hostError = !isHostValid(trimmedHost);
  const dnsError = !isDnsList(dns);
  const mtuError = !isMtuValid(mtu);
  const portValue = parsePort(port);
  const portError =
    portValue === null || portValue < MIN_PORT || portValue > MAX_PORT;
  const invalid = hostError || dnsError || mtuError || portError;

  // Only fields that differ from the interface are sent; absent fields are
  // unchanged on the backend.
  const request: WireGuardUpdateInterfaceRequest = { name: iface.name };
  if (trimmedHost !== iface.host) request.host = trimmedHost;
  const dnsList = splitList(dns);
  if (!sameList(dnsList, iface.dns)) request.dns = dnsList;
  const mtuValue = mtu.trim() === "" ? 0 : Number(mtu);
  if (!mtuError && mtuValue !== iface.mtu) request.mtu = mtuValue;
  if (portValue !== null && portValue !== iface.port) {
    request.listenPort = portValue;
  }
  for (const kind of HOOK_KINDS) {
    const lines = splitLines(hooks[kind]);
    if (!sameList(lines, iface[kind])) request[kind] = lines;
  }
  const hasChanges = Object.keys(request).length > 1;
  const busy = updateInterface.isPending;

  const handleSave = () => {
    if (invalid || !hasChanges || busy) return;
    setError(null);
    updateInterface.mutate(request);
  };

  return (
    <GeneralDialog
      aria-busy={busy}
      disableEscapeKeyDown={busy}
      fullWidth
      maxWidth="sm"
      onClose={busy ? undefined : onClose}
      open={open}
    >
      <AppDialogTitle>Edit {iface.name}</AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
          <AppAlert severity="info">
            Changing port, MTU or hooks restarts {iface.name} and reconnects
            peers. Changing the endpoint host or DNS rewrites client configs,
            which peers must re-import.
          </AppAlert>
          <AppTextField
            disabled={busy}
            error={hostError}
            fullWidth
            helperText={
              hostError
                ? "Enter a hostname or IP address"
                : "Leave empty to use the detected public IP."
            }
            label="Endpoint host"
            onChange={(e) => setHost(e.target.value)}
            placeholder="e.g. vpn.example.org"
            value={host}
          />
          <AppTextField
            disabled={busy}
            error={dnsError}
            fullWidth
            helperText={
              dnsError
                ? "Separate entries with commas"
                : "Default DNS for clients. Empty means the gateway."
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
                ? "Use 0 or 1280 to 65535"
                : "Server-side MTU. Empty leaves the default."
            }
            label="MTU"
            onChange={(e) => setMtu(e.target.value)}
            value={mtu}
          />
          <AppTextField
            disabled={busy}
            error={portError}
            fullWidth
            helperText={
              portError ? `Use ${MIN_PORT} to ${MAX_PORT}` : "UDP listen port."
            }
            label="Port"
            onChange={(e) => setPort(e.target.value)}
            value={port}
          />
          {HOOK_KINDS.map((kind) => (
            <AppTextField
              disabled={busy}
              fullWidth
              helperText="One command per line."
              key={kind}
              label={HOOK_LABELS[kind]}
              multiline
              onChange={(e) =>
                setHooks((current) => ({ ...current, [kind]: e.target.value }))
              }
              rows={3}
              value={hooks[kind]}
            />
          ))}
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
  );
};

export default EditInterfaceDialog;
