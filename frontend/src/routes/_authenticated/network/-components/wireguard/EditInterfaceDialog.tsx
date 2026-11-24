import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  getMutationErrorMessage,
  linuxio,
  type WireGuardHookKind,
  type WireGuardHooks,
  type WireGuardInterface,
  type WireGuardUpdateInterfaceRequest,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import { useScopedToast } from "@/hooks/useScopedToast";

import { isMtuValid } from "./CreateInterfaceDialog";
import { getEgressNICOptions } from "./wireguardEgressNICs";

const WIREGUARD_TOAST_META = {
  label: "Open WireGuard",
  to: "/network/wireguard",
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

// The server's tunnel address: the first host of an IPv4 subnet from /24 to
// /30, or the subnet itself, which stands for it.
const isServerAddress = (value: string) => {
  const match = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/.exec(
    value,
  );
  if (!match) return false;
  const octets = match.slice(1, 5).map(Number);
  const prefix = Number(match[5]);
  if (octets.some((octet) => octet > 255) || prefix < 24 || prefix > 30) {
    return false;
  }
  return ((octets[3] ?? 0) & ((1 << (32 - prefix)) - 1)) <= 1;
};

// One field per wg-quick hook kind, in the order the config file lists them.
// Keyed by the generated union, so typecheck fails when the backend adds or
// drops a kind.
const HOOK_KINDS = Object.keys({
  PreUp: true,
  PostUp: true,
  PreDown: true,
  PostDown: true,
} satisfies Record<WireGuardHookKind, true>) as WireGuardHookKind[];

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
  const currentAddress = iface.address.split(",")[0]?.trim() ?? "";
  const [address, setAddress] = useState(currentAddress);
  const [nic, setNic] = useState(iface.egressNic);
  const [host, setHost] = useState(iface.host);
  const [dns, setDns] = useState(iface.dns.join(", "));
  const [mtu, setMtu] = useState(iface.mtu ? String(iface.mtu) : "");
  const [port, setPort] = useState(String(iface.port));
  const [hooks, setHooks] = useState(
    () =>
      Object.fromEntries(
        HOOK_KINDS.map((kind) => [kind, (iface.hooks[kind] ?? []).join("\n")]),
      ) as Record<WireGuardHookKind, string>,
  );
  const [error, setError] = useState<string | null>(null);

  // The card can move only where LinuxIO wrote the NAT lines.
  const { data: networkData } = useQuery({
    ...linuxio.network.get_network_info,
    enabled: open && iface.egressNic !== "",
  });
  const nicOptions = getEgressNICOptions(networkData);
  if (
    iface.egressNic &&
    !nicOptions.some((option) => option.name === iface.egressNic)
  ) {
    nicOptions.unshift({
      name: iface.egressNic,
      label: `${iface.egressNic} (current)`,
    });
  }

  const updateInterface = linuxio.wireguard.update_interface.useMutation({
    success: () => {
      toast.success(`Interface '${iface.name}' updated`);
      onClose();
    },
    error: (mutationError) =>
      setError(
        getMutationErrorMessage(mutationError, "Failed to update interface"),
      ),
  });

  const trimmedAddress = address.trim();
  const addressChanged = trimmedAddress !== currentAddress;
  const addressError = addressChanged && !isServerAddress(trimmedAddress);
  const trimmedHost = host.trim();
  const hostError = !isHostValid(trimmedHost);
  const dnsError = !isDnsList(dns);
  const mtuError = !isMtuValid(mtu);
  const portValue = parsePort(port);
  const portError =
    portValue === null || portValue < MIN_PORT || portValue > MAX_PORT;
  const invalid =
    addressError || hostError || dnsError || mtuError || portError;

  // Only fields that differ from the interface are sent; absent fields are
  // unchanged on the backend.
  const request: WireGuardUpdateInterfaceRequest = { name: iface.name };
  if (addressChanged && !addressError) request.address = trimmedAddress;
  if (nic !== iface.egressNic) request.egressNic = nic;
  if (trimmedHost !== iface.host) request.host = trimmedHost;
  const dnsList = splitList(dns);
  if (!sameList(dnsList, iface.dns)) request.dns = dnsList;
  const mtuValue = mtu.trim() === "" ? 0 : Number(mtu);
  if (!mtuError && mtuValue !== iface.mtu) request.mtu = mtuValue;
  if (portValue !== null && portValue !== iface.port) {
    request.listenPort = portValue;
  }
  const hookChanges: WireGuardHooks = {};
  for (const kind of HOOK_KINDS) {
    const lines = splitLines(hooks[kind]);
    if (!sameList(lines, iface.hooks[kind] ?? [])) hookChanges[kind] = lines;
  }
  if (Object.keys(hookChanges).length > 0) request.hooks = hookChanges;
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
            Changing the address, NIC, port, MTU or hooks restarts {iface.name}{" "}
            and reconnects peers; the NAT lines in PostUp and PostDown follow
            the address and the NIC. Changing the endpoint host or DNS rewrites
            client configs, which peers must re-import.
          </AppAlert>
          <AppTextField
            disabled={busy}
            error={addressError}
            fullWidth
            helperText={
              addressError
                ? "Use the first address of a /24 to /30 subnet, such as 10.10.30.1/24"
                : "The server's tunnel address. Devices are numbered in its subnet."
            }
            label="Address"
            onChange={(e) => setAddress(e.target.value)}
            value={address}
          />
          {addressChanged && !addressError && (
            <AppAlert severity="warning">
              Every device gets a new address: after saving, import the
              configuration of each device again (scan its QR code).
            </AppAlert>
          )}
          {iface.egressNic && (
            <AppSelect
              disabled={busy}
              fullWidth
              label="NIC"
              onChange={(e) => setNic(e.target.value)}
              value={nic}
            >
              {nicOptions.map((option) => (
                <option key={option.name} value={option.name}>
                  {option.label}
                </option>
              ))}
            </AppSelect>
          )}
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
              label={kind}
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
